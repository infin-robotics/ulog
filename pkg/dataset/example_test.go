package dataset_test

import (
	"fmt"
	"os"

	"github.com/infin-robotics/ulog/pkg/dataset"
)

func ExampleRead() {
	source, err := os.Open("flight.ulg")
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := source.Close(); err != nil {
			panic(err)
		}
	}()

	file, err := dataset.Read(source)
	if err != nil {
		panic(err)
	}
	attitude, err := file.Dataset("vehicle_attitude", 0)
	if err != nil {
		panic(err)
	}
	timestamps, ok := attitude.Column("timestamp")
	if !ok {
		panic("timestamp column is missing")
	}
	values := timestamps.Values().([]uint64)
	fmt.Printf("%d attitude samples, from %d µs\n", attitude.Len(), values[0])
}
