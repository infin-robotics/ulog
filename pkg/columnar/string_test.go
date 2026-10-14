package columnar

import (
	"bytes"
	"context"
	"encoding"
	"encoding/binary"
	"reflect"
	"testing"

	arrowlib "github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	parquetfile "github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"

	"github.com/infin-robotics/ulog"
	"github.com/infin-robotics/ulog/pkg/dataset"
	"github.com/infin-robotics/ulog/pkg/wire"
)

func TestToArrowMapsCharacterArraysToStrings(t *testing.T) {
	dataset := characterArrayDataset(t, "vehicle-zōda")
	record, err := ToArrow(dataset, memory.DefaultAllocator)
	if err != nil {
		t.Fatalf("ToArrow() error = %v", err)
	}
	defer record.Release()

	if got, want := fieldNames(record.Schema()), []string{"timestamp", "labels.name", "labels.payload[0]", "labels.payload[1]", "labels.code"}; !reflect.DeepEqual(got, want) {
		t.Errorf("FieldNames() = %v, want %v", got, want)
	}
	name, ok := record.Column(1).(*array.String)
	if !ok {
		t.Fatalf("name column type = %T, want *array.String", record.Column(1))
	}
	if got, want := name.Value(0), "vehicle-zōda"; got != want {
		t.Errorf("name value = %q, want %q", got, want)
	}
	for _, index := range []int{2, 3, 4} {
		if _, ok := record.Column(index).(*array.Uint8); !ok {
			t.Errorf("column %q type = %T, want *array.Uint8", record.Schema().Field(index).Name, record.Column(index))
		}
	}
}

func TestToArrowRejectsInvalidUTF8CharacterArrays(t *testing.T) {
	dataset := characterArrayDataset(t, string([]byte{'a', 0xff}))
	allocator := memory.NewCheckedAllocator(memory.DefaultAllocator)
	_, err := ToArrow(dataset, allocator)
	if err == nil {
		t.Fatal("ToArrow() succeeded for invalid UTF-8")
	}
	if got, want := err.Error(), `column "labels.name" row 0 is not valid UTF-8`; got != want {
		t.Errorf("ToArrow() error = %q, want %q", got, want)
	}
	allocator.AssertSize(t, 0)
}

func TestWriteParquetRejectsInvalidUTF8CharacterArrays(t *testing.T) {
	dataset := characterArrayDataset(t, string([]byte{'a', 0xff}))
	var destination bytes.Buffer
	err := WriteParquet(&destination, dataset)
	if err == nil {
		t.Fatal("WriteParquet() succeeded for invalid UTF-8")
	}
	if got, want := err.Error(), `column "labels.name" row 0 is not valid UTF-8`; got != want {
		t.Errorf("WriteParquet() error = %q, want %q", got, want)
	}
	if destination.Len() != 0 {
		t.Errorf("WriteParquet() wrote %d bytes before rejecting invalid UTF-8", destination.Len())
	}
}

func TestNestedArrayCharacterFieldsReachArrowAndParquet(t *testing.T) {
	dataset := nestedArrayCharacterDataset(t)
	record, err := ToArrow(dataset, memory.DefaultAllocator)
	if err != nil {
		t.Fatalf("ToArrow() error = %v", err)
	}
	if got, want := fieldNames(record.Schema()), []string{
		"timestamp", "labels[0].name", "labels[1].name",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("FieldNames() = %v, want %v", got, want)
	}
	if got, want := record.Column(1).(*array.String).Value(0), "zōda"; got != want {
		t.Errorf("labels[0].name = %q, want %q", got, want)
	}
	if got, want := record.Column(2).(*array.String).Value(0), "hi\x00x"; got != want {
		t.Errorf("labels[1].name = %q, want %q", got, want)
	}
	record.Release()

	var destination bytes.Buffer
	if err := WriteParquet(&destination, dataset); err != nil {
		t.Fatalf("WriteParquet() error = %v", err)
	}
	reader, err := parquetfile.NewParquetReader(bytes.NewReader(destination.Bytes()))
	if err != nil {
		t.Fatalf("NewParquetReader() error = %v", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Errorf("reader.Close() error = %v", err)
		}
	}()
	arrowReader, err := pqarrow.NewFileReader(reader, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		t.Fatalf("NewFileReader() error = %v", err)
	}
	table, err := arrowReader.ReadTable(context.Background())
	if err != nil {
		t.Fatalf("ReadTable() error = %v", err)
	}
	defer table.Release()
	if got, want := parquetColumn(t, table, "labels[0].name").(*array.String).Value(0), "zōda"; got != want {
		t.Errorf("Parquet labels[0].name = %q, want %q", got, want)
	}
	if got, want := parquetColumn(t, table, "labels[1].name").(*array.String).Value(0), "hi\x00x"; got != want {
		t.Errorf("Parquet labels[1].name = %q, want %q", got, want)
	}
}

func TestToArrowPreservesNullCharacterArrays(t *testing.T) {
	dataset := versionedCharacterArrayDataset(t)
	record, err := ToArrow(dataset, memory.DefaultAllocator)
	if err != nil {
		t.Fatalf("ToArrow() error = %v", err)
	}
	defer record.Release()

	name := record.Column(1).(*array.String)
	if got, want := name.NullN(), 1; got != want {
		t.Errorf("name.NullN() = %d, want %d", got, want)
	}
	if !name.IsValid(0) || !name.IsNull(1) {
		t.Errorf("name validity = [%t, %t], want [valid, null]", name.IsValid(0), name.IsValid(1))
	}
}

func TestWriteParquetPreservesCharacterArrayStrings(t *testing.T) {
	dataset := versionedCharacterArrayDataset(t)
	var destination bytes.Buffer
	if err := WriteParquet(&destination, dataset); err != nil {
		t.Fatalf("WriteParquet() error = %v", err)
	}

	reader, err := parquetfile.NewParquetReader(bytes.NewReader(destination.Bytes()))
	if err != nil {
		t.Fatalf("NewParquetReader() error = %v", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Errorf("reader.Close() error = %v", err)
		}
	}()
	arrowReader, err := pqarrow.NewFileReader(reader, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		t.Fatalf("NewFileReader() error = %v", err)
	}
	table, err := arrowReader.ReadTable(context.Background())
	if err != nil {
		t.Fatalf("ReadTable() error = %v", err)
	}
	defer table.Release()
	name, ok := parquetColumn(t, table, "name").(*array.String)
	if !ok {
		t.Fatalf("name column type = %T, want *array.String", parquetColumn(t, table, "name"))
	}
	if got, want := name.Value(0), "vehicle-zoda"; got != want {
		t.Errorf("name value = %q, want %q", got, want)
	}
	if !name.IsNull(1) {
		t.Error("name row 1 is valid, want null")
	}
}

func parquetColumn(t *testing.T, table arrowlib.Table, name string) arrowlib.Array {
	t.Helper()
	indices := table.Schema().FieldIndices(name)
	if len(indices) != 1 {
		t.Fatalf("Parquet field %q indices = %v, want one", name, indices)
	}
	column := table.Column(indices[0]).Data()
	chunks := column.Chunks()
	if len(chunks) != 1 {
		t.Fatalf("Parquet field %q chunks = %d, want one", name, len(chunks))
	}
	return chunks[0]
}

func characterArrayDataset(t *testing.T, name string) *dataset.Dataset {
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
			{Name: "code", Type: ulog.TypeChar},
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
	return instance
}

func versionedCharacterArrayDataset(t *testing.T) *dataset.Dataset {
	t.Helper()
	var magic [7]byte
	copy(magic[:], wire.FileMagic)
	data, err := binary.Append(nil, binary.LittleEndian, wire.FileHeader{
		Magic: magic, Version: wire.FileVersion,
	})
	if err != nil {
		t.Fatalf("append file header: %v", err)
	}
	appendRawMessage := func(messageType wire.MessageType, payload []byte) {
		t.Helper()
		header := wire.MessageHeader{Size: uint16(len(payload)), Type: messageType} // #nosec G115 -- bounded test payloads.
		var appendErr error
		data, appendErr = binary.Append(data, binary.LittleEndian, header)
		if appendErr != nil {
			t.Fatalf("append %q header: %v", messageType, appendErr)
		}
		data = append(data, payload...)
	}
	appendMessage := func(messageType wire.MessageType, message encoding.BinaryAppender) {
		t.Helper()
		payload, appendErr := message.AppendBinary(nil)
		if appendErr != nil {
			t.Fatalf("append %q payload: %v", messageType, appendErr)
		}
		appendRawMessage(messageType, payload)
	}
	flagBits, err := binary.Append(nil, binary.LittleEndian, wire.FlagBitsMessage{})
	if err != nil {
		t.Fatalf("append flag bits: %v", err)
	}
	appendRawMessage(wire.MessageTypeFlagBits, flagBits)
	appendMessage(wire.MessageTypeFormat, wire.FormatMessage{
		Format: "text_versioned:uint64_t timestamp;char[24] name;",
	})
	appendMessage(wire.MessageTypeSubscription, wire.SubscriptionMessage{
		MessageID: 1, MessageName: "text_versioned",
	})
	complete := binary.LittleEndian.AppendUint64(nil, 1)
	complete = append(complete, []byte("vehicle-zoda")...)
	complete = append(complete, make([]byte, 24-len("vehicle-zoda"))...)
	appendMessage(wire.MessageTypeData, wire.DataMessage{MessageID: 1, Data: complete})
	appendMessage(wire.MessageTypeData, wire.DataMessage{
		MessageID: 1, Data: binary.LittleEndian.AppendUint64(nil, 2),
	})

	file, err := dataset.Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	instance, err := file.Dataset("text_versioned", 0)
	if err != nil {
		t.Fatalf("Dataset() error = %v", err)
	}
	return instance
}

func nestedArrayCharacterDataset(t *testing.T) *dataset.Dataset {
	t.Helper()
	var source bytes.Buffer
	writer, err := ulog.NewWriter(&source)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.Define(ulog.Format{
		Name: "label",
		Fields: []ulog.Field{
			{Name: "name", Type: ulog.TypeChar, ArrayLength: 8},
		},
	}); err != nil {
		t.Fatalf("Define() error = %v", err)
	}
	stream, err := writer.RegisterFormat(ulog.Format{
		Name: "nested_text_sample",
		Fields: []ulog.Field{
			{Name: "timestamp", Type: ulog.TypeUint64},
			{Name: "labels", Type: "label", ArrayLength: 2},
		},
	})
	if err != nil {
		t.Fatalf("RegisterFormat() error = %v", err)
	}
	payload := binary.LittleEndian.AppendUint64(nil, 42)
	payload = append(payload, []byte("zōda")...)
	payload = append(payload, make([]byte, 8-len("zōda"))...)
	payload = append(payload, 'h', 'i', 0, 'x', 0, 0, 0, 0)
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
	instance, err := file.Dataset("nested_text_sample", 0)
	if err != nil {
		t.Fatalf("Dataset() error = %v", err)
	}
	return instance
}
