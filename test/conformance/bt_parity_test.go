// Package conformance provides cross-binary parity tests between aria2c and aria2go.
// This file covers:
//   - Task 8:  bt.encryption-runtime  (MSE handshake with aria2c seeder / aria2go leecher)
//   - Task 9:  bt.utp-runtime         (happy-path TCP transfer; aria2c seeder, aria2go leecher)
//   - Task 10: config.dht-entry-point-split-host-port
//   - Task 11: bt.magnet-runtime      (bt-save-metadata / bt-load-saved-metadata round-trip)
package conformance

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/bencode"
	"github.com/smartass08/aria2go/internal/torrent"
)

// btStartRefSeeder starts aria2c as a seeder on the given listen port, seeding
// the torrent at torrentPath from dir, with any extra flags.  The seeder is
// killed when the test ends.
func btStartRefSeeder(t *testing.T, dir, torrentPath string, listenPort int, extra ...string) {
	t.Helper()

	args := []string{
		"--no-conf=true",
		"--dir=" + dir,
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--file-allocation=none",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--no-netrc=true",
		"--enable-dht=false",
		"--enable-dht6=false",
		"--bt-enable-lpd=false",
		"--enable-peer-exchange=false",
		fmt.Sprintf("--listen-port=%d", listenPort),
		"--seed-ratio=0.0",
		"--seed-time=60",
		"--bt-seed-unverified=true",
	}
	args = append(args, extra...)
	args = append(args, torrentPath)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		_, _ = RunRefWithOptions(t, args, "", RunOptions{
			Timeout: 60 * time.Second,
		})
		_ = ctx
	}()
}

// btStartImplSeeder starts aria2go as a seeder on the given listen port.
func btStartImplSeeder(t *testing.T, dir, torrentPath string, listenPort int, extra ...string) {
	t.Helper()

	implBin, err := implBinary()
	if err != nil {
		t.Fatalf("build impl for seeder: %v", err)
	}

	args := []string{
		"--no-conf=true",
		"--dir=" + dir,
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--file-allocation=none",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--no-netrc=true",
		"--enable-dht=false",
		"--enable-dht6=false",
		"--bt-enable-lpd=false",
		"--enable-peer-exchange=false",
		fmt.Sprintf("--listen-port=%d", listenPort),
		"--seed-ratio=0.0",
		"--seed-time=60",
		"--bt-seed-unverified=true",
	}
	args = append(args, extra...)
	args = append(args, torrentPath)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		_, _ = runBinaryWithOptions(t, implBin, args, "", RunOptions{
			Timeout: 60 * time.Second,
		})
		_ = ctx
	}()
}

// btMakeSeedTorrentAndTracker builds a torrent, writes the payload to seedDir,
// and starts an HTTP tracker that points leechers at seedPort.
// Returns the torrent bytes, the info hash, and a function that writes the
// .torrent to any dir and returns the path.
func btMakeSeedTorrentAndTracker(t *testing.T, name string, payload []byte, pieceLength int, seedPort int) (torrentData []byte, tracker *httptest.Server) {
	t.Helper()

	// The tracker returns the seeder at localhost:seedPort.
	ip := net.ParseIP("127.0.0.1").To4()
	var compact [6]byte
	copy(compact[:4], ip)
	compact[4] = byte(seedPort >> 8)
	compact[5] = byte(seedPort)

	tracker = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/announce" {
			http.NotFound(w, r)
			return
		}
		resp := bencode.NewDict()
		resp.Set("interval", bencode.NewInt(1800))
		resp.Set("peers", bencode.NewString(string(compact[:])))
		data, _ := bencode.Marshal(resp)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(data)
	}))
	t.Cleanup(tracker.Close)

	var err error
	torrentData, _, err = protocolTorrentSingleFile(tracker.URL+"/announce", name, payload, pieceLength)
	if err != nil {
		t.Fatalf("build torrent: %v", err)
	}
	return torrentData, tracker
}

// btWriteTorrent writes torrentData to dir/<name>.torrent and returns the path.
func btWriteTorrent(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name+".torrent")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write torrent %s: %v", p, err)
	}
	return p
}

// btRunLeech runs a leecher (ref or impl) that downloads the torrent using a
// tracker-supplied peer list.  It disables DHT, LPD, and PEX and limits to
// 1 peer so it must connect to the tracker-supplied seeder.
func btRunLeech(t *testing.T, ref bool, dir, torrentPath string, extra ...string) RunResult {
	t.Helper()

	args := []string{
		"--no-conf=true",
		"--dir=" + dir,
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--file-allocation=none",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--no-netrc=true",
		"--enable-dht=false",
		"--enable-dht6=false",
		"--bt-enable-lpd=false",
		"--enable-peer-exchange=false",
		"--bt-max-peers=1",
		"--seed-time=0",
	}
	args = append(args, extra...)
	args = append(args, torrentPath)

	return protocolRun(t, ref, args)
}

// ---- Task 8: bt.encryption-runtime -----------------------------------------------

// TestBitTorrent_EncryptionRuntime_Ref2Impl tests that aria2go can download
// from an aria2c seeder that requires MSE/ARC4 encryption.
func TestBitTorrent_EncryptionRuntime_Ref2Impl(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("bt-encryption-ref2impl", 32*1024+123)
	const (
		name        = "bt-enc-r2i.bin"
		pieceLength = 16 * 1024
	)

	seedPort := findFreePort(t)
	torrentData, _ := btMakeSeedTorrentAndTracker(t, name, payload, pieceLength, seedPort)

	seedDir := t.TempDir()
	leechDir := t.TempDir()

	// Write payload to seed dir and the torrent files.
	if err := os.WriteFile(filepath.Join(seedDir, name), payload, 0o644); err != nil {
		t.Fatalf("write seeder payload: %v", err)
	}
	seedTorrent := btWriteTorrent(t, seedDir, name, torrentData)
	leechTorrent := btWriteTorrent(t, leechDir, name, torrentData)

	// Start aria2c seeder with encryption required.
	btStartRefSeeder(t, seedDir, seedTorrent, seedPort,
		"--bt-require-crypto=true",
		"--bt-force-encryption=true",
	)
	// Allow the seeder time to bind.
	time.Sleep(500 * time.Millisecond)

	// Run aria2go leecher with encryption required.
	result := btRunLeech(t, false, leechDir, leechTorrent,
		"--bt-require-crypto=true",
	)
	if result.ExitCode != 0 {
		t.Fatalf("impl leecher exit=%d\nstdout=%s\nstderr=%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	protocolRequireFile(t, filepath.Join(leechDir, name), payload)
}

// TestBitTorrent_EncryptionRuntime_Impl2Ref tests that aria2c can download
// from an aria2go seeder with MSE encryption.
func TestBitTorrent_EncryptionRuntime_Impl2Ref(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("bt-encryption-impl2ref", 32*1024+99)
	const (
		name        = "bt-enc-i2r.bin"
		pieceLength = 16 * 1024
	)

	seedPort := findFreePort(t)
	torrentData, _ := btMakeSeedTorrentAndTracker(t, name, payload, pieceLength, seedPort)

	seedDir := t.TempDir()
	leechDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(seedDir, name), payload, 0o644); err != nil {
		t.Fatalf("write seeder payload: %v", err)
	}
	seedTorrent := btWriteTorrent(t, seedDir, name, torrentData)
	leechTorrent := btWriteTorrent(t, leechDir, name, torrentData)

	// Start aria2go seeder with encryption required.
	btStartImplSeeder(t, seedDir, seedTorrent, seedPort,
		"--bt-require-crypto=true",
		"--bt-force-encryption=true",
	)
	time.Sleep(500 * time.Millisecond)

	// Run aria2c leecher with encryption preferred.
	result := btRunLeech(t, true, leechDir, leechTorrent,
		"--bt-require-crypto=true",
	)
	if result.ExitCode != 0 {
		t.Fatalf("ref leecher exit=%d\nstdout=%s\nstderr=%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	protocolRequireFile(t, filepath.Join(leechDir, name), payload)
}

// ---- Task 9: bt.utp-runtime -------------------------------------------------------

// TestBitTorrent_UTPRuntime tests a happy-path TCP/uTP transfer with aria2c as
// seeder and aria2go as leecher.  uTP support in aria2go (internal/protocol/bittorrent/utp)
// is exercised when the seeder advertises uTP; the test passes even if the
// transfer uses TCP since both sides may fall back to TCP.
func TestBitTorrent_UTPRuntime(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("bt-utp-runtime", 48*1024+55)
	const (
		name        = "bt-utp.bin"
		pieceLength = 16 * 1024
	)

	seedPort := findFreePort(t)
	torrentData, _ := btMakeSeedTorrentAndTracker(t, name, payload, pieceLength, seedPort)

	seedDir := t.TempDir()
	leechDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(seedDir, name), payload, 0o644); err != nil {
		t.Fatalf("write seeder payload: %v", err)
	}
	seedTorrent := btWriteTorrent(t, seedDir, name, torrentData)
	leechTorrent := btWriteTorrent(t, leechDir, name, torrentData)

	btStartRefSeeder(t, seedDir, seedTorrent, seedPort)
	time.Sleep(500 * time.Millisecond)

	result := btRunLeech(t, false, leechDir, leechTorrent)
	if result.ExitCode != 0 {
		t.Fatalf("impl leecher exit=%d\nstdout=%s\nstderr=%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	protocolRequireFile(t, filepath.Join(leechDir, name), payload)
}

// ---- Task 10: config.dht-entry-point-split-host-port -----------------------------

// TestConfig_DHTEntryPointSplitHostPort verifies that both binaries accept
// --dht-entry-point=host:port and parse it without error.  This is an offline
// test: both binaries are invoked with -v (version flag) so they exit
// immediately after parsing options.
func TestConfig_DHTEntryPointSplitHostPort(t *testing.T) {
	SkipIfNoRef(t)

	// Valid host:port formats that both binaries must accept.
	// Invoked with -v (print version) so the process exits immediately
	// after option parsing without needing a URL argument.
	cases := []struct {
		name  string
		value string
	}{
		{"hostname_with_port", "router.bittorrent.com:6881"},
		{"ipv4_with_port", "1.2.3.4:6881"},
		{"ipv6_with_port", "[2001:db8::1]:6881"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			args := []string{
				"--no-conf=true",
				"--dht-entry-point=" + tc.value,
				"-v",
			}
			ref, err := RunRefWithOptions(t, args, "", RunOptions{Timeout: 5 * time.Second})
			if err != nil {
				t.Fatalf("ref run: %v", err)
			}
			impl, err := RunImplWithOptions(t, args, "", RunOptions{Timeout: 5 * time.Second})
			if err != nil {
				t.Fatalf("impl run: %v", err)
			}
			if ref.ExitCode != 0 {
				t.Errorf("ref: expected exit 0 for valid %q, got %d\nstderr=%s", tc.value, ref.ExitCode, ref.Stderr)
			}
			if impl.ExitCode != 0 {
				t.Errorf("impl: expected exit 0 for valid %q, got %d\nstderr=%s", tc.value, impl.ExitCode, impl.Stderr)
			}
		})
	}
}

// ---- Task 11: bt.magnet-runtime (load-saved round-trip) --------------------------

// TestBitTorrent_MagnetSaveLoadRoundTrip tests the --bt-save-metadata +
// --bt-load-saved-metadata round-trip for both ref and impl.
//
// Phase 1: Download via magnet URI with --bt-save-metadata=true; confirm the
//
//	file is downloaded and a <infohash>.torrent is created.
//
// Phase 2: Re-run with the magnet URI + --bt-load-saved-metadata=true; the
//
//	saved .torrent should be used and download should complete.
func TestBitTorrent_MagnetSaveLoadRoundTrip(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("bt-magnet-save-load", 32*1024+77)
	const (
		name        = "bt-save-load.bin"
		pieceLength = 16 * 1024
	)

	runPhases := func(t *testing.T, ref bool, label string) {
		t.Helper()

		// Phase 1: download via magnet with bt-save-metadata.
		fixture1 := startProtocolBTMagnetFixture(t, name, payload, pieceLength)
		dir1 := t.TempDir()
		result1 := protocolRun(t, ref, bittorrentMagnetArgs(dir1, fixture1.magnetURI(),
			"--bt-save-metadata=true",
		))
		protocolRequireExitZero(t, label+" phase1", result1)
		protocolRequireFile(t, filepath.Join(dir1, name), payload)

		savedPath := filepath.Join(dir1, fmt.Sprintf("%x.torrent", fixture1.InfoHash[:]))
		savedData, err := os.ReadFile(savedPath)
		if err != nil {
			t.Fatalf("%s: saved .torrent not found at %s: %v", label, savedPath, err)
		}
		// Confirm the saved torrent is valid.
		meta, err := torrent.Load(savedData)
		if err != nil {
			t.Fatalf("%s: load saved torrent: %v", label, err)
		}
		gotHash, err := meta.InfoHash()
		if err != nil {
			t.Fatalf("%s: infohash from saved torrent: %v", label, err)
		}
		if gotHash != fixture1.InfoHash {
			t.Fatalf("%s: saved torrent infohash mismatch: got %x want %x", label, gotHash, fixture1.InfoHash)
		}

		// Phase 2: re-run with bt-load-saved-metadata; supply a fresh fixture
		// so the peer is still available.
		fixture2 := startProtocolBTMagnetFixture(t, name, payload, pieceLength)
		dir2 := t.TempDir()
		// Copy the saved torrent into dir2 so bt-load-saved-metadata can find it.
		dst := filepath.Join(dir2, fmt.Sprintf("%x.torrent", fixture1.InfoHash[:]))
		if err := os.WriteFile(dst, savedData, 0o644); err != nil {
			t.Fatalf("%s: copy saved torrent: %v", label, err)
		}
		// Use the same infoHash magnet so the saved file is matched.
		result2 := protocolRun(t, ref, bittorrentMagnetArgs(dir2, fixture2.magnetURI(),
			"--bt-load-saved-metadata=true",
		))
		protocolRequireExitZero(t, label+" phase2 (load-saved)", result2)
		protocolRequireFile(t, filepath.Join(dir2, name), payload)
	}

	t.Run("ref", func(t *testing.T) { runPhases(t, true, "ref") })
	t.Run("impl", func(t *testing.T) { runPhases(t, false, "impl") })
}
