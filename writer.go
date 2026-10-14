package ulog

import (
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/infin-robotics/ulog/pkg/wire"
)

// WriterOption configures [NewWriter].
type WriterOption func(*writerConfig) error

type writerConfig struct {
	startTimestamp uint64
}

// WithStartTimestamp records when logging started, in microseconds, in the file
// header. Without this option [NewWriter] writes zero.
func WithStartTimestamp(timestamp uint64) WriterOption {
	return func(config *writerConfig) error {
		config.startTimestamp = timestamp
		return nil
	}
}

// StreamOption configures [Register] or [Writer.RegisterFormat].
type StreamOption func(*streamConfig) error

type streamConfig struct {
	multiID    uint8
	formatName string
}

// WithMultiID identifies one of several streams using the same format. Zero is
// the first and default instance.
func WithMultiID(multiID uint8) StreamOption {
	return func(config *streamConfig) error {
		config.multiID = multiID
		return nil
	}
}

// WithFormatName sets the root format name used by [Register] or
// [Writer.RegisterFormat]. For typed streams it replaces the name derived from
// the Go type; nested format names are unchanged.
func WithFormatName(name string) StreamOption {
	return func(config *streamConfig) error {
		if !formatNamePattern.MatchString(name) {
			return fmt.Errorf("invalid ULog format name %q", name)
		}
		config.formatName = name
		return nil
	}
}

// Writer serialises a ULog stream. All definitions, information, parameters,
// and stream registrations must be complete before the first data or log write.
// Writer methods and registered stream writes are safe for concurrent use;
// concurrent writes are serialised, but their relative order is unspecified.
type Writer struct {
	mu            sync.Mutex
	destination   io.Writer
	formats       map[string]Format
	registrations []*writerRegistration
	nextMessageID uint32
	dataStarted   bool
	closed        bool
	writeErr      error
}

type writerRegistration struct {
	messageID uint16
	multiID   uint8
	name      string
}

// NewWriter immediately writes the ULog file header and required flag-bits
// message to destination. Neither [Writer.Close] nor a failed constructor closes
// destination.
func NewWriter(destination io.Writer, options ...WriterOption) (*Writer, error) {
	if destination == nil {
		return nil, errors.New("nil ULog destination")
	}

	var config writerConfig
	for _, option := range options {
		if option == nil {
			return nil, errors.New("nil writer option")
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}

	writer := &Writer{
		destination: destination,
		formats:     make(map[string]Format),
	}
	var magic [7]byte
	copy(magic[:], wire.FileMagic)
	header, err := binary.Append(nil, binary.LittleEndian, wire.FileHeader{
		Magic:     magic,
		Version:   wire.FileVersion,
		Timestamp: config.startTimestamp,
	})
	if err != nil {
		return nil, fmt.Errorf("encode ULog file header: %w", err)
	}
	if err := writeAll(destination, header); err != nil {
		return nil, fmt.Errorf("write ULog file header: %w", err)
	}

	flagBits, err := binary.Append(nil, binary.LittleEndian, wire.FlagBitsMessage{})
	if err != nil {
		return nil, fmt.Errorf("encode ULog flag bits: %w", err)
	}
	if err := writeFramed(destination, wire.MessageTypeFlagBits, flagBits); err != nil {
		return nil, fmt.Errorf("write ULog flag bits: %w", err)
	}
	return writer, nil
}

// Define validates and writes a dynamic [Format]. Definitions must be added
// before the first data or log write. Repeating an identical definition is a
// no-op; redefining the same name differently returns an error.
func (w *Writer) Define(format Format) error {
	if w == nil {
		return errors.New("nil ULog writer")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.defineLocked(format)
}

// WriteInformation writes one initial metadata entry before the data section.
// value may be a non-empty string or a scalar Go value supported by the ULog
// primitive mapping. Calls after the first data or log write return an error.
func (w *Writer) WriteInformation(name string, value any) error {
	if w == nil {
		return errors.New("nil ULog writer")
	}
	key, encoded, err := encodeKeyValue(name, value)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.requireDefinitionSectionLocked("information"); err != nil {
		return err
	}
	return w.writeMessageLocked(wire.MessageTypeInformation, wire.InformationMessage{Key: key, Value: encoded})
}

// WriteParameter writes a vehicle parameter's value at the start of logging.
// ULog permits int32 and float32 values. Calls after the first data or log write
// return an error.
func (w *Writer) WriteParameter(name string, value any) error {
	if w == nil {
		return errors.New("nil ULog writer")
	}
	typ := reflect.TypeOf(value)
	if typ == nil {
		return fmt.Errorf("ULog parameter %q requires int32 or float32, got %T", name, value)
	}
	typeID, ok := primitiveTypeFor(typ)
	if !ok || (typeID != TypeInt32 && typeID != TypeFloat32) {
		return fmt.Errorf("ULog parameter %q requires int32 or float32, got %T", name, value)
	}
	key, encoded, err := encodeKeyValue(name, value)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.requireDefinitionSectionLocked("parameter"); err != nil {
		return err
	}
	return w.writeMessageLocked(wire.MessageTypeParameter, wire.ParameterMessage{Key: key, Value: encoded})
}

// WriteLog writes untagged printf-style output in the data section. timestamp is
// in microseconds. The first call starts the data section and prevents further
// definitions, registrations, initial information, or initial parameters.
func (w *Writer) WriteLog(level LogLevel, timestamp uint64, message string) error {
	if w == nil {
		return errors.New("nil ULog writer")
	}
	if level < LogLevelEmergency || level > LogLevelDebug {
		return fmt.Errorf("invalid ULog log level %q", level)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("ULog writer is closed")
	}
	if w.writeErr != nil {
		return w.writeErr
	}
	if err := w.startDataLocked(); err != nil {
		return err
	}
	return w.writeMessageLocked(wire.MessageTypeLogging, wire.LoggingMessage{
		Level: wire.LogLevel(level), Timestamp: timestamp, Message: message,
	})
}

// WriteDropout marks a period in which logging messages were lost. duration must
// be a non-negative whole number of milliseconds no greater than 65,535 ms. The
// first call starts the data section.
func (w *Writer) WriteDropout(duration time.Duration) error {
	if w == nil {
		return errors.New("nil ULog writer")
	}
	if duration < 0 || duration%time.Millisecond != 0 {
		return fmt.Errorf("dropout duration %s must be a non-negative whole number of milliseconds", duration)
	}
	milliseconds := duration / time.Millisecond
	if milliseconds > math.MaxUint16 {
		return fmt.Errorf("dropout duration %s exceeds %d milliseconds", duration, math.MaxUint16)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("ULog writer is closed")
	}
	if w.writeErr != nil {
		return w.writeErr
	}
	if err := w.startDataLocked(); err != nil {
		return err
	}
	payload, err := binary.Append(nil, binary.LittleEndian, wire.DropoutMessage{
		Duration: uint16(milliseconds), // #nosec G115 -- checked against MaxUint16 above.
	})
	if err != nil {
		return fmt.Errorf("encode dropout: %w", err)
	}
	return w.writePayloadLocked(wire.MessageTypeDropout, payload)
}

func (w *Writer) requireDefinitionSectionLocked(kind string) error {
	if w.closed {
		return errors.New("ULog writer is closed")
	}
	if w.writeErr != nil {
		return w.writeErr
	}
	if w.dataStarted {
		return fmt.Errorf("cannot write initial %s after ULog data has started", kind)
	}
	return nil
}

func (w *Writer) defineLocked(format Format) error {
	if w.closed {
		return errors.New("ULog writer is closed")
	}
	if w.writeErr != nil {
		return w.writeErr
	}
	if w.dataStarted {
		return errors.New("cannot define a format after ULog data has started")
	}
	parsed, err := ParseFormat(format.String())
	if err != nil {
		return err
	}
	format = cloneFormat(*parsed)
	if existing, ok := w.formats[format.Name]; ok {
		if existing.String() != format.String() {
			return fmt.Errorf("format %q is already defined incompatibly", format.Name)
		}
		return nil
	}

	message := wire.FormatMessage{Format: format.String()}
	if err := w.writeMessageLocked(wire.MessageTypeFormat, message); err != nil {
		return err
	}
	w.formats[format.Name] = format
	return nil
}

// RegisterFormat validates and defines format, then registers a [RawStream]. The
// format must contain a scalar uint64_t timestamp field. Definitions referenced
// by nested fields must first be added with [Writer.Define]. Registration must
// finish before the data section starts.
func (w *Writer) RegisterFormat(format Format, options ...StreamOption) (*RawStream, error) {
	if w == nil {
		return nil, errors.New("nil ULog writer")
	}
	config, err := applyStreamOptions(options)
	if err != nil {
		return nil, err
	}
	if config.formatName != "" {
		format.Name = config.formatName
	}
	parsed, err := ParseFormat(format.String())
	if err != nil {
		return nil, err
	}
	format = *parsed
	if err := validateSubscriptionFormat(format); err != nil {
		return nil, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.defineLocked(format); err != nil {
		return nil, err
	}
	root := w.formats[format.Name]
	layout, err := resolveLayout(root, w.formats)
	if err != nil {
		return nil, fmt.Errorf("resolve format %q: %w", root.Name, err)
	}
	registration, err := w.registerLocked(root.Name, config.multiID)
	if err != nil {
		return nil, err
	}
	fullSize, omittedPaddingSize := rawPayloadSizes(root, layout)
	return &RawStream{
		writer:             w,
		registration:       registration,
		payloadSize:        fullSize,
		omittedPaddingSize: omittedPaddingSize,
	}, nil
}

// Register derives the formats required by T using the same mapping as
// [FormatsFor], defines them, then registers a typed [Stream]. T must contain an
// exported scalar field that maps to the case-sensitive ULog name "timestamp"
// and wire type uint64_t. Registration must finish before the data section
// starts. Values are encoded in field order without Go struct padding.
func Register[T any](writer *Writer, options ...StreamOption) (*Stream[T], error) {
	if writer == nil {
		return nil, errors.New("nil ULog writer")
	}
	schema, err := typedSchemaFor(reflect.TypeFor[T]())
	if err != nil {
		return nil, err
	}
	config, err := applyStreamOptions(options)
	if err != nil {
		return nil, err
	}
	formats := cloneFormats(schema.formats)
	root := &formats[len(formats)-1]
	if config.formatName != "" {
		root.Name = config.formatName
	}
	if err := validateSubscriptionFormat(*root); err != nil {
		return nil, err
	}

	writer.mu.Lock()
	defer writer.mu.Unlock()
	for _, format := range formats {
		if err := writer.defineLocked(format); err != nil {
			return nil, err
		}
	}
	registration, err := writer.registerLocked(root.Name, config.multiID)
	if err != nil {
		return nil, err
	}
	return &Stream[T]{writer: writer, registration: registration, schema: schema}, nil
}

func applyStreamOptions(options []StreamOption) (streamConfig, error) {
	var config streamConfig
	for _, option := range options {
		if option == nil {
			return streamConfig{}, errors.New("nil stream option")
		}
		if err := option(&config); err != nil {
			return streamConfig{}, err
		}
	}
	return config, nil
}

func (w *Writer) registerLocked(name string, multiID uint8) (*writerRegistration, error) {
	if w.closed {
		return nil, errors.New("ULog writer is closed")
	}
	if w.writeErr != nil {
		return nil, w.writeErr
	}
	if w.dataStarted {
		return nil, errors.New("cannot register a stream after ULog data has started")
	}
	for _, registration := range w.registrations {
		if registration.name == name && registration.multiID == multiID {
			return nil, fmt.Errorf("format %q multi ID %d is already registered", name, multiID)
		}
	}
	if w.nextMessageID > math.MaxUint16 {
		return nil, errors.New("ULog message ID space exhausted")
	}
	registration := &writerRegistration{
		messageID: uint16(w.nextMessageID), // #nosec G115 -- checked against MaxUint16 above.
		multiID:   multiID,
		name:      name,
	}
	w.nextMessageID++
	w.registrations = append(w.registrations, registration)
	return registration, nil
}

// Stream writes values of T to one ULog subscription created by [Register].
type Stream[T any] struct {
	writer       *Writer
	registration *writerRegistration
	schema       *typedSchema
}

// Write encodes value using the format fixed by [Register]. The first stream
// write emits every registered subscription and starts the data section.
func (s *Stream[T]) Write(value T) error {
	if s == nil || s.writer == nil {
		return errors.New("nil ULog stream")
	}
	payload, err := encodeTypedValue(s.schema, reflect.ValueOf(value))
	if err != nil {
		return err
	}
	return s.writer.writeData(s.registration, payload)
}

// RawStream writes payloads for one dynamic [Format] registered by
// [Writer.RegisterFormat].
type RawStream struct {
	writer             *Writer
	registration       *writerRegistration
	payloadSize        int
	omittedPaddingSize int
}

// Write writes one format-defined payload. Top-level trailing padding may be
// omitted as permitted by ULog; all other fields must be encoded.
func (s *RawStream) Write(payload []byte) error {
	if s == nil || s.writer == nil {
		return errors.New("nil ULog raw stream")
	}
	if len(payload) != s.payloadSize && len(payload) != s.omittedPaddingSize {
		if s.omittedPaddingSize == s.payloadSize {
			return fmt.Errorf("raw payload has size %d, want %d", len(payload), s.payloadSize)
		}
		return fmt.Errorf(
			"raw payload has size %d, want %d or %d without trailing padding",
			len(payload), s.payloadSize, s.omittedPaddingSize,
		)
	}
	return s.writer.writeData(s.registration, payload)
}

func (w *Writer) writeData(registration *writerRegistration, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("ULog writer is closed")
	}
	if w.writeErr != nil {
		return w.writeErr
	}
	if err := w.startDataLocked(); err != nil {
		return err
	}
	return w.writeMessageLocked(wire.MessageTypeData, wire.DataMessage{
		MessageID: registration.messageID,
		Data:      payload,
	})
}

func (w *Writer) startDataLocked() error {
	if w.dataStarted {
		return nil
	}
	for _, registration := range w.registrations {
		message := wire.SubscriptionMessage{
			MultiID:     registration.multiID,
			MessageID:   registration.messageID,
			MessageName: registration.name,
		}
		if err := w.writeMessageLocked(wire.MessageTypeSubscription, message); err != nil {
			return err
		}
	}
	w.dataStarted = true
	return nil
}

// Close writes any subscriptions not yet emitted, then prevents further writes.
// It does not close the underlying destination and is safe to call more than
// once. Close returns the first destination write error, including on later
// calls.
func (w *Writer) Close() error {
	if w == nil {
		return errors.New("nil ULog writer")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return w.writeErr
	}
	if w.writeErr == nil {
		_ = w.startDataLocked()
	}
	w.closed = true
	return w.writeErr
}

func (w *Writer) writeMessageLocked(messageType wire.MessageType, message encoding.BinaryAppender) error {
	payload, err := message.AppendBinary(nil)
	if err != nil {
		return err
	}
	return w.writePayloadLocked(messageType, payload)
}

func (w *Writer) writePayloadLocked(messageType wire.MessageType, payload []byte) error {
	if err := writeFramed(w.destination, messageType, payload); err != nil {
		w.writeErr = fmt.Errorf("write ULog %q message: %w", messageType, err)
		return w.writeErr
	}
	return nil
}

func writeFramed(destination io.Writer, messageType wire.MessageType, payload []byte) error {
	if len(payload) > math.MaxUint16 {
		return fmt.Errorf("ULog message payload has size %d, maximum is %d", len(payload), math.MaxUint16)
	}
	header := []byte{0, 0, byte(messageType)}
	binary.LittleEndian.PutUint16(header[:2], uint16(len(payload))) // #nosec G115 -- checked against MaxUint16 above.
	if err := writeAll(destination, header); err != nil {
		return err
	}
	return writeAll(destination, payload)
}

func writeAll(destination io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := destination.Write(data)
		if written < 0 || written > len(data) {
			return fmt.Errorf("invalid write count %d for %d bytes", written, len(data))
		}
		data = data[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func layoutByteSize(layout []layoutField) int {
	if len(layout) == 0 {
		return 0
	}
	last := layout[len(layout)-1]
	return last.offset + last.size
}

func rawPayloadSizes(root Format, layout []layoutField) (int, int) {
	fullSize := layoutByteSize(layout)
	if len(root.Fields) == 0 {
		return fullSize, fullSize
	}
	padding := root.Fields[len(root.Fields)-1]
	if !strings.HasPrefix(padding.Name, "_padding") {
		return fullSize, fullSize
	}
	for _, field := range layout {
		if field.name == padding.Name ||
			strings.HasPrefix(field.name, padding.Name+"[") ||
			strings.HasPrefix(field.name, padding.Name+".") {
			return fullSize, field.offset
		}
	}
	return fullSize, fullSize
}

func encodeTypedValue(schema *typedSchema, value reflect.Value) ([]byte, error) {
	if !value.IsValid() || value.Type() != schema.root {
		return nil, fmt.Errorf("typed stream requires %s", schema.root)
	}
	var payload []byte
	for _, leaf := range schema.leaves {
		field := value
		for _, step := range leaf.steps {
			field = field.Field(step.field)
			if step.array >= 0 {
				field = field.Index(step.array)
			}
		}
		if leaf.typeID == TypeChar && leaf.arrayLength > 0 {
			text := field.String()
			if len(text) > leaf.arrayLength {
				return nil, fmt.Errorf("encode field %q: string has %d bytes, maximum is %d", leaf.path, len(text), leaf.arrayLength)
			}
			payload = append(payload, text...)
			payload = append(payload, make([]byte, leaf.arrayLength-len(text))...)
			continue
		}
		var err error
		payload, err = binary.Append(payload, binary.LittleEndian, field.Interface())
		if err != nil {
			return nil, fmt.Errorf("encode field %q: %w", leaf.path, err)
		}
	}
	return payload, nil
}
