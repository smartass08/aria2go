# aria2go

[![Go Reference](https://pkg.go.dev/badge/github.com/smartass08/aria2go.svg)](https://pkg.go.dev/github.com/smartass08/aria2go)
[![Go Report Card](https://goreportcard.com/badge/github.com/smartass08/aria2go)](https://goreportcard.com/report/github.com/smartass08/aria2go)

`aria2go` is a pure Go rewrite of `aria2c`, built from the behavior of aria2
1.37.0. It is an AI-assisted port, written for educational purposes and for the
old itch of having an aria2-style downloader in Go without linking to the C++
code through cgo.

This is not a claim that cgo is the wrong answer. For some projects, cgo may
still be the cleaner and safer way to reuse a mature C++ codebase. The point of
this repository is different: to see how close a clean Go port can get when it
is repeatedly checked against the original implementation.

The covered paths are battle-tested with side-by-side conformance tests against
the reference `aria2c` binary. Those tests run offline using local HTTP, FTP,
SFTP, BitTorrent, Metalink, and RPC fixtures, then compare exit codes, files,
request traces, stdout/stderr behavior, RPC responses, and edge case behavior
against `aria2c`.

This is not full aria2 parity yet. The current feature ledger is tracked in
[docs/feature-matrix.md](docs/feature-matrix.md), backed by a machine-readable
matrix that makes conformance fail if a feature is marked implemented without
runtime coverage.

For the longer write-up on how the AI rewrite was done, see
[docs/ai-rewrite.md](docs/ai-rewrite.md).

## Build

```bash
go build -o aria2go ./cmd/aria2go
```

Run it like `aria2c`:

```bash
./aria2go https://example.com/file
```

## Test

The normal Go suite:

```bash
go test ./...
go test -race ./...
go vet ./...
```

The side-by-side conformance suite expects a reference `aria2c` binary in
`$PATH` or at the repository root:

```bash
go test ./test/conformance -count=1
```

The conformance harness is intentionally offline. It starts local servers and
feeds both binaries the same arguments, input files, environment variables, and
RPC requests.

## License

See [LICENSE](LICENSE).
