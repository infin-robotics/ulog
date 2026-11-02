package columnar

import (
	"bytes"
	"encoding"
	"encoding/binary"
	"io"
	"testing"

	parquet "github.com/parquet-go/parquet-go"

	"github.com/infin-robotics/ulog/pkg/dataset"
	"github.com/infin-robotics/ulog/pkg/wire"
)

func TestWriteParquetPreservesPresentZerosAndMissingValues(t *testing.T) {
	tests := []struct {
		typeName string
		zero     any
		nonzero  any
	}{
		{"int8_t", int8(0), int8(-7)},
		{"uint8_t", uint8(0), uint8(255)},
		{"int16_t", int16(0), int16(-300)},
		{"uint16_t", uint16(0), uint16(65535)},
		{"int32_t", int32(0), int32(-70000)},
		{"uint32_t", uint32(0), uint32(1<<32 - 1)},
		{"int64_t", int64(0), int64(-1 << 40)},
		{"uint64_t", uint64(0), uint64(1<<64 - 1)},
		{"float", float32(0), float32(-1.25)},
		{"double", float64(0), float64(2.5)},
		{"bool", false, true},
		{"char", uint8(0), uint8('Z')},
		{"char[4]", [4]byte{}, [4]byte{'a', 'b'}},
		{"float[2]", [2]float32{}, [2]float32{1.25, -2.5}},
	}
	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			source := nullableParquetDataset(t, tt.typeName, tt.zero, tt.nonzero)
			var destination bytes.Buffer
			if err := WriteParquet(&destination, source); err != nil {
				t.Fatalf("WriteParquet() error = %v", err)
			}
			reader := parquet.NewReader(bytes.NewReader(destination.Bytes()))
			defer func() {
				if err := reader.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			}()
			rows := make([]parquet.Row, source.Len())
			n, err := reader.ReadRows(rows)
			if err != nil && err != io.EOF {
				t.Fatalf("ReadRows() error = %v", err)
			}
			if n != source.Len() {
				t.Fatalf("ReadRows() = %d rows, want %d", n, source.Len())
			}
			paths := reader.Schema().Columns()
			for rowIndex, row := range rows {
				if len(row) != len(paths) {
					t.Fatalf("row %d has %d cells, want %d", rowIndex, len(row), len(paths))
				}
				for _, cell := range row {
					name := paths[cell.Column()][0]
					column, ok := source.Column(name)
					if !ok {
						t.Fatalf("unexpected Parquet column %q", name)
					}
					value, valid := column.Value(rowIndex)
					// The last ULog sample has only a timestamp; all other
					// fields are missing, not present zero values.
					wantValid := name == "timestamp" || rowIndex < 2
					if valid != wantValid {
						t.Fatalf("source %s row %d validity = %t, want %t", name, rowIndex, valid, wantValid)
					}
					want := parquet.NullValue()
					definitionLevel := 0
					if valid {
						want = parquet.ValueOf(value)
						definitionLevel = 1
					}
					if !parquet.Equal(cell, want) || cell.DefinitionLevel() != definitionLevel || cell.RepetitionLevel() != 0 {
						t.Errorf("%s row %d = %v (definition %d, repetition %d), want %v (definition %d, repetition 0)", name, rowIndex, cell, cell.DefinitionLevel(), cell.RepetitionLevel(), want, definitionLevel)
					}
				}
			}
		})
	}
}

func nullableParquetDataset(t *testing.T, typeName string, zero, nonzero any) *dataset.Dataset {
	t.Helper()
	var magic [7]byte
	copy(magic[:], wire.FileMagic)
	data, err := binary.Append(nil, binary.LittleEndian, wire.FileHeader{Magic: magic, Version: wire.FileVersion})
	if err != nil {
		t.Fatal(err)
	}
	appendRaw := func(kind wire.MessageType, payload []byte) {
		t.Helper()
		header := wire.MessageHeader{Size: uint16(len(payload)), Type: kind} // #nosec G115 -- bounded test payloads.
		data, err = binary.Append(data, binary.LittleEndian, header)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, payload...)
	}
	appendMessage := func(kind wire.MessageType, message encoding.BinaryAppender) {
		t.Helper()
		payload, err := message.AppendBinary(nil)
		if err != nil {
			t.Fatal(err)
		}
		appendRaw(kind, payload)
	}
	flags, err := binary.Append(nil, binary.LittleEndian, wire.FlagBitsMessage{})
	if err != nil {
		t.Fatal(err)
	}
	appendRaw(wire.MessageTypeFlagBits, flags)
	// Dataset wire order deliberately differs from Parquet's field order.
	appendMessage(wire.MessageTypeFormat, wire.FormatMessage{Format: "nullable:uint64_t timestamp;" + typeName + " reading;"})
	appendMessage(wire.MessageTypeSubscription, wire.SubscriptionMessage{MessageID: 1, MessageName: "nullable"})
	for rowIndex, value := range []any{zero, nonzero, nil} {
		payload := binary.LittleEndian.AppendUint64(nil, uint64(rowIndex))
		if value != nil {
			payload, err = binary.Append(payload, binary.LittleEndian, value)
			if err != nil {
				t.Fatal(err)
			}
		}
		appendMessage(wire.MessageTypeData, wire.DataMessage{MessageID: 1, Data: payload})
	}
	file, err := dataset.Read(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	source, err := file.Dataset("nullable", 0)
	if err != nil {
		t.Fatal(err)
	}
	return source
}
