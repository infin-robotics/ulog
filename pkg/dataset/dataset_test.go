package dataset_test

import (
	"bytes"
	"encoding"
	"encoding/binary"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/infin-robotics/ulog"
	"github.com/infin-robotics/ulog/pkg/dataset"
	"github.com/infin-robotics/ulog/pkg/wire"
)

type pointSample struct {
	Timestamp uint64
	X         int16
	Y         float32
}

func TestReadBuildsTypedDatasets(t *testing.T) {
	var source bytes.Buffer
	writer, err := ulog.NewWriter(&source)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	stream, err := ulog.Register[pointSample](writer, ulog.WithMultiID(3), ulog.WithFormatName("typed_point"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	for _, sample := range []pointSample{{Timestamp: 1, X: -2, Y: 1.25}, {Timestamp: 2, X: 7, Y: -3.5}} {
		if err := stream.Write(sample); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	file, err := dataset.Read(bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	instance, err := file.Dataset("typed_point", 3)
	if err != nil {
		t.Fatalf("Dataset() error = %v", err)
	}
	if got, want := instance.Len(), 2; got != want {
		t.Errorf("Len() = %d, want %d", got, want)
	}

	x, ok := instance.Column("x")
	if !ok {
		t.Fatal("Column(x) not found")
	}
	if got, want := x.Values(), []int16{-2, 7}; !reflect.DeepEqual(got, want) {
		t.Errorf("x.Values() = %#v, want %#v", got, want)
	}
	y, ok := instance.Column("y")
	if !ok {
		t.Fatal("Column(y) not found")
	}
	if got, want := y.Values(), []float32{1.25, -3.5}; !reflect.DeepEqual(got, want) {
		t.Errorf("y.Values() = %#v, want %#v", got, want)
	}
	if got := y.Type(); got != ulog.TypeFloat32 {
		t.Errorf("y.Type() = %q, want %q", got, ulog.TypeFloat32)
	}
	if got, valid := y.Value(1); !valid || got != float32(-3.5) {
		t.Errorf("y.Value(1) = %#v, %t, want -3.5, true", got, valid)
	}
}

func TestReadStoresCharacterArraysAsStrings(t *testing.T) {
	var source bytes.Buffer
	writer, err := ulog.NewWriter(&source)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	stream, err := writer.RegisterFormat(ulog.Format{
		Name: "text_sample",
		Fields: []ulog.Field{
			{Name: "timestamp", Type: ulog.TypeUint64},
			{Name: "name", Type: ulog.TypeChar, ArrayLength: 8},
			{Name: "payload", Type: ulog.TypeUint8, ArrayLength: 2},
			{Name: "code", Type: ulog.TypeChar},
		},
	})
	if err != nil {
		t.Fatalf("RegisterFormat() error = %v", err)
	}
	payload := binary.LittleEndian.AppendUint64(nil, 42)
	payload = append(payload, 'h', 'i', 0, 'x', 0, 0, 0, 0)
	payload = append(payload, 1, 2, 'Z')
	if err := stream.Write(payload); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	file, err := dataset.Read(bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	instance, err := file.Dataset("text_sample", 0)
	if err != nil {
		t.Fatalf("Dataset() error = %v", err)
	}
	if _, ok := instance.Column("name[0]"); ok {
		t.Fatal("Column(name[0]) exists, want one name string column")
	}
	name, ok := instance.Column("name")
	if !ok {
		t.Fatal("Column(name) not found")
	}
	if got, want := name.Values(), []string{"hi\x00x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("name.Values() = %#v, want %#v", got, want)
	}
	values := name.Values().([]string)
	values[0] = "changed"
	if got, valid := name.Value(0); !valid || got != "hi\x00x" {
		t.Errorf("name.Value(0) after caller mutation = %#v, %t, want %q, true", got, valid, "hi\x00x")
	}
	if got, want := name.ArrayLength(), 8; got != want {
		t.Errorf("name.ArrayLength() = %d, want %d", got, want)
	}
	if got, want := instance.Columns()[2].Name(), "payload[0]"; got != want {
		t.Errorf("Columns()[2].Name() = %q, want %q", got, want)
	}
	code, ok := instance.Column("code")
	if !ok {
		t.Fatal("Column(code) not found")
	}
	if got, want := code.Values(), []uint8{'Z'}; !reflect.DeepEqual(got, want) {
		t.Errorf("code.Values() = %#v, want %#v", got, want)
	}
}

func TestReadMarksMissingTrailingCharacterArrayNull(t *testing.T) {
	fixture := newFixture(t, 0)
	fixture.message(t, wire.MessageTypeFormat, wire.FormatMessage{
		Format: "text_versioned:uint64_t timestamp;char[8] name;",
	})
	fixture.message(t, wire.MessageTypeSubscription, wire.SubscriptionMessage{MessageID: 1, MessageName: "text_versioned"})
	complete := binary.LittleEndian.AppendUint64(nil, 1)
	complete = append(complete, 'z', 'o', 'd', 'a', 0, 0, 0, 0)
	fixture.message(t, wire.MessageTypeData, wire.DataMessage{MessageID: 1, Data: complete})
	fixture.message(t, wire.MessageTypeData, wire.DataMessage{
		MessageID: 1,
		Data:      binary.LittleEndian.AppendUint64(nil, 2),
	})

	file, err := dataset.Read(bytes.NewReader(fixture.bytes()))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	instance, err := file.Dataset("text_versioned", 0)
	if err != nil {
		t.Fatalf("Dataset() error = %v", err)
	}
	name, ok := instance.Column("name")
	if !ok {
		t.Fatal("Column(name) not found")
	}
	if got, want := name.Values(), []string{"zoda", ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("name.Values() = %#v, want %#v", got, want)
	}
	if _, valid := name.Value(0); !valid {
		t.Error("name.Value(0) is null, want valid")
	}
	if value, valid := name.Value(1); valid || value != nil {
		t.Errorf("name.Value(1) = %#v, %t, want nil, false", value, valid)
	}
}

func TestReadMarksMissingTrailingValuesNull(t *testing.T) {
	fixture := newFixture(t, 0)
	fixture.message(t, wire.MessageTypeFormat, wire.FormatMessage{
		Format: "versioned:uint64_t timestamp;float old_value;uint32_t new_value;",
	})
	fixture.message(t, wire.MessageTypeSubscription, wire.SubscriptionMessage{MessageID: 1, MessageName: "versioned"})

	complete := binary.LittleEndian.AppendUint64(nil, 1)
	complete = binary.LittleEndian.AppendUint32(complete, math.Float32bits(1.5))
	complete = binary.LittleEndian.AppendUint32(complete, 10)
	fixture.message(t, wire.MessageTypeData, wire.DataMessage{MessageID: 1, Data: complete})

	older := binary.LittleEndian.AppendUint64(nil, 2)
	older = binary.LittleEndian.AppendUint32(older, math.Float32bits(2.5))
	fixture.message(t, wire.MessageTypeData, wire.DataMessage{MessageID: 1, Data: older})

	file, err := dataset.Read(bytes.NewReader(fixture.bytes()))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	instance, err := file.Dataset("versioned", 0)
	if err != nil {
		t.Fatalf("Dataset() error = %v", err)
	}
	column, ok := instance.Column("new_value")
	if !ok {
		t.Fatal("Column(new_value) not found")
	}
	if got, want := column.Values(), []uint32{10, 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("Values() = %#v, want %#v", got, want)
	}
	if _, valid := column.Value(0); !valid {
		t.Error("Value(0) is null, want valid")
	}
	if value, valid := column.Value(1); valid || value != nil {
		t.Errorf("Value(1) = %#v, %t, want nil, false", value, valid)
	}
}

func TestFileDatasetsPreserveFirstSeenOrder(t *testing.T) {
	var source bytes.Buffer
	writer, err := ulog.NewWriter(&source)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	first, err := ulog.Register[pointSample](writer, ulog.WithMultiID(2), ulog.WithFormatName("typed_point"))
	if err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	second, err := ulog.Register[pointSample](writer, ulog.WithMultiID(1), ulog.WithFormatName("typed_point"))
	if err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}
	if err := second.Write(pointSample{}); err != nil {
		t.Fatalf("second.Write() error = %v", err)
	}
	if err := first.Write(pointSample{}); err != nil {
		t.Fatalf("first.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	file, err := dataset.Read(bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	datasets := file.Datasets()
	if got, want := []uint8{datasets[0].MultiID(), datasets[1].MultiID()}, []uint8{1, 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("dataset order = %v, want %v", got, want)
	}
}

func TestReadPreservesMetadataAndEvents(t *testing.T) {
	var source bytes.Buffer
	writer, err := ulog.NewWriter(&source)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.WriteInformation("system_name", "sunfish"); err != nil {
		t.Fatalf("WriteInformation() error = %v", err)
	}
	if err := writer.WriteParameter("gain", float32(1.25)); err != nil {
		t.Fatalf("WriteParameter() error = %v", err)
	}
	stream, err := ulog.Register[pointSample](writer)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := stream.Write(pointSample{}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.WriteLog(ulog.LogLevelInfo, 1234, "ready"); err != nil {
		t.Fatalf("WriteLog() error = %v", err)
	}
	if err := writer.WriteDropout(25 * time.Millisecond); err != nil {
		t.Fatalf("WriteDropout() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	file, err := dataset.Read(bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got, want := file.Information(), []ulog.KeyValue{{Name: "system_name", Type: ulog.TypeChar, ArrayLength: 7, Value: "sunfish"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Information() = %#v, want %#v", got, want)
	}
	if got, want := file.Parameters(), []ulog.KeyValue{{Name: "gain", Type: ulog.TypeFloat32, Value: float32(1.25)}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Parameters() = %#v, want %#v", got, want)
	}
	if got, want := file.Logs(), []ulog.LogEntry{{Level: ulog.LogLevelInfo, Timestamp: 1234, Message: "ready"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Logs() = %#v, want %#v", got, want)
	}
	if got, want := file.Dropouts(), []ulog.Dropout{{Duration: 25 * time.Millisecond}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Dropouts() = %#v, want %#v", got, want)
	}
}

func TestFileReturnsIndependentMetadataValues(t *testing.T) {
	fixture := newFixture(t, 0)
	defaultValue := binary.LittleEndian.AppendUint32(nil, math.Float32bits(1.25))
	fixture.message(t, wire.MessageTypeDefaultParameter, wire.DefaultParameterMessage{
		Types: wire.DefaultParameterSystemWide,
		Key:   "float gain/default",
		Value: defaultValue,
	})

	file, err := dataset.Read(bytes.NewReader(fixture.bytes()))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	want := []ulog.DefaultParameter{{
		Types:    ulog.DefaultParameterSystemWide,
		KeyValue: ulog.KeyValue{Name: "gain/default", Type: ulog.TypeFloat32, Value: float32(1.25)},
	}}
	got := file.DefaultParameters()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultParameters() = %#v, want %#v", got, want)
	}
	got[0].Name = "changed"
	if next := file.DefaultParameters(); !reflect.DeepEqual(next, want) {
		t.Errorf("DefaultParameters() after caller mutation = %#v, want %#v", next, want)
	}
}

func TestFilePreservesIndependentMultiInformationGroups(t *testing.T) {
	fixture := newFixture(t, 0)
	fixture.message(t, wire.MessageTypeMultiInformation, wire.MultiInformationMessage{
		Key: "char[7] vehicle-id", Value: []byte("sunfish"),
	})
	fixture.message(t, wire.MessageTypeMultiInformation, wire.MultiInformationMessage{
		IsContinued: 1, Key: "char[3] vehicle-id", Value: []byte("-01"),
	})

	file, err := dataset.Read(bytes.NewReader(fixture.bytes()))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	want := []ulog.MultiInformationGroup{{
		Name: "vehicle-id",
		Values: []ulog.MultiInformationValue{
			{KeyValue: ulog.KeyValue{Name: "vehicle-id", Type: ulog.TypeChar, ArrayLength: 7, Value: "sunfish"}, IsArray: true},
			{KeyValue: ulog.KeyValue{Name: "vehicle-id", Type: ulog.TypeChar, ArrayLength: 3, Value: "-01"}, IsArray: true},
		},
	}}
	got := file.MultiInformation()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MultiInformation() = %#v, want %#v", got, want)
	}
	got[0].Values[0].Name = "changed"
	if next := file.MultiInformation(); !reflect.DeepEqual(next, want) {
		t.Errorf("MultiInformation() after caller mutation = %#v, want %#v", next, want)
	}
}

type fixture struct {
	data []byte
}

func newFixture(t *testing.T, timestamp uint64) *fixture {
	t.Helper()
	var magic [7]byte
	copy(magic[:], wire.FileMagic)
	header := wire.FileHeader{Magic: magic, Version: wire.FileVersion, Timestamp: timestamp}
	data, err := binary.Append(nil, binary.LittleEndian, header)
	if err != nil {
		t.Fatalf("append file header: %v", err)
	}
	result := &fixture{data: data}
	flagBits, err := binary.Append(nil, binary.LittleEndian, wire.FlagBitsMessage{})
	if err != nil {
		t.Fatalf("append flag bits: %v", err)
	}
	result.rawMessage(t, wire.MessageTypeFlagBits, flagBits)
	return result
}

func (f *fixture) message(t *testing.T, messageType wire.MessageType, message encoding.BinaryAppender) {
	t.Helper()
	payload, err := message.AppendBinary(nil)
	if err != nil {
		t.Fatalf("append %q message: %v", messageType, err)
	}
	f.rawMessage(t, messageType, payload)
}

func (f *fixture) rawMessage(t *testing.T, messageType wire.MessageType, payload []byte) {
	t.Helper()
	if len(payload) > math.MaxUint16 {
		t.Fatalf("payload size %d exceeds ULog limit", len(payload))
	}
	header := wire.MessageHeader{Size: uint16(len(payload)), Type: messageType} // #nosec G115 -- bounded above.
	var err error
	f.data, err = binary.Append(f.data, binary.LittleEndian, header)
	if err != nil {
		t.Fatalf("append %q header: %v", messageType, err)
	}
	f.data = append(f.data, payload...)
}

func (f *fixture) bytes() []byte {
	return bytes.Clone(f.data)
}
