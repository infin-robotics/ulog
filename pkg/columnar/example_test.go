package columnar_test

import (
	"bytes"
	"fmt"

	"github.com/infin-robotics/ulog"
	"github.com/infin-robotics/ulog/pkg/columnar"
	"github.com/infin-robotics/ulog/pkg/dataset"
)

type sample struct {
	Timestamp uint64
	Pressure  float32
}

func ExampleToArrow() {
	dataset := exampleDataset()
	record, err := columnar.ToArrow(dataset, nil)
	if err != nil {
		panic(err)
	}
	defer record.Release()

	fmt.Printf("%d rows, %d columns\n", record.NumRows(), record.NumCols())

	// Output:
	// 2 rows, 2 columns
}

func ExampleWriteParquet() {
	dataset := exampleDataset()
	var destination bytes.Buffer
	if err := columnar.WriteParquet(
		&destination,
		dataset,
		columnar.WithCompression(columnar.CompressionGzip),
	); err != nil {
		panic(err)
	}

	fmt.Printf("%s ... %s\n", destination.Bytes()[:4], destination.Bytes()[destination.Len()-4:])

	// Output:
	// PAR1 ... PAR1
}

func exampleDataset() *dataset.Dataset {
	var source bytes.Buffer
	writer, err := ulog.NewWriter(&source)
	if err != nil {
		panic(err)
	}
	stream, err := ulog.Register[sample](writer)
	if err != nil {
		panic(err)
	}
	for _, value := range []sample{
		{Timestamp: 1_000, Pressure: 101.25},
		{Timestamp: 2_000, Pressure: 101.5},
	} {
		if err := stream.Write(value); err != nil {
			panic(err)
		}
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	file, err := dataset.Read(bytes.NewReader(source.Bytes()))
	if err != nil {
		panic(err)
	}
	dataset, err := file.Dataset("sample", 0)
	if err != nil {
		panic(err)
	}
	return dataset
}
