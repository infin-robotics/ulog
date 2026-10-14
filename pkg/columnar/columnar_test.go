package columnar

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"testing"

	arrowlib "github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/compress"
	parquetfile "github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"

	"github.com/infin-robotics/ulog"
	"github.com/infin-robotics/ulog/pkg/dataset"
)

type sample struct {
	Timestamp uint64
	X         int16
	Values    [2]float32
	Valid     bool
}

func TestToArrowPreservesTypesValuesAndMetadata(t *testing.T) {
	dataset := sampleDataset(t)
	allocator := memory.NewCheckedAllocator(memory.DefaultAllocator)
	record, err := ToArrow(dataset, allocator)
	if err != nil {
		t.Fatalf("ToArrow() error = %v", err)
	}
	defer func() {
		record.Release()
		allocator.AssertSize(t, 0)
	}()

	if got, want := record.NumRows(), int64(2); got != want {
		t.Errorf("NumRows() = %d, want %d", got, want)
	}
	if got, want := fieldNames(record.Schema()), []string{"timestamp", "x", "values[0]", "values[1]", "valid"}; !reflect.DeepEqual(got, want) {
		t.Errorf("FieldNames() = %v, want %v", got, want)
	}
	metadata := record.Schema().Metadata()
	if got, ok := metadata.GetValue("ulog.format"); !ok || got != "sample" {
		t.Errorf("ulog.format metadata = %q, %t, want sample, true", got, ok)
	}
	if got, ok := metadata.GetValue("ulog.multi_id"); !ok || got != "2" {
		t.Errorf("ulog.multi_id metadata = %q, %t, want 2, true", got, ok)
	}

	x, ok := record.Column(1).(*array.Int16)
	if !ok {
		t.Fatalf("x column type = %T, want *array.Int16", record.Column(1))
	}
	if got, want := x.Int16Values(), []int16{-3, 7}; !reflect.DeepEqual(got, want) {
		t.Errorf("x values = %v, want %v", got, want)
	}
	if got := record.Schema().Field(1).Type; got != arrowlib.PrimitiveTypes.Int16 {
		t.Errorf("x Arrow type = %v, want %v", got, arrowlib.PrimitiveTypes.Int16)
	}
}

func TestWriteParquetProducesReadableTable(t *testing.T) {
	dataset := sampleDataset(t)
	var destination bytes.Buffer
	if err := WriteParquet(&destination, dataset); err != nil {
		t.Fatalf("WriteParquet() error = %v", err)
	}
	data := destination.Bytes()
	if len(data) < 8 || string(data[:4]) != "PAR1" || string(data[len(data)-4:]) != "PAR1" {
		t.Fatal("WriteParquet() did not produce a Parquet file")
	}

	reader, err := parquetfile.NewParquetReader(bytes.NewReader(data))
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
	if got, want := table.NumRows(), int64(2); got != want {
		t.Errorf("Parquet rows = %d, want %d", got, want)
	}
	gotFields := fieldNames(table.Schema())
	slices.Sort(gotFields)
	wantFields := []string{"timestamp", "valid", "values[0]", "values[1]", "x"}
	if !reflect.DeepEqual(gotFields, wantFields) {
		t.Errorf("Parquet fields = %v, want %v", gotFields, wantFields)
	}
}

func TestWriteParquetAppliesCompression(t *testing.T) {
	tests := []struct {
		compression Compression
		want        compress.Compression
	}{
		{CompressionUncompressed, compress.Codecs.Uncompressed},
		{CompressionSnappy, compress.Codecs.Snappy},
		{CompressionGzip, compress.Codecs.Gzip},
		{CompressionBrotli, compress.Codecs.Brotli},
		{CompressionZstd, compress.Codecs.Zstd},
		{CompressionLZ4Raw, compress.Codecs.Lz4Raw},
	}
	for _, tt := range tests {
		t.Run(string(tt.compression), func(t *testing.T) {
			dataset := sampleDataset(t)
			var destination bytes.Buffer
			if err := WriteParquet(
				&destination,
				dataset,
				WithCompression(tt.compression),
			); err != nil {
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
			metadata := reader.MetaData()
			for rowGroupIndex := range metadata.RowGroups {
				rowGroup := metadata.RowGroup(rowGroupIndex)
				for columnIndex := range rowGroup.NumColumns() {
					column, err := rowGroup.ColumnChunk(columnIndex)
					if err != nil {
						t.Fatalf("ColumnChunk(%d) error = %v", columnIndex, err)
					}
					if got := column.Compression(); got != tt.want {
						t.Errorf("column %d compression = %v, want %v", columnIndex, got, tt.want)
					}
				}
			}
		})
	}
}

func TestWriteParquetRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name   string
		option ParquetOption
	}{
		{"nil", nil},
		{"unsupported compression", WithCompression(Compression("rot13"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var destination bytes.Buffer
			err := WriteParquet(&destination, sampleDataset(t), tt.option)
			if err == nil {
				t.Fatal("WriteParquet() succeeded with invalid options")
			}
			if destination.Len() != 0 {
				t.Fatalf("WriteParquet() wrote %d bytes before rejecting invalid options", destination.Len())
			}
		})
	}
}

func sampleDataset(t *testing.T) *dataset.Dataset {
	t.Helper()
	var source bytes.Buffer
	writer, err := ulog.NewWriter(&source)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	stream, err := ulog.Register[sample](writer, ulog.WithMultiID(2))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	for _, value := range []sample{
		{Timestamp: 10, X: -3, Values: [2]float32{1.25, -2.5}, Valid: true},
		{Timestamp: 20, X: 7, Values: [2]float32{3.5, 4.75}},
	} {
		if err := stream.Write(value); err != nil {
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
	dataset, err := file.Dataset("sample", 2)
	if err != nil {
		t.Fatalf("Dataset() error = %v", err)
	}
	return dataset
}

func fieldNames(schema *arrowlib.Schema) []string {
	fields := schema.Fields()
	names := make([]string, len(fields))
	for i, field := range fields {
		names[i] = field.Name
	}
	return names
}
