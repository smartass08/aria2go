package conformance

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	protocolRequireFTPPASVOnly(t, "ref FTP", refFTP.snapshotCommands())
	protocolRequireFTPPASVOnly(t, "impl FTP", implFTP.snapshotCommands())
}

func TestProtocol_FTPProxyMethodGETParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("ftp-proxy-get", 48*1024+11)
	refProxy := startProtocolFTPProxyFixture(t, map[string][]byte{"/proxy.bin": payload})
	implProxy := startProtocolFTPProxyFixture(t, map[string][]byte{"/proxy.bin": payload})

	target := "ftp://example.invalid/proxy.bin"
	refDir := t.TempDir()
	implDir := t.TempDir()
	refArgs := append(protocolBaseArgs(refDir), "--out=proxy.bin", "--all-proxy="+refProxy.URL(), target)
	implArgs := append(protocolBaseArgs(implDir), "--out=proxy.bin", "--all-proxy="+implProxy.URL(), target)

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref FTP proxy GET", ref)
	protocolRequireExitZero(t, "impl FTP proxy GET", impl)
	protocolRequireFile(t, filepath.Join(refDir, "proxy.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "proxy.bin"), payload)
	protocolRequireProxyFTPURL(t, "ref FTP proxy GET", refProxy.snapshotRequests(), http.MethodGet, target)
	protocolRequireProxyFTPURL(t, "impl FTP proxy GET", implProxy.snapshotRequests(), http.MethodGet, target)
}

func TestProtocol_FTPProxyMethodTunnelParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("ftp-proxy-tunnel", 40*1024+29)
	refFTP := startProtocolFTPFixture(t, map[string][]byte{"/fixture.bin": payload})
	implFTP := startProtocolFTPFixture(t, map[string][]byte{"/fixture.bin": payload})
	refProxy := startProtocolFTPProxyFixture(t, nil)
	implProxy := startProtocolFTPProxyFixture(t, nil)

	refDir := t.TempDir()
	implDir := t.TempDir()
	refArgs := append(protocolBaseArgs(refDir), "--no-proxy=", "--out=fixture.bin", "--all-proxy="+refProxy.URL(), "--proxy-method=tunnel", refFTP.URL("/fixture.bin"))
	implArgs := append(protocolBaseArgs(implDir), "--no-proxy=", "--out=fixture.bin", "--all-proxy="+implProxy.URL(), "--proxy-method=tunnel", implFTP.URL("/fixture.bin"))

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref FTP proxy tunnel", ref)
	protocolRequireExitZero(t, "impl FTP proxy tunnel", impl)
	protocolRequireFile(t, filepath.Join(refDir, "fixture.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "fixture.bin"), payload)
	protocolRequireProxyMethodTarget(t, "ref FTP proxy tunnel", refProxy.snapshotRequests(), "CONNECT", refFTP.ln.Addr().String())
	protocolRequireProxyMethodTarget(t, "impl FTP proxy tunnel", implProxy.snapshotRequests(), "CONNECT", implFTP.ln.Addr().String())
	protocolRequireFTPTransfer(t, "ref FTP proxy tunnel", refFTP.snapshotCommands())
	protocolRequireFTPTransfer(t, "impl FTP proxy tunnel", implFTP.snapshotCommands())
}

func TestProtocol_FTPActiveDownloadParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("ftp-active-download", 64*1024+23)
	refFTP := startProtocolFTPFixture(t, map[string][]byte{"/fixture.bin": payload})
	implFTP := startProtocolFTPFixture(t, map[string][]byte{"/fixture.bin": payload})

	refDir := t.TempDir()
	implDir := t.TempDir()
	refArgs := append(protocolBaseArgs(refDir), "--out=fixture.bin", "--ftp-pasv=false", refFTP.URL("/fixture.bin"))
	implArgs := append(protocolBaseArgs(implDir), "--out=fixture.bin", "--ftp-pasv=false", implFTP.URL("/fixture.bin"))

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref FTP active", ref)
	protocolRequireExitZero(t, "impl FTP active", impl)
	protocolRequireFile(t, filepath.Join(refDir, "fixture.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "fixture.bin"), payload)
	protocolRequireFTPActiveTransfer(t, "ref FTP active", refFTP.snapshotCommands())
	protocolRequireFTPActiveTransfer(t, "impl FTP active", implFTP.snapshotCommands())
}

func TestProtocol_FTPSizeUnsupportedDownloadParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("ftp-size-unsupported", 72*1024+19)
	refFTP := startProtocolFTPFixtureWithOptions(t, map[string][]byte{"/fixture.bin": payload}, protocolFTPFixtureOptions{
		SupportSize: false,
	})
	implFTP := startProtocolFTPFixtureWithOptions(t, map[string][]byte{"/fixture.bin": payload}, protocolFTPFixtureOptions{
		SupportSize: false,
	})

	refDir := t.TempDir()
	implDir := t.TempDir()
	refArgs := append(protocolBaseArgs(refDir), "--out=fixture.bin", "--ftp-pasv=true", refFTP.URL("/fixture.bin"))
	implArgs := append(protocolBaseArgs(implDir), "--out=fixture.bin", "--ftp-pasv=true", implFTP.URL("/fixture.bin"))

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref FTP size unsupported", ref)
	protocolRequireExitZero(t, "impl FTP size unsupported", impl)
	protocolRequireFile(t, filepath.Join(refDir, "fixture.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "fixture.bin"), payload)
	protocolRequireFTPTransfer(t, "ref FTP size unsupported", refFTP.snapshotCommands())
	protocolRequireFTPTransfer(t, "impl FTP size unsupported", implFTP.snapshotCommands())
	protocolRequireFTPCommandArg(t, "ref FTP size unsupported", refFTP.snapshotCommandDetails(), "SIZE", "/fixture.bin")
	protocolRequireFTPCommandArg(t, "impl FTP size unsupported", implFTP.snapshotCommandDetails(), "SIZE", "/fixture.bin")
}

func TestProtocol_FTPPercentDecodedPathParity(t *testing.T) {
	SkipIfNoRef(t)

	const name = "space # file.bin"
	payload := protocolPayload("ftp-percent-decoded-path", 40*1024+7)
	refFTP := startProtocolFTPFixture(t, map[string][]byte{"/" + name: payload})
	implFTP := startProtocolFTPFixture(t, map[string][]byte{"/" + name: payload})

	refDir := t.TempDir()
	implDir := t.TempDir()
	ref := protocolRun(t, true, append(protocolBaseArgs(refDir), "--ftp-pasv=true", refFTP.URL("/"+name)))
	impl := protocolRun(t, false, append(protocolBaseArgs(implDir), "--ftp-pasv=true", implFTP.URL("/"+name)))

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref FTP percent-decoded path", ref)
	protocolRequireExitZero(t, "impl FTP percent-decoded path", impl)
	protocolRequireFile(t, filepath.Join(refDir, name), payload)
	protocolRequireFile(t, filepath.Join(implDir, name), payload)
	protocolRequireFTPCommandArg(t, "ref FTP percent-decoded path", refFTP.snapshotCommandDetails(), "RETR", "/"+name)
	protocolRequireFTPCommandArg(t, "impl FTP percent-decoded path", implFTP.snapshotCommandDetails(), "RETR", "/"+name)
}

func TestProtocol_FTPRemoteTimeParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("ftp-remote-time", 56*1024+13)
	refFTP := startProtocolFTPFixture(t, map[string][]byte{"/remote-time.bin": payload})
	implFTP := startProtocolFTPFixture(t, map[string][]byte{"/remote-time.bin": payload})

	refDir := t.TempDir()
	implDir := t.TempDir()
	refArgs := append(protocolBaseArgs(refDir), "--out=remote-time.bin", "--ftp-pasv=true", "--remote-time=true", refFTP.URL("/remote-time.bin"))
	implArgs := append(protocolBaseArgs(implDir), "--out=remote-time.bin", "--ftp-pasv=true", "--remote-time=true", implFTP.URL("/remote-time.bin"))

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref FTP remote-time", ref)
	protocolRequireExitZero(t, "impl FTP remote-time", impl)
	protocolRequireFile(t, filepath.Join(refDir, "remote-time.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "remote-time.bin"), payload)
	conformanceRequireFileModTimeNear(t, "ref FTP remote-time", filepath.Join(refDir, "remote-time.bin"), protocolFTPMTime())
	conformanceRequireFileModTimeNear(t, "impl FTP remote-time", filepath.Join(implDir, "remote-time.bin"), protocolFTPMTime())
	protocolRequireFTPCommandArg(t, "ref FTP remote-time", refFTP.snapshotCommandDetails(), "MDTM", "/remote-time.bin")
	protocolRequireFTPCommandArg(t, "impl FTP remote-time", implFTP.snapshotCommandDetails(), "MDTM", "/remote-time.bin")
}

func TestProtocol_FTPTypeCommandParity(t *testing.T) {
	SkipIfNoRef(t)

	cases := []struct {
		name    string
		ftpType string
		typeArg string
	}{
		{name: "ascii", ftpType: "ascii", typeArg: "A"},
		{name: "binary", ftpType: "binary", typeArg: "I"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte("alpha\r\nbeta\r\ngamma\r\n")
			refFTP := startProtocolFTPFixture(t, map[string][]byte{"/fixture.txt": payload})
			implFTP := startProtocolFTPFixture(t, map[string][]byte{"/fixture.txt": payload})

			refDir := t.TempDir()
			implDir := t.TempDir()
			refArgs := append(protocolBaseArgs(refDir), "--out=fixture.txt", "--ftp-pasv=true", "--ftp-type="+tc.ftpType, refFTP.URL("/fixture.txt"))
			implArgs := append(protocolBaseArgs(implDir), "--out=fixture.txt", "--ftp-pasv=true", "--ftp-type="+tc.ftpType, implFTP.URL("/fixture.txt"))

			ref := protocolRun(t, true, refArgs)
			impl := protocolRun(t, false, implArgs)

			AssertEqualExit(t, ref, impl)
			protocolRequireExitZero(t, "ref FTP type "+tc.name, ref)
			protocolRequireExitZero(t, "impl FTP type "+tc.name, impl)
			protocolRequireFile(t, filepath.Join(refDir, "fixture.txt"), payload)
			protocolRequireFile(t, filepath.Join(implDir, "fixture.txt"), payload)
			protocolRequireFTPCommandArg(t, "ref FTP type "+tc.name, refFTP.snapshotCommandDetails(), "TYPE", tc.typeArg)
			protocolRequireFTPCommandArg(t, "impl FTP type "+tc.name, implFTP.snapshotCommandDetails(), "TYPE", tc.typeArg)
		})
	}
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

func protocolRequireFTPPASVOnly(t *testing.T, label string, commands []string) {
	t.Helper()

	var sawPASV bool
	for _, cmd := range commands {
		if cmd == "PASV" {
			sawPASV = true
		}
		if cmd == "EPSV" {
			t.Fatalf("%s issued EPSV on IPv4 passive transfer; commands=%v", label, commands)
		}
	}
	if !sawPASV {
		t.Fatalf("%s did not issue PASV; commands=%v", label, commands)
	}
}

func protocolRequireProxyMethodTarget(t *testing.T, label string, records []protocolFTPProxyRequest, wantMethod, wantTarget string) {
	t.Helper()

	for _, record := range records {
		if record.Method == wantMethod && record.Target == wantTarget {
			return
		}
	}
	t.Fatalf("%s did not issue %s %s; records=%v", label, wantMethod, wantTarget, records)
}

func protocolRequireProxyFTPURL(t *testing.T, label string, records []protocolFTPProxyRequest, wantMethod, rawURL string) {
	t.Helper()

	want, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("%s parse wanted proxy URL: %v", label, err)
	}
	for _, record := range records {
		if record.Method != wantMethod {
			continue
		}
		got, err := url.Parse(record.Target)
		if err != nil {
			continue
		}
		if strings.EqualFold(got.Scheme, want.Scheme) && strings.EqualFold(got.Hostname(), want.Hostname()) && got.Path == want.Path {
			return
		}
	}
	t.Fatalf("%s did not issue %s for %s; records=%v", label, wantMethod, rawURL, records)
}

func protocolRequireFTPCommandArg(t *testing.T, label string, commands []protocolFTPCommand, wantName, wantArg string) {
	t.Helper()

	altArg := strings.TrimPrefix(wantArg, "/")
	for _, cmd := range commands {
		if cmd.Name == wantName && (cmd.Arg == wantArg || cmd.Arg == altArg) {
			return
		}
	}
	t.Fatalf("%s did not issue %s %q; commands=%v", label, wantName, wantArg, commands)
}

func protocolRequireFTPActiveTransfer(t *testing.T, label string, commands []string) {
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
	if !seen["PORT"] && !seen["EPRT"] {
		t.Fatalf("%s did not issue PORT or EPRT; commands=%v", label, commands)
	}
}

func conformanceRequireFileModTimeNear(t *testing.T, label, path string, want time.Time) {
	t.Helper()

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s stat %s: %v", label, path, err)
	}
	got := st.ModTime()
	if got.Before(want.Add(-2*time.Second)) || got.After(want.Add(2*time.Second)) {
		t.Fatalf("%s modtime = %s, want near %s", label, got.UTC().Format(time.RFC3339), want.UTC().Format(time.RFC3339))
	}
}

func writeProtocolFixtureFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
