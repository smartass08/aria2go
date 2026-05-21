package contracts

import (
	"context"

	"github.com/smartass08/aria2go/internal/core"
)

// Verify result constants returned by TorrentLifecycleControl.Verify().
const (
	VerifyOK      = 0  // piece verified successfully (hash matches)
	VerifyMissing = -1 // piece not yet downloaded
	VerifyBad     = -2 // piece downloaded but hash mismatch
)

// TorrentStatusProjector renders the BT-specific subset of aria2.tellStatus
// response fields. The engine calls Project() to merge BT-specific keys into
// the generic status map.
//
// Return keys map to aria2's RPC response fields:
//
//	infoHash              (string, 40-char hex)    — SHA-1 info hash
//	bitfield              (string, hex bytes)      — piece completion bitmap
//	numPieces             (string, decimal)        — total piece count
//	pieceLength           (string, decimal)        — piece length in bytes
//	numSeeders            (string, decimal)        — connected seeders count
//	seeder                (string, "true"/"false")  — local seeder state
//	bittorrent            (map[string]any)         — composite BT metadata dict
//	bittorrent.announceList ([][]string)           — tracker announce tiers
//	bittorrent.comment    (string)                 — torrent comment (if present)
//	bittorrent.creationDate (int64)                — torrent creation timestamp (if present)
//	bittorrent.mode       (string, "single"/"multi") — BT file mode
//	bittorrent.info       (map[string]any)         — torrent info dict
//	bittorrent.name       (string)                 — torrent name from info dict
//	files                 ([]map[string]any)       — per-file selection/completion (multi-file only)
//	verifiedLength        (string, decimal)        — bytes verified (during/after rehash)
//	verifyIntegrityPending (string, "true"/"false") — verify/rehash in progress
//
// All numeric values are formatted as decimal strings for aria2 RPC byte-compat.
// Keys not applicable to the current state are omitted from the map.
// Implementations must be safe for concurrent use.
type TorrentStatusProjector interface {
	Project(gid core.GID, keys []string) map[string]any
}

// FileSlice describes a single file within a torrent, including its piece
// range boundaries. Used by FilePieceMap.Files() and the engine's getFiles /
// changePosition RPC handling.
type FileSlice struct {
	Index      int    // 0-based file index within the torrent
	Path       string // relative path as listed in torrent info.files (UTF-8)
	FirstPiece int    // inclusive — the piece index where this file begins
	LastPiece  int    // exclusive — the piece index after the last piece of this file
	Length     int64  // file size in bytes
	Selected   bool   // whether this file is selected for download (respects --select-file)
}

// FilePieceMap maps torrent files to piece ranges, enabling the engine to
// respond to getFiles/changePosition RPCs and compute which pieces to download
// when --select-file restricts the download to a subset of files.
type FilePieceMap interface {
	Files(gid core.GID) []FileSlice
	PiecesForFile(gid core.GID, idx int) (firstPiece, lastPiece int)
}

// TorrentLifecycleControl provides the engine with a handle to control the
// torrent download lifecycle — pause, stop, rehash, and verify operations.
type TorrentLifecycleControl interface {
	Pause() error
	Stop(force bool) error
	RehashAll(ctx context.Context) error
	Verify(ctx context.Context) ([]int, error)
}

// TorrentRPCProjection adapts BT peer and tracker state to the RPC response
// format without leaking BT-specific types into the RPC packages.
//
// Peers() returns the set of connected BT peers (one map per peer).
// Each peer map contains: peerId, ip, port, bitfield, amChoking,
// peerChoking, downloadSpeed, uploadSpeed, seeder.
//
// Servers() returns tracker and DHT node state (one map per announce tier).
// This is distinct from aria2's generic getServers which returns in-flight
// HTTP/FTP request info for all download types. For BT downloads, Servers()
// reports the tracker URLs and their announce status, not data servers.
//
// All numeric values are decimal strings; maps must be serializable via
// encoding/json. Implementations must be safe for concurrent use.
type TorrentRPCProjection interface {
	Peers(gid core.GID) []map[string]any
	Servers(gid core.GID) []map[string]any
}
