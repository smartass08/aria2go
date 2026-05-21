package conformance

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/smartass08/aria2go/internal/protocol/metalink"
	"github.com/smartass08/aria2go/internal/torrent"
)

func TestProtocolFixtureStrategyCatalog(t *testing.T) {
	strategies := protocolFixtureStrategies()
	if len(strategies) != 4 {
		t.Fatalf("fixture strategy count = %d, want 4", len(strategies))
	}

	byName := make(map[string]protocolFixtureStrategy, len(strategies))
	for _, strategy := range strategies {
		byName[strategy.Name] = strategy
		if !strategy.Offline {
			t.Errorf("%s fixture is not marked offline", strategy.Name)
		}
		if strategy.Notes == "" {
			t.Errorf("%s fixture has empty notes", strategy.Name)
		}
	}

	for _, name := range []string{"FTP", "SFTP", "Metalink", "BitTorrent"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("fixture strategy %s missing", name)
		}
	}
	if byName["SFTP"].Ready {
		t.Fatal("SFTP should remain scaffold-only until a reusable SSH/SFTP fixture is available")
	}
	if !byName["FTP"].Differential || !byName["Metalink"].Differential {
		t.Fatal("FTP and Metalink should be marked ready for differential coverage")
	}
}

func TestProtocolFixtureGeneratedArtifactsParse(t *testing.T) {
	payload := protocolPayload("generated-artifacts", 65*1024)

	httpFixture := startProtocolHTTPFixture(t, map[string][]byte{
		"/payload.bin": payload,
	})
	defer httpFixture.Close()

	meta4 := protocolMetalinkV4(t, []protocolMetalinkFile{{
		Name: "payload.bin",
		URL:  httpFixture.URLPath("/payload.bin"),
		Data: payload,
	}})
	doc, err := metalink.Parse(bytes.NewReader(meta4))
	if err != nil {
		t.Fatalf("parse generated metalink: %v", err)
	}
	if len(doc.Files) != 1 || doc.Files[0].Name != "payload.bin" {
		t.Fatalf("generated metalink file entries = %#v", doc.Files)
	}

	torrentData, _, err := protocolTorrentSingleFile("http://127.0.0.1:9/announce", "payload.bin", payload, 16*1024)
	if err != nil {
		t.Fatalf("build generated torrent: %v", err)
	}
	meta, err := torrent.Load(torrentData)
	if err != nil {
		t.Fatalf("parse generated torrent: %v", err)
	}
	if meta.Info.Name != "payload.bin" || meta.Info.Length != int64(len(payload)) {
		t.Fatalf("generated torrent info = name %q length %d", meta.Info.Name, meta.Info.Length)
	}
}

func TestProtocol_FTPPassiveDownloadParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("ftp-passive-download", 96*1024)
	refFTP := startProtocolFTPFixture(t, map[string][]byte{"/fixture.bin": payload})
	implFTP := startProtocolFTPFixture(t, map[string][]byte{"/fixture.bin": payload})

	refDir := t.TempDir()
	implDir := t.TempDir()
	refArgs := append(protocolBaseArgs(refDir), "--out=fixture.bin", "--ftp-pasv=true", refFTP.URL("/fixture.bin"))
	implArgs := append(protocolBaseArgs(implDir), "--out=fixture.bin", "--ftp-pasv=true", implFTP.URL("/fixture.bin"))

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref FTP", ref)
	protocolRequireExitZero(t, "impl FTP", impl)
	protocolRequireFile(t, filepath.Join(refDir, "fixture.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "fixture.bin"), payload)
	protocolRequireFTPTransfer(t, "ref FTP", refFTP.snapshotCommands())
	protocolRequireFTPTransfer(t, "impl FTP", implFTP.snapshotCommands())
}

func TestProtocol_MetalinkHTTPDownloadParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("metalink-http-download", 80*1024)
	refHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/metalink.bin": payload})
	implHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/metalink.bin": payload})
	defer refHTTP.Close()
	defer implHTTP.Close()

	refDir := t.TempDir()
	implDir := t.TempDir()
	refMetalink := filepath.Join(refDir, "fixture.meta4")
	implMetalink := filepath.Join(implDir, "fixture.meta4")
	writeProtocolFixtureFile(t, refMetalink, protocolMetalinkV4(t, []protocolMetalinkFile{{
		Name: "metalink.bin",
		URL:  refHTTP.URLPath("/metalink.bin"),
		Data: payload,
	}}))
	writeProtocolFixtureFile(t, implMetalink, protocolMetalinkV4(t, []protocolMetalinkFile{{
		Name: "metalink.bin",
		URL:  implHTTP.URLPath("/metalink.bin"),
		Data: payload,
	}}))

	refArgs := append(protocolBaseArgs(refDir), "--metalink-file="+refMetalink)
	implArgs := append(protocolBaseArgs(implDir), "--metalink-file="+implMetalink)
	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref metalink", ref)
	protocolRequireExitZero(t, "impl metalink", impl)
	protocolRequireFile(t, filepath.Join(refDir, "metalink.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "metalink.bin"), payload)
}

func TestProtocol_BitTorrentFixtureScaffold(t *testing.T) {
	payload := protocolPayload("bittorrent-fixture", 48*1024)
	bt := startProtocolBTFixture(t, "bt-fixture.bin", payload, 16*1024)

	dir := t.TempDir()
	torrentPath := bt.writeTorrentFile(t, dir)
	if _, err := os.Stat(torrentPath); err != nil {
		t.Fatalf("stat torrent fixture: %v", err)
	}
	meta, err := torrent.Load(bt.TorrentData)
	if err != nil {
		t.Fatalf("parse bittorrent fixture: %v", err)
	}
	infoHash, err := meta.InfoHash()
	if err != nil {
		t.Fatalf("infohash: %v", err)
	}
	if infoHash != bt.InfoHash {
		t.Fatalf("infohash mismatch: meta=%x fixture=%x", infoHash, bt.InfoHash)
	}
}

func protocolRequireFTPTransfer(t *testing.T, label string, commands []string) {
	t.Helper()

	seen := make(map[string]bool, len(commands))
	for _, cmd := range commands {
		seen[cmd] = true
	}
	for _, cmd := range []string{"USER", "PASS", "TYPE", "RETR"} {
		if !seen[cmd] {
			t.Fatalf("%s did not issue %s; commands=%v", label, cmd, commands)
		}
	}
	if !seen["EPSV"] && !seen["PASV"] {
		t.Fatalf("%s did not issue EPSV or PASV; commands=%v", label, commands)
	}
}

func writeProtocolFixtureFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
