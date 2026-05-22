package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/disk"
	"github.com/smartass08/aria2go/internal/hash"
)

func TestAddRejectsInvalidChecksumSpec(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = e.Add(AddSpec{
		URIs:    []string{"http://example.com/file.bin"},
		Options: &config.Options{Checksum: "sha-1=xyz"},
	})
	if err == nil {
		t.Fatal("Add() error = nil, want invalid checksum error")
	}
}

func TestChooseAllocatorRespectsNoFileAllocationLimit(t *testing.T) {
	opts := &config.Options{
		FileAllocation:        "falloc",
		NoFileAllocationLimit: "5M",
	}

	if got := chooseAllocator(opts, 4*1024*1024).Name(); got != "none" {
		t.Fatalf("allocator below limit = %q, want none", got)
	}
	if got := chooseAllocator(opts, 5*1024*1024).Name(); got != "falloc" {
		t.Fatalf("allocator at limit = %q, want falloc", got)
	}
	if got := chooseAllocator(&config.Options{FileAllocation: "trunc"}, 32).Name(); got != "trunc" {
		t.Fatalf("allocator trunc = %q, want trunc", got)
	}
	if got := chooseAllocator(&config.Options{}, 32).Name(); got != "prealloc" {
		t.Fatalf("default allocator = %q, want prealloc", got)
	}
}

func TestEffectiveLowestSpeedLimitAdaptive(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("lowered to quarter of known max", func(t *testing.T) {
		stat := NewServerStat("fast.example", "http")
		stat.SetDownloadSpeed(96 * 1024)
		stat.SetSingleConnectionAvgSpeed(160 * 1024)
		e.serverStats.Add(stat)

		rg := &requestGroup{opts: &config.Options{
			LowestSpeedLimit: "100K",
			URISelector:      "adaptive",
		}}

		got := e.effectiveLowestSpeedLimit(rg, []string{"http://fast.example/file.bin"})
		want := int64(40 * 1024)
		if got != want {
			t.Fatalf("effectiveLowestSpeedLimit() = %d, want %d", got, want)
		}
	})

	t.Run("lowered to 4K with no server stat clue", func(t *testing.T) {
		rg := &requestGroup{opts: &config.Options{
			LowestSpeedLimit: "20K",
			URISelector:      "adaptive",
		}}

		got := e.effectiveLowestSpeedLimit(rg, []string{"http://unknown.example/file.bin"})
		want := int64(4 * 1024)
		if got != want {
			t.Fatalf("effectiveLowestSpeedLimit() = %d, want %d", got, want)
		}
	})
}

func TestSelectDownloadURIsFeedbackUsesServerStatsAndCaps(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	fast := NewServerStat("fast.example", "http")
	fast.SetDownloadSpeed(64 * 1024)
	e.serverStats.Add(fast)

	bad := NewServerStat("bad.example", "http")
	bad.SetError()
	e.serverStats.Add(bad)

	rg := &requestGroup{
		gid: 1,
		opts: &config.Options{
			URISelector:            "feedback",
			MaxConnectionPerServer: 1,
		},
		uris: []string{
			"http://slow.example/file.bin",
			"http://fast.example/file.bin",
			"http://bad.example/file.bin",
		},
	}

	got := e.selectDownloadURIs(rg, 2)
	want := []string{
		"http://fast.example/file.bin",
		"http://slow.example/file.bin",
	}
	if len(got) != len(want) {
		t.Fatalf("selectDownloadURIs() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selectDownloadURIs()[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestSelectDownloadURIsReuseURIRespectsPerHostLimit(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	rg := &requestGroup{
		gid: 1,
		opts: &config.Options{
			URISelector:            "inorder",
			MaxConnectionPerServer: 2,
			ReuseURI:               true,
		},
		uris: []string{
			"http://one.example/file.bin",
			"http://two.example/file.bin",
		},
	}

	got := e.selectDownloadURIs(rg, 3)
	want := []string{
		"http://one.example/file.bin",
		"http://two.example/file.bin",
		"http://one.example/file.bin",
	}
	if len(got) != len(want) {
		t.Fatalf("selectDownloadURIs() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selectDownloadURIs()[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestSelectDownloadURIsSkipsBlockedResumeFallbackURIs(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	rg := &requestGroup{
		gid: 1,
		opts: &config.Options{
			URISelector:            "inorder",
			MaxConnectionPerServer: 1,
		},
		uris: []string{
			"http://first.example/file.bin",
			"http://second.example/file.bin",
		},
		resumeBlockedURIs: map[string]struct{}{
			"http://first.example/file.bin": {},
		},
	}

	got := e.selectDownloadURIs(rg, 1)
	if len(got) != 1 || got[0] != "http://second.example/file.bin" {
		t.Fatalf("selectDownloadURIs() = %v, want second mirror only", got)
	}
}

func TestNextHTTPResumeFallbackURIUsesOtherMirrorBeforeScratchRestart(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	rg := &requestGroup{
		opts: &config.Options{
			MaxResumeFailureTries: 2,
		},
		uris: []string{
			"http://first.example/file.bin",
			"http://second.example/file.bin",
			"http://third.example/file.bin",
		},
	}

	next, restart := e.nextHTTPResumeFallbackURI(rg, "http://first.example/file.bin", 0)
	if restart {
		t.Fatal("nextHTTPResumeFallbackURI() restarted scratch before trying another URI")
	}
	if next != "http://second.example/file.bin" {
		t.Fatalf("nextHTTPResumeFallbackURI() = %q, want second URI", next)
	}
	if rg.resumeFailureCount != 1 {
		t.Fatalf("resumeFailureCount = %d, want 1", rg.resumeFailureCount)
	}

	next, restart = e.nextHTTPResumeFallbackURI(rg, next, 0)
	if next != "" || !restart {
		t.Fatalf("second nextHTTPResumeFallbackURI() = (%q, %v), want scratch restart", next, restart)
	}
}

func TestSpeedGuardUsesCurrentSampleInsteadOfLifetimeAverage(t *testing.T) {
	guard := newSpeedGuard(8*1024, 0, "fast.example")
	now := time.Now()
	guard.start = now.Add(-10 * time.Second)
	guard.sampleStart = now.Add(-2 * time.Second)
	guard.sampleBytes = 24 * 1024

	if err := guard.Add(1); err != nil {
		t.Fatalf("Add() error = %v, want nil for fast current sample", err)
	}
}

func TestSpeedGuardRejectsSlowCurrentSample(t *testing.T) {
	guard := newSpeedGuard(8*1024, 0, "slow.example")
	now := time.Now()
	guard.start = now.Add(-2 * time.Second)
	guard.sampleStart = now.Add(-2 * time.Second)
	guard.sampleBytes = 2 * 1024

	if err := guard.Add(1); err == nil {
		t.Fatal("Add() error = nil, want slow sample failure")
	}
}

func TestVerifyIntegrityPrefersPieceHashesOverWholeChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.bin")
	data := []byte("abcdefgh")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	pieceHashes := [][]byte{
		mustDigest(t, hash.SHA1, data[:4]),
		mustDigest(t, hash.SHA1, data[4:]),
	}

	adaptor, err := disk.NewSingleFile(path, int64(len(data)), disk.AllocatorNone{})
	if err != nil {
		t.Fatalf("NewSingleFile() error = %v", err)
	}
	defer adaptor.Close()

	rg := &requestGroup{
		totalLength: int64(len(data)),
		integrity: downloadIntegrity{
			wholeKind:   hash.MD5,
			wholeDigest: bytes.Repeat([]byte{0xff}, hash.MD5.Size()),
			pieceKind:   hash.SHA1,
			pieceHashes: pieceHashes,
			pieceLen:    4,
		},
	}

	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	mode, bad, err := e.verifyIntegrity(context.Background(), rg, adaptor, path)
	if err != nil {
		t.Fatalf("verifyIntegrity() error = %v", err)
	}
	if mode != "piece" {
		t.Fatalf("verifyIntegrity() mode = %q, want piece", mode)
	}
	if len(bad) != 0 {
		t.Fatalf("verifyIntegrity() bad pieces = %v, want none", bad)
	}
}

func TestServerStatsSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.stats")

	cfg := testOpts()
	cfg.ServerStatOf = path
	e, err := New(cfg, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	stat := NewServerStat("mirror.example", "http")
	stat.SetDownloadSpeed(96 * 1024)
	stat.SetSingleConnectionAvgSpeed(64 * 1024)
	stat.SetMultiConnectionAvgSpeed(128 * 1024)
	stat.SetCounter(3)
	e.serverStats.Add(stat)
	if err := e.saveServerStats(); err != nil {
		t.Fatalf("saveServerStats() error = %v", err)
	}

	cfg2 := testOpts()
	cfg2.ServerStatIf = path
	loadedEngine, err := New(cfg2, testLogger(t))
	if err != nil {
		t.Fatalf("New(load) error = %v", err)
	}
	loaded := loadedEngine.serverStats.Find("mirror.example", "http")
	if loaded == nil {
		t.Fatal("loaded server stat = nil")
	}
	if loaded.DownloadSpeed() != 96*1024 {
		t.Fatalf("loaded DownloadSpeed = %d, want %d", loaded.DownloadSpeed(), 96*1024)
	}
	if loaded.SingleConnectionAvgSpeed() != 64*1024 {
		t.Fatalf("loaded SingleConnectionAvgSpeed = %d, want %d", loaded.SingleConnectionAvgSpeed(), 64*1024)
	}
	if loaded.MultiConnectionAvgSpeed() != 128*1024 {
		t.Fatalf("loaded MultiConnectionAvgSpeed = %d, want %d", loaded.MultiConnectionAvgSpeed(), 128*1024)
	}
	if loaded.Counter() != 3 {
		t.Fatalf("loaded Counter = %d, want 3", loaded.Counter())
	}
}

func TestChangeGlobalOptionUpdatesRateLimits(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = e.ChangeGlobalOption(&config.Options{
		MaxOverallDownloadLimit: "32K",
		MaxOverallUploadLimit:   "16K",
	})
	if err != nil {
		t.Fatalf("ChangeGlobalOption() error = %v", err)
	}
	if got := e.rateGlobal.rate.Load(); got != 32*1024 {
		t.Fatalf("rateGlobal = %d, want %d", got, 32*1024)
	}
	if got := e.rateGlobalUp.rate.Load(); got != 16*1024 {
		t.Fatalf("rateGlobalUp = %d, want %d", got, 16*1024)
	}
}

func mustDigest(t *testing.T, kind hash.Kind, data []byte) []byte {
	t.Helper()
	sum, err := hash.SumReader(bytes.NewReader(data), kind)
	if err != nil {
		t.Fatalf("SumReader(%s) error = %v", kind, err)
	}
	return sum
}
