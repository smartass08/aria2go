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
