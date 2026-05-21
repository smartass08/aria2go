# Config File Format Byte-Compat Spec

**Source:** aria2 1.37.0 C++ source (`OptionParser.cc`, `OptionHandlerImpl.cc`,
`util.h`, `util.cc`, `a2functional.h`, `option_processing.cc`, `Option.h`,
`Option.cc`). Zero aria2 LOC — English behavioral spec only.
**Status:** Spec-authored from clean-room reading of the aria2 parser.
**Target:** Implementers of the `aria2.conf` byte-compat config parser.

---

## 1. File Location and Loading

aria2 looks for its config file at a default path that depends on the platform
and environment. The logic is:

- If `--conf-path=PATH` is given (CLI or RPC options), use that path.
- Otherwise, use the hardcoded default, which is resolved as:
  - If `$XDG_CONFIG_HOME` is set: `$XDG_CONFIG_HOME/aria2/aria2.conf`
  - Otherwise: `$HOME/.aria2/aria2.conf`
- If `--no-conf=true` is set, skip config file loading entirely.

If the file does not exist:
- When a custom path was given via `--conf-path`, aria2 prints an error message
  (`"Configuration file %s is not found."`), shows usage, and exits with
  `UNKNOWN_ERROR`.
- When the default path is used, aria2 silently continues with default values
  only.

The file is read entirely into memory via `BufferedFile::transfer` before
parsing begins.

---

## 2. Basic Syntax

Each line in the config file follows this grammar:

```
key=value
```

Where:
- `key` is the long option name without the `--` prefix (e.g., `max-concurrent-downloads`).
- `value` is the option value. No quoting is applied. The entire value string
  after whitespace stripping is passed to the option handler as-is.
- One option per line. There is no multi-line continuation syntax.

**Whitespace stripping:** Both the key and value are stripped of leading and
trailing characters from the set `"\r\n\t "` (carriage return, newline, tab,
space). This stripping happens before any further processing.

**Line splitting:** `aria2.conf` is parsed using `std::getline()`, which splits
on `'\n'`. Any `'\r'` remaining in the line is stripped as part of the
whitespace set. This means Windows CRLF line endings work correctly.

**Empty lines:** Lines that are empty (zero length) or become empty after
whitespace stripping are silently skipped.

**Lines without `=`:** The split is performed on the first `=` character on the
line. If the key portion is empty after stripping (i.e., the line starts with
`=` or contains only whitespace before `=`), the line is silently skipped.

**Multiple `=` signs:** Only the first `=` acts as the key/value delimiter.
Everything after the first `=` becomes the value, including any additional `=`
signs. For example:
```
header=Authorization: Bearer token=abc123
```
The key is `header`, the value is `Authorization: Bearer token=abc123`.

---

## 3. Comments

A line whose **first character** is `#` is treated as a comment and skipped
entirely. The check is on the raw first character of the line, before
whitespace stripping.

**There is no inline comment support.** A `#` appearing after the first
character is NOT treated as a comment delimiter. For example:
```
dir=/tmp/downloads  # this is NOT a comment
```
The value stored for `dir` is `"/tmp/downloads  # this is NOT a comment"`.

**Blank lines** (empty or whitespace-only) are skipped.

---

## 4. Boolean Values

The boolean parser (`BooleanOptionHandler`) accepts only these exact strings
as **true**:
- `true` (case-sensitive, lowercase only)
- An empty string (when the option's argument type is `OPT_ARG` or `NO_ARG`;
  this applies to options that have `--option[=BOOL]` syntax on the CLI, but is
  irrelevant in config file parsing, where `some-option` without `=` is the
  same as a key with an empty value — when the key portion would be empty, the
  line is skipped anyway).

As **false**:
- `false` (case-sensitive, lowercase only)

Any other value (including `True`, `FALSE`, `yes`, `no`, `1`, `0`, `on`,
`off`) causes a parse error (`DL_ABORT_EX`) with the message `"must be either
'true' or 'false'."`. This error is **fatal** and stops config file parsing.

Examples:
```
check-integrity=true     # valid
check-integrity=false    # valid
enable-rpc=True          # INVALID — error, parsing stops
enable-rpc=yes           # INVALID — error, parsing stops
```

---

## 5. Quoting and Escaping

**There is no quoting support.** Values are taken verbatim after whitespace
stripping. Single quotes (`'`) and double quotes (`"`) are treated as literal
characters and become part of the value.

**There is no escape mechanism.** Backslash (`\`) is a literal character.

If you write:
```
user-agent="aria2/1.37.0"
```
The value stored is the literal string `"aria2/1.37.0"` (with the double
quotes included).

For options that require quoting on the command line to protect shell
metacharacters, the config file does not require any quoting.

---

## 6. Multi-Line Values / Continuation

**No multi-line continuation syntax exists.** Each line is a complete,
independent `key=value` pair. There is no backslash continuation, no heredoc,
no indentation-based continuation.

---

## 7. Include Directive

**aria2 1.37.0 does NOT support an `include` directive.** There is no code in
the parser for recursively loading additional config files. Any attempt to use
`include=...` in a config file will be treated as an unknown option, which
aria2 logs as a warning and skips.

**aria2go extension:** aria2go MAY add an `include=` directive as an
extension. If implemented, it:
- Must be processed during config file parsing, not as a regular option.
- Relative paths in the included file are resolved relative to the including
  file's directory.
- Nesting is limited to 10 levels deep to prevent infinite recursion.
- Cyclic includes are detected and cause a fatal error.
- Includes are processed inline at the point of the `include=` directive.

---

## 8. Option Ordering

**Order does not matter** for individual options within the config file.
However, with the exception of cumulative options (see §9), if the same option
appears multiple times, **the last occurrence wins**.

The `Option::put` method replaces the value at the option's slot without
inspection:
```cpp
void Option::put(PrefPtr pref, const std::string& value) {
    setBit(use_, pref);
    table_[pref->i] = value;
}
```

Example:
```
dir=/tmp/old
dir=/tmp/new
```
The effective value of `dir` is `/tmp/new`.

**Cumulative options** (see §9) append rather than replace.

---

## 9. Cumulative (Repeatable) Options

aria2 uses two mechanisms for options that can be specified multiple times:

### True Cumulative Options

These use `CumulativeOptionHandler` and append each occurrence with a
delimiter:

| Option | Delimiter | Behavior |
|--------|-----------|----------|
| `header` | `\n` | Each `header=NAME: VALUE` appends to the stored value. |
| `index-out` | `\n` | Each `index-out=INDEX=PATH` appends to the stored value. |

For example:
```
header=X-Custom: one
header=X-Custom: two
```
The stored value is `"X-Custom: one\nX-Custom: two\n"`.

### Comma-Separated List Options

These are **not** cumulative at the config parser level. They accept a single
comma-separated string value. The runtime engine splits the value on commas
using `util::split(..., ',', true)`. If specified multiple times, the last
occurrence wins (replaces entirely).

| Option | Runtime Split Behavior |
|--------|----------------------|
| `bt-tracker` | Split by `,` after loading. |
| `bt-exclude-tracker` | Split by `,` after loading. |
| `dht-entry-point` | Single `HOST:PORT`, no split. |
| `dht-entry-point6` | Single `HOST:PORT`, no split. |

In aria2's config file, you write:
```
bt-tracker=udp://t1.example.com:6969/announce,udp://t2.example.com:6969/announce
```

**NOT:**
```
bt-tracker=udp://t1.example.com:6969/announce
bt-tracker=udp://t2.example.com:6969/announce   # This REPLACES the first line
```

> **aria2go note:** For user-friendliness, aria2go MAY treat bt-tracker,
> bt-exclude-tracker, dht-entry-point, and dht-entry-point6 as truly
> accumulative (append on repeated lines) IN ADDITION TO supporting
> comma-separated lists. This is a superset of aria2 behavior and is
> backward-compatible for single-line usage.

---

## 10. Size Values

Size values use `getRealSize()`, which supports the following grammar:

```
SIZE := INTEGER [ SUFFIX ]
INTEGER := <non-negative decimal integer>
SUFFIX := 'K' | 'k' | 'M' | 'm'
```

**Multipliers:**
| Suffix | Multiplier | Bytes |
|--------|-----------|-------|
| (none) | ×1 | exact |
| `K` or `k` | ×1024 | kibibytes (KiB) |
| `M` or `m` | ×1048576 | mebibytes (MiB) |

**`G` and `g` suffixes are NOT supported** by `getRealSize()`. The maximum
argument for some size options (e.g., `min-split-size` max = 1073741824,
which is 1 GiB) must be expressed using the M suffix (e.g., `1024M`).

**Integer overflow:** If `value × multiplier` overflows `INT64_MAX`, a
`DL_ABORT_EX` error is thrown: `"overflow/underflow"`.

**Negative values:** A negative value or a value that fails to parse as an
integer throws `"Bad or negative value detected: <input>"`.

Examples:
```
min-split-size=20M    # 20971520 bytes
min-split-size=0      # 0 bytes
min-split-size=1024K  # 1048576 bytes
min-split-size=1024   # 1024 bytes
piece-length=1M       # 1048576 bytes
disk-cache=16M        # 16777216 bytes
```

---

## 11. Time / Duration Values

Duration options (timeouts, intervals) are stored as **integer seconds**.
The `NumberOptionHandler` parses them with `parseLLIntNoThrow`, which accepts
plain decimal integers (no suffixes).

There is **no time-unit suffix support** in the config parser for duration
values. Values like `5s`, `1m`, `1h`, `1d` are NOT recognized and will cause a
parse error (`"Bad number <...>"`).

The `seed-time` option is a floating-point value (parsed with `strtod`) and
represents fractional minutes.

Examples:
```
connect-timeout=60    # 60 seconds
timeout=120           # 120 seconds
max-tries=5           # 5 retries
retry-wait=10         # 10 seconds
summary-interval=0    # 0 = suppress summaries
server-stat-timeout=86400  # 86400 seconds (24 hours)
stop=3600             # stop after 3600 seconds
seed-time=60.0        # 60.0 minutes (floating point)
```

---

## 12. Lists and Multi-Value Syntax

aria2 uses several strategies for multi-value options:

**Strategy A — Comma-separated in a single value:**
Used by `bt-tracker`, `bt-exclude-tracker`, `no-proxy`, `select-file`,
`listen-port`, `metalink-location`, `multiple-interface`, `async-dns-server`.
All values are concatenated with commas in a single line.

```
bt-tracker=udp://t1:6969/announce,udp://t2:6969/announce
select-file=1,3,5-8
no-proxy=localhost,192.168.0.0/16
```

**Strategy B — `\n`-delimited repeatable lines:**
Used by `header` and `index-out`. Each occurrence appends with a `\n`
delimiter.

```
header=X-Custom-One: value1
header=X-Custom-Two: value2
```

**Strategy C — Single `HOST:PORT` pair:**
Used by `dht-entry-point`, `dht-entry-point6`. Only one host:port value.

**Strategy D — Special format with embedded `=` and `\n` delimiter:**
Used by `index-out` where each value is in `INDEX=PATH` format.

```
index-out=1=/output/file1.dat
index-out=4=/output/file4.dat
```

---

## 13. Special Characters and Encoding

**Encoding:** The config file is treated as a sequence of bytes. There is no
encoding validation, no Unicode normalization, and no BOM handling. Config
files should be written in UTF-8 for portability. If a BOM (byte order mark)
is present at the start of the file, it will become part of the first key name
and cause that line to be treated as an unknown option.

**Special characters:** All characters except `\r`, `\n`, `\t`, and space
(which are stripped from key/value boundaries) are preserved literally. This
includes:
- Forward slash `/`, backslash `\`
- Colon `:`, semicolon `;`
- Pipe `|`, ampersand `&`
- Angle brackets `<`, `>`
- Any UTF-8 multi-byte sequences

**`${HOME}` expansion:** For file path options (options handled by
`LocalFilePathOptionHandler`), the literal string `${HOME}` in the value is
replaced with the user's home directory path. Only `${HOME}` is expanded —
`$HOME`, `~`, `~/`, and other shell variables are NOT expanded.

Options that support `${HOME}` expansion include: `dir`, `log`,
`input-file`, `conf-path`, `save-session`, `server-stat-of`, `server-stat-if`,
`dht-file-path`, `dht-file-path6`, `load-cookies`, `save-cookies`,
`ca-certificate`, `certificate`, `private-key`, `rpc-certificate`,
`rpc-private-key`, `netrc-path`, `metalink-file`, `torrent-file`.

Example:
```
dir=${HOME}/downloads
dht-file-path=${HOME}/.aria2/dht.dat
```

---

## 14. Error Handling

The config file parser has the following error semantics:

### Unknown Options

When a line contains a key that does not match any known option name, aria2
logs a warning and **continues parsing**. The unknown option is skipped:
```
A2_LOG_WARN(fmt("Unknown option: %s", line.c_str()));
```

The key lookup is done via `option::k2p()`, which does an exact
case-sensitive match against the known option names.

### Invalid Values

When a known option's value fails validation (wrong type, out of range, bad
format), the handler throws `OptionHandlerException`. This exception is caught
by `option_processing()`:
- An error message `"Parse error in <path>"` is printed.
- The stack trace is printed.
- If the handler has a description, usage is printed.
- Parsing stops and the function returns the error code from the exception.

The error handling is strict: **a single invalid value aborts the entire config
file parse.** Subsequent lines are not processed.

### Config File Existence

- Non-existent custom config path → error, exit with `UNKNOWN_ERROR`.
- Non-existent default config path → silent, continue with defaults.

---

## 15. Windows vs Unix

| Aspect | Unix | Windows |
|--------|------|---------|
| Line endings | `\n` | `\r\n` (the `\r` is stripped by whitespace stripping) |
| Path separators (in file values) | `/` | `\` (handled by `File` class) |
| Default config path | `$HOME/.aria2/aria2.conf` | `%HOMEDRIVE%%HOMEPATH%\.aria2\aria2.conf` |
| Home directory resolution | `getenv("HOME")` | `GetEnvironmentVariable("HOME")` or `HOMEDRIVE` + `HOMEPATH` |
| `${HOME}` expansion | OS-specific home path | OS-specific home path |

The config file format itself is identical on all platforms. Line ending
differences are handled transparently by the whitespace stripping logic.

---

## 16. Configuration Precedence

Options are resolved from multiple sources and merged with a strict precedence
order. The merging uses a parent/child `Option` chain:

### Source Loading Order

1. **Hardcoded defaults** — `OptionParser::parseDefaultValues()` sets every
   option to its default `A2STR::NIL` (empty string) value. This forms the
   basis.

2. **Config file** (`aria2.conf`) — parsed with `OptionParser::parse(option,
   istream&)`. Options from the config file override the defaults.

3. **Environment variables** — specific proxy environment variables
   (`http_proxy`, `https_proxy`, `ftp_proxy`, `all_proxy`, `no_proxy`) are
   read via `getenv()` and override config file values for their corresponding
   options. If any environment variable causes a parse error, the error is
   printed but execution continues with the config file value.

4. **Input-file options** (per-URI options from `--input-file` lines) —
   override config/env values for specific download items.

5. **CLI arguments** — override config file, environment, and defaults.
   CLI arguments do NOT apply to the config file's own Option object; they
   create a child `Option` with the config `Option` as the parent.

6. **RPC options** (KeyVals from `aria2.changeOption` etc.) — override CLI,
   config, and defaults for the session.

### Precedence Summary Table

| Priority | Source | Overrides |
|----------|--------|-----------|
| 1 (lowest) | Defaults | — |
| 2 | Config file | Defaults |
| 3 | Environment vars | Config, Defaults |
| 4 | Input-file per-URI | Config, Env, Defaults |
| 5 | CLI arguments | Config, Env, Defaults |
| 6 (highest) | RPC KeyVals / changeOption | Everything above |

### Parent/Child Chain

The `Option` object uses parent/child lookup chains:
- When `Option::get(pref)` is called, it first checks the local `use_` bit for
  `pref`. If not set, it delegates to `parent_->get(pref)`.
- This means a CLI option explicitly set to `""` (empty string) shadows the
  config file value, returning `""` rather than falling through to the parent.
- An option that is **not set at all** in the child falls through to the
  parent.

**Example showing precedence:**

```
# aria2.conf (config file)
dir=/downloads
max-concurrent-downloads=5
http-user=configuser

# Environment
http_proxy=http://envproxy:8080

# CLI
aria2c --dir=/cli-downloads --http-proxy=http://cliproxy:9999 https://example.com/file.zip
```

Result:
- `dir` → `/cli-downloads` (CLI overrides config)
- `max-concurrent-downloads` → `5` (config file value, no CLI override)
- `http-user` → `configuser` (config file, no CLI override)
- `http-proxy` → `http://cliproxy:9999` (CLI overrides env)
- All other options → their hardcoded defaults

---

## 17. Complete Example `aria2.conf`

```config
# aria2.conf — Complete example configuration
# This file demonstrates all major option categories supported by aria2 1.37.0.

## Basic Options
dir=${HOME}/downloads
input-file=${HOME}/.aria2/uris.txt
log=${HOME}/.aria2/aria2.log
max-concurrent-downloads=10
check-integrity=true
continue=true
log-level=info
console-log-level=notice
split=5
max-connection-per-server=5
min-split-size=20M
max-overall-download-limit=0
max-download-limit=0
max-overall-upload-limit=0
max-upload-limit=0
quiet=false
show-console-readout=true
truncate-console-readout=true
human-readable=true
summary-interval=60
download-result=default
optimize-concurrent-downloads=true
force-sequential=false
stderr=false

## HTTP Options
http-user=myuser
http-passwd=mypassword
user-agent=aria2/1.37.0
referer=*
enable-http-keep-alive=true
enable-http-pipelining=false
http-accept-gzip=false
http-auth-challenge=false
http-no-cache=false
no-want-digest-header=false
use-head=false
header=X-Forwarded-For: 203.0.113.1
header=X-Custom-Header: custom-value
load-cookies=${HOME}/.aria2/cookies.txt
save-cookies=${HOME}/.aria2/cookies-out.txt
ca-certificate=${HOME}/.aria2/ca-bundle.crt
check-certificate=true
http-proxy=http://proxy.example.com:8080
http-proxy-user=proxyuser
http-proxy-passwd=proxypass
https-proxy=http://sslproxy.example.com:8443

## FTP/SFTP Options
ftp-user=anonymous
ftp-passwd=aria2@example.com
ftp-pasv=true
ftp-type=binary
ftp-reuse-connection=true
ftp-proxy=http://ftpproxy.example.com:2121
netrc-path=${HOME}/.netrc
no-netrc=false

## HTTP/FTP/SFTP Shared Options
connect-timeout=60
timeout=120
max-tries=5
retry-wait=10
max-file-not-found=0
lowest-speed-limit=0
remote-time=false
reuse-uri=true
uri-selector=feedback
stream-piece-selector=default
server-stat-of=${HOME}/.aria2/server-stat
server-stat-if=${HOME}/.aria2/server-stat
server-stat-timeout=86400
proxy-method=get
all-proxy=http://allproxy.example.com:3128
no-proxy=localhost,127.0.0.0/8,10.0.0.0/8
dry-run=false
parameterized-uri=false

## Checksum Options
checksum=sha-256=deadbeefcafe...
realtime-chunk-checksum=true

## BitTorrent Options
bt-metadata-only=false
bt-save-metadata=false
bt-load-saved-metadata=false
bt-enable-lpd=false
bt-tracker=udp://tracker.opentrackr.org:1337/announce,udp://tracker.coppersurfer.tk:6969/announce
bt-exclude-tracker=udp://tracker.example.com:6969/announce
bt-tracker-connect-timeout=60
bt-tracker-timeout=60
bt-tracker-interval=0
bt-max-peers=55
bt-request-peer-speed-limit=50K
bt-stop-timeout=0
bt-prioritize-piece=head=1M,tail=1M
bt-hash-check-seed=true
bt-seed-unverified=false
bt-remove-unselected-file=false
bt-max-open-files=100
bt-detach-seed-only=false
bt-enable-hook-after-hash-check=true
bt-force-encryption=false
bt-require-crypto=false
bt-min-crypto-level=plain
bt-external-ip=203.0.113.45
peer-id-prefix=A2-1-37-0-
peer-agent=aria2/1.37.0
seed-ratio=1.0
seed-time=0
listen-port=6881-6999
follow-torrent=true
select-file=1,3
index-out=1=/downloads/video.mp4
dscp=0
enable-dht=true
enable-dht6=false
dht-listen-port=6881-6999
dht-entry-point=router.bittorrent.com:6881
dht-entry-point6=router.bittorrent.com:6881
dht-file-path=${HOME}/.aria2/dht.dat
dht-file-path6=${HOME}/.aria2/dht6.dat
dht-message-timeout=10
enable-peer-exchange=true

## Metalink Options
follow-metalink=true
metalink-base-uri=https://example.com/mirrors/
metalink-language=en
metalink-location=us,jp
metalink-os=linux
metalink-preferred-protocol=none
metalink-enable-unique-protocol=true

## RPC Options
enable-rpc=true
rpc-listen-port=6800
rpc-listen-all=false
rpc-allow-origin-all=false
rpc-secret=mysecrettoken
rpc-secure=false
rpc-max-request-size=2M
rpc-save-upload-metadata=true
pause=false
pause-metadata=false

## Advanced Options
allow-overwrite=false
allow-piece-length-change=false
always-resume=true
max-resume-failure-tries=0
auto-file-renaming=true
conditional-get=false
content-disposition-default-utf8=false
disk-cache=16M
file-allocation=prealloc
no-file-allocation-limit=5M
enable-mmap=false
max-mmap-limit=9223372036854775807
force-save=false
save-not-found=true
save-session=${HOME}/.aria2/aria2.session
save-session-interval=0
auto-save-interval=60
remove-control-file=false
hash-check-only=false
stop=0
interface=eth0
disable-ipv6=false
async-dns=true
async-dns-server=8.8.8.8,8.8.4.4
min-tls-version=TLSv1.2
piece-length=1M
socket-recv-buffer-size=0
deferred-input=false
max-download-result=1000
keep-unfinished-download-result=true
enable-color=true
on-download-start=${HOME}/.aria2/hooks/on-start.sh
on-download-pause=${HOME}/.aria2/hooks/on-pause.sh
on-download-stop=${HOME}/.aria2/hooks/on-stop.sh
on-download-complete=${HOME}/.aria2/hooks/on-complete.sh
on-download-error=${HOME}/.aria2/hooks/on-error.sh
on-bt-download-complete=${HOME}/.aria2/hooks/on-bt-complete.sh
```

---

## 18. Implementation Reference Tables

### Line Parsing Flow

```
For each line read from file:
  1. If line is empty → skip
  2. If line starts with '#' → skip (comment)
  3. Split on first '=' → (key, value)
     Strip whitespace from both key and value (chars: \r \n \t space)
  4. If key is empty after stripping → skip
  5. Look up key in option table (exact, case-sensitive match)
  6. If not found → log warning, skip line, continue
  7. If found → call handler->parse(option, value)
     If handler throws → print error, abort config parse
```

### Whitespace Stripping Set

```
DEFAULT_STRIP_CHARSET = "\r\n\t "
```

Characters stripped from both ends of key and value:
- `\r` (carriage return, 0x0D)
- `\n` (line feed, 0x0A)
- `\t` (horizontal tab, 0x09)
- ` ` (space, 0x20)

### Boolean Storage

Internally, boolean options store the string `"true"` or `"false"`:

```
A2_V_TRUE  = "true"
A2_V_FALSE = "false"
```

`Option::getAsBool(pref)` returns `true` if the stored value equals `"true"`,
`false` for any other value (including `"false"`, empty string, or unset).

### Size Multipliers (exact values)

| Constant | Value | Bytes |
|----------|-------|-------|
| `1_k` | `1 × 1024` | 1,024 |
| `1_m` | `1 × 1024 × 1024` | 1,048,576 |
| `1_g` | `1 × 1024 × 1024 × 1024` | 1,073,741,824 |

Note: `1_g` exists as a constant but is NOT a recognized suffix in
`getRealSize()`. Only `K`, `k`, `M`, `m` suffixes are parsed.

### Cumulative Option Delimiters

| Option | Handler | Delimiter | Appends? |
|--------|---------|-----------|----------|
| `header` | `CumulativeOptionHandler` | `"\n"` | Yes |
| `index-out` | `IndexOutOptionHandler` | `"\n"` | Yes |
| `bt-tracker` | `DefaultOptionHandler` | N/A (comma-sep string) | No, last wins |
| `bt-exclude-tracker` | `DefaultOptionHandler` | N/A (comma-sep string) | No, last wins |
| `dht-entry-point` | `HostPortOptionHandler` | N/A (single value) | No, last wins |
| `dht-entry-point6` | `HostPortOptionHandler` | N/A (single value) | No, last wins |

---

## 19. Byte-Compat Test Cases

The following minimal input/output pairs serve as authoritative test
expectations for the config file parser:

### Test 1: Basic key=value
```
dir=/tmp/aria2
```
→ `dir` = `"/tmp/aria2"`

### Test 2: Whitespace stripping
```
  dir  =  /tmp/aria2  
```
→ `dir` = `"/tmp/aria2"`

### Test 3: Comments
```
# this is a comment
dir=/tmp/aria2
```
→ `dir` = `"/tmp/aria2"` (comment line ignored)

### Test 4: No inline comments
```
dir=/tmp/aria2  # attempt inline comment
```
→ `dir` = `"/tmp/aria2  # attempt inline comment"`

### Test 5: Duplicate options (last wins)
```
dir=/tmp/first
dir=/tmp/second
```
→ `dir` = `"/tmp/second"`

### Test 6: Boolean true
```
enable-rpc=true
```
→ `enable-rpc` = `"true"`

### Test 7: Boolean false
```
enable-rpc=false
```
→ `enable-rpc` = `"false"`

### Test 8: Invalid boolean
```
enable-rpc=yes
```
→ Parse error (aborts config parsing)

### Test 9: Size values
```
min-split-size=20M
piece-length=1048576
disk-cache=16M
```
→ `min-split-size` = 20971520, `piece-length` = 1048576, `disk-cache` = 16777216

### Test 10: Size with lowercase k
```
max-download-limit=50k
```
→ `max-download-limit` = 51200

### Test 11: Unknown option (warning, not error)
```
made-up-option=somevalue
dir=/tmp/aria2
```
→ Warning logged, `made-up-option` skipped, `dir` = `"/tmp/aria2"` (parsing continues)

### Test 12: Cumulative header
```
header=X-One: value1
header=X-Two: value2
```
→ `header` = `"X-One: value1\nX-Two: value2\n"`

### Test 13: Cumulative index-out
```
index-out=1=/out/file1.dat
index-out=4=/out/file4.dat
```
→ `index-out` = `"1=/out/file1.dat\n4=/out/file4.dat\n"`

### Test 14: ${HOME} expansion
```
dir=${HOME}/downloads
```
→ `dir` = `"/home/user/downloads"` (or platform equivalent)

### Test 15: bt-tracker comma-separated (single line)
```
bt-tracker=udp://t1:6969/announce,udp://t2:6969/announce
```
→ `bt-tracker` = `"udp://t1:6969/announce,udp://t2:6969/announce"`

### Test 16: Multiple = signs in value
```
header=Authorization: Bearer token=abc123
```
→ `header` = `"Authorization: Bearer token=abc123"`

### Test 17: Empty value
```
http-user=
```
→ `http-user` = `""` (empty string)

### Test 18: Line with only = (skipped)
```
=
```
→ Skipped (key is empty after stripping), no error

### Test 19: Duration integer (no suffix)
```
connect-timeout=60
timeout=120
```
→ `connect-timeout` = 60, `timeout` = 120

### Test 20: CRLF line endings
```
dir=/tmp/aria2\r\n
```
→ `dir` = `"/tmp/aria2"` (the `\r` is stripped)
