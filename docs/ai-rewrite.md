# How aria2go Was Rewritten With AI

This project is an AI-assisted rewrite of aria2 1.37.0 in Go. I am writing this
down because the process is more interesting than a normal “AI wrote some code”
story, and because it explains why the repository has so much conformance
infrastructure around it.

This was done for educational purposes. I always wanted a Go port of aria2. In
the past, cgo linking scared me enough that I kept avoiding the idea. Even now,
cgo may still be the better engineering answer for some uses. The point here
was to see whether a pure Go rewrite could copy aria2's behavior closely, then
reshape and enhance the implementation around how Go wants code to be written.

## The Shape of the Rewrite

The first pass was not “ask a model to write aria2.” That would have been a
mess. The useful approach was to split the work into layers.

Large planning models, mainly Opus-class models and Codex, were used to read
the original aria2 source tree and turn it into specs, module boundaries, and
implementation tickets. The goal was to capture intent: protocols, options,
state machines, file formats, edge cases, and compatibility constraints.

The planning output was intentionally boring. That was the point. I did not
want a magical prompt that said “rewrite aria2.” I wanted a pile of small,
reviewable artifacts that other agents could pick up without needing the whole
project in context.

There were three important kinds of planning files.

First were the module specs. A spec described a module the way a senior engineer
would hand work to a team: package name, responsibility, scope, out-of-scope
items, public API surface, invariants, concurrency rules, error handling,
config behavior, and tests.

For example, the disk storage spec was not just “write file code.” It split the
area into a Go package and concrete responsibilities:

```text
Module: 12-disk-storage
Package: internal/disk/
Responsibility: file I/O substrate for downloads and seeding.
Scope:
  - common Adaptor interface
  - SingleFile and MultiFile adaptors
  - file allocation strategies: none, trunc, falloc, prealloc
  - piece completion tracking
  - piece hash verification
  - sparse-file behavior
  - write coalescing
Out of scope:
  - engine-level overwrite/resume decisions
  - direct I/O
  - hash-check-only mode
```

That same spec then sketched the Go API the ticket agents were expected to
implement. It had things like an `Adaptor` interface, `FileEntry`, `Allocator`,
`Verifier`, and rules about `ReadAt`, `WriteAt`, `Sync`, `Close`, and piece
bitfields. The important part was not that the final code had to preserve every
name exactly. The important part was that every agent knew the shape of the
module, its invariants, and what was not its job.

A logging spec looked different. It was about behavior compatibility and
cross-cutting policy:

```text
Module: 16-logging-metrics
Package: internal/log/
Responsibility: one slog-based logging substrate for the whole app.
Classic format:
  YYYY-MM-DD HH:MM:SS.NNN [LEVEL] [FILE:LINE] message
Rules:
  - no package creates its own logger
  - classic handler for aria2-like output
  - JSON handler for structured output
  - handlers must be concurrency-safe
  - no panics in library code
```

That kind of spec forced the implementation agents to think in terms of
contracts instead of isolated functions.

Second were byte-compat and contract docs. These were the “do not guess”
documents. They covered things like config keys, RPC method response shapes,
wire formats, session file layout, DHT persistence, and CLI flags. One ticket,
for example, asked DeepSeek to build a CLI flags table from the local manual and
the already-written config key contract. The ticket did not say “support CLI
flags someday.” It required a table for every accepted `aria2c` option:

```text
Ticket: T021 - CLI Flags Table
Target file: plans/byte-compat/cli-flags-table.md
Input context:
  - plans/contracts/config-keys.md
  - aria2c manual source
Required fields per option:
  - long form
  - short form
  - command-line value syntax
  - default behavior
  - help category
  - value grammar
  - accumulative behavior
  - boolean optional-argument behavior
Also cover:
  - --help tags
  - --version output
  - config path behavior
  - environment proxy variables
  - $HOME expansion
  - $VERSION substitution
```

When DeepSeek filled that ticket, the useful output was not just the table. It
also left behind the boring details that are easy to lose in a port: which
flags are cumulative, which booleans accept an omitted value, which paths expand
`$HOME`, how proxy variables interact with command-line options, and what input
files mean when the same syntax appears outside the shell. Later agents could
then implement CLI parsing from a local contract instead of rediscovering the
manual every time.

Third was the machine-readable ticket manifest. That was the queue and the guard
rail. Each ticket said what it depended on, which files it was allowed to edit,
which files should contain tests, which specs or ADRs had to be read first, and
which gates had to pass before the work was considered done. It kept the agents
from all “helpfully” editing the engine at the same time.

One implementation ticket looked like this conceptually:

```text
id: T031
title: internal/log: logger setup, levels, New()
module: 16-logging-metrics
depends_on: []
target_files:
  - internal/log/log.go
test_files:
  - internal/log/log_test.go
context_files:
  - plans/modules/16-logging-metrics/SPEC.md
  - plans/decisions/ADR-0011-logging-policy.md
gates:
  - go-vet
  - go-test
  - race
complexity: S
```

Another one, for disk storage, had a dependency chain:

```text
id: T045
title: internal/disk/single_file.go: SingleFile adaptor
depends_on:
  - T044
target_files:
  - internal/disk/single_file.go
test_files:
  - internal/disk/single_file_test.go
context_files:
  - plans/modules/12-disk-storage/SPEC.md
  - internal/disk/adaptor.go
  - internal/ioutilx/pool.go
gates:
  - go-vet
  - go-test
  - race
```

This mattered a lot. It meant a worker did not get the whole world. It got the
files it was allowed to touch, the context it had to read, and the gates it had
to pass. That constraint was more useful than a huge prompt.

Each module could also have its own `AGENTS.md` contract. The disk module, for
example, had rules like:

```text
- ADR-0009 is law: OS-specific syscalls stay in internal/platform.
- Four allocator strategies only: none, trunc, falloc, prealloc.
- Piece Have(i)=true means data is on disk and hash-verified.
- WriteAt/ReadAt must be concurrency-safe.
- Close is idempotent.
- Buffer pools are mandatory for hot paths.
```

That was the deep-planning layer: a spec, a local agent contract, ticket rows,
dependency edges, allowed write sets, and gates. It was deliberately mechanical.

After that, individual implementation tickets were picked up by DeepSeek V4 Pro
with maximum reasoning. Each ticket stayed narrow: one protocol piece, parser,
RPC method group, config behavior, disk behavior, or test area at a time. This
was repeated across roughly 80 to 100 subagents. In total, the work went through
about 600M combined cached input tokens.

## How DeepSeek Filled A Ticket

The DeepSeek workers were not asked to improvise the architecture. A typical
ticket loop looked like this:

1. Claim a ticket from the manifest.
2. Read the module spec, relevant ADRs, and any contract docs listed in
   `context_files`.
3. Read the corresponding aria2 C++ source under `source-truth/aria2/src`.
4. Edit only the allowed `target_files` and `test_files`.
5. Run the ticket gates.
6. If the implementation diverged from the planning notes, append an
   implementation log.

At the end of each implementation there was also a validation pass. This was
separate from “does the test pass.” The validation pass checked two things.

First, it checked whether the Go was still good Go: current stdlib APIs,
context-aware concurrency, no unnecessary goroutines, no cgo shortcuts, no
global default HTTP client, no panic-based library control flow, and no weird
translation of C++ shapes when Go had a cleaner local pattern.

Second, it checked the aria2 source again for the ugly parts: niche behavior,
old compatibility decisions, error messages, weird option precedence, partial
failure paths, and the caveats that do not show up in a happy-path spec. This
was where a lot of “almost correct” code got corrected before it reached the
next layer.

The implementation log was important. It forced the worker to leave a trail
when reality disagreed with the initial plan. For spec tickets it looked like:

```text
Implementation Log

No divergence from Implementation Notes.
Spec covers all 197 option entries across 10 categories.
Key documented behaviors:
  - boolean optional-argument convention
  - accumulative flags
  - help tags
  - proxy precedence
  - environment variable equivalents
  - $HOME expansion
  - input-file option syntax
```

For code tickets, the equivalent was usually in the final agent response and
the tests. A worker might say: “Implemented `SingleFile`, added concurrent
`WriteAt` tests, verified close idempotency, and ran `go test -race
./internal/disk`.” That sounded mundane, but it was exactly what made the
process tractable. Every subagent produced a bounded diff and a bounded
evidence trail.

The good parts of this approach:

- A bad worker could only damage a small part of the tree.
- Dependencies prevented agents from implementing consumers before contracts
  existed.
- Test gates were attached to the ticket, not remembered by the orchestrator.
- The module spec carried the reasoning; the implementation worker did not need
  to rediscover everything.

The bad part was that local correctness did not imply global correctness. A
parser could be good, an engine queue could be good, and an RPC method could be
good, while the actual CLI path still wired them together incorrectly. That is
exactly what happened.

That got the repository surprisingly far, but it also created the predictable
failure mode of this style of work: many individual parts existed, but the
wiring between them was uneven. Some modules were faithful in isolation while
the CLI, engine, RPC layer, config layer, and progress/output behavior disagreed
at the seams.

## The Wiring Pass

The individual wiring done by the first wave of agents was not good enough.
That is where the second layer mattered.

Gemini 3.5 Pro was useful as a fast tester and scout. It could quickly inspect
large areas, run targeted checks, and point out where behavior diverged. GPT-5.5
with xhigh reasoning was used as the slower fixer/reviewer layer, always
checking the Go behavior against the aria2 C++ source before changing anything.

The rule became simple: if a fix touched compatibility, first confirm how aria2
does it in `source-truth/aria2/src`, then implement the Go version
idiomatically. No translating C++ line by line, no copying comments, and no
guessing when source-truth had the answer.

This final pass looked very different from the first ticket pass. Instead of
asking “does package X compile,” the fixer loop asked questions like:

```text
aria2c behavior:
  - what does addUri do when position is omitted?
  - what is the JSON-RPC shape inside system.multicall?
  - does removeDownloadResult remove active downloads?
  - what does addTorrent return for invalid bencode?
  - does --http-auth-challenge send eager Basic auth?
  - when does Content-Disposition override the output path?
  - what exit code does SFTP host-key mismatch produce?

aria2go behavior:
  - run the same scenario locally
  - compare exit code, file bytes, request trace, RPC JSON, stdout/stderr
  - patch only after source-truth explains the mismatch
```

That is where many of the real bugs were found. The original DeepSeek pass had
created most of the pieces, but not always the exact behavior at the boundaries.
The final loop was less glamorous and much more valuable.

## Conformance Became The Real Test

Unit tests were not enough. A port like this can pass a lot of internal tests
and still not behave like `aria2c`.

Original tests were ported where they still made sense, and new tests were
written where the Go rewrite needed a better oracle. The final layer was
side-by-side conformance testing. The harness runs each scenario against the
reference `aria2c` binary and against `aria2go`, using local offline fixtures.
Then it compares behavior instead of just checking that an option exists.

The conformance tests cover things like:

- command-line parsing, config files, input files, stdin input, env vars, netrc,
  sessions, hooks, and output routing
- HTTP behavior including ranges, redirects, auth challenge, cookies,
  retryable status codes, gzip, conditional requests, remote timestamps, and
  Content-Disposition filenames
- FTP, SFTP, Metalink, and BitTorrent downloads using local servers and
  generated fixtures
- BitTorrent file selection and output mapping for generated multi-file torrents
- JSON-RPC, XML-RPC, HTTP GET/JSONP, WebSocket notifications, auth behavior,
  `saveSession`, and RPC option effects
- stdout/stderr behavior, download result formatting, quiet mode, and
  `download-result=hide`

This is the part that made the port feel real. The first conformance expansion
found lots of mismatches: wrong RPC auth status codes, missing `changeUri`
wiring, different `system.multicall` error shape, invalid upload metadata being
accepted, missing input-file stdin support, missing parameterized URI expansion,
Content-Disposition not influencing output names, remote-time not being applied,
SFTP host-key digest differences, and more.

Each mismatch was fixed by checking the C++ source, changing the Go code, and
then rerunning the differential tests.

## What the Current State Means

The repository is now much closer to aria2 than the initial generated port. It
has been tested against the original binary through offline conformance
scenarios rather than only through internal unit tests.

I still do not want to oversell it as mathematically perfect. aria2 has a huge
surface area and decades of edge cases. But this is no longer just an
AI-generated pile of modules. It has been repeatedly compared against the real
`aria2c`, fixed where it diverged, and pushed through race tests, vet, source
boundary checks, and conformance suites.

The interesting lesson for me was not that AI can write a big codebase in one
shot. It cannot do that reliably. The lesson was that AI can help with a large
rewrite when the work is decomposed, every compatibility claim is checked
against source-truth, and the final judge is an executable conformance suite
instead of vibes.
