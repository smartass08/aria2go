package core

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"time"
)

const hexDigits = "0123456789abcdef"

// GID is a download identifier (uint64), matching aria2's GID/a2_gid_t.
// The zero value (0) is invalid; valid GIDs are >= 1.
type GID uint64

// ParseGID parses a GID from a string. It accepts either a decimal string
// or a 16-character lowercase hex string (unprefixed).
func ParseGID(s string) (GID, error) {
	if len(s) == 16 && isHexString(s) {
		v, err := strconv.ParseUint(s, 16, 64)
		if err != nil {
			return 0, fmt.Errorf("core: invalid GID %q: %w", s, err)
		}
		return GID(v), nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("core: invalid GID %q: %w", s, err)
	}
	return GID(v), nil
}

func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// String returns the decimal representation of the GID.
func (g GID) String() string { return strconv.FormatUint(uint64(g), 10) }

// HexTo writes the 16-character lowercase hex representation into buf.
// Callers that already own a [16]byte buffer can use this to avoid allocation.
func (g GID) HexTo(buf *[16]byte) {
	v := uint64(g)
	for i := 15; i >= 0; i-- {
		buf[i] = hexDigits[v&0xf]
		v >>= 4
	}
}

// Hex returns the 16-character lowercase hex representation of the GID.
func (g GID) Hex() string {
	var buf [16]byte
	g.HexTo(&buf)
	return string(buf[:])
}

// Status represents the lifecycle state of a download.
type Status uint8

const (
	StatusWaiting  Status = iota // 0 — queued, not yet started
	StatusActive                 // 1 — actively downloading
	StatusPaused                 // 2 — paused by user or scheduler
	StatusComplete               // 3 — download finished successfully
	StatusError                  // 4 — download terminated with an error
	StatusRemoved                // 5 — download removed by user
)

var statusStrings = [...]string{
	"waiting",
	"active",
	"paused",
	"complete",
	"error",
	"removed",
}

// String returns the lowercase string representation of the Status.
func (s Status) String() string {
	if int(s) < len(statusStrings) {
		return statusStrings[s]
	}
	return fmt.Sprintf("unknown(%d)", s)
}

// ErrorCode maps 1:1 to aria2's error_code::Value enum.
// Code 0 means success; codes 1..32 are specific error conditions.
// The sentinel -1 (UNDEFINED) is internal only and never returned as an exit code.
type ErrorCode int

const (
	ExitSuccess                 ErrorCode = 0
	ExitUnknownError            ErrorCode = 1
	ExitTimeout                 ErrorCode = 2
	ExitResourceNotFound        ErrorCode = 3
	ExitMaxFileNotFound         ErrorCode = 4
	ExitTooSlow                 ErrorCode = 5
	ExitNetworkProblem          ErrorCode = 6
	ExitUnfinishedDownloads     ErrorCode = 7
	ExitRemoteFileError         ErrorCode = 8
	ExitNotEnoughDiskSpace      ErrorCode = 9
	ExitPieceLengthChanged      ErrorCode = 10
	ExitSameFileDownloading     ErrorCode = 11
	ExitSameInfoHashDownloading ErrorCode = 12
	ExitFileAlreadyExists       ErrorCode = 13
	ExitRenameFailed            ErrorCode = 14
	ExitOpenFileError           ErrorCode = 15
	ExitFileCreateError         ErrorCode = 16
	ExitFileIOError             ErrorCode = 17
	ExitDirCreateError          ErrorCode = 18
	ExitNameResolveError        ErrorCode = 19
	ExitMetalinkParseError      ErrorCode = 20
	ExitFTPProtocolError        ErrorCode = 21
	ExitHTTPProtocolError       ErrorCode = 22
	ExitTooManyRedirects        ErrorCode = 23
	ExitHTTPAuthFailed          ErrorCode = 24
	ExitBencodeParseError       ErrorCode = 25
	ExitTorrentParseError       ErrorCode = 26
	ExitMagnetParseError        ErrorCode = 27
	ExitBadOption               ErrorCode = 28
	ExitHTTPServiceUnavailable  ErrorCode = 29
	ExitJSONParseError          ErrorCode = 30
	ExitRemoved                 ErrorCode = 31
	ExitChecksumError           ErrorCode = 32
	ExitInProgress              ErrorCode = 255
)

var errorCodeStrings = [...]string{
	0:  "success",
	1:  "unknown error",
	2:  "timeout",
	3:  "resource not found",
	4:  "max file not found",
	5:  "too slow",
	6:  "network problem",
	7:  "unfinished downloads",
	8:  "remote file error",
	9:  "not enough disk space",
	10: "piece length changed",
	11: "same file downloading",
	12: "same info hash downloading",
	13: "file already exists",
	14: "rename failed",
	15: "open file error",
	16: "file create error",
	17: "file i/o error",
	18: "dir create error",
	19: "name resolve error",
	20: "metalink parse error",
	21: "ftp protocol error",
	22: "http protocol error",
	23: "too many redirects",
	24: "http auth failed",
	25: "bencode parse error",
	26: "torrent parse error",
	27: "magnet parse error",
	28: "bad option",
	29: "http service unavailable",
	30: "json parse error",
	31: "removed",
	32: "checksum error",
}

// String returns the human-readable name for the error code.
func (c ErrorCode) String() string {
	if c == ExitInProgress {
		return "in-progress"
	}
	if int(c) >= 0 && int(c) < len(errorCodeStrings) {
		return errorCodeStrings[c]
	}
	return fmt.Sprintf("unknown_code(%d)", c)
}

// EventKind identifies the type of a download lifecycle event.
type EventKind uint8

const (
	EvStart      EventKind = iota // download started
	EvPause                       // download paused
	EvStop                        // download stopped
	EvComplete                    // download completed
	EvError                       // download error
	EvBTComplete                  // BitTorrent download completed (seeding)
)

var eventKindStrings = [...]string{
	"start",
	"pause",
	"stop",
	"complete",
	"error",
	"btcomplete",
}

// String returns the lowercase string representation of the EventKind.
func (k EventKind) String() string {
	if int(k) < len(eventKindStrings) {
		return eventKindStrings[k]
	}
	return fmt.Sprintf("unknown(%d)", k)
}

// Event represents a download lifecycle event, emitted by the engine to notify
// listeners (e.g. RPC server, log/metrics).
type Event struct {
	Kind EventKind
	GID  GID
	Time time.Time
}

var eventPool = sync.Pool{
	New: func() any { return new(Event) },
}

// AcquireEvent returns an Event from the pool, populated with the given values.
// Caller must call ReleaseEvent when done.
func AcquireEvent(kind EventKind, gid GID, t time.Time) *Event {
	e := eventPool.Get().(*Event)
	e.Kind = kind
	e.GID = gid
	e.Time = t
	return e
}

// ReleaseEvent returns e to the pool after zeroing all fields.
func ReleaseEvent(e *Event) {
	*e = Event{}
	eventPool.Put(e)
}

// InfoHashV1 is a 20-byte SHA-1 info hash, used in BitTorrent v1.
type InfoHashV1 [20]byte

// String returns the lowercase hex encoding of the info hash.
func (h InfoHashV1) String() string { return fmt.Sprintf("%x", h[:]) }

// AppendTo appends the lowercase hex encoding of h to dst and returns the extended slice.
// Use this on hot paths to avoid allocation from String().
func (h InfoHashV1) AppendTo(dst []byte) []byte {
	for _, b := range h {
		dst = append(dst, hexDigits[b>>4], hexDigits[b&0xf])
	}
	return dst
}

// ParseInfoHashV1 parses a 40-character hex string into a 20-byte InfoHashV1.
func ParseInfoHashV1(s string) (InfoHashV1, error) {
	var h InfoHashV1
	if len(s) != 40 {
		return h, fmt.Errorf("core: infohash v1 must be 40 hex chars, got %d", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return h, fmt.Errorf("core: invalid infohash v1 %q: %w", s, err)
	}
	copy(h[:], b)
	return h, nil
}

// InfoHashV2 is a 32-byte SHA-256 info hash, used in BitTorrent v2.
type InfoHashV2 [32]byte

// String returns the lowercase hex encoding of the info hash.
func (h InfoHashV2) String() string { return fmt.Sprintf("%x", h[:]) }

// AppendTo appends the lowercase hex encoding of h to dst and returns the extended slice.
// Use this on hot paths to avoid allocation from String().
func (h InfoHashV2) AppendTo(dst []byte) []byte {
	for _, b := range h {
		dst = append(dst, hexDigits[b>>4], hexDigits[b&0xf])
	}
	return dst
}

// ParseInfoHashV2 parses a 64-character hex string into a 32-byte InfoHashV2.
func ParseInfoHashV2(s string) (InfoHashV2, error) {
	var h InfoHashV2
	if len(s) != 64 {
		return h, fmt.Errorf("core: infohash v2 must be 64 hex chars, got %d", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return h, fmt.Errorf("core: invalid infohash v2 %q: %w", s, err)
	}
	copy(h[:], b)
	return h, nil
}
