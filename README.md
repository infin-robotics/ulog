# ulog

[![CI][ci-badge]][ci]
[![Go Reference][reference-badge]][go-reference]
[![License: Apache-2.0][license-badge]][license]

A pure-Go reader, writer, and analysis library for [the PX4 ULog format][reference]. It reads file-defined schemas dynamically, with reflection and generics available as optional conveniences for Go types.

```console
go get github.com/infin-robotics/ulog
```

## Usage

The [package documentation][go-reference] contains executable examples for the common workflows:

- stream dynamically typed records without materialising the complete log;
- decode selected formats into typed Go structs;
- write records from typed Go structs;
- load a file into column-oriented datasets and export individual datasets as CSV with [`pkg/dataset`][dataset-reference]; and
- convert datasets to Apache Arrow or Parquet with [`pkg/columnar`][columnar-reference].

ULog `F` format messages remain authoritative. `FormatFor[T]`, `Decode[T]`, and `Register[T]` provide optional typed adapters without requiring a matching Go struct to read a file. Numeric arrays and nested formats use flattened paths such as `q[0]` and `position.x`. Character arrays remain one string-valued field; scalar `char` and `uint8_t` arrays remain byte-valued. Typed string fields declare their wire width explicitly with a tag such as `ulog:"name,char[80]"`.

The root `ulog` and `pkg/dataset` packages use only the Go standard library. Importing `pkg/columnar` adds Apache Arrow.

## Compatibility

Wire codecs have golden-byte tests. A committed file written by `pyulog` is read during ordinary Go tests, while a separate pinned CI job verifies both directions semantically:

```text
Go writer → pyulog reader
Go writer → pyulog rewrite → Go reader
```

The pinned environment, test implementation, fixture, and provenance live together under [`tests`](tests).

## Current scope

- ULog version 1 writing and forward-compatible header and flag-bit reading
- dynamic primitive, fixed-array, and nested-format data
- streaming and eager reads
- typed and dynamic writing
- information and multi-information, parameters and defaults, logging, and dropouts
- reading appended data sections, including sections following an interrupted message
- CSV, Arrow record batches, and Parquet output

Multi-information, default-parameter, and tagged-log writing are available at the lower-level `pkg/wire` boundary but do not yet have root-package writer conveniences.

## License

This project is released under the [Apache License, Version 2.0](LICENSE).

[ci]: https://github.com/infin-robotics/ulog/actions/workflows/ci.yml
[ci-badge]: https://github.com/infin-robotics/ulog/actions/workflows/ci.yml/badge.svg
[go-reference]: https://pkg.go.dev/github.com/infin-robotics/ulog
[dataset-reference]: https://pkg.go.dev/github.com/infin-robotics/ulog/pkg/dataset
[columnar-reference]: https://pkg.go.dev/github.com/infin-robotics/ulog/pkg/columnar
[license]: LICENSE
[license-badge]: https://img.shields.io/badge/license-Apache--2.0-blue.svg
[reference]: https://docs.px4.io/main/en/dev_log/ulog_file_format
[reference-badge]: https://pkg.go.dev/badge/github.com/infin-robotics/ulog.svg
