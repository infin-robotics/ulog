package ulog

import (
	"bytes"
	"encoding"
	"encoding/binary"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/infin-robotics/ulog/pkg/wire"
)

func TestReaderResolvesDynamicRecords(t *testing.T) {
	data := newULogFixture(t, 42)
	data.message(t, wire.MessageTypeFormat, wire.FormatMessage{Format: "sample:int16_t x;float y;"})
	data.message(t, wire.MessageTypeFormat, wire.FormatMessage{Format: "telemetry:uint64_t timestamp;float[2] q;sample[2] samples;uint8_t _padding0;"})
	data.message(t, wire.MessageTypeSubscription, wire.SubscriptionMessage{
		MultiID:     3,
		MessageID:   17,
		MessageName: "telemetry",
	})

	payload := make([]byte, 0, 31)
	payload = binary.LittleEndian.AppendUint64(payload, 0x0102030405060708)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(1.5))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(-2.25))
	payload = append(payload, 0x85, 0xff)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(3.5))
	payload = binary.LittleEndian.AppendUint16(payload, 456)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(-4.75))
	payload = append(payload, 0)
	data.message(t, wire.MessageTypeData, wire.DataMessage{MessageID: 17, Data: payload})

	reader, err := NewReader(bytes.NewReader(data.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if got := reader.Header().Timestamp; got != 42 {
		t.Errorf("Header().Timestamp = %d, want 42", got)
	}
	if !reader.Next() {
		t.Fatalf("Next() = false, error = %v", reader.Err())
	}

	record := reader.Record()
	if got, want := record.Name(), "telemetry"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got, want := record.MultiID(), uint8(3); got != want {
		t.Errorf("MultiID() = %d, want %d", got, want)
	}
	if got, want := record.MessageID(), uint16(17); got != want {
		t.Errorf("MessageID() = %d, want %d", got, want)
	}

	wantValues := []FieldValue{
		{Name: "timestamp", Type: TypeUint64, Value: uint64(0x0102030405060708)},
		{Name: "q[0]", Type: TypeFloat32, Value: float32(1.5)},
		{Name: "q[1]", Type: TypeFloat32, Value: float32(-2.25)},
		{Name: "samples[0].x", Type: TypeInt16, Value: int16(-123)},
		{Name: "samples[0].y", Type: TypeFloat32, Value: float32(3.5)},
		{Name: "samples[1].x", Type: TypeInt16, Value: int16(456)},
		{Name: "samples[1].y", Type: TypeFloat32, Value: float32(-4.75)},
	}
	values, err := record.Values()
	if err != nil {
		t.Fatalf("Values() error = %v", err)
	}
	if !reflect.DeepEqual(values, wantValues) {
		t.Errorf("Values() = %#v, want %#v", values, wantValues)
	}

	value, err := record.Value("samples[1].x")
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	if value != int16(456) {
		t.Errorf("Value() = %#v, want int16(456)", value)
	}

	copied := record.Bytes()
	copied[0] = 0
	if got := record.Bytes()[0]; got == 0 {
		t.Error("Bytes() aliases the record payload")
	}

	if reader.Next() {
		t.Fatal("second Next() = true, want false")
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
}

func TestReaderAcceptsRecordsWithMissingTrailingFields(t *testing.T) {
	data := newULogFixture(t, 0)
	data.message(t, wire.MessageTypeFormat, wire.FormatMessage{Format: "versioned:uint64_t timestamp;float old_value;uint32_t new_value;"})
	data.message(t, wire.MessageTypeSubscription, wire.SubscriptionMessage{MessageID: 1, MessageName: "versioned"})

	payload := binary.LittleEndian.AppendUint64(nil, 99)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(12.5))
	data.message(t, wire.MessageTypeData, wire.DataMessage{MessageID: 1, Data: payload})

	reader, err := NewReader(bytes.NewReader(data.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if !reader.Next() {
		t.Fatalf("Next() = false, error = %v", reader.Err())
	}

	values, err := reader.Record().Values()
	if err != nil {
		t.Fatalf("Values() error = %v", err)
	}
	if got, want := len(values), 2; got != want {
		t.Fatalf("len(Values()) = %d, want %d", got, want)
	}
	fields := reader.Record().Fields()
	if got, want := len(fields), 3; got != want {
		t.Fatalf("len(Fields()) = %d, want %d", got, want)
	}
	if got, want := fields[2].Name, "new_value"; got != want {
		t.Errorf("Fields()[2].Name = %q, want %q", got, want)
	}
	if _, err := reader.Record().Value("new_value"); err == nil {
		t.Fatal("Value(new_value) succeeded for an omitted trailing field")
	}
}

func TestReaderRejectsUnknownDataMessageID(t *testing.T) {
	data := newULogFixture(t, 0)
	data.message(t, wire.MessageTypeData, wire.DataMessage{MessageID: 99})

	reader, err := NewReader(bytes.NewReader(data.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if reader.Next() {
		t.Fatal("Next() = true, want false")
	}
	if err := reader.Err(); err == nil || !strings.Contains(err.Error(), "message ID 99") {
		t.Fatalf("Err() = %v, want unknown message ID error", err)
	}
}

func TestReaderRejectsPartiallyEncodedFields(t *testing.T) {
	data := newULogFixture(t, 0)
	data.message(t, wire.MessageTypeFormat, wire.FormatMessage{Format: "partial:uint64_t timestamp;uint32_t value;"})
	data.message(t, wire.MessageTypeSubscription, wire.SubscriptionMessage{MessageID: 1, MessageName: "partial"})
	payload := binary.LittleEndian.AppendUint64(nil, 1)
	payload = append(payload, 1, 2)
	data.message(t, wire.MessageTypeData, wire.DataMessage{MessageID: 1, Data: payload})

	reader, err := NewReader(bytes.NewReader(data.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if !reader.Next() {
		t.Fatalf("Next() = false, error = %v", reader.Err())
	}
	if _, err := reader.Record().Values(); err == nil {
		t.Fatal("Values() succeeded for a partially encoded field")
	}
}

func TestReaderRejectsNestedLayoutsLargerThanADataMessage(t *testing.T) {
	data := newULogFixture(t, 0)
	data.message(t, wire.MessageTypeFormat, wire.FormatMessage{Format: "inner:uint16_t value;"})
	data.message(t, wire.MessageTypeFormat, wire.FormatMessage{Format: "outer:uint64_t timestamp;inner[65533] values;"})
	data.message(t, wire.MessageTypeSubscription, wire.SubscriptionMessage{MessageID: 1, MessageName: "outer"})

	reader, err := NewReader(bytes.NewReader(data.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if reader.Next() {
		t.Fatal("Next() = true, want false")
	}
	if err := reader.Err(); err == nil || !strings.Contains(err.Error(), "maximum data payload") {
		t.Fatalf("Err() = %v, want oversized layout error", err)
	}
}

func TestReaderRejectsSubscribedFormatWithoutTimestamp(t *testing.T) {
	data := newULogFixture(t, 0)
	data.message(t, wire.MessageTypeFormat, wire.FormatMessage{Format: "invalid:uint32_t value;"})
	data.message(t, wire.MessageTypeSubscription, wire.SubscriptionMessage{MessageID: 1, MessageName: "invalid"})

	reader, err := NewReader(bytes.NewReader(data.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if reader.Next() {
		t.Fatal("Next() = true, want false")
	}
	if err := reader.Err(); err == nil || !strings.Contains(err.Error(), "timestamp") {
		t.Fatalf("Err() = %v, want timestamp error", err)
	}
}

func TestReaderAcceptsFutureVersionAndExtendedFlagBits(t *testing.T) {
	data := newULogFixture(t, 0)
	data.data[7] = 99
	binary.LittleEndian.PutUint16(data.data[16:18], 42)
	data.data = append(data.data, 0xaa, 0xbb)

	reader, err := NewReader(bytes.NewReader(data.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if got := reader.Header().Version; got != 99 {
		t.Errorf("Header().Version = %d, want 99", got)
	}
	if reader.Next() {
		t.Fatal("Next() = true, want false")
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
}

func TestReaderContinuesAtAppendedDataAfterInterruptedMessage(t *testing.T) {
	data := newULogFixture(t, 0)
	data.message(t, wire.MessageTypeFormat, wire.FormatMessage{Format: "sample:uint64_t timestamp;uint16_t value;"})
	data.message(t, wire.MessageTypeSubscription, wire.SubscriptionMessage{MessageID: 1, MessageName: "sample"})

	data.rawMessageHeader(t, wire.MessageTypeData, 12)
	data.data = append(data.data, 1, 0, 0xff)
	appendedOffset := uint64(len(data.data))

	data.message(t, wire.MessageTypeData, wire.DataMessage{MessageID: 1, Data: samplePayload(99, 42)})
	data.setAppendedOffsets(t, appendedOffset)

	reader, err := NewReader(bytes.NewReader(data.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if !reader.Next() {
		t.Fatalf("Next() = false, error = %v", reader.Err())
	}

	assertRecordValue(t, reader.Record(), 42)
	if reader.Next() {
		t.Fatal("second Next() = true, want false")
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
}

func TestReaderContinuesAcrossMultipleAppendedSections(t *testing.T) {
	data := newULogFixture(t, 0)
	data.message(t, wire.MessageTypeFormat, wire.FormatMessage{Format: "sample:uint64_t timestamp;uint16_t value;"})
	data.message(t, wire.MessageTypeSubscription, wire.SubscriptionMessage{MessageID: 1, MessageName: "sample"})

	data.rawMessageHeader(t, wire.MessageTypeData, 12)
	data.data = append(data.data, 1, 0)
	firstOffset := uint64(len(data.data))
	data.message(t, wire.MessageTypeData, wire.DataMessage{MessageID: 1, Data: samplePayload(10, 1)})

	data.data = append(data.data, 0xff, 0xff)
	secondOffset := uint64(len(data.data))
	data.message(t, wire.MessageTypeData, wire.DataMessage{MessageID: 1, Data: samplePayload(20, 2)})
	data.setAppendedOffsets(t, firstOffset, secondOffset)

	reader, err := NewReader(bytes.NewReader(data.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	for i, want := range []uint16{1, 2} {
		if !reader.Next() {
			t.Fatalf("Next() for record %d = false, error = %v", i, reader.Err())
		}
		assertRecordValue(t, reader.Record(), want)
	}
	if reader.Next() {
		t.Fatal("third Next() = true, want false")
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
}

func TestReaderRejectsInvalidAppendedOffsets(t *testing.T) {
	tests := map[string]struct {
		offsets     []uint64
		wantMessage string
	}{
		"missing": {
			wantMessage: "has no appended offsets",
		},
		"before flag bits": {
			offsets:     []uint64{1},
			wantMessage: "invalid ULog appended offset 1",
		},
		"not increasing": {
			offsets:     []uint64{100, 100},
			wantMessage: "invalid ULog appended offset 100 after offset 100",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			data := newULogFixture(t, 0)
			data.setAppendedOffsets(t, tt.offsets...)

			reader, err := NewReader(bytes.NewReader(data.bytes()))
			if err != nil {
				t.Fatalf("NewReader() error = %v", err)
			}
			if reader.Next() {
				t.Fatal("Next() = true, want false")
			}
			if err := reader.Err(); err == nil || !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("Err() = %v, want error containing %q", err, tt.wantMessage)
			}
		})
	}
}

func samplePayload(timestamp uint64, value uint16) []byte {
	payload := binary.LittleEndian.AppendUint64(nil, timestamp)
	return binary.LittleEndian.AppendUint16(payload, value)
}

func assertRecordValue(t *testing.T, record Record, want uint16) {
	t.Helper()
	value, err := record.Value("value")
	if err != nil {
		t.Fatalf("Value(value) error = %v", err)
	}
	if value != want {
		t.Errorf("Value(value) = %#v, want %#v", value, want)
	}
}

type ulogFixture struct {
	data []byte
}

func newULogFixture(t *testing.T, timestamp uint64) *ulogFixture {
	t.Helper()

	var magic [7]byte
	copy(magic[:], wire.FileMagic)
	data, err := binary.Append(nil, binary.LittleEndian, wire.FileHeader{
		Magic:     magic,
		Version:   wire.FileVersion,
		Timestamp: timestamp,
	})
	if err != nil {
		t.Fatalf("append file header: %v", err)
	}

	fixture := &ulogFixture{data: data}
	flagBits, err := binary.Append(nil, binary.LittleEndian, wire.FlagBitsMessage{})
	if err != nil {
		t.Fatalf("append flag bits: %v", err)
	}
	fixture.rawMessage(t, wire.MessageTypeFlagBits, flagBits)
	return fixture
}

func (f *ulogFixture) message(t *testing.T, messageType wire.MessageType, message encoding.BinaryAppender) {
	t.Helper()
	payload, err := message.AppendBinary(nil)
	if err != nil {
		t.Fatalf("append %q message: %v", messageType, err)
	}
	f.rawMessage(t, messageType, payload)
}

func (f *ulogFixture) rawMessage(t *testing.T, messageType wire.MessageType, payload []byte) {
	t.Helper()
	if len(payload) > math.MaxUint16 {
		t.Fatalf("payload size %d exceeds ULog limit", len(payload))
	}
	f.rawMessageHeader(t, messageType, uint16(len(payload))) // #nosec G115 -- bounded above.
	f.data = append(f.data, payload...)
}

func (f *ulogFixture) rawMessageHeader(t *testing.T, messageType wire.MessageType, size uint16) {
	t.Helper()
	header := wire.MessageHeader{Size: size, Type: messageType}
	var err error
	f.data, err = binary.Append(f.data, binary.LittleEndian, header)
	if err != nil {
		t.Fatalf("append message header: %v", err)
	}
}

func (f *ulogFixture) setAppendedOffsets(t *testing.T, offsets ...uint64) {
	t.Helper()
	if len(offsets) > len(wire.FlagBitsMessage{}.AppendedOffsets) {
		t.Fatalf("got %d appended offsets, maximum is %d", len(offsets), len(wire.FlagBitsMessage{}.AppendedOffsets))
	}

	flags := wire.FlagBitsMessage{IncompatibilityFlags: wire.IncompatibilityFlagDataAppended}
	copy(flags.AppendedOffsets[:], offsets)
	payload, err := binary.Append(nil, binary.LittleEndian, flags)
	if err != nil {
		t.Fatalf("append flag bits: %v", err)
	}
	payloadOffset := binary.Size(wire.FileHeader{}) + binary.Size(wire.MessageHeader{})
	copy(f.data[payloadOffset:payloadOffset+len(payload)], payload)
}

func (f *ulogFixture) bytes() []byte {
	return bytes.Clone(f.data)
}
