package engine

import (
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/smartass08/aria2go/internal/bencode"
	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/disk"
	btprogress "github.com/smartass08/aria2go/internal/protocol/bittorrent/progress"
	"github.com/smartass08/aria2go/internal/torrent"
)

func newRPCTestEngine(t *testing.T) *Engine {
	t.Helper()

	e, err := New(&config.Options{
		Dir:                    t.TempDir(),
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return e
}

func TestTellStatusStoppedPreservesSnapshot(t *testing.T) {
	e := newRPCTestEngine(t)

	gid, err := e.Add(AddSpec{URIs: []string{"http://example.com/archive.iso"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	e.fillRequestGroupFromReserver()
	if err := e.Remove(gid, false); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	status, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	if status.Status != core.StatusRemoved {
		t.Fatalf("status = %s, want removed", status.Status)
	}
	if status.ErrorCode != core.ExitRemoved {
		t.Fatalf("errorCode = %v, want %v", status.ErrorCode, core.ExitRemoved)
	}
	if len(status.Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(status.Files))
	}
	if len(status.Files[0].URIs) != 2 {
		t.Fatalf("len(uris) = %d, want 2", len(status.Files[0].URIs))
	}
	if got := status.Files[0].URIs[0]; got.Status != "used" || got.URI != "http://example.com/archive.iso" {
		t.Fatalf("first stopped URI = %+v, want used archive URI", got)
	}
	if got := status.Files[0].URIs[1]; got.Status != "waiting" || got.URI != "http://example.com/archive.iso" {
		t.Fatalf("second stopped URI = %+v, want waiting archive URI", got)
	}
}

func TestTellStoppedUsesFullSnapshot(t *testing.T) {
	e := newRPCTestEngine(t)

	gid, err := e.Add(AddSpec{URIs: []string{"http://example.com/archive.iso"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	e.fillRequestGroupFromReserver()
	if err := e.Remove(gid, false); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	stopped := e.TellStopped(0, 10)
	if len(stopped) != 1 {
		t.Fatalf("len(stopped) = %d, want 1", len(stopped))
	}
	if stopped[0].GID != gid {
		t.Fatalf("gid = %s, want %s", stopped[0].GID, gid)
	}
	if len(stopped[0].Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(stopped[0].Files))
	}
	if stopped[0].Dir == "" {
		t.Fatal("dir is empty")
	}
}

func TestMakeStoppedStatusCompletedLengthUsesCompletedPieces(t *testing.T) {
	rg := &requestGroup{
		gid:             1,
		opts:            &config.Options{},
		state:           core.StatusActive,
		totalLength:     1048576,
		completedLength: 1024,
		controlInfo: &btprogress.Info{
			PieceLength: 1048576,
			TotalLength: 1048576,
			Bitfield:    []byte{0x00},
		},
	}

	status := (&Engine{}).makeStoppedStatus(rg, core.StatusRemoved, core.ExitRemoved, "")

	if got := status.CompletedLength; got != 0 {
		t.Fatalf("CompletedLength = %d, want 0 from completed pieces", got)
	}
}

func TestMakeStoppedStatusCompletedLengthUsesSelectedPieces(t *testing.T) {
	const pieceLength = 1024
	rg := &requestGroup{
		gid:             1,
		opts:            &config.Options{},
		state:           core.StatusActive,
		totalLength:     3 * pieceLength,
		completedLength: 3 * pieceLength,
		fileEntries: []disk.FileEntry{
			{Name: "file1.bin", Length: pieceLength, Offset: 0, Requested: false},
			{Name: "file2.bin", Length: pieceLength, Offset: pieceLength, Requested: true},
			{Name: "file3.bin", Length: pieceLength, Offset: 2 * pieceLength, Requested: false},
		},
		controlInfo: &btprogress.Info{
			PieceLength: pieceLength,
			TotalLength: 3 * pieceLength,
			Bitfield:    []byte{0xe0},
		},
	}

	status := (&Engine{}).makeStoppedStatus(rg, core.StatusRemoved, core.ExitRemoved, "")

	if got := status.CompletedLength; got != pieceLength {
		t.Fatalf("CompletedLength = %d, want selected completed length %d", got, int64(pieceLength))
	}
}

func TestMakeStoppedStatusDuplicatesOnlyCurrentActiveURI(t *testing.T) {
	first := "http://mirror1.example/archive.iso"
	second := "http://mirror2.example/archive.iso"
	third := "http://mirror3.example/archive.iso"
	rg := &requestGroup{
		gid:        1,
		opts:       &config.Options{},
		state:      core.StatusActive,
		uris:       []string{first, second, third},
		filePath:   "archive.iso",
		activeURI:  first,
		activeURIs: []string{first, second},
		uriUsed:    true,
		haltReason: haltReasonUserRequest,
	}

	status := (&Engine{}).makeStoppedStatus(rg, core.StatusRemoved, core.ExitRemoved, "")

	want := []URIStatus{
		{URI: first, Status: "used"},
		{URI: first, Status: "waiting"},
		{URI: second, Status: "waiting"},
		{URI: third, Status: "waiting"},
	}
	if got := status.Files[0].URIs; !reflect.DeepEqual(got, want) {
		t.Fatalf("stopped URIs = %#v, want %#v", got, want)
	}
}

func TestMakeStatusPopulatesBittorrentFromCachedTorrentMetadata(t *testing.T) {
	pieces := make([]byte, 20)
	info := testBencodeDict(
		"name", bencode.StringVal{S: "payload.bin"},
		"piece length", bencode.IntVal{I: 8},
		"pieces", bencode.StringVal{S: string(pieces)},
		"length", bencode.IntVal{I: 8},
	)
	data, err := bencode.Marshal(testBencodeDict(
		"announce", bencode.StringVal{S: "udp://tracker1.example/announce"},
		"announce-list", bencode.ListVal{L: []bencode.Value{
			bencode.ListVal{L: []bencode.Value{
				bencode.StringVal{S: "udp://tracker1.example/announce"},
				bencode.StringVal{S: "udp://tracker2.example/announce"},
			}},
			bencode.ListVal{L: []bencode.Value{
				bencode.StringVal{S: "https://tracker3.example/announce"},
			}},
		}},
		"comment", bencode.StringVal{S: "test torrent"},
		"creation date", bencode.IntVal{I: 1700000000},
		"info", info,
	))
	if err != nil {
		t.Fatalf("marshal torrent: %v", err)
	}

	rg := &requestGroup{gid: 1, opts: &config.Options{}, state: core.StatusWaiting}
	cacheBTStatusMetadataForTest(t, rg, data)
	status := (&Engine{}).makeStatus(rg)

	if status.Bittorrent == nil {
		t.Fatal("Bittorrent is nil")
	}
	if got := status.Bittorrent["comment"]; got != "test torrent" {
		t.Fatalf("comment = %v, want test torrent", got)
	}
	if got := status.Bittorrent["creationDate"]; got != int64(1700000000) {
		t.Fatalf("creationDate = %v, want 1700000000", got)
	}
	if got := status.Bittorrent["mode"]; got != "single" {
		t.Fatalf("mode = %v, want single", got)
	}
	wantAnnounceList := [][]string{
		{"udp://tracker1.example/announce", "udp://tracker2.example/announce"},
		{"https://tracker3.example/announce"},
	}
	if got := status.Bittorrent["announceList"]; !reflect.DeepEqual(got, wantAnnounceList) {
		t.Fatalf("announceList = %#v, want %#v", got, wantAnnounceList)
	}
	infoDict, ok := status.Bittorrent["info"].(map[string]any)
	if !ok {
		t.Fatalf("info = %T, want map[string]any", status.Bittorrent["info"])
	}
	if got := infoDict["name"]; got != "payload.bin" {
		t.Fatalf("info.name = %v, want payload.bin", got)
	}
}

func TestMakeStatusDoesNotPopulateBittorrentFromOptionsOnly(t *testing.T) {
	status := (&Engine{}).makeStatus(&requestGroup{
		gid: 1,
		opts: &config.Options{BTTracker: []string{
			"udp://tracker1.example/announce",
			"https://tracker2.example/announce",
		}, TorrentFile: "/tmp/payload.torrent"},
		state: core.StatusWaiting,
	})

	if status.Bittorrent != nil {
		t.Fatalf("Bittorrent = %#v, want nil without parsed torrent metadata", status.Bittorrent)
	}
}

func TestMakeStatusOmitsBittorrentWithoutMetadata(t *testing.T) {
	status := (&Engine{}).makeStatus(&requestGroup{
		gid:   1,
		opts:  &config.Options{TorrentFile: "/tmp/payload.torrent"},
		state: core.StatusWaiting,
	})

	if status.Bittorrent != nil {
		t.Fatalf("Bittorrent = %#v, want nil without parsed torrent metadata", status.Bittorrent)
	}
}

func TestTellStatusTorrentMetadataCacheIncludesInfoHashBeforeControlInfo(t *testing.T) {
	e := newRPCTestEngine(t)
	data := testRPCStatusTorrent(t, "payload.bin", "udp://tracker.example/announce", nil)
	wantInfoHash := testTorrentInfoHash(t, data)

	gid, err := e.Add(AddSpec{
		Torrent: data,
		Options: &config.Options{Pause: true},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	status, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	if status.Bittorrent == nil {
		t.Fatal("Bittorrent is nil")
	}
	if status.InfoHash != wantInfoHash {
		t.Fatalf("InfoHash = %q, want %q", status.InfoHash, wantInfoHash)
	}
}

func TestMakeStatusAdjustsTorrentAnnounceListFromOptions(t *testing.T) {
	data := testRPCStatusTorrent(t, "payload.bin", "", [][]string{
		{"udp://tracker1.example/announce", "udp://tracker2.example/announce"},
		{"https://tracker3.example/announce"},
	})

	rg := &requestGroup{
		gid: 1,
		opts: &config.Options{
			BTExcludeTracker: []string{"udp://tracker1.example/announce,https://tracker3.example/announce"},
			BTTracker: []string{
				"udp://extra1.example/announce",
				"https://extra2.example/announce",
			},
		},
		state: core.StatusWaiting,
	}
	cacheBTStatusMetadataForTest(t, rg, data)
	status := (&Engine{}).makeStatus(rg)

	want := [][]string{
		{"udp://tracker2.example/announce"},
		{"udp://extra1.example/announce"},
		{"https://extra2.example/announce"},
	}
	if got := status.Bittorrent["announceList"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("announceList = %#v, want %#v", got, want)
	}
}

func TestTellStatusCachedBittorrentAnnounceListIgnoresLaterOptionChanges(t *testing.T) {
	e := newRPCTestEngine(t)
	data := testRPCStatusTorrent(t, "payload.bin", "", [][]string{
		{"udp://tracker1.example/announce", "udp://tracker2.example/announce"},
		{"https://tracker3.example/announce"},
	})

	gid, err := e.Add(AddSpec{
		Torrent: data,
		Options: &config.Options{
			Pause:            true,
			BTExcludeTracker: []string{"udp://tracker1.example/announce,https://tracker3.example/announce"},
			BTTracker: []string{
				"udp://extra1.example/announce",
				"https://extra2.example/announce",
			},
		},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := e.ChangeOption(gid, &config.Options{
		BTExcludeTracker: []string{"*"},
		BTTracker:        []string{"udp://changed.example/announce"},
	}); err != nil {
		t.Fatalf("ChangeOption() error = %v", err)
	}

	status, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	want := [][]string{
		{"udp://tracker2.example/announce"},
		{"udp://extra1.example/announce"},
		{"https://extra2.example/announce"},
	}
	if got := status.Bittorrent["announceList"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("announceList = %#v, want cached %#v", got, want)
	}
}

func TestMakeStoppedStatusUsesCachedBittorrentMetadataWithoutTorrentBytes(t *testing.T) {
	data := testRPCStatusTorrent(t, "payload.bin", "udp://tracker.example/announce", nil)
	meta, err := torrent.Load(data)
	if err != nil {
		t.Fatalf("load torrent: %v", err)
	}

	rg := &requestGroup{
		gid:   1,
		opts:  &config.Options{},
		state: core.StatusActive,
	}
	rg.cacheBTStatusMetadata(meta, rg.opts)

	status := (&Engine{}).makeStoppedStatus(rg, core.StatusComplete, core.ExitSuccess, "")
	if len(rg.torrent) != 0 {
		t.Fatalf("rg.torrent mutated to %d bytes", len(rg.torrent))
	}
	if status.Bittorrent == nil {
		t.Fatal("Bittorrent is nil")
	}
	if got := status.InfoHash; got != testTorrentInfoHash(t, data) {
		t.Fatalf("InfoHash = %q, want torrent metadata hash", got)
	}
	infoDict, ok := status.Bittorrent["info"].(map[string]any)
	if !ok {
		t.Fatalf("info = %T, want map[string]any", status.Bittorrent["info"])
	}
	if got := infoDict["name"]; got != "payload.bin" {
		t.Fatalf("info.name = %v, want payload.bin", got)
	}
	wantAnnounceList := [][]string{{"udp://tracker.example/announce"}}
	if got := status.Bittorrent["announceList"]; !reflect.DeepEqual(got, wantAnnounceList) {
		t.Fatalf("announceList = %#v, want %#v", got, wantAnnounceList)
	}
}

func testRPCStatusTorrent(t *testing.T, name, announce string, announceList [][]string) []byte {
	t.Helper()

	pieces := make([]byte, 20)
	info := testBencodeDict(
		"name", bencode.StringVal{S: name},
		"piece length", bencode.IntVal{I: 8},
		"pieces", bencode.StringVal{S: string(pieces)},
		"length", bencode.IntVal{I: 8},
	)
	pairs := []any{"info", info}
	if announce != "" {
		pairs = append([]any{"announce", bencode.StringVal{S: announce}}, pairs...)
	}
	if announceList != nil {
		tiers := make([]bencode.Value, 0, len(announceList))
		for _, tier := range announceList {
			urls := make([]bencode.Value, 0, len(tier))
			for _, url := range tier {
				urls = append(urls, bencode.StringVal{S: url})
			}
			tiers = append(tiers, bencode.ListVal{L: urls})
		}
		pairs = append([]any{"announce-list", bencode.ListVal{L: tiers}}, pairs...)
	}

	data, err := bencode.Marshal(testBencodeDict(pairs...))
	if err != nil {
		t.Fatalf("marshal torrent: %v", err)
	}
	return data
}

func testTorrentInfoHash(t *testing.T, data []byte) string {
	t.Helper()

	meta, err := torrent.Load(data)
	if err != nil {
		t.Fatalf("load torrent: %v", err)
	}
	infoHash, err := meta.InfoHash()
	if err != nil {
		t.Fatalf("info hash: %v", err)
	}
	return hex.EncodeToString(infoHash[:])
}

func cacheBTStatusMetadataForTest(t *testing.T, rg *requestGroup, data []byte) {
	t.Helper()

	meta, err := torrent.Load(data)
	if err != nil {
		t.Fatalf("load torrent: %v", err)
	}
	rg.cacheBTStatusMetadata(meta, rg.opts)
}
