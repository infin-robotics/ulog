//go:build pyulog

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/infin-robotics/ulog"
	"github.com/infin-robotics/ulog/pkg/dataset"
)

type pyulogSample struct {
	Timestamp   uint64
	SignedValue int32
	Value       float32
	Vector      [3]float32
	Enabled     bool
}

type oracleDocument struct {
	StartTimestamp    uint64             `json:"start_timestamp"`
	Information       map[string]any     `json:"information"`
	InitialParameters map[string]float64 `json:"initial_parameters"`
	Logs              []oracleLog        `json:"logs"`
	Dropouts          []oracleDropout    `json:"dropouts"`
	Datasets          []oracleDataset    `json:"datasets"`
}

type oracleLog struct {
	Level     uint8  `json:"level"`
	Timestamp uint64 `json:"timestamp"`
	Message   string `json:"message"`
}

type oracleDropout struct {
	Duration  uint16 `json:"duration"`
	Timestamp uint64 `json:"timestamp"`
}

type oracleDataset struct {
	Name    string        `json:"name"`
	MultiID uint8         `json:"multi_id"`
	Fields  []oracleField `json:"fields"`
}

type oracleField struct {
	Name   string    `json:"name"`
	DType  string    `json:"dtype"`
	Values []float64 `json:"values"`
}

func TestPyULogReadsGoAndGoReadsPyULog(t *testing.T) {
	uvPath, err := exec.LookPath("uv")
	if err != nil {
		t.Skip("uv is not available on PATH")
	}
	artifacts := interopArtifactDir(t)
	goFile := filepath.Join(artifacts, "go-generated.ulg")
	pythonFile := filepath.Join(artifacts, "pyulog-rewritten.ulg")
	writePyULogFixture(t, goFile)

	output := runPyULogOracle(t, uvPath, "inspect", goFile)
	var got oracleDocument
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("decode pyulog inspection: %v\n%s", err, output)
	}
	want := oracleDocument{
		StartTimestamp:    424,
		Information:       map[string]any{"build_id": float64(0x10203040), "system_name": "sunfish"},
		InitialParameters: map[string]float64{"gain": 1.25},
		Logs:              []oracleLog{{Level: uint8(ulog.LogLevelInfo), Timestamp: 2500, Message: "ready"}},
		Dropouts:          []oracleDropout{{Duration: 25, Timestamp: 2000}},
		Datasets: []oracleDataset{
			{
				Name:    "pyulog_sample",
				MultiID: 0,
				Fields: []oracleField{
					{Name: "enabled", DType: "int8", Values: []float64{1, 0}},
					{Name: "signed_value", DType: "int32", Values: []float64{-123456, 789}},
					{Name: "timestamp", DType: "uint64", Values: []float64{1000, 2000}},
					{Name: "value", DType: "float32", Values: []float64{-12.5, 6.25}},
					{Name: "vector[0]", DType: "float32", Values: []float64{1, -4}},
					{Name: "vector[1]", DType: "float32", Values: []float64{-2, 5}},
					{Name: "vector[2]", DType: "float32", Values: []float64{3.5, -6.5}},
				},
			},
			{
				Name:    "pyulog_sample",
				MultiID: 1,
				Fields: []oracleField{
					{Name: "enabled", DType: "int8", Values: []float64{1}},
					{Name: "signed_value", DType: "int32", Values: []float64{-7}},
					{Name: "timestamp", DType: "uint64", Values: []float64{1500}},
					{Name: "value", DType: "float32", Values: []float64{0.5}},
					{Name: "vector[0]", DType: "float32", Values: []float64{9}},
					{Name: "vector[1]", DType: "float32", Values: []float64{8}},
					{Name: "vector[2]", DType: "float32", Values: []float64{7}},
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pyulog inspection = %#v, want %#v", got, want)
	}

	runPyULogOracle(t, uvPath, "rewrite", goFile, pythonFile)
	rewritten, err := os.Open(pythonFile) // #nosec G304 -- test path is created under the controlled artifact directory.
	if err != nil {
		t.Fatalf("open pyulog rewrite: %v", err)
	}
	defer func() {
		if err := rewritten.Close(); err != nil {
			t.Errorf("close pyulog rewrite: %v", err)
		}
	}()
	file, err := dataset.Read(rewritten)
	if err != nil {
		t.Fatalf("dataset.Read(pyulog rewrite) error = %v", err)
	}
	wantInformation := []ulog.KeyValue{
		{Name: "system_name", Type: ulog.TypeChar, ArrayLength: 7, Value: "sunfish"},
		{Name: "build_id", Type: ulog.TypeUint32, Value: uint32(0x10203040)},
	}
	if got := file.Information(); !reflect.DeepEqual(got, wantInformation) {
		t.Errorf("rewritten information = %#v, want %#v", got, wantInformation)
	}
	wantParameters := []ulog.KeyValue{{Name: "gain", Type: ulog.TypeFloat32, Value: float32(1.25)}}
	if got := file.Parameters(); !reflect.DeepEqual(got, wantParameters) {
		t.Errorf("rewritten parameters = %#v, want %#v", got, wantParameters)
	}
	wantLogs := []ulog.LogEntry{{Level: ulog.LogLevelInfo, Timestamp: 2500, Message: "ready"}}
	if got := file.Logs(); !reflect.DeepEqual(got, wantLogs) {
		t.Errorf("rewritten logs = %#v, want %#v", got, wantLogs)
	}
	wantDropouts := []ulog.Dropout{{Duration: 25 * time.Millisecond}}
	if got := file.Dropouts(); !reflect.DeepEqual(got, wantDropouts) {
		t.Errorf("rewritten dropouts = %#v, want %#v", got, wantDropouts)
	}
	instance, err := file.Dataset("pyulog_sample", 1)
	if err != nil {
		t.Fatalf("Dataset(pyulog_sample, 1) error = %v", err)
	}
	timestamps, ok := instance.Column("timestamp")
	if !ok {
		t.Fatal("timestamp column missing from pyulog rewrite")
	}
	if got, want := timestamps.Values(), []uint64{1500}; !reflect.DeepEqual(got, want) {
		t.Errorf("rewritten timestamps = %#v, want %#v", got, want)
	}
}

func writePyULogFixture(t *testing.T, filename string) {
	t.Helper()
	destination, err := os.Create(filename) // #nosec G304 -- test path is controlled by the caller.
	if err != nil {
		t.Fatalf("create Go fixture: %v", err)
	}
	writer, err := ulog.NewWriter(destination, ulog.WithStartTimestamp(424))
	if err != nil {
		_ = destination.Close()
		t.Fatalf("ulog.NewWriter() error = %v", err)
	}
	if err := writer.WriteInformation("system_name", "sunfish"); err != nil {
		t.Fatalf("WriteInformation(system_name) error = %v", err)
	}
	if err := writer.WriteInformation("build_id", uint32(0x10203040)); err != nil {
		t.Fatalf("WriteInformation(build_id) error = %v", err)
	}
	if err := writer.WriteParameter("gain", float32(1.25)); err != nil {
		t.Fatalf("WriteParameter(gain) error = %v", err)
	}
	first, err := ulog.Register[pyulogSample](writer)
	if err != nil {
		_ = destination.Close()
		t.Fatalf("ulog.Register(first) error = %v", err)
	}
	second, err := ulog.Register[pyulogSample](writer, ulog.WithMultiID(1))
	if err != nil {
		_ = destination.Close()
		t.Fatalf("ulog.Register(second) error = %v", err)
	}
	values := []struct {
		stream *ulog.Stream[pyulogSample]
		value  pyulogSample
	}{
		{first, pyulogSample{Timestamp: 1000, SignedValue: -123456, Value: -12.5, Vector: [3]float32{1, -2, 3.5}, Enabled: true}},
		{second, pyulogSample{Timestamp: 1500, SignedValue: -7, Value: 0.5, Vector: [3]float32{9, 8, 7}, Enabled: true}},
		{first, pyulogSample{Timestamp: 2000, SignedValue: 789, Value: 6.25, Vector: [3]float32{-4, 5, -6.5}}},
	}
	for _, item := range values {
		if err := item.stream.Write(item.value); err != nil {
			_ = destination.Close()
			t.Fatalf("Write() error = %v", err)
		}
	}
	if err := writer.WriteLog(ulog.LogLevelInfo, 2500, "ready"); err != nil {
		t.Fatalf("WriteLog() error = %v", err)
	}
	if err := writer.WriteDropout(25 * time.Millisecond); err != nil {
		t.Fatalf("WriteDropout() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		_ = destination.Close()
		t.Fatalf("writer.Close() error = %v", err)
	}
	if err := destination.Close(); err != nil {
		t.Fatalf("destination.Close() error = %v", err)
	}
}

func interopArtifactDir(t *testing.T) string {
	t.Helper()
	directory := os.Getenv("ULOG_INTEROP_ARTIFACT_DIR")
	if directory == "" {
		return t.TempDir()
	}
	if err := os.MkdirAll(directory, 0o750); err != nil { // #nosec G703 -- the caller explicitly selects the test artifact directory.
		t.Fatalf("create interoperability artifact directory: %v", err)
	}
	return directory
}

func runPyULogOracle(t *testing.T, uvPath string, arguments ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	commandArguments := []string{
		"run",
		"--frozen",
		"--project", ".",
		"python", "oracle.py",
	}
	commandArguments = append(commandArguments, arguments...)
	command := exec.CommandContext(ctx, uvPath, commandArguments...) // #nosec G204 -- arguments are controlled by this test.
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("pyulog oracle timed out: %v", ctx.Err())
	}
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			t.Fatalf("pyulog oracle exited %d: %v\n%s", exitError.ExitCode(), err, output)
		}
		t.Fatalf("run pyulog oracle: %v\n%s", err, output)
	}
	return output
}
