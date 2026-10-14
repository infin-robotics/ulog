package ulog

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/infin-robotics/ulog/pkg/wire"
)

func TestWriterRejectsInitialMetadataAfterDataStarts(t *testing.T) {
	var source bytes.Buffer
	writer, err := NewWriter(&source)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	stream, err := Register[typedPointSample](writer)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := stream.Write(typedPointSample{}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.WriteInformation("late", uint32(1)); err == nil {
		t.Fatal("WriteInformation() succeeded after data started")
	}
	if err := writer.WriteParameter("late", int32(1)); err == nil {
		t.Fatal("WriteParameter() succeeded after data started")
	}
}

func TestReaderPreservesMultiInformationGroupsAndDefaultParameters(t *testing.T) {
	data := newULogFixture(t, 0)
	data.message(t, wire.MessageTypeMultiInformation, wire.MultiInformationMessage{
		Key: "char[7] vehicle-id", Value: []byte("sunfish"),
	})
	data.message(t, wire.MessageTypeMultiInformation, wire.MultiInformationMessage{
		IsContinued: 1, Key: "char[3] vehicle-id", Value: []byte("-01"),
	})
	data.message(t, wire.MessageTypeMultiInformation, wire.MultiInformationMessage{
		IsContinued: 1, Key: "char[0] vehicle-id",
	})
	data.message(t, wire.MessageTypeMultiInformation, wire.MultiInformationMessage{
		Key: "char[4] vehicle-id", Value: []byte("zoda"),
	})
	data.message(t, wire.MessageTypeMultiInformation, wire.MultiInformationMessage{
		Key: "uint8_t[3] metadata_events", Value: []byte{1, 2, 3},
	})
	data.message(t, wire.MessageTypeMultiInformation, wire.MultiInformationMessage{
		Key: "uint32_t reboot_count", Value: binary.LittleEndian.AppendUint32(nil, 2),
	})
	defaultValue := binary.LittleEndian.AppendUint32(nil, math.Float32bits(1.25))
	data.message(t, wire.MessageTypeDefaultParameter, wire.DefaultParameterMessage{
		Types: wire.DefaultParameterSystemWide | wire.DefaultParameterCurrentConfiguration,
		Key:   "float gain/default",
		Value: defaultValue,
	})

	reader, err := NewReader(bytes.NewReader(data.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if reader.Next() {
		t.Fatal("Next() = true, want false")
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if got := reader.Information(); len(got) != 0 {
		t.Errorf("Information() = %#v, want no entries", got)
	}
	wantMultiInformation := []MultiInformationGroup{
		{
			Name: "vehicle-id",
			Values: []MultiInformationValue{
				{KeyValue: KeyValue{Name: "vehicle-id", Type: TypeChar, ArrayLength: 7, Value: "sunfish"}, IsArray: true},
				{KeyValue: KeyValue{Name: "vehicle-id", Type: TypeChar, ArrayLength: 3, Value: "-01"}, IsArray: true},
				{KeyValue: KeyValue{Name: "vehicle-id", Type: TypeChar, Value: ""}, IsArray: true},
			},
		},
		{
			Name:   "vehicle-id",
			Values: []MultiInformationValue{{KeyValue: KeyValue{Name: "vehicle-id", Type: TypeChar, ArrayLength: 4, Value: "zoda"}, IsArray: true}},
		},
		{
			Name:   "metadata_events",
			Values: []MultiInformationValue{{KeyValue: KeyValue{Name: "metadata_events", Type: TypeUint8, ArrayLength: 3, Value: []uint8{1, 2, 3}}, IsArray: true}},
		},
		{
			Name:   "reboot_count",
			Values: []MultiInformationValue{{KeyValue: KeyValue{Name: "reboot_count", Type: TypeUint32, Value: uint32(2)}}},
		},
	}
	gotMultiInformation := reader.MultiInformation()
	if !reflect.DeepEqual(gotMultiInformation, wantMultiInformation) {
		t.Fatalf("MultiInformation() = %#v, want %#v", gotMultiInformation, wantMultiInformation)
	}
	gotMultiInformation[2].Values[0].Value.([]uint8)[0] = 99
	if next := reader.MultiInformation(); !reflect.DeepEqual(next, wantMultiInformation) {
		t.Errorf("MultiInformation() after caller mutation = %#v, want %#v", next, wantMultiInformation)
	}
	wantDefaults := []DefaultParameter{{
		Types:    DefaultParameterSystemWide | DefaultParameterCurrentConfiguration,
		KeyValue: KeyValue{Name: "gain/default", Type: TypeFloat32, Value: float32(1.25)},
	}}
	if got := reader.DefaultParameters(); !reflect.DeepEqual(got, wantDefaults) {
		t.Errorf("DefaultParameters() = %#v, want %#v", got, wantDefaults)
	}
}

func TestReaderRejectsMalformedMultiInformationAndDefaultParameters(t *testing.T) {
	tests := []struct {
		name        string
		messageType wire.MessageType
		payload     []byte
	}{
		{name: "multi-information", messageType: wire.MessageTypeMultiInformation, payload: []byte{0}},
		{name: "default parameter", messageType: wire.MessageTypeDefaultParameter, payload: []byte{0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := newULogFixture(t, 0)
			data.rawMessage(t, test.messageType, test.payload)
			reader, err := NewReader(bytes.NewReader(data.bytes()))
			if err != nil {
				t.Fatalf("NewReader() error = %v", err)
			}
			if reader.Next() {
				t.Fatal("Next() = true, want false")
			}
			if err := reader.Err(); err == nil {
				t.Fatal("Err() = nil, want malformed payload error")
			}
		})
	}
}

func TestReaderRejectsMultiInformationContinuationWithoutGroup(t *testing.T) {
	data := newULogFixture(t, 0)
	data.message(t, wire.MessageTypeMultiInformation, wire.MultiInformationMessage{
		IsContinued: 1, Key: "char[4] vehicle-id", Value: []byte("zoda"),
	})

	reader, err := NewReader(bytes.NewReader(data.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if reader.Next() {
		t.Fatal("Next() = true, want false")
	}
	if err := reader.Err(); err == nil {
		t.Fatal("Err() = nil, want unmatched continuation error")
	}
}

func TestWriterAcceptsULogMetadataNames(t *testing.T) {
	var destination bytes.Buffer
	writer, err := NewWriter(&destination)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.WriteInformation("vehicle-id", "sunfish"); err != nil {
		t.Fatalf("WriteInformation() error = %v", err)
	}
	if err := writer.WriteParameter("gain/default", float32(1.25)); err != nil {
		t.Fatalf("WriteParameter() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestWriterValidatesMetadataMessages(t *testing.T) {
	var destination bytes.Buffer
	writer, err := NewWriter(&destination)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	tests := []struct {
		name string
		call func() error
	}{
		{name: "nil parameter", call: func() error { return writer.WriteParameter("gain", nil) }},
		{name: "unsupported parameter type", call: func() error { return writer.WriteParameter("gain", uint32(1)) }},
		{name: "invalid log level", call: func() error { return writer.WriteLog(LogLevel('8'), 0, "bad") }},
		{name: "fractional millisecond dropout", call: func() error { return writer.WriteDropout(1500 * time.Microsecond) }},
		{name: "oversized dropout", call: func() error { return writer.WriteDropout(65536 * time.Millisecond) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); err == nil {
				t.Fatal("call succeeded, want validation error")
			}
		})
	}
}
