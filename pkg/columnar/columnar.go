// Package columnar converts [dataset.Dataset] values to Apache Arrow and
// Parquet. Flattened ULog field paths become nullable column names, character
// arrays become UTF-8 strings, and the ULog format name, definition, and multi ID
// are preserved as Arrow schema metadata.
package columnar

import (
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"

	arrowlib "github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/infin-robotics/ulog"
	"github.com/infin-robotics/ulog/pkg/dataset"
)

// ToArrow converts source into one Arrow record batch. The caller owns the
// returned batch and must call its Release method. A nil allocator uses
// [memory.DefaultAllocator].
func ToArrow(source *dataset.Dataset, allocator memory.Allocator) (arrowlib.RecordBatch, error) {
	if source == nil {
		return nil, errors.New("nil ULog dataset")
	}
	if allocator == nil {
		allocator = memory.DefaultAllocator
	}

	columns := source.Columns()
	fields := make([]arrowlib.Field, len(columns))
	for i, column := range columns {
		dataType, err := arrowType(column.Type(), column.ArrayLength())
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", column.Name(), err)
		}
		fields[i] = arrowlib.Field{Name: column.Name(), Type: dataType, Nullable: true}
	}
	metadata := arrowlib.MetadataFrom(map[string]string{
		"ulog.format":            source.Name(),
		"ulog.format_definition": source.Format().String(),
		"ulog.multi_id":          strconv.FormatUint(uint64(source.MultiID()), 10),
	})
	schema := arrowlib.NewSchema(fields, &metadata)
	builder := array.NewRecordBuilder(allocator, schema)
	defer builder.Release()
	for i, column := range columns {
		valid := make([]bool, column.Len())
		for row := range valid {
			_, valid[row] = column.Value(row)
		}
		if err := appendColumn(builder.Field(i), column, valid); err != nil {
			return nil, err
		}
	}
	return builder.NewRecordBatch(), nil
}

func arrowType(typeID ulog.Type, arrayLength int) (arrowlib.DataType, error) {
	switch typeID {
	case ulog.TypeInt8:
		return arrowlib.PrimitiveTypes.Int8, nil
	case ulog.TypeUint8:
		return arrowlib.PrimitiveTypes.Uint8, nil
	case ulog.TypeChar:
		if arrayLength > 0 {
			return arrowlib.BinaryTypes.String, nil
		}
		return arrowlib.PrimitiveTypes.Uint8, nil
	case ulog.TypeInt16:
		return arrowlib.PrimitiveTypes.Int16, nil
	case ulog.TypeUint16:
		return arrowlib.PrimitiveTypes.Uint16, nil
	case ulog.TypeInt32:
		return arrowlib.PrimitiveTypes.Int32, nil
	case ulog.TypeUint32:
		return arrowlib.PrimitiveTypes.Uint32, nil
	case ulog.TypeInt64:
		return arrowlib.PrimitiveTypes.Int64, nil
	case ulog.TypeUint64:
		return arrowlib.PrimitiveTypes.Uint64, nil
	case ulog.TypeFloat32:
		return arrowlib.PrimitiveTypes.Float32, nil
	case ulog.TypeFloat64:
		return arrowlib.PrimitiveTypes.Float64, nil
	case ulog.TypeBool:
		return arrowlib.FixedWidthTypes.Boolean, nil
	default:
		return nil, fmt.Errorf("unsupported ULog type %q", typeID)
	}
}

func appendColumn(builder array.Builder, column dataset.Column, valid []bool) error {
	switch column.Type() {
	case ulog.TypeInt8:
		builder, ok := builder.(*array.Int8Builder)
		if !ok {
			return builderTypeError(column, builder)
		}
		builder.AppendValues(column.Values().([]int8), valid)
	case ulog.TypeUint8:
		builder, ok := builder.(*array.Uint8Builder)
		if !ok {
			return builderTypeError(column, builder)
		}
		builder.AppendValues(column.Values().([]uint8), valid)
	case ulog.TypeChar:
		if column.ArrayLength() == 0 {
			builder, ok := builder.(*array.Uint8Builder)
			if !ok {
				return builderTypeError(column, builder)
			}
			builder.AppendValues(column.Values().([]uint8), valid)
			break
		}
		builder, ok := builder.(*array.StringBuilder)
		if !ok {
			return builderTypeError(column, builder)
		}
		values := column.Values().([]string)
		for row, value := range values {
			if valid[row] && !utf8.ValidString(value) {
				return fmt.Errorf("column %q row %d is not valid UTF-8", column.Name(), row)
			}
		}
		builder.AppendValues(values, valid)
	case ulog.TypeInt16:
		builder, ok := builder.(*array.Int16Builder)
		if !ok {
			return builderTypeError(column, builder)
		}
		builder.AppendValues(column.Values().([]int16), valid)
	case ulog.TypeUint16:
		builder, ok := builder.(*array.Uint16Builder)
		if !ok {
			return builderTypeError(column, builder)
		}
		builder.AppendValues(column.Values().([]uint16), valid)
	case ulog.TypeInt32:
		builder, ok := builder.(*array.Int32Builder)
		if !ok {
			return builderTypeError(column, builder)
		}
		builder.AppendValues(column.Values().([]int32), valid)
	case ulog.TypeUint32:
		builder, ok := builder.(*array.Uint32Builder)
		if !ok {
			return builderTypeError(column, builder)
		}
		builder.AppendValues(column.Values().([]uint32), valid)
	case ulog.TypeInt64:
		builder, ok := builder.(*array.Int64Builder)
		if !ok {
			return builderTypeError(column, builder)
		}
		builder.AppendValues(column.Values().([]int64), valid)
	case ulog.TypeUint64:
		builder, ok := builder.(*array.Uint64Builder)
		if !ok {
			return builderTypeError(column, builder)
		}
		builder.AppendValues(column.Values().([]uint64), valid)
	case ulog.TypeFloat32:
		builder, ok := builder.(*array.Float32Builder)
		if !ok {
			return builderTypeError(column, builder)
		}
		builder.AppendValues(column.Values().([]float32), valid)
	case ulog.TypeFloat64:
		builder, ok := builder.(*array.Float64Builder)
		if !ok {
			return builderTypeError(column, builder)
		}
		builder.AppendValues(column.Values().([]float64), valid)
	case ulog.TypeBool:
		builder, ok := builder.(*array.BooleanBuilder)
		if !ok {
			return builderTypeError(column, builder)
		}
		builder.AppendValues(column.Values().([]bool), valid)
	default:
		return fmt.Errorf("column %q has unsupported ULog type %q", column.Name(), column.Type())
	}
	return nil
}

func builderTypeError(column dataset.Column, builder array.Builder) error {
	return fmt.Errorf("column %q has unexpected Arrow builder %T", column.Name(), builder)
}
