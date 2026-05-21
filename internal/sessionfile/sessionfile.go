// Package sessionfile reads and writes aria2-compatible session files.
//
// The session file is a line-oriented UTF-8 text file. Each download entry
// consists of a URI line (tab-separated URIs) followed by option lines
// (tab-prefixed key=value pairs). The serialization is byte-compatible with
// aria2 1.37.0's SessionSerializer output.
package sessionfile

import "github.com/smartass08/aria2go/internal/core"

// Entry represents one download in an aria2 session file.
type Entry struct {
	URIs         []string          // download URIs (tab-separated in file)
	GID          core.GID          // 16-char lowercase hex in file
	Status       core.Status       // inferred: Waiting or Paused
	Options      map[string]string // all recognized key-value pairs
	Unknown      map[string]string // unrecognized keys preserved for round-trip
	UnknownOrder []OptionLine      // unrecognized lines in input order, including duplicates
}

// OptionLine is a single option key-value line preserved from a session file.
type OptionLine struct {
	Key   string
	Value string
}

// knownKeys is the set of all aria2 option names (IDs 1–214).
var knownKeys = buildKnownKeys()

// canonicalKeyOrder lists all aria2 option names in their Pref ID order.
// This matches the iteration order of SessionSerializer::writeOption
// (option::i2p(i) for i=1..countOption()-1), which is the registration
// order of preferences in aria2's preference table.
//
// Note: gid (ID 99) and pause (ID 90) are emitted as special-first-keys
// BEFORE the general iteration, AND again at their natural positions in
// this list. Duplicate emission is intentional per aria2 behavior.
//
// Cumulative keys (header, bt-tracker, bt-exclude-tracker, index-out) have
// each value written on its own line — the value is split by newline
// on write and joined by newline on read.
var canonicalKeyOrder = []string{
	// Section 1: General Preferences (IDs 3–110; 1=version, 2=help excluded: no handler registered)
	"timeout",
	"dns-timeout",
	"connect-timeout",
	"max-tries",
	"auto-save-interval",
	"log",
	"dir",
	"out",
	"split",
	"daemon",
	"referer",
	"lowest-speed-limit",
	"piece-length",
	"max-overall-download-limit",
	"max-download-limit",
	"startup-idle-time",
	"file-allocation",
	"no-file-allocation-limit",
	"allow-overwrite",
	"realtime-chunk-checksum",
	"check-integrity",
	"netrc-path",
	"continue",
	"no-netrc",
	"max-downloads",
	"input-file",
	"deferred-input",
	"max-concurrent-downloads",
	"optimize-concurrent-downloads",
	"optimize-concurrent-downloads-coeffA",
	"optimize-concurrent-downloads-coeffB",
	"force-sequential",
	"auto-file-renaming",
	"parameterized-uri",
	"allow-piece-length-change",
	"no-conf",
	"conf-path",
	"stop",
	"quiet",
	"async-dns",
	"summary-interval",
	"log-level",
	"console-log-level",
	"uri-selector",
	"server-stat-timeout",
	"server-stat-if",
	"server-stat-of",
	"remote-time",
	"max-file-not-found",
	"event-poll",
	"enable-rpc",
	"rpc-listen-port",
	"rpc-user",
	"rpc-passwd",
	"rpc-max-request-size",
	"rpc-listen-all",
	"rpc-allow-origin-all",
	"rpc-certificate",
	"rpc-private-key",
	"rpc-secure",
	"rpc-save-upload-metadata",
	"dry-run",
	"reuse-uri",
	"on-download-start",
	"on-download-pause",
	"on-download-stop",
	"on-download-complete",
	"on-download-error",
	"interface",
	"multiple-interface",
	"disable-ipv6",
	"human-readable",
	"remove-control-file",
	"always-resume",
	"max-resume-failure-tries",
	"save-session",
	"max-connection-per-server",
	"min-split-size",
	"conditional-get",
	"select-least-used-host",
	"enable-async-dns6",
	"max-download-result",
	"retry-wait",
	"async-dns-server",
	"show-console-readout",
	"stream-piece-selector",
	"truncate-console-readout",
	"pause",
	"download-result",
	"hash-check-only",
	"checksum",
	"stop-with-process",
	"enable-mmap",
	"force-save",
	"save-not-found",
	"disk-cache",
	"gid",
	"save-session-interval",
	"enable-color",
	"rpc-secret",
	"dscp",
	"pause-metadata",
	"rlimit-nofile",
	"min-tls-version",
	"socket-recv-buffer-size",
	"max-mmap-limit",
	"stderr",
	"keep-unfinished-download-result",

	// Section 2: FTP Preferences (IDs 111–116)
	"ftp-user",
	"ftp-passwd",
	"ftp-type",
	"ftp-pasv",
	"ftp-reuse-connection",
	"ssh-host-key-md",

	// Section 3: HTTP Preferences (IDs 117–135)
	"http-user",
	"http-passwd",
	"user-agent",
	"load-cookies",
	"save-cookies",
	"enable-http-keep-alive",
	"enable-http-pipelining",
	"max-http-pipelining",
	"header",
	"certificate",
	"private-key",
	"ca-certificate",
	"check-certificate",
	"use-head",
	"http-auth-challenge",
	"http-no-cache",
	"http-accept-gzip",
	"content-disposition-default-utf8",
	"no-want-digest-header",

	// Section 4: Proxy Preferences (IDs 136–149)
	"http-proxy",
	"https-proxy",
	"ftp-proxy",
	"all-proxy",
	"no-proxy",
	"proxy-method",
	"http-proxy-user",
	"http-proxy-passwd",
	"https-proxy-user",
	"https-proxy-passwd",
	"ftp-proxy-user",
	"ftp-proxy-passwd",
	"all-proxy-user",
	"all-proxy-passwd",

	// Section 5: BitTorrent Preferences (IDs 150–205)
	"peer-connection-timeout",
	"bt-timeout",
	"bt-request-timeout",
	"show-files",
	"max-overall-upload-limit",
	"max-upload-limit",
	"torrent-file",
	"listen-port",
	"follow-torrent",
	"select-file",
	"seed-time",
	"seed-ratio",
	"bt-keep-alive-interval",
	"peer-id-prefix",
	"peer-agent",
	"enable-peer-exchange",
	"enable-dht",
	"dht-listen-addr",
	"dht-listen-port",
	"dht-entry-point-host",
	"dht-entry-point-port",
	"dht-entry-point",
	"dht-file-path",
	"enable-dht6",
	"dht-listen-addr6",
	"dht-entry-point-host6",
	"dht-entry-point-port6",
	"dht-entry-point6",
	"dht-file-path6",
	"bt-min-crypto-level",
	"bt-require-crypto",
	"bt-request-peer-speed-limit",
	"bt-max-open-files",
	"bt-seed-unverified",
	"bt-hash-check-seed",
	"bt-max-peers",
	"bt-external-ip",
	"index-out",
	"bt-tracker-interval",
	"bt-stop-timeout",
	"bt-prioritize-piece",
	"bt-save-metadata",
	"bt-metadata-only",
	"bt-enable-lpd",
	"bt-lpd-interface",
	"bt-tracker-timeout",
	"bt-tracker-connect-timeout",
	"dht-message-timeout",
	"on-bt-download-complete",
	"bt-tracker",
	"bt-exclude-tracker",
	"bt-remove-unselected-file",
	"bt-detach-seed-only",
	"bt-force-encryption",
	"bt-enable-hook-after-hash-check",
	"bt-load-saved-metadata",

	// Section 6: Metalink Preferences (IDs 206–214)
	"metalink-file",
	"metalink-version",
	"metalink-language",
	"metalink-os",
	"metalink-location",
	"follow-metalink",
	"metalink-preferred-protocol",
	"metalink-enable-unique-protocol",
	"metalink-base-uri",
}

// canonicalIndex maps option key name to its position in canonicalKeyOrder
// (0-indexed). Used for sorting unknown keys during write.
var canonicalIndex = func() map[string]int {
	m := make(map[string]int, len(canonicalKeyOrder))
	for i, k := range canonicalKeyOrder {
		m[k] = i
	}
	return m
}()

// cumulativeKeys are options whose values are split by newline on write
// and joined by newline on read. Each value is written on its own line.
var cumulativeKeys = map[string]bool{
	"header":             true,
	"bt-tracker":         true,
	"bt-exclude-tracker": true,
	"index-out":          true,
}

func buildKnownKeys() map[string]bool {
	m := make(map[string]bool, len(canonicalKeyOrder))
	for _, k := range canonicalKeyOrder {
		m[k] = true
	}
	return m
}
