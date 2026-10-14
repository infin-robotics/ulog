// Package ulog reads and writes PX4 ULog streams.
//
// Each ULog file defines the formats of its own data records. [Reader] resolves
// those definitions and subscriptions as it advances. The current record is
// replaced on the next successful read, while metadata, parameters, logs, and
// dropouts remain available through the reader's accessors. The reader accepts
// future file versions and ignores unknown message types, but currently rejects
// unknown incompatibility flags and logs marked as containing appended data.
//
// [FormatsFor], [FormatFor], [Register], and [Decode] adapt named Go structs to
// that dynamic model. Exported fields become lower_snake_case by default; a
// “ulog” struct tag can rename a field or exclude it with “-”. The adapter
// supports ULog primitive types, fixed-size arrays, and nested named structs. A
// string field requires its wire width in a tag such as ulog:"name,char[80]".
// Dynamic records expose character arrays as strings with trailing NUL padding
// removed. Scalar characters and uint8 arrays remain byte-valued.
//
// Use [Reader] when records can be processed as a stream. Package [dataset]
// loads a complete file into typed columns, and package [columnar] converts
// those datasets to Apache Arrow or Parquet.
//
// [dataset]: https://pkg.go.dev/github.com/infin-robotics/ulog/pkg/dataset
// [columnar]: https://pkg.go.dev/github.com/infin-robotics/ulog/pkg/columnar
package ulog
