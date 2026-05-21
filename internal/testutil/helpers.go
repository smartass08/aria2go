package testutil

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/sessionfile"
	"github.com/smartass08/aria2go/internal/torrent"
	"github.com/smartass08/aria2go/internal/tracker"
)

// TestConfig returns an Options suitable for testing.
// It uses aria2 defaults with Dir set to "." and LogLevel to "debug".
func TestConfig() *config.Options {
	return config.Default()
}

// TestLogger returns a slog.Logger that writes to os.Stderr and uses
// slog.LevelDebug. Suitable for integration tests; use a discard logger
// for unit tests with TestDiscardLogger.
func TestLogger() *slog.Logger {
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return slog.New(h)
}

// TestDiscardLogger returns a slog.Logger that discards all output.
func TestDiscardLogger() *slog.Logger {
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.Level(slog.LevelError + 1),
	})
	return slog.New(h)
}

// DownloadResult mirrors the engine's download result for testing
// session file round-tripping and status reporting.
type DownloadResult struct {
	GID        core.GID
	Status     core.Status
	ErrorCode  core.ErrorCode
	URIs       []string
	FileLength int64
}

// CreateDownloadResult returns a DownloadResult with the given error
// code and URI. Mirrors C++ TestUtil::createDownloadResult.
func CreateDownloadResult(code core.ErrorCode, uri string) *DownloadResult {
	return &DownloadResult{
		GID:       1,
		Status:    core.StatusComplete,
		ErrorCode: code,
		URIs:      []string{uri},
	}
}

// CreateRequestGroup creates a minimal test RequestGroup-like structure
// for testing engine behavior. Mirrors C++ TestUtil::createRequestGroup.
func CreateRequestGroup(pieceLength int32, totalLength int64, path, uri string, opts *config.Options) *RequestGroupStub {
	return &RequestGroupStub{
		PieceLength: pieceLength,
		TotalLength: totalLength,
		Path:        path,
		URI:         uri,
		Opts:        opts,
	}
}

// RequestGroupStub is a lightweight stand-in for engine.requestGroup
// used in test helpers. It carries the fields needed by session
// serialization tests.
type RequestGroupStub struct {
	PieceLength int32
	TotalLength int64
	Path        string
	URI         string
	Opts        *config.Options
}

// MakeTorrent creates a minimal valid .torrent byte slice for testing.
// The returned torrent has one file "test.txt" of 1024 bytes, piece
// length 256 KiB, and a single 20-byte SHA-1 piece hash set to zero.
func MakeTorrent() []byte {
	return MinimalTorrentBytes()
}

// MakeMagnet creates a minimal magnet URI for testing.
func MakeMagnet(infoHash string) string {
	return "magnet:?xt=urn:btih:" + infoHash + "&dn=test"
}

// CreateCookie returns a Netscape-format cookie line for testing.
// Mirrors C++ TestUtil::createCookie (simplified).
func CreateCookie(name, value, domain, path string, secure bool) string {
	flag := "FALSE"
	if strings.HasPrefix(domain, ".") {
		flag = "TRUE"
	}
	sec := "FALSE"
	if secure {
		sec = "TRUE"
	}
	return domain + "\t" + flag + "\t" + path + "\t" + sec + "\t0\t" + name + "\t" + value
}

// CreateCookieExpiry returns a Netscape-format cookie line with an
// explicit expiry timestamp.
func CreateCookieExpiry(name, value string, expiry time.Time, domain, path string, secure bool) string {
	flag := "FALSE"
	if strings.HasPrefix(domain, ".") {
		flag = "TRUE"
	}
	sec := "FALSE"
	if secure {
		sec = "TRUE"
	}
	return domain + "\t" + flag + "\t" + path + "\t" + sec + "\t" + strconv.FormatInt(expiry.Unix(), 10) + "\t" + name + "\t" + value
}

// CreateSessionEntry returns a sessionfile.Entry for testing session
// serialization round-trips.
func CreateSessionEntry(gid core.GID, uris []string, status core.Status) *sessionfile.Entry {
	return &sessionfile.Entry{
		GID:     gid,
		URIs:    uris,
		Status:  status,
		Options: make(map[string]string),
		Unknown: make(map[string]string),
	}
}

// MakeAnnounceRequest creates a tracker.AnnounceRequest with default
// test values for BT announce testing.
func MakeAnnounceRequest(infoHash, peerID [20]byte, event string) tracker.AnnounceRequest {
	return tracker.AnnounceRequest{
		InfoHash:   infoHash,
		PeerID:     peerID,
		Port:       6881,
		Uploaded:   0,
		Downloaded: 0,
		Left:       1024 * 1024,
		Event:      event,
		NumWant:    50,
	}
}

// MakeAnnounceResponse creates a minimal tracker.AnnounceResponse for
// mock announce testing.
func MakeAnnounceResponse() *tracker.AnnounceResponse {
	return &tracker.AnnounceResponse{
		Interval:    1800,
		MinInterval: 900,
		Complete:    10,
		Incomplete:  5,
	}
}

// ParseInfoHashV1 parses a 40-char hex string into [20]byte.
func ParseInfoHashV1(s string) [20]byte {
	h, err := core.ParseInfoHashV1(s)
	if err != nil {
		panic("testutil: invalid infohash: " + err.Error())
	}
	return h
}

// TorrentMeta wraps a parsed torrent.MetaInfo for testing.
func TorrentMeta(data []byte) *torrent.MetaInfo {
	m, err := torrent.Load(data)
	if err != nil {
		panic("testutil: failed to load torrent: " + err.Error())
	}
	return m
}
