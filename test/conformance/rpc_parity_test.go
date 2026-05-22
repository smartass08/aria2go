package conformance

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRPCParity_UploadedMetadataSavedToSession(t *testing.T) {
	SkipIfNoRef(t)

	root := t.TempDir()
	refDir := filepath.Join(root, "ref")
	implDir := filepath.Join(root, "impl")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatalf("mkdir ref dir: %v", err)
	}
	if err := os.MkdirAll(implDir, 0o755); err != nil {
		t.Fatalf("mkdir impl dir: %v", err)
	}
	refSession := filepath.Join(root, "ref.session")
	implSession := filepath.Join(root, "impl.session")

	refPort, implPort := startRPCPairFocused(t,
		[]string{"--no-conf", "--pause=true", "--dir=" + refDir, "--save-session=" + refSession, "--rpc-save-upload-metadata=true", "--enable-dht=false", "--bt-enable-lpd=false"},
		[]string{"--no-conf", "--pause=true", "--dir=" + implDir, "--save-session=" + implSession, "--rpc-save-upload-metadata=true", "--enable-dht=false", "--bt-enable-lpd=false"},
	)

	cases := []struct {
		name       string
		method     string
		fixtureRel string
		ext        string
		params     func(string) []any
	}{
		{
			name:       "torrent",
			method:     "aria2.addTorrent",
			fixtureRel: "internal/torrent/testdata/single.torrent",
			ext:        ".torrent",
			params: func(payload string) []any {
				return []any{payload, []string{}, map[string]string{"pause": "true", "torrent-file": "/tmp/ignored.torrent"}}
			},
		},
		{
			name:       "metalink",
			method:     "aria2.addMetalink",
			fixtureRel: "internal/protocol/metalink/testdata/basic.meta4",
			ext:        ".meta4",
			params: func(payload string) []any {
				return []any{payload, map[string]string{"pause": "true", "metalink-file": "/tmp/ignored.meta4"}}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := readFixtureFocused(t, tc.fixtureRel)
			payload := base64.StdEncoding.EncodeToString(raw)

			ref := rpcCallOK(t, refPort, tc.method, tc.params(payload))
			impl := rpcCallOK(t, implPort, tc.method, tc.params(payload))
			if ref.Error != nil || impl.Error != nil {
				t.Fatalf("%s unexpected errors: ref=%#v impl=%#v", tc.method, ref.Error, impl.Error)
			}

			rpcCallOK(t, refPort, "aria2.saveSession", []any{})
			rpcCallOK(t, implPort, "aria2.saveSession", []any{})

			hash := sha1.Sum(raw)
			wantRef := filepath.Join(refDir, fmt.Sprintf("%x%s", hash, tc.ext))
			wantImpl := filepath.Join(implDir, fmt.Sprintf("%x%s", hash, tc.ext))
			assertSessionContains(t, refSession, wantRef)
			assertSessionContains(t, implSession, wantImpl)
		})
	}
}

func TestRPCParity_AddMetalinkMultiGID(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPairFocused(t,
		[]string{"--no-conf", "--pause=true", "--dir=/tmp", "--enable-dht=false", "--bt-enable-lpd=false"},
		[]string{"--no-conf", "--pause=true", "--dir=/tmp", "--enable-dht=false", "--bt-enable-lpd=false"},
	)

	payload := base64.StdEncoding.EncodeToString([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="a.bin">
    <size>12</size>
    <url>http://example.com/a.bin</url>
  </file>
  <file name="b.bin">
    <size>34</size>
    <url>http://example.com/b.bin</url>
  </file>
</metalink>`))
	params := []any{payload, map[string]string{"pause": "true", "gid": "00000000000000ab"}}

	ref := gidListResultFocused(t, rpcCallOK(t, refPort, "aria2.addMetalink", params))
	impl := gidListResultFocused(t, rpcCallOK(t, implPort, "aria2.addMetalink", params))
	if len(ref) != 2 || len(impl) != 2 {
		t.Fatalf("addMetalink gid lengths: ref=%d impl=%d, want 2/2", len(ref), len(impl))
	}
	if ref[0] == "00000000000000ab" || ref[1] == "00000000000000ab" {
		t.Fatalf("reference unexpectedly honored gid for metalink: %v", ref)
	}
	if impl[0] == "00000000000000ab" || impl[1] == "00000000000000ab" {
		t.Fatalf("impl unexpectedly honored gid for metalink: %v", impl)
	}
	if ref[1] == ref[0] || impl[1] == impl[0] {
		t.Fatalf("second gid should be distinct: ref=%v impl=%v", ref, impl)
	}
}

func TestRPCParity_OptionValidationEdges(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPairFocused(t,
		[]string{"--no-conf", "--dir=/tmp"},
		[]string{"--no-conf", "--dir=/tmp"},
	)

	t.Run("invalid numeric option", func(t *testing.T) {
		params := []any{map[string]any{"max-concurrent-downloads": "not_a_number"}}
		refErr := rpcCallExpectError(t, refPort, "aria2.changeGlobalOption", params)
		implErr := rpcCallExpectError(t, implPort, "aria2.changeGlobalOption", params)
		if refErr.Code != implErr.Code || refErr.Message != implErr.Message {
			t.Fatalf("numeric option error mismatch: ref=%#v impl=%#v", refErr, implErr)
		}
	})

	t.Run("invalid boolean option", func(t *testing.T) {
		params := []any{map[string]any{"pause-metadata": "maybe"}}
		refErr := rpcCallExpectError(t, refPort, "aria2.changeGlobalOption", params)
		implErr := rpcCallExpectError(t, implPort, "aria2.changeGlobalOption", params)
		if refErr.Code != implErr.Code || refErr.Message != implErr.Message {
			t.Fatalf("boolean option error mismatch: ref=%#v impl=%#v", refErr, implErr)
		}
	})

	t.Run("request-only ignored and string arrays filtered", func(t *testing.T) {
		params := []any{map[string]any{"out": "ignored.bin", "header": []any{"X-Test: 1", 2, true}}}
		if rpcResultString(t, rpcCallOK(t, refPort, "aria2.changeGlobalOption", params)) != "OK" {
			t.Fatal("reference changeGlobalOption did not return OK")
		}
		if rpcResultString(t, rpcCallOK(t, implPort, "aria2.changeGlobalOption", params)) != "OK" {
			t.Fatal("impl changeGlobalOption did not return OK")
		}

		refOpt := mustStringMap(t, "ref getGlobalOption", rpcCallOK(t, refPort, "aria2.getGlobalOption", []any{}).Result)
		implOpt := mustStringMap(t, "impl getGlobalOption", rpcCallOK(t, implPort, "aria2.getGlobalOption", []any{}).Result)
		if refOpt["header"] != implOpt["header"] || refOpt["header"] != "X-Test: 1\n" {
			t.Fatalf("header mismatch: ref=%q impl=%q", refOpt["header"], implOpt["header"])
		}
		if _, ok := refOpt["out"]; ok {
			t.Fatalf("reference unexpectedly exposes out: %#v", refOpt)
		}
		if _, ok := implOpt["out"]; ok {
			t.Fatalf("impl unexpectedly exposes out: %#v", implOpt)
		}
	})
}

func TestRPCParity_ChangeGlobalRuntimeEffects(t *testing.T) {
	SkipIfNoRef(t)

	root := t.TempDir()
	initialDir := filepath.Join(root, "initial")
	changedDir := filepath.Join(root, "changed")
	if err := os.MkdirAll(initialDir, 0o755); err != nil {
		t.Fatalf("mkdir initial dir: %v", err)
	}
	if err := os.MkdirAll(changedDir, 0o755); err != nil {
		t.Fatalf("mkdir changed dir: %v", err)
	}

	refPort, implPort := startRPCPairFocused(t,
		[]string{"--no-conf", "--pause=true", "--dir=" + initialDir},
		[]string{"--no-conf", "--pause=true", "--dir=" + initialDir},
	)

	change := []any{map[string]string{"dir": changedDir, "max-download-limit": "65536"}}
	rpcCallOK(t, refPort, "aria2.changeGlobalOption", change)
	rpcCallOK(t, implPort, "aria2.changeGlobalOption", change)

	const gid = "0000000000000f30"
	const uri = "http://127.0.0.1:1/global-option.bin"
	addPausedURIFocused(t, refPort, gid, uri)
	addPausedURIFocused(t, implPort, gid, uri)

	refOpt := mustStringMap(t, "ref getOption", rpcCallOK(t, refPort, "aria2.getOption", []any{gid}).Result)
	implOpt := mustStringMap(t, "impl getOption", rpcCallOK(t, implPort, "aria2.getOption", []any{gid}).Result)
	if refOpt["dir"] != implOpt["dir"] || refOpt["dir"] != changedDir {
		t.Fatalf("dir mismatch: ref=%q impl=%q want=%q", refOpt["dir"], implOpt["dir"], changedDir)
	}
	if refOpt["max-download-limit"] != implOpt["max-download-limit"] || refOpt["max-download-limit"] != "65536" {
		t.Fatalf("max-download-limit mismatch: ref=%q impl=%q", refOpt["max-download-limit"], implOpt["max-download-limit"])
	}
}

func TestRPCParity_GetPeersMissingGID(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPairFocused(t,
		[]string{"--no-conf", "--dir=/tmp"},
		[]string{"--no-conf", "--dir=/tmp"},
	)

	params := []any{"0000000000000bad"}
	refErr := rpcCallExpectError(t, refPort, "aria2.getPeers", params)
	implErr := rpcCallExpectError(t, implPort, "aria2.getPeers", params)
	if refErr.Code != implErr.Code || refErr.Message != implErr.Message {
		t.Fatalf("getPeers missing gid mismatch: ref=%#v impl=%#v", refErr, implErr)
	}
}

func TestRPCParity_GetServersMissingGID(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPairFocused(t,
		[]string{"--no-conf", "--dir=/tmp"},
		[]string{"--no-conf", "--dir=/tmp"},
	)

	params := []any{"0000000000000bad"}
	refErr := rpcCallExpectError(t, refPort, "aria2.getServers", params)
	implErr := rpcCallExpectError(t, implPort, "aria2.getServers", params)
	if refErr.Code != implErr.Code || refErr.Message != implErr.Message {
		t.Fatalf("getServers missing gid mismatch: ref=%#v impl=%#v", refErr, implErr)
	}
}

func TestRPCParity_GetOptionMissingGID(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPairFocused(t,
		[]string{"--no-conf", "--dir=/tmp"},
		[]string{"--no-conf", "--dir=/tmp"},
	)

	params := []any{"0000000000000bad"}
	refErr := rpcCallExpectError(t, refPort, "aria2.getOption", params)
	implErr := rpcCallExpectError(t, implPort, "aria2.getOption", params)
	if refErr.Code != implErr.Code || refErr.Message != implErr.Message {
		t.Fatalf("getOption missing gid mismatch: ref=%#v impl=%#v", refErr, implErr)
	}
}

func startRPCPairFocused(t *testing.T, refArgs, implArgs []string) (refPort, implPort int) {
	t.Helper()

	refPort = findFreePort(t)
	refSrv := startRPCRef(t, refPort, refArgs...)
	t.Cleanup(func() { refSrv.Stop(t) })
	refSrv.WaitReady(t)

	implPort = findFreePort(t)
	implSrv := startRPCImpl(t, implPort, implArgs...)
	t.Cleanup(func() { implSrv.Stop(t) })
	implSrv.WaitReadyOrSkip(t)

	return refPort, implPort
}

func readFixtureFocused(t *testing.T, rel string) []byte {
	t.Helper()

	root, err := projectRoot()
	if err != nil {
		t.Fatalf("project root: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return data
}

func gidListResultFocused(t *testing.T, rr rpcResponse) []string {
	t.Helper()

	var gids []string
	if err := json.Unmarshal(rr.Result, &gids); err != nil {
		t.Fatalf("unmarshal gid list: %v", err)
	}
	return gids
}

func addPausedURIFocused(t *testing.T, port int, gid string, uri string) {
	t.Helper()

	rr := rpcCallOK(t, port, "aria2.addUri", []any{
		[]string{uri},
		map[string]string{"gid": gid, "pause": "true"},
	})
	if got := rpcResultString(t, rr); got != gid {
		t.Fatalf("addUri gid = %s, want %s", got, gid)
	}
}

func assertSessionContains(t *testing.T, path string, want string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session file %s: %v", path, err)
	}
	if !bytes.Contains(data, []byte(want)) {
		t.Fatalf("session file %s does not contain %q:\n%s", path, want, string(data))
	}
}
