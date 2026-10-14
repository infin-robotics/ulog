package dataset_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/infin-robotics/ulog"
	"github.com/infin-robotics/ulog/pkg/dataset"
	"github.com/infin-robotics/ulog/pkg/wire"
)

type csvSample struct {
	Timestamp uint64
	Signed    int8
	Unsigned  uint32
	Single    float32
	Double    float64
	Armed     bool
}

func TestDatasetWriteCSVWritesColumnsInWireOrder(t *testing.T) {
	instance := readCSVDataset(t)

	var destination bytes.Buffer
	if err := instance.WriteCSV(&destination); err != nil {
		t.Fatalf("WriteCSV() error = %v", err)
	}

	want := "timestamp,signed,unsigned,single,double,armed\n" +
		"42,-7,9,1.2345678,-3.5000000000000004,true\n"
	if got := destination.String(); got != want {
		t.Errorf("WriteCSV() = %q, want %q", got, want)
	}
}

func TestDatasetWriteCSVWritesCharacterArraysAsStrings(t *testing.T) {
	instance := readTextDataset(t, "vehicle,\"zoda\"\n")

	var destination bytes.Buffer
	if err := instance.WriteCSV(&destination); err != nil {
		t.Fatalf("WriteCSV() error = %v", err)
	}

	want := "timestamp,labels.name,labels.payload[0],labels.payload[1]\n" +
		"42,\"vehicle,\"\"zoda\"\"\n\",1,2\n"
	if got := destination.String(); got != want {
		t.Errorf("WriteCSV() = %q, want %q", got, want)
	}
}

func TestDatasetWriteCSVWritesNullsAsEmptyFields(t *testing.T) {
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

	var destination bytes.Buffer
	if err := instance.WriteCSV(&destination); err != nil {
		t.Fatalf("WriteCSV() error = %v", err)
	}

	want := "timestamp,old_value,new_value\n1,1.5,10\n2,2.5,\n"
	if got := destination.String(); got != want {
		t.Errorf("WriteCSV() = %q, want %q", got, want)
	}
}

func TestDatasetWriteCSVReturnsDestinationErrors(t *testing.T) {
	instance := readCSVDataset(t)
	want := errors.New("CSV destination failed")

	err := instance.WriteCSV(errorWriter{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("WriteCSV() error = %v, want %v", err, want)
	}
}

func readCSVDataset(t *testing.T) *dataset.Dataset {
	t.Helper()
	var source bytes.Buffer
	writer, err := ulog.NewWriter(&source)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	stream, err := ulog.Register[csvSample](writer, ulog.WithFormatName("csv_sample"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := stream.Write(csvSample{
		Timestamp: 42,
		Signed:    -7,
		Unsigned:  9,
		Single:    1.2345678,
		Double:    -3.5000000000000004,
		Armed:     true,
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	file, err := dataset.Read(bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	instance, err := file.Dataset("csv_sample", 0)
	if err != nil {
		t.Fatalf("Dataset() error = %v", err)
	}
	return instance
}

func readTextDataset(t *testing.T, name string) *dataset.Dataset {
	t.Helper()
	const nameWidth = 24
	if len(name) > nameWidth {
		t.Fatalf("test name has %d bytes, maximum is %d", len(name), nameWidth)
	}
	var source bytes.Buffer
	writer, err := ulog.NewWriter(&source)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.Define(ulog.Format{
		Name: "labels",
		Fields: []ulog.Field{
			{Name: "name", Type: ulog.TypeChar, ArrayLength: nameWidth},
			{Name: "payload", Type: ulog.TypeUint8, ArrayLength: 2},
		},
	}); err != nil {
		t.Fatalf("Define() error = %v", err)
	}
	stream, err := writer.RegisterFormat(ulog.Format{
		Name: "text_sample",
		Fields: []ulog.Field{
			{Name: "timestamp", Type: ulog.TypeUint64},
			{Name: "labels", Type: "labels"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterFormat() error = %v", err)
	}
	payload := binary.LittleEndian.AppendUint64(nil, 42)
	payload = append(payload, name...)
	payload = append(payload, make([]byte, nameWidth-len(name))...)
	payload = append(payload, 1, 2)
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
	return instance
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}
