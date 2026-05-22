# test/conformance

Dual-run conformance harness that compares aria2go against the reference
aria2c binary.

## Quickstart

```sh
# Run with reference binary available:
go test -short ./test/conformance/...

# Run without reference binary (reference tests are skipped):
go test -short ./test/conformance/...
```

## How it works

The harness runs the **same** set of CLI arguments, input files, and
environment variables against both:

1. **aria2c** (reference) — the official C++ binary, looked up via:
   - `aria2c` in `$PATH`, or
   - `./aria2c-ref` at the project root.

2. **aria2go** (implementation) — our Go implementation, built on-demand
   from `./cmd/aria2go/` with `go build`.

Captured stdout, stderr, and exit codes are then compared using the
assertion helpers.

## Feature Matrix Guard

`feature_matrix.json` is the machine-readable parity ledger. The guard tests in
`feature_matrix_test.go` enforce that:

- every feature has a valid status,
- file references point at real local files,
- `implemented` entries include `test/conformance` coverage,
- every Go config option is owned by one feature row,
- every advertised RPC method and notification is owned by one feature row,
- every Go option exists in the C++ `prefs.cc` source truth, with explicit
  exceptions for source-only helper prefs such as split DHT host/port entries.

Use this matrix as the gate for feature claims. Parser-only or unit-test-only
work should be marked `partial`, `missing`, or `tests-only` until a runtime
conformance test exists.

## Available helpers

| Function              | What it does                                                |
|-----------------------|-------------------------------------------------------------|
| `SkipIfNoRef(t)`      | Skips the test if aria2c is not available.                  |
| `RunRef(t, args, stdin)`  | Runs reference aria2c and captures output.             |
| `RunImpl(t, args, stdin)` | Builds and runs aria2go, captures output.              |
| `AssertEqualStdout(t, ref, impl)` | Compares normalized stdout.                 |
| `AssertEqualExit(t, ref, impl)`   | Compares exit codes.                          |

## Writing a conformance test

```go
func TestMyFeature(t *testing.T) {
    SkipIfNoRef(t)

    ref, err := RunRef(t, []string{"--some-flag", "value"}, "")
    if err != nil {
        t.Fatalf("RunRef: %v", err)
    }

    impl, err := RunImpl(t, []string{"--some-flag", "value"}, "")
    if err != nil {
        t.Fatalf("RunImpl: %v", err)
    }

    AssertEqualExit(t, ref, impl)
    AssertEqualStdout(t, ref, impl)
}
```

## Normalization

`AssertEqualStdout` normalizes output before comparison:
- `\r\n` line endings → `\n`
- Trailing whitespace trimmed from each line
