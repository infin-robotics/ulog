package tests

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/infin-robotics/ulog"
	"github.com/infin-robotics/ulog/pkg/dataset"
)

func TestReadPyULogFixture(t *testing.T) {
	source, err := os.Open("testdata/pyulog-1.2.4.ulg") // #nosec G304 -- repository fixture path is constant.
	if err != nil {
		t.Fatalf("open pyulog fixture: %v", err)
	}
	defer func() {
		if err := source.Close(); err != nil {
			t.Errorf("close pyulog fixture: %v", err)
		}
	}()

	file, err := dataset.Read(source)
	if err != nil {
		t.Fatalf("dataset.Read() error = %v", err)
	}
	if got, want := file.Header().Timestamp, uint64(424); got != want {
		t.Errorf("header timestamp = %d, want %d", got, want)
	}
	wantInformation := []ulog.KeyValue{
		{Name: "system_name", Type: ulog.TypeChar, ArrayLength: 7, Value: "sunfish"},
		{Name: "build_id", Type: ulog.TypeUint32, Value: uint32(0x10203040)},
	}
	if got := file.Information(); !reflect.DeepEqual(got, wantInformation) {
		t.Errorf("information = %#v, want %#v", got, wantInformation)
	}
	if got, want := file.Parameters(), []ulog.KeyValue{{Name: "gain", Type: ulog.TypeFloat32, Value: float32(1.25)}}; !reflect.DeepEqual(got, want) {
		t.Errorf("parameters = %#v, want %#v", got, want)
	}
	if got, want := file.Logs(), []ulog.LogEntry{{Level: ulog.LogLevelInfo, Timestamp: 2500, Message: "ready"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("logs = %#v, want %#v", got, want)
	}
	if got, want := file.Dropouts(), []ulog.Dropout{{Duration: 25 * time.Millisecond}}; !reflect.DeepEqual(got, want) {
		t.Errorf("dropouts = %#v, want %#v", got, want)
	}

	instance, err := file.Dataset("pyulog_sample", 1)
	if err != nil {
		t.Fatalf("Dataset(pyulog_sample, 1) error = %v", err)
	}
	column, ok := instance.Column("signed_value")
	if !ok {
		t.Fatal("signed_value column missing")
	}
	if got, want := column.Values(), []int32{-7}; !reflect.DeepEqual(got, want) {
		t.Errorf("signed values = %#v, want %#v", got, want)
	}
}
