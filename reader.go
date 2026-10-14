package ulog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/infin-robotics/ulog/pkg/wire"
)

// Header contains the ULog version and the logging start time from the fixed
// file header.
type Header struct {
	// Version is the file-format version declared by the log.
	Version uint8
	// Timestamp is when logging started, in microseconds.
	Timestamp uint64
}

// Reader streams format-resolved [Record] values from ULog. As [Reader.Next]
// advances, it also collects information, multi-information groups, parameters,
// logs, and dropouts.
type Reader struct {
	source          io.Reader
	offset          uint64
	appendedOffsets []uint64
	header          Header
	formats         map[string]Format
	subscriptions   map[uint16]subscription
	information     []KeyValue
	multiInfo       []MultiInformationGroup
	multiInfoLast   map[string]int
	parameters      []KeyValue
	defaults        []DefaultParameter
	logs            []LogEntry
	dropouts        []Dropout
	record          Record
	err             error
}

type subscription struct {
	messageID uint16
	multiID   uint8
	format    Format
	layout    []layoutField
}

type layoutField struct {
	name        string
	typeID      Type
	arrayLength int
	offset      int
	size        int
	hidden      bool
}

// NewReader consumes the fixed file header from source and checks its magic
// bytes. It accepts later file-format versions for forward compatibility and
// does not close source.
func NewReader(source io.Reader) (*Reader, error) {
	if source == nil {
		return nil, errors.New("nil ULog source")
	}

	data := make([]byte, binary.Size(wire.FileHeader{}))
	if _, err := io.ReadFull(source, data); err != nil {
		return nil, fmt.Errorf("read ULog file header: %w", err)
	}

	var fileHeader wire.FileHeader
	if _, err := binary.Decode(data, binary.LittleEndian, &fileHeader); err != nil {
		return nil, fmt.Errorf("decode ULog file header: %w", err)
	}
	if string(fileHeader.Magic[:]) != wire.FileMagic {
		return nil, fmt.Errorf("invalid ULog file magic %x", fileHeader.Magic)
	}
	return &Reader{
		source: source,
		offset: uint64(len(data)),
		header: Header{
			Version:   fileHeader.Version,
			Timestamp: fileHeader.Timestamp,
		},
		formats:       make(map[string]Format),
		subscriptions: make(map[uint16]subscription),
		multiInfoLast: make(map[string]int),
	}, nil
}

// Header returns the version and logging start time consumed by [NewReader]. It
// is available before the first call to [Reader.Next].
func (r *Reader) Header() Header {
	return r.header
}

// Next consumes messages until it reaches the next data record. It returns false
// at end of stream or after the first error; call [Reader.Err] to distinguish
// the two. Definition, subscription, metadata, log, and dropout messages update
// the reader's state without producing a record. Unknown message types are
// skipped unless the log advertises an unsupported incompatibility feature.
func (r *Reader) Next() bool {
	if r == nil || r.err != nil {
		return false
	}

	for {
		messageType, payload, err := r.readMessage()
		if errors.Is(err, io.EOF) {
			return false
		}
		if err != nil {
			r.err = err
			return false
		}

		record, found, err := r.consume(messageType, payload)
		if err != nil {
			r.err = err
			return false
		}
		if found {
			r.record = record
			return true
		}
	}
}

// Record returns the [Record] selected by the most recent successful
// [Reader.Next]. Its value is unchanged after Next returns false.
func (r *Reader) Record() Record {
	return r.record
}

// Err returns the first error encountered by [Reader.Next], or nil after a clean
// end of stream.
func (r *Reader) Err() error {
	if r == nil {
		return errors.New("nil ULog reader")
	}
	return r.err
}

// Information returns independent copies of the typed metadata entries
// encountered so far, in file order.
func (r *Reader) Information() []KeyValue {
	if r == nil {
		return nil
	}
	return cloneKeyValues(r.information)
}

// MultiInformation returns independent copies of the grouped multi-information
// values encountered so far, in the order each group started. Every value keeps
// the type and array length declared by its own wire message.
func (r *Reader) MultiInformation() []MultiInformationGroup {
	if r == nil {
		return nil
	}
	return cloneMultiInformation(r.multiInfo)
}

// Parameters returns independent copies of the initial parameter values and
// later changes encountered so far, in file order.
func (r *Reader) Parameters() []KeyValue {
	if r == nil {
		return nil
	}
	return cloneKeyValues(r.parameters)
}

// DefaultParameters returns independent copies of the parameter defaults
// encountered so far, in file order. Missing defaults are not synthesised.
func (r *Reader) DefaultParameters() []DefaultParameter {
	if r == nil {
		return nil
	}
	return cloneDefaultParameters(r.defaults)
}

// Logs returns the tagged and untagged text messages encountered so far, in file
// order.
func (r *Reader) Logs() []LogEntry {
	if r == nil {
		return nil
	}
	return append([]LogEntry(nil), r.logs...)
}

// Dropouts returns the periods of lost logging messages encountered so far, in
// file order.
func (r *Reader) Dropouts() []Dropout {
	if r == nil {
		return nil
	}
	return append([]Dropout(nil), r.dropouts...)
}

func (r *Reader) readMessage() (wire.MessageType, []byte, error) {
	for {
		if r.atAppendedSection() {
			r.appendedOffsets = r.appendedOffsets[1:]
		}

		var headerBytes [3]byte
		if r.crossesAppendedOffset(uint64(len(headerBytes))) {
			if err := r.skipToAppendedSection(); err != nil {
				return 0, nil, err
			}
			continue
		}
		n, err := r.readFull(headerBytes[:])
		if errors.Is(err, io.EOF) && n == 0 {
			return 0, nil, io.EOF
		}
		if err != nil {
			return 0, nil, fmt.Errorf("read ULog message header: %w", err)
		}

		size := binary.LittleEndian.Uint16(headerBytes[:2])
		if r.crossesAppendedOffset(uint64(size)) {
			if err := r.skipToAppendedSection(); err != nil {
				return 0, nil, err
			}
			continue
		}
		payload := make([]byte, int(size))
		if _, err := r.readFull(payload); err != nil {
			return 0, nil, fmt.Errorf("read ULog %q message payload: %w", headerBytes[2], err)
		}
		return wire.MessageType(headerBytes[2]), payload, nil
	}
}

func (r *Reader) readFull(data []byte) (int, error) {
	n, err := io.ReadFull(r.source, data)
	r.offset += uint64(n) // #nosec G115 -- io.ReadFull returns a non-negative byte count.
	return n, err
}

func (r *Reader) atAppendedSection() bool {
	return len(r.appendedOffsets) > 0 && r.offset == r.appendedOffsets[0]
}

func (r *Reader) crossesAppendedOffset(size uint64) bool {
	return len(r.appendedOffsets) > 0 && size > r.appendedOffsets[0]-r.offset
}

func (r *Reader) skipToAppendedSection() error {
	remaining := r.appendedOffsets[0] - r.offset
	// crossesAppendedOffset limits remaining to a three-byte header or uint16 payload.
	if _, err := io.CopyN(io.Discard, r.source, int64(remaining)); err != nil { // #nosec G115 -- bounded above.
		return fmt.Errorf("skip interrupted ULog message at appended offset %d: %w", r.appendedOffsets[0], err)
	}
	r.offset += remaining
	r.appendedOffsets = r.appendedOffsets[1:]
	return nil
}

func (r *Reader) consume(messageType wire.MessageType, payload []byte) (Record, bool, error) {
	switch messageType {
	case wire.MessageTypeFlagBits:
		flagSize := binary.Size(wire.FlagBitsMessage{})
		if len(payload) < flagSize {
			return Record{}, false, fmt.Errorf("flag-bits payload has size %d, minimum is %d", len(payload), flagSize)
		}
		var flags wire.FlagBitsMessage
		if _, err := binary.Decode(payload[:flagSize], binary.LittleEndian, &flags); err != nil {
			return Record{}, false, fmt.Errorf("decode flag bits: %w", err)
		}
		unknown := flags.IncompatibilityFlags &^ wire.IncompatibilityFlagDataAppended
		if unknown != 0 {
			return Record{}, false, fmt.Errorf("unsupported ULog incompatibility flags %#x", unknown)
		}
		if flags.IncompatibilityFlags&wire.IncompatibilityFlagDataAppended != 0 {
			previous := r.offset
			for _, offset := range flags.AppendedOffsets {
				if offset == 0 {
					break
				}
				if offset <= previous {
					return Record{}, false, fmt.Errorf("invalid ULog appended offset %d after offset %d", offset, previous)
				}
				r.appendedOffsets = append(r.appendedOffsets, offset)
				previous = offset
			}
			if len(r.appendedOffsets) == 0 {
				return Record{}, false, errors.New("ULog data-appended flag has no appended offsets")
			}
		}
	case wire.MessageTypeFormat:
		var message wire.FormatMessage
		if err := message.UnmarshalBinary(payload); err != nil {
			return Record{}, false, fmt.Errorf("decode format message: %w", err)
		}
		format, err := ParseFormat(message.Format)
		if err != nil {
			return Record{}, false, err
		}
		if existing, ok := r.formats[format.Name]; ok && existing.String() != format.String() {
			return Record{}, false, fmt.Errorf("format %q is redefined incompatibly", format.Name)
		}
		r.formats[format.Name] = cloneFormat(*format)
	case wire.MessageTypeInformation:
		var message wire.InformationMessage
		if err := message.UnmarshalBinary(payload); err != nil {
			return Record{}, false, fmt.Errorf("decode information message: %w", err)
		}
		entry, err := decodeKeyValue(message.Key, message.Value)
		if err != nil {
			return Record{}, false, fmt.Errorf("decode information value: %w", err)
		}
		r.information = append(r.information, entry)
	case wire.MessageTypeMultiInformation:
		var message wire.MultiInformationMessage
		if err := message.UnmarshalBinary(payload); err != nil {
			return Record{}, false, fmt.Errorf("decode multi-information message: %w", err)
		}
		entry, err := decodeMultiInformationValue(message.Key, message.Value)
		if err != nil {
			return Record{}, false, fmt.Errorf("decode multi-information value: %w", err)
		}
		if message.IsContinued == 0 {
			r.multiInfo = append(r.multiInfo, MultiInformationGroup{
				Name: entry.Name, Values: []MultiInformationValue{entry},
			})
			r.multiInfoLast[entry.Name] = len(r.multiInfo) - 1
			break
		}
		index, ok := r.multiInfoLast[entry.Name]
		if !ok {
			return Record{}, false, fmt.Errorf("multi-information continuation for %q has no matching previous message", entry.Name)
		}
		r.multiInfo[index].Values = append(r.multiInfo[index].Values, entry)
	case wire.MessageTypeParameter:
		var message wire.ParameterMessage
		if err := message.UnmarshalBinary(payload); err != nil {
			return Record{}, false, fmt.Errorf("decode parameter message: %w", err)
		}
		entry, err := decodeKeyValue(message.Key, message.Value)
		if err != nil {
			return Record{}, false, fmt.Errorf("decode parameter value: %w", err)
		}
		r.parameters = append(r.parameters, entry)
	case wire.MessageTypeDefaultParameter:
		var message wire.DefaultParameterMessage
		if err := message.UnmarshalBinary(payload); err != nil {
			return Record{}, false, fmt.Errorf("decode default-parameter message: %w", err)
		}
		entry, err := decodeKeyValue(message.Key, message.Value)
		if err != nil {
			return Record{}, false, fmt.Errorf("decode default-parameter value: %w", err)
		}
		r.defaults = append(r.defaults, DefaultParameter{
			KeyValue: entry,
			Types:    DefaultParameterTypes(message.Types),
		})
	case wire.MessageTypeSubscription:
		var message wire.SubscriptionMessage
		if err := message.UnmarshalBinary(payload); err != nil {
			return Record{}, false, fmt.Errorf("decode subscription message: %w", err)
		}
		format, ok := r.formats[message.MessageName]
		if !ok {
			return Record{}, false, fmt.Errorf("subscription %d references unknown format %q", message.MessageID, message.MessageName)
		}
		if err := validateSubscriptionFormat(format); err != nil {
			return Record{}, false, err
		}
		if _, exists := r.subscriptions[message.MessageID]; exists {
			return Record{}, false, fmt.Errorf("message ID %d is already subscribed", message.MessageID)
		}
		layout, err := resolveLayout(format, r.formats)
		if err != nil {
			return Record{}, false, fmt.Errorf("resolve format %q: %w", format.Name, err)
		}
		r.subscriptions[message.MessageID] = subscription{
			messageID: message.MessageID,
			multiID:   message.MultiID,
			format:    cloneFormat(format),
			layout:    layout,
		}
	case wire.MessageTypeUnsubscription:
		if len(payload) != binary.Size(wire.UnsubscriptionMessage{}) {
			return Record{}, false, fmt.Errorf("unsubscription payload has size %d, want %d", len(payload), binary.Size(wire.UnsubscriptionMessage{}))
		}
		var message wire.UnsubscriptionMessage
		if _, err := binary.Decode(payload, binary.LittleEndian, &message); err != nil {
			return Record{}, false, fmt.Errorf("decode unsubscription message: %w", err)
		}
		delete(r.subscriptions, message.MessageID)
	case wire.MessageTypeData:
		var message wire.DataMessage
		if err := message.UnmarshalBinary(payload); err != nil {
			return Record{}, false, fmt.Errorf("decode data message: %w", err)
		}
		subscription, ok := r.subscriptions[message.MessageID]
		if !ok {
			return Record{}, false, fmt.Errorf("data references unknown message ID %d", message.MessageID)
		}
		return Record{
			messageID: subscription.messageID,
			multiID:   subscription.multiID,
			format:    cloneFormat(subscription.format),
			layout:    append([]layoutField(nil), subscription.layout...),
			data:      bytes.Clone(message.Data),
		}, true, nil
	case wire.MessageTypeLogging:
		var message wire.LoggingMessage
		if err := message.UnmarshalBinary(payload); err != nil {
			return Record{}, false, fmt.Errorf("decode logging message: %w", err)
		}
		r.logs = append(r.logs, LogEntry{
			Level: LogLevel(message.Level), Timestamp: message.Timestamp, Message: message.Message,
		})
	case wire.MessageTypeTaggedLogging:
		var message wire.TaggedLoggingMessage
		if err := message.UnmarshalBinary(payload); err != nil {
			return Record{}, false, fmt.Errorf("decode tagged logging message: %w", err)
		}
		r.logs = append(r.logs, LogEntry{
			Level: LogLevel(message.Level), Timestamp: message.Timestamp, Message: message.Message,
			Tag: message.Tag, Tagged: true,
		})
	case wire.MessageTypeDropout:
		if len(payload) != binary.Size(wire.DropoutMessage{}) {
			return Record{}, false, fmt.Errorf("dropout payload has size %d, want %d", len(payload), binary.Size(wire.DropoutMessage{}))
		}
		var message wire.DropoutMessage
		if _, err := binary.Decode(payload, binary.LittleEndian, &message); err != nil {
			return Record{}, false, fmt.Errorf("decode dropout message: %w", err)
		}
		r.dropouts = append(r.dropouts, Dropout{Duration: time.Duration(message.Duration) * time.Millisecond})
	}

	return Record{}, false, nil
}

func resolveLayout(root Format, formats map[string]Format) ([]layoutField, error) {
	layout, _, err := resolveFormatLayout(root.Name, "", 0, false, formats, make(map[string]bool))
	return layout, err
}

func resolveFormatLayout(
	name string,
	prefix string,
	offset int,
	hidden bool,
	formats map[string]Format,
	active map[string]bool,
) ([]layoutField, int, error) {
	format, ok := formats[name]
	if !ok {
		return nil, 0, fmt.Errorf("unknown nested format %q", name)
	}
	if active[name] {
		return nil, 0, fmt.Errorf("format cycle involving %q", name)
	}
	active[name] = true
	defer delete(active, name)

	start := offset
	var layout []layoutField
	for _, field := range format.Fields {
		fieldHidden := hidden || strings.HasPrefix(field.Name, "_padding")
		if field.Type == TypeChar && field.ArrayLength > 0 {
			path := field.Name
			if prefix != "" {
				path = prefix + "." + path
			}
			if offset > maxDataPayloadSize-field.ArrayLength {
				return nil, 0, fmt.Errorf("format %q exceeds maximum data payload of %d bytes", name, maxDataPayloadSize)
			}
			layout = append(layout, layoutField{
				name:        path,
				typeID:      field.Type,
				arrayLength: field.ArrayLength,
				offset:      offset,
				size:        field.ArrayLength,
				hidden:      fieldHidden,
			})
			offset += field.ArrayLength
			continue
		}
		count := field.ArrayLength
		if count == 0 {
			count = 1
		}
		for i := range count {
			fieldName := field.Name
			if field.ArrayLength > 0 {
				fieldName += fmt.Sprintf("[%d]", i)
			}
			path := fieldName
			if prefix != "" {
				path = prefix + "." + fieldName
			}
			if size, primitive := primitiveSize(field.Type); primitive {
				if offset > maxDataPayloadSize-size {
					return nil, 0, fmt.Errorf("format %q exceeds maximum data payload of %d bytes", name, maxDataPayloadSize)
				}
				layout = append(layout, layoutField{
					name:   path,
					typeID: field.Type,
					offset: offset,
					size:   size,
					hidden: fieldHidden,
				})
				offset += size
				continue
			}

			nested, nestedSize, err := resolveFormatLayout(string(field.Type), path, offset, fieldHidden, formats, active)
			if err != nil {
				return nil, 0, err
			}
			layout = append(layout, nested...)
			offset += nestedSize
		}
	}
	return layout, offset - start, nil
}

func cloneFormat(format Format) Format {
	format.Fields = append([]Field(nil), format.Fields...)
	return format
}

// FieldValue is one dynamically decoded value from a [Record]. [FieldValue.Name]
// is flattened: numeric arrays and nested formats use paths such as "q[0]" and
// "position.x". Character arrays remain one string-valued field.
type FieldValue struct {
	// Name is the flattened field path.
	Name string
	// Type is the primitive wire type of Value.
	Type Type
	// ArrayLength is the fixed byte width of a character array, or zero for a
	// scalar value.
	ArrayLength int
	// Value has the Go scalar type corresponding to Type. Character arrays are
	// strings with trailing NUL padding removed.
	Value any
}

// ScalarField describes one flattened, non-padding value in a [Record].
type ScalarField struct {
	// Name is the flattened field path.
	Name string
	// Type is the field's primitive wire type.
	Type Type
	// ArrayLength is the fixed byte width of a character array, or zero for a
	// scalar value.
	ArrayLength int
}

// Record is one ULog data message paired with the subscription and [Format]
// needed to interpret it.
type Record struct {
	messageID uint16
	multiID   uint8
	format    Format
	layout    []layoutField
	data      []byte
}

// Name returns the case-sensitive [Format.Name] selected by the record's
// subscription.
func (r Record) Name() string { return r.format.Name }

// MessageID returns the runtime subscription identifier. It is only meaningful
// within this log.
func (r Record) MessageID() uint16 { return r.messageID }

// MultiID returns the instance identifier for the subscribed format. Zero is the
// first and default instance.
func (r Record) MultiID() uint8 { return r.multiID }

// Format returns an independent copy of the dynamic schema selected by the
// record's subscription.
func (r Record) Format() Format { return cloneFormat(r.format) }

// Fields returns every flattened, non-padding value in the selected
// format, in wire order. A compatible older record may omit trailing fields;
// [Record.Value] reports those fields as unavailable.
func (r Record) Fields() []ScalarField {
	fields := make([]ScalarField, 0, len(r.layout))
	for _, field := range r.layout {
		if field.hidden {
			continue
		}
		fields = append(fields, ScalarField{Name: field.name, Type: field.typeID, ArrayLength: field.arrayLength})
	}
	return fields
}

// Bytes returns an independent copy of the raw bytes described by
// [Record.Format], excluding the subscription ID and enclosing message header.
// The payload may omit trailing top-level padding permitted by ULog.
func (r Record) Bytes() []byte { return bytes.Clone(r.data) }

// Values decodes every available non-padding value in wire order.
// Missing trailing fields are omitted to support compatible schema extension.
func (r Record) Values() ([]FieldValue, error) {
	values := make([]FieldValue, 0, len(r.layout))
	for _, field := range r.layout {
		if len(r.data) <= field.offset {
			break
		}
		end := field.offset + field.size
		if len(r.data) < end {
			return nil, fmt.Errorf("field %q is truncated: have %d of %d bytes", field.name, len(r.data)-field.offset, field.size)
		}
		if field.hidden {
			continue
		}

		value, err := decodeLayoutValue(field, r.data[field.offset:end])
		if err != nil {
			return nil, fmt.Errorf("decode field %q: %w", field.name, err)
		}
		values = append(values, FieldValue{
			Name: field.name, Type: field.typeID, ArrayLength: field.arrayLength, Value: value,
		})
	}
	return values, nil
}

// Value decodes a field by its flattened path. It reports an error when
// the field is unknown, omitted as trailing data, or truncated.
func (r Record) Value(name string) (any, error) {
	values, err := r.Values()
	if err != nil {
		return nil, err
	}
	for _, value := range values {
		if value.Name == name {
			return value.Value, nil
		}
	}
	return nil, fmt.Errorf("field %q is not available in record %q", name, r.Name())
}

func decodeLayoutValue(field layoutField, data []byte) (any, error) {
	if field.typeID == TypeChar && field.arrayLength > 0 {
		return string(bytes.TrimRight(data, "\x00")), nil
	}
	return decodePrimitive(field.typeID, data)
}

func decodePrimitive(typeID Type, data []byte) (any, error) {
	switch typeID {
	case TypeInt8:
		return decodeSigned[int8](data)
	case TypeUint8, TypeChar:
		return data[0], nil
	case TypeBool:
		return data[0] != 0, nil
	case TypeInt16:
		return decodeSigned[int16](data)
	case TypeUint16:
		return binary.LittleEndian.Uint16(data), nil
	case TypeInt32:
		return decodeSigned[int32](data)
	case TypeUint32:
		return binary.LittleEndian.Uint32(data), nil
	case TypeInt64:
		return decodeSigned[int64](data)
	case TypeUint64:
		return binary.LittleEndian.Uint64(data), nil
	case TypeFloat32:
		return math.Float32frombits(binary.LittleEndian.Uint32(data)), nil
	case TypeFloat64:
		return math.Float64frombits(binary.LittleEndian.Uint64(data)), nil
	default:
		return nil, fmt.Errorf("unsupported primitive type %q", typeID)
	}
}

func decodeSigned[T int8 | int16 | int32 | int64](data []byte) (T, error) {
	var value T
	if _, err := binary.Decode(data, binary.LittleEndian, &value); err != nil {
		return 0, err
	}
	return value, nil
}
