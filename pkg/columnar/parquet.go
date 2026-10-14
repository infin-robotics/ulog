package columnar

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"

	parquet "github.com/parquet-go/parquet-go"
	parquetcompress "github.com/parquet-go/parquet-go/compress"

	"github.com/infin-robotics/ulog"
	"github.com/infin-robotics/ulog/pkg/dataset"
)

// Compression identifies a Parquet compression codec. Compression applies to
// every column; per-column policies are deliberately outside this package's
// current export boundary.
type Compression string

const (
	// CompressionUncompressed disables Parquet compression.
	CompressionUncompressed Compression = "uncompressed"
	// CompressionSnappy selects Snappy compression.
	CompressionSnappy Compression = "snappy"
	// CompressionGzip selects Gzip compression.
	CompressionGzip Compression = "gzip"
	// CompressionBrotli selects Brotli compression.
	CompressionBrotli Compression = "brotli"
	// CompressionZstd selects Zstandard compression.
	CompressionZstd Compression = "zstd"
	// CompressionLZ4Raw selects raw LZ4 compression.
	CompressionLZ4Raw Compression = "lz4_raw"
)

// ParquetOption configures [WriteParquet].
type ParquetOption func(*parquetConfig) error

type parquetConfig struct {
	compression parquetcompress.Codec
}

// WithCompression configures one compression codec for every Parquet column.
func WithCompression(compression Compression) ParquetOption {
	return func(config *parquetConfig) error {
		codec, err := parquetCompression(compression)
		if err != nil {
			return err
		}
		config.compression = codec
		return nil
	}
}

func parquetCompression(compression Compression) (parquetcompress.Codec, error) {
	switch compression {
	case CompressionUncompressed:
		return &parquet.Uncompressed, nil
	case CompressionSnappy:
		return &parquet.Snappy, nil
	case CompressionGzip:
		return &parquet.Gzip, nil
	case CompressionBrotli:
		return &parquet.Brotli, nil
	case CompressionZstd:
		return &parquet.Zstd, nil
	case CompressionLZ4Raw:
		return &parquet.Lz4Raw, nil
	default:
		return nil, fmt.Errorf("unsupported Parquet compression %q", compression)
	}
}

func parquetConfigFrom(options []ParquetOption) (parquetConfig, error) {
	config := parquetConfig{compression: &parquet.Uncompressed}
	for _, option := range options {
		if option == nil {
			return parquetConfig{}, errors.New("nil Parquet option")
		}
		if err := option(&config); err != nil {
			return parquetConfig{}, err
		}
	}
	return config, nil
}

// WriteParquet writes source as one Parquet table. Flattened ULog field paths
// become nullable columns, character arrays become UTF-8 strings, and the ULog
// format name, definition, and multi ID are preserved as file metadata. Options
// configure Parquet encoding behaviour. WriteParquet does not close destination.
func WriteParquet(destination io.Writer, source *dataset.Dataset, options ...ParquetOption) error {
	if destination == nil {
		return errors.New("nil Parquet destination")
	}
	if source == nil {
		return errors.New("nil ULog dataset")
	}
	config, err := parquetConfigFrom(options)
	if err != nil {
		return fmt.Errorf("configure Parquet writer: %w", err)
	}

	columns := source.Columns()
	group := make(parquet.Group, len(columns))
	for _, column := range columns {
		node, err := parquetNode(column.Type(), column.ArrayLength())
		if err != nil {
			return fmt.Errorf("column %q: %w", column.Name(), err)
		}
		group[column.Name()] = parquet.Optional(node)
	}

	rows := make([]map[string]any, source.Len())
	for rowIndex := range rows {
		row := make(map[string]any, len(columns))
		for _, column := range columns {
			value, valid := column.Value(rowIndex)
			if !valid {
				continue
			}
			if text, ok := value.(string); ok && !utf8.ValidString(text) {
				return fmt.Errorf("column %q row %d is not valid UTF-8", column.Name(), rowIndex)
			}
			row[column.Name()] = value
		}
		rows[rowIndex] = row
	}

	writer := parquet.NewGenericWriter[map[string]any](
		destination,
		parquet.NewSchema("schema", group),
		parquet.Compression(config.compression),
	)
	writer.SetKeyValueMetadata("ulog.format", source.Name())
	writer.SetKeyValueMetadata("ulog.format_definition", source.Format().String())
	writer.SetKeyValueMetadata("ulog.multi_id", strconv.FormatUint(uint64(source.MultiID()), 10))
	if _, err := writer.Write(rows); err != nil {
		return errors.Join(fmt.Errorf("write Parquet table: %w", err), writer.Close())
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("write Parquet table: %w", err)
	}
	return nil
}

func parquetNode(typeID ulog.Type, arrayLength int) (parquet.Node, error) {
	switch typeID {
	case ulog.TypeInt8:
		return parquet.Int(8), nil
	case ulog.TypeUint8:
		return parquet.Uint(8), nil
	case ulog.TypeChar:
		if arrayLength > 0 {
			return parquet.String(), nil
		}
		return parquet.Uint(8), nil
	case ulog.TypeInt16:
		return parquet.Int(16), nil
	case ulog.TypeUint16:
		return parquet.Uint(16), nil
	case ulog.TypeInt32:
		return parquet.Int(32), nil
	case ulog.TypeUint32:
		return parquet.Uint(32), nil
	case ulog.TypeInt64:
		return parquet.Int(64), nil
	case ulog.TypeUint64:
		return parquet.Uint(64), nil
	case ulog.TypeFloat32:
		return parquet.Leaf(parquet.FloatType), nil
	case ulog.TypeFloat64:
		return parquet.Leaf(parquet.DoubleType), nil
	case ulog.TypeBool:
		return parquet.Leaf(parquet.BooleanType), nil
	default:
		return nil, fmt.Errorf("unsupported ULog type %q", typeID)
	}
}
