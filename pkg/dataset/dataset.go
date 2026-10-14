package dataset

import (
	"errors"
	"fmt"
	"io"

	"github.com/infin-robotics/ulog"
)

// File owns the datasets and metadata loaded from one complete ULog stream.
type File struct {
	header      ulog.Header
	datasets    []*Dataset
	byKey       map[datasetKey]*Dataset
	information []ulog.KeyValue
	multiInfo   []ulog.MultiInformationGroup
	parameters  []ulog.KeyValue
	defaults    []ulog.DefaultParameter
	logs        []ulog.LogEntry
	dropouts    []ulog.Dropout
}

type datasetKey struct {
	name    string
	multiID uint8
}

// Read consumes source to the end and groups records by format name and multi ID.
// It returns no partial [File] if the ULog stream is invalid. Read does not close
// source.
func Read(source io.Reader) (*File, error) {
	reader, err := ulog.NewReader(source)
	if err != nil {
		return nil, err
	}
	file := &File{
		header: reader.Header(),
		byKey:  make(map[datasetKey]*Dataset),
	}
	for reader.Next() {
		record := reader.Record()
		key := datasetKey{name: record.Name(), multiID: record.MultiID()}
		dataset, ok := file.byKey[key]
		if !ok {
			dataset = newDataset(record)
			file.byKey[key] = dataset
			file.datasets = append(file.datasets, dataset)
		}
		if err := dataset.appendRecord(record); err != nil {
			return nil, fmt.Errorf("append %q multi ID %d record: %w", record.Name(), record.MultiID(), err)
		}
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	file.information = reader.Information()
	file.multiInfo = reader.MultiInformation()
	file.parameters = reader.Parameters()
	file.defaults = reader.DefaultParameters()
	file.logs = reader.Logs()
	file.dropouts = reader.Dropouts()
	return file, nil
}

// Header returns the ULog version and logging start time read by [Read].
func (f *File) Header() ulog.Header {
	if f == nil {
		return ulog.Header{}
	}
	return f.header
}

// Information returns independent copies of the typed metadata entries in file
// order.
func (f *File) Information() []ulog.KeyValue {
	if f == nil {
		return nil
	}
	return cloneKeyValues(f.information)
}

// MultiInformation returns independent copies of grouped multi-information
// values in the order each group started.
func (f *File) MultiInformation() []ulog.MultiInformationGroup {
	if f == nil {
		return nil
	}
	return cloneMultiInformation(f.multiInfo)
}

// Parameters returns independent copies of the initial parameter values and
// later changes in file order.
func (f *File) Parameters() []ulog.KeyValue {
	if f == nil {
		return nil
	}
	return cloneKeyValues(f.parameters)
}

// DefaultParameters returns independent copies of the parameter defaults in file
// order. Missing defaults are not synthesised.
func (f *File) DefaultParameters() []ulog.DefaultParameter {
	if f == nil {
		return nil
	}
	return cloneDefaultParameters(f.defaults)
}

// Logs returns the tagged and untagged text messages in file order.
func (f *File) Logs() []ulog.LogEntry {
	if f == nil {
		return nil
	}
	return append([]ulog.LogEntry(nil), f.logs...)
}

// Dropouts returns the periods of lost logging messages in file order.
func (f *File) Dropouts() []ulog.Dropout {
	if f == nil {
		return nil
	}
	return append([]ulog.Dropout(nil), f.dropouts...)
}

// Datasets returns datasets in order of their first data record.
func (f *File) Datasets() []*Dataset {
	if f == nil {
		return nil
	}
	return append([]*Dataset(nil), f.datasets...)
}

// Dataset returns the dataset for the case-sensitive format name and instance
// identifier, or an error if [Read] saw no matching data record.
func (f *File) Dataset(name string, multiID uint8) (*Dataset, error) {
	if f == nil {
		return nil, errors.New("nil ULog file")
	}
	dataset, ok := f.byKey[datasetKey{name: name, multiID: multiID}]
	if !ok {
		return nil, fmt.Errorf("dataset %q multi ID %d not found", name, multiID)
	}
	return dataset, nil
}

// Dataset contains every record for one format name and multi ID. Numeric arrays
// and nested formats are flattened into stable paths; character arrays remain
// string-valued [Column] values. A record that omits compatible trailing fields
// contributes nulls to the corresponding columns.
type Dataset struct {
	name        string
	multiID     uint8
	format      ulog.Format
	columns     []Column
	columnIndex map[string]int
	length      int
}

func newDataset(record ulog.Record) *Dataset {
	dataset := &Dataset{
		name:        record.Name(),
		multiID:     record.MultiID(),
		format:      record.Format(),
		columnIndex: make(map[string]int),
	}
	for _, field := range record.Fields() {
		dataset.columnIndex[field.Name] = len(dataset.columns)
		dataset.columns = append(dataset.columns, newColumn(field.Name, field.Type, field.ArrayLength))
	}
	return dataset
}

func (d *Dataset) appendRecord(record ulog.Record) error {
	values, err := record.Values()
	if err != nil {
		return err
	}
	valueIndex := 0
	for i := range d.columns {
		column := &d.columns[i]
		if valueIndex < len(values) && values[valueIndex].Name == column.name {
			if err := column.append(values[valueIndex].Value, true); err != nil {
				return err
			}
			valueIndex++
			continue
		}
		if err := column.append(nil, false); err != nil {
			return err
		}
	}
	d.length++
	return nil
}

// Name returns the case-sensitive format name used to group the dataset.
func (d *Dataset) Name() string {
	if d == nil {
		return ""
	}
	return d.name
}

// MultiID returns the format's instance identifier. Zero is the first and
// default instance.
func (d *Dataset) MultiID() uint8 {
	if d == nil {
		return 0
	}
	return d.multiID
}

// Format returns an independent copy of the [ulog.Format] that defined the
// dataset's first record.
func (d *Dataset) Format() ulog.Format {
	if d == nil {
		return ulog.Format{}
	}
	return cloneFormat(d.format)
}

// Len returns the number of data records in the dataset, including records with
// null trailing fields.
func (d *Dataset) Len() int {
	if d == nil {
		return 0
	}
	return d.length
}

// Columns returns the flattened, non-padding columns in wire order.
// Mutating the returned slice does not change the dataset.
func (d *Dataset) Columns() []Column {
	if d == nil {
		return nil
	}
	return append([]Column(nil), d.columns...)
}

// Column returns a flattened scalar column by its case-sensitive path, such as
// "q[0]" or "position.x".
func (d *Dataset) Column(name string) (Column, bool) {
	if d == nil {
		return Column{}, false
	}
	index, ok := d.columnIndex[name]
	if !ok {
		return Column{}, false
	}
	return d.columns[index], true
}

// Column is a nullable dataset column. [Column.Values] returns one of []int8,
// []uint8, []int16, []uint16, []int32, []uint32, []int64, []uint64, []float32,
// []float64, []bool, or []string. Character arrays use []string; scalar
// characters use []uint8.
type Column struct {
	name        string
	typeID      ulog.Type
	arrayLength int
	values      any
	valid       []bool
}

func newColumn(name string, typeID ulog.Type, arrayLength int) Column {
	column := Column{name: name, typeID: typeID, arrayLength: arrayLength}
	switch typeID {
	case ulog.TypeInt8:
		column.values = []int8(nil)
	case ulog.TypeUint8:
		column.values = []uint8(nil)
	case ulog.TypeChar:
		if arrayLength > 0 {
			column.values = []string(nil)
		} else {
			column.values = []uint8(nil)
		}
	case ulog.TypeInt16:
		column.values = []int16(nil)
	case ulog.TypeUint16:
		column.values = []uint16(nil)
	case ulog.TypeInt32:
		column.values = []int32(nil)
	case ulog.TypeUint32:
		column.values = []uint32(nil)
	case ulog.TypeInt64:
		column.values = []int64(nil)
	case ulog.TypeUint64:
		column.values = []uint64(nil)
	case ulog.TypeFloat32:
		column.values = []float32(nil)
	case ulog.TypeFloat64:
		column.values = []float64(nil)
	case ulog.TypeBool:
		column.values = []bool(nil)
	}
	return column
}

func (c *Column) append(value any, valid bool) error {
	c.valid = append(c.valid, valid)
	switch c.typeID {
	case ulog.TypeInt8:
		return appendColumnValue(c, value, valid, func(values []int8, value int8) any { return append(values, value) })
	case ulog.TypeUint8:
		return appendColumnValue(c, value, valid, func(values []uint8, value uint8) any { return append(values, value) })
	case ulog.TypeChar:
		if c.arrayLength > 0 {
			return appendColumnValue(c, value, valid, func(values []string, value string) any { return append(values, value) })
		}
		return appendColumnValue(c, value, valid, func(values []uint8, value uint8) any { return append(values, value) })
	case ulog.TypeInt16:
		return appendColumnValue(c, value, valid, func(values []int16, value int16) any { return append(values, value) })
	case ulog.TypeUint16:
		return appendColumnValue(c, value, valid, func(values []uint16, value uint16) any { return append(values, value) })
	case ulog.TypeInt32:
		return appendColumnValue(c, value, valid, func(values []int32, value int32) any { return append(values, value) })
	case ulog.TypeUint32:
		return appendColumnValue(c, value, valid, func(values []uint32, value uint32) any { return append(values, value) })
	case ulog.TypeInt64:
		return appendColumnValue(c, value, valid, func(values []int64, value int64) any { return append(values, value) })
	case ulog.TypeUint64:
		return appendColumnValue(c, value, valid, func(values []uint64, value uint64) any { return append(values, value) })
	case ulog.TypeFloat32:
		return appendColumnValue(c, value, valid, func(values []float32, value float32) any { return append(values, value) })
	case ulog.TypeFloat64:
		return appendColumnValue(c, value, valid, func(values []float64, value float64) any { return append(values, value) })
	case ulog.TypeBool:
		return appendColumnValue(c, value, valid, func(values []bool, value bool) any { return append(values, value) })
	default:
		return fmt.Errorf("unsupported column type %q", c.typeID)
	}
}

func appendColumnValue[T any](column *Column, value any, valid bool, appendValue func([]T, T) any) error {
	values, ok := column.values.([]T)
	if !ok {
		return fmt.Errorf("column %q has invalid storage %T", column.name, column.values)
	}
	var typed T
	if valid {
		var valueOK bool
		typed, valueOK = value.(T)
		if !valueOK {
			return fmt.Errorf("column %q requires %T values, got %T", column.name, typed, value)
		}
	}
	column.values = appendValue(values, typed)
	return nil
}

// Name returns the flattened field path for the column.
func (c Column) Name() string { return c.name }

// Type returns the ULog primitive type stored by the column.
func (c Column) Type() ulog.Type { return c.typeID }

// ArrayLength returns the fixed byte width of a character array, or zero for a
// scalar value.
func (c Column) ArrayLength() int { return c.arrayLength }

// Len returns the number of rows in the column.
func (c Column) Len() int { return len(c.valid) }

// Values returns an independent copy of the typed values. Null rows contain the
// primitive type's zero value; use [Column.Value] to distinguish them.
func (c Column) Values() any {
	switch values := c.values.(type) {
	case []int8:
		return append([]int8(nil), values...)
	case []uint8:
		return append([]uint8(nil), values...)
	case []int16:
		return append([]int16(nil), values...)
	case []uint16:
		return append([]uint16(nil), values...)
	case []int32:
		return append([]int32(nil), values...)
	case []uint32:
		return append([]uint32(nil), values...)
	case []int64:
		return append([]int64(nil), values...)
	case []uint64:
		return append([]uint64(nil), values...)
	case []float32:
		return append([]float32(nil), values...)
	case []float64:
		return append([]float64(nil), values...)
	case []bool:
		return append([]bool(nil), values...)
	case []string:
		return append([]string(nil), values...)
	default:
		return nil
	}
}

// Value returns one value and whether it is valid. It returns nil, false for a
// null or out-of-range row.
func (c Column) Value(index int) (any, bool) {
	if index < 0 || index >= len(c.valid) || !c.valid[index] {
		return nil, false
	}
	switch values := c.values.(type) {
	case []int8:
		return values[index], true
	case []uint8:
		return values[index], true
	case []int16:
		return values[index], true
	case []uint16:
		return values[index], true
	case []int32:
		return values[index], true
	case []uint32:
		return values[index], true
	case []int64:
		return values[index], true
	case []uint64:
		return values[index], true
	case []float32:
		return values[index], true
	case []float64:
		return values[index], true
	case []bool:
		return values[index], true
	case []string:
		return values[index], true
	default:
		return nil, false
	}
}

func cloneFormat(format ulog.Format) ulog.Format {
	format.Fields = append([]ulog.Field(nil), format.Fields...)
	return format
}

func cloneKeyValues(values []ulog.KeyValue) []ulog.KeyValue {
	cloned := append([]ulog.KeyValue(nil), values...)
	for i := range cloned {
		cloned[i].Value = clonePrimitiveSlice(cloned[i].Value)
	}
	return cloned
}

func cloneMultiInformation(groups []ulog.MultiInformationGroup) []ulog.MultiInformationGroup {
	cloned := make([]ulog.MultiInformationGroup, len(groups))
	for i, group := range groups {
		cloned[i] = group
		cloned[i].Values = append([]ulog.MultiInformationValue(nil), group.Values...)
		for j := range cloned[i].Values {
			cloned[i].Values[j].Value = clonePrimitiveSlice(cloned[i].Values[j].Value)
		}
	}
	return cloned
}

func cloneDefaultParameters(values []ulog.DefaultParameter) []ulog.DefaultParameter {
	cloned := append([]ulog.DefaultParameter(nil), values...)
	for i := range cloned {
		cloned[i].Value = clonePrimitiveSlice(cloned[i].Value)
	}
	return cloned
}

func clonePrimitiveSlice(value any) any {
	switch value := value.(type) {
	case []int8:
		return append([]int8(nil), value...)
	case []uint8:
		return append([]uint8(nil), value...)
	case []int16:
		return append([]int16(nil), value...)
	case []uint16:
		return append([]uint16(nil), value...)
	case []int32:
		return append([]int32(nil), value...)
	case []uint32:
		return append([]uint32(nil), value...)
	case []int64:
		return append([]int64(nil), value...)
	case []uint64:
		return append([]uint64(nil), value...)
	case []float32:
		return append([]float32(nil), value...)
	case []float64:
		return append([]float64(nil), value...)
	case []bool:
		return append([]bool(nil), value...)
	default:
		return value
	}
}
