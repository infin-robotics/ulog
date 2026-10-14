package ulog

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/infin-robotics/ulog/pkg/wire"
)

func TestRecordFieldsReturnsFlattenedScalarSchema(t *testing.T) {
	type position struct {
		X float32
		Y float32
	}
	type sample struct {
		Timestamp uint64
		Position  position
		Values    [2]int16
		Padding   [2]uint8 `ulog:"_padding"`
	}

	var source bytes.Buffer
	writer, err := NewWriter(&source)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	stream, err := Register[sample](writer)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := stream.Write(sample{}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reader, err := NewReader(bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if !reader.Next() {
		t.Fatalf("Next() = false, error = %v", reader.Err())
	}
	want := []ScalarField{
		{Name: "timestamp", Type: TypeUint64},
		{Name: "position.x", Type: TypeFloat32},
		{Name: "position.y", Type: TypeFloat32},
		{Name: "values[0]", Type: TypeInt16},
		{Name: "values[1]", Type: TypeInt16},
	}
	if got := reader.Record().Fields(); !reflect.DeepEqual(got, want) {
		t.Errorf("Fields() = %#v, want %#v", got, want)
	}
}

func TestRecordFieldsTreatsCharacterArraysAsStrings(t *testing.T) {
	var source bytes.Buffer
	writer, err := NewWriter(&source)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.Define(Format{
		Name: "labels",
		Fields: []Field{
			{Name: "name", Type: TypeChar, ArrayLength: 8},
			{Name: "payload", Type: TypeUint8, ArrayLength: 2},
			{Name: "code", Type: TypeChar},
		},
	}); err != nil {
		t.Fatalf("Define() error = %v", err)
	}
	stream, err := writer.RegisterFormat(Format{
		Name: "text_sample",
		Fields: []Field{
			{Name: "timestamp", Type: TypeUint64},
			{Name: "labels", Type: "labels"},
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

	reader, err := NewReader(bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if !reader.Next() {
		t.Fatalf("Next() = false, error = %v", reader.Err())
	}
	wantFields := []ScalarField{
		{Name: "timestamp", Type: TypeUint64},
		{Name: "labels.name", Type: TypeChar, ArrayLength: 8},
		{Name: "labels.payload[0]", Type: TypeUint8},
		{Name: "labels.payload[1]", Type: TypeUint8},
		{Name: "labels.code", Type: TypeChar},
	}
	if got := reader.Record().Fields(); !reflect.DeepEqual(got, wantFields) {
		t.Errorf("Fields() = %#v, want %#v", got, wantFields)
	}
	wantValues := []FieldValue{
		{Name: "timestamp", Type: TypeUint64, Value: uint64(42)},
		{Name: "labels.name", Type: TypeChar, ArrayLength: 8, Value: "hi\x00x"},
		{Name: "labels.payload[0]", Type: TypeUint8, Value: uint8(1)},
		{Name: "labels.payload[1]", Type: TypeUint8, Value: uint8(2)},
		{Name: "labels.code", Type: TypeChar, Value: uint8('Z')},
	}
	values, err := reader.Record().Values()
	if err != nil {
		t.Fatalf("Values() error = %v", err)
	}
	if !reflect.DeepEqual(values, wantValues) {
		t.Errorf("Values() = %#v, want %#v", values, wantValues)
	}
}

func TestRecordValuesRejectsTruncatedCharacterArray(t *testing.T) {
	fixture := newULogFixture(t, 0)
	fixture.message(t, wire.MessageTypeFormat, wire.FormatMessage{
		Format: "text_sample:uint64_t timestamp;char[8] name;",
	})
	fixture.message(t, wire.MessageTypeSubscription, wire.SubscriptionMessage{
		MessageID: 1, MessageName: "text_sample",
	})
	payload := binary.LittleEndian.AppendUint64(nil, 42)
	payload = append(payload, 'z', 'o')
	fixture.message(t, wire.MessageTypeData, wire.DataMessage{MessageID: 1, Data: payload})

	reader, err := NewReader(bytes.NewReader(fixture.bytes()))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if !reader.Next() {
		t.Fatalf("Next() = false, error = %v", reader.Err())
	}
	_, err = reader.Record().Values()
	if err == nil {
		t.Fatal("Values() succeeded for a character array truncated inside the field")
	}
	if got, want := err.Error(), `field "name" is truncated: have 2 of 8 bytes`; !strings.Contains(got, want) {
		t.Errorf("Values() error = %q, want it to contain %q", got, want)
	}
}
