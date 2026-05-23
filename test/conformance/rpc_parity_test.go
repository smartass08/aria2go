package conformance

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestRPCParity_StoppedResultFidelityHTTP(t *testing.T) {
	SkipIfNoRef(t)

	fileSrv := newBlockingDownloadServer(t)
	root := t.TempDir()
	refDir := filepath.Join(root, "ref")
	implDir := filepath.Join(root, "impl")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatalf("mkdir ref dir: %v", err)
	}
	if err := os.MkdirAll(implDir, 0o755); err != nil {
		t.Fatalf("mkdir impl dir: %v", err)
	}

	refPort, implPort := startRPCPairFocused(t,
		[]string{"--no-conf", "--dir=" + refDir, "--split=1", "--max-connection-per-server=1", "--allow-overwrite=true", "--auto-file-renaming=false"},
		[]string{"--no-conf", "--dir=" + implDir, "--split=1", "--max-connection-per-server=1", "--allow-overwrite=true", "--auto-file-renaming=false"},
	)

	const gid = "0000000000000f51"
	uri := fileSrv.URL + "/stopped-http.bin"
	params := []any{[]string{uri}, map[string]string{"gid": gid}}
	if got := rpcResultString(t, rpcCallOK(t, refPort, "aria2.addUri", params)); got != gid {
		t.Fatalf("ref addUri gid got %q want %q", got, gid)
	}
	if got := rpcResultString(t, rpcCallOK(t, implPort, "aria2.addUri", params)); got != gid {
		t.Fatalf("impl addUri gid got %q want %q", got, gid)
	}

	waitForRPCStatus(t, refPort, gid, "active")
	waitForRPCStatus(t, implPort, gid, "active")
	waitForRPCStringFieldFocused(t, refPort, gid, "totalLength", "1048576")
	waitForRPCStringFieldFocused(t, implPort, gid, "totalLength", "1048576")

	if got := rpcResultString(t, rpcCallOK(t, refPort, "aria2.remove", []any{gid})); got != gid {
		t.Fatalf("ref remove gid got %q want %q", got, gid)
	}
	if got := rpcResultString(t, rpcCallOK(t, implPort, "aria2.remove", []any{gid})); got != gid {
		t.Fatalf("impl remove gid got %q want %q", got, gid)
	}
	waitForRPCStatus(t, refPort, gid, "removed")
	waitForRPCStatus(t, implPort, gid, "removed")

	keys := []string{"gid", "status", "errorCode", "totalLength", "completedLength", "uploadLength", "downloadSpeed", "uploadSpeed", "connections", "bitfield", "pieceLength", "numPieces", "files", "dir"}
	refStatus := rpcCallOK(t, refPort, "aria2.tellStatus", []any{gid, keys})
	implStatus := rpcCallOK(t, implPort, "aria2.tellStatus", []any{gid, keys})
	refStatusMap := mustJSONMap(t, "ref stopped http tellStatus", refStatus.Result)
	implStatusMap := mustJSONMap(t, "impl stopped http tellStatus", implStatus.Result)
	requireJSONFieldsFocused(t, "ref stopped http tellStatus", refStatusMap, keys)
	requireJSONFieldsFocused(t, "impl stopped http tellStatus", implStatusMap, keys)
	requireStoppedHTTPFilesFocused(t, "ref stopped http tellStatus.files", refStatusMap["files"])
	requireStoppedHTTPFilesFocused(t, "impl stopped http tellStatus.files", implStatusMap["files"])

	replacements := rpcParityReplacementsFocused(t, refDir, implDir, gid, gid)
	compareJSONValueEqual(t, "stopped http tellStatus", normalizeJSONFocused(t, "ref stopped http tellStatus", refStatus.Result, replacements), normalizeJSONFocused(t, "impl stopped http tellStatus", implStatus.Result, replacements))

	refStopped := rpcCallOK(t, refPort, "aria2.tellStopped", []any{float64(0), float64(1), keys})
	implStopped := rpcCallOK(t, implPort, "aria2.tellStopped", []any{float64(0), float64(1), keys})
	refStoppedEntries := mustJSONListFocused(t, "ref stopped http tellStopped", refStopped.Result)
	implStoppedEntries := mustJSONListFocused(t, "impl stopped http tellStopped", implStopped.Result)
	if len(refStoppedEntries) != 1 || len(implStoppedEntries) != 1 {
		t.Fatalf("tellStopped entry lengths: ref=%d impl=%d, want 1/1", len(refStoppedEntries), len(implStoppedEntries))
	}
	compareJSONValueEqual(t, "stopped http tellStopped", normalizeJSONFocused(t, "ref stopped http tellStopped", refStoppedEntries[0], replacements), normalizeJSONFocused(t, "impl stopped http tellStopped", implStoppedEntries[0], replacements))
	compareJSONValueEqual(t, "ref stopped http tellStopped mirrors tellStatus", normalizeJSONFocused(t, "ref stopped http tellStatus", refStatus.Result, replacements), normalizeJSONFocused(t, "ref stopped http tellStopped", refStoppedEntries[0], replacements))
	compareJSONValueEqual(t, "impl stopped http tellStopped mirrors tellStatus", normalizeJSONFocused(t, "impl stopped http tellStatus", implStatus.Result, replacements), normalizeJSONFocused(t, "impl stopped http tellStopped", implStoppedEntries[0], replacements))

	refGetFiles := rpcCallOK(t, refPort, "aria2.getFiles", []any{gid})
	implGetFiles := rpcCallOK(t, implPort, "aria2.getFiles", []any{gid})
	compareJSONValueEqual(t, "stopped http getFiles", normalizeJSONFocused(t, "ref stopped http getFiles", refGetFiles.Result, replacements), normalizeJSONFocused(t, "impl stopped http getFiles", implGetFiles.Result, replacements))
	compareJSONValueEqual(t, "ref stopped http getFiles mirrors tellStatus.files", normalizeJSONFocused(t, "ref stopped http tellStatus.files", refStatusMap["files"], replacements), normalizeJSONFocused(t, "ref stopped http getFiles", refGetFiles.Result, replacements))
	compareJSONValueEqual(t, "impl stopped http getFiles mirrors tellStatus.files", normalizeJSONFocused(t, "impl stopped http tellStatus.files", implStatusMap["files"], replacements), normalizeJSONFocused(t, "impl stopped http getFiles", implGetFiles.Result, replacements))
}

func TestRPCParity_StoppedResultFidelityBittorrent(t *testing.T) {
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

	refPort, implPort := startRPCPairFocused(t,
		[]string{"--no-conf", "--dir=" + refDir, "--enable-dht=false", "--bt-enable-lpd=false", "--bt-tracker-connect-timeout=1", "--bt-tracker-timeout=1"},
		[]string{"--no-conf", "--dir=" + implDir, "--enable-dht=false", "--bt-enable-lpd=false", "--bt-tracker-connect-timeout=1", "--bt-tracker-timeout=1"},
	)

	payload := base64.StdEncoding.EncodeToString(readFixtureFocused(t, "internal/torrent/testdata/single.torrent"))
	params := []any{payload, []string{}, map[string]string{"pause": "false"}}
	refGID := requireRPCGIDResult(t, "ref addTorrent", rpcCallOK(t, refPort, "aria2.addTorrent", params))
	implGID := requireRPCGIDResult(t, "impl addTorrent", rpcCallOK(t, implPort, "aria2.addTorrent", params))

	waitForRPCStatus(t, refPort, refGID, "active")
	waitForRPCStatus(t, implPort, implGID, "active")

	refRemove := rpcCallOK(t, refPort, "aria2.remove", []any{refGID})
	implRemove := rpcCallOK(t, implPort, "aria2.remove", []any{implGID})
	if got := rpcResultString(t, refRemove); got != refGID {
		t.Fatalf("ref remove gid got %q want %q", got, refGID)
	}
	if got := rpcResultString(t, implRemove); got != implGID {
		t.Fatalf("impl remove gid got %q want %q", got, implGID)
	}

	waitForRPCStatus(t, refPort, refGID, "removed")
	waitForRPCStatus(t, implPort, implGID, "removed")

	keys := []string{"status", "infoHash", "bittorrent", "files", "bitfield", "pieceLength", "numPieces", "totalLength", "completedLength"}
	refStatus := rpcCallOK(t, refPort, "aria2.tellStatus", []any{refGID, keys})
	implStatus := rpcCallOK(t, implPort, "aria2.tellStatus", []any{implGID, keys})
	refMap := mustJSONMap(t, "ref stopped torrent status", refStatus.Result)
	implMap := mustJSONMap(t, "impl stopped torrent status", implStatus.Result)

	assertJSONFieldEqualFocused(t, "stopped torrent status", refMap, implMap, "status")
	assertJSONFieldEqualFocused(t, "stopped torrent infoHash", refMap, implMap, "infoHash")
	for _, key := range []string{"bitfield", "pieceLength", "numPieces", "totalLength", "completedLength"} {
		assertJSONFieldEqualFocused(t, "stopped torrent "+key, refMap, implMap, key)
	}
	replacements := rpcParityReplacementsFocused(t, refDir, implDir, refGID, implGID)
	assertNormalizedJSONFieldEqualFocused(t, "stopped torrent files", refMap, implMap, "files", replacements)
	requireStoppedTorrentFilesFocused(t, "ref stopped torrent files", refMap["files"])
	requireStoppedTorrentFilesFocused(t, "impl stopped torrent files", implMap["files"])

	refStopped := rpcCallOK(t, refPort, "aria2.tellStopped", []any{float64(0), float64(1), keys})
	implStopped := rpcCallOK(t, implPort, "aria2.tellStopped", []any{float64(0), float64(1), keys})
	refStoppedEntries := mustJSONListFocused(t, "ref stopped torrent tellStopped", refStopped.Result)
	implStoppedEntries := mustJSONListFocused(t, "impl stopped torrent tellStopped", implStopped.Result)
	if len(refStoppedEntries) != 1 || len(implStoppedEntries) != 1 {
		t.Fatalf("torrent tellStopped entry lengths: ref=%d impl=%d, want 1/1", len(refStoppedEntries), len(implStoppedEntries))
	}
	compareJSONValueEqual(t, "stopped torrent tellStopped", normalizeJSONFocused(t, "ref stopped torrent tellStopped", refStoppedEntries[0], replacements), normalizeJSONFocused(t, "impl stopped torrent tellStopped", implStoppedEntries[0], replacements))
	compareJSONValueEqual(t, "ref stopped torrent tellStopped mirrors tellStatus", normalizeJSONFocused(t, "ref stopped torrent tellStatus", refStatus.Result, replacements), normalizeJSONFocused(t, "ref stopped torrent tellStopped", refStoppedEntries[0], replacements))
	compareJSONValueEqual(t, "impl stopped torrent tellStopped mirrors tellStatus", normalizeJSONFocused(t, "impl stopped torrent tellStatus", implStatus.Result, replacements), normalizeJSONFocused(t, "impl stopped torrent tellStopped", implStoppedEntries[0], replacements))

	refGetFiles := rpcCallOK(t, refPort, "aria2.getFiles", []any{refGID})
	implGetFiles := rpcCallOK(t, implPort, "aria2.getFiles", []any{implGID})
	compareJSONValueEqual(t, "stopped torrent getFiles", normalizeJSONFocused(t, "ref stopped torrent getFiles", refGetFiles.Result, replacements), normalizeJSONFocused(t, "impl stopped torrent getFiles", implGetFiles.Result, replacements))
	compareJSONValueEqual(t, "ref stopped torrent getFiles mirrors tellStatus.files", normalizeJSONFocused(t, "ref stopped torrent tellStatus.files", refMap["files"], replacements), normalizeJSONFocused(t, "ref stopped torrent getFiles", refGetFiles.Result, replacements))
	compareJSONValueEqual(t, "impl stopped torrent getFiles mirrors tellStatus.files", normalizeJSONFocused(t, "impl stopped torrent tellStatus.files", implMap["files"], replacements), normalizeJSONFocused(t, "impl stopped torrent getFiles", implGetFiles.Result, replacements))

	refBT := requireJSONMapFieldFocused(t, "ref stopped torrent status", refMap, "bittorrent")
	implBT := requireJSONMapFieldFocused(t, "impl stopped torrent status", implMap, "bittorrent")
	compareJSONValueEqual(t, "stopped torrent bittorrent", refMap["bittorrent"], implMap["bittorrent"])

	assertJSONFieldEqualFocused(t, "stopped torrent bittorrent.announceList", refBT, implBT, "announceList")
	refInfo := requireJSONMapFieldFocused(t, "ref stopped torrent bittorrent", refBT, "info")
	implInfo := requireJSONMapFieldFocused(t, "impl stopped torrent bittorrent", implBT, "info")
	assertJSONFieldEqualFocused(t, "stopped torrent bittorrent.info.name", refInfo, implInfo, "name")
	for _, key := range []string{"comment", "creationDate", "mode"} {
		assertOptionalJSONFieldEqualFocused(t, "stopped torrent bittorrent", refBT, implBT, key)
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

func waitForRPCStringFieldFocused(t *testing.T, port int, gid string, key string, want string) {
	t.Helper()

	var last map[string]string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rr := rpcCall(t, port, "aria2.tellStatus", []any{gid, []string{key}})
		if rr.Error == nil {
			values := mustStringMap(t, "tellStatus "+key, rr.Result)
			last = values
			if values[key] == want {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("GID %s on port %d did not reach %s=%q; last=%v", gid, port, key, want, last)
}

func requireJSONFieldsFocused(t *testing.T, label string, values map[string]json.RawMessage, keys []string) {
	t.Helper()

	for _, key := range keys {
		if _, ok := values[key]; !ok {
			t.Fatalf("%s missing %q key; keys=%v", label, key, mapKeys(values))
		}
	}
}

func requireStoppedHTTPFilesFocused(t *testing.T, label string, raw json.RawMessage) {
	t.Helper()

	files := mustJSONMapSliceFocused(t, label, raw)
	if len(files) != 1 {
		t.Fatalf("%s len = %d, want 1", label, len(files))
	}
	requireJSONFieldsFocused(t, label+"[0]", files[0], []string{"index", "path", "length", "completedLength", "selected", "uris"})
	if got := mustJSONStringFocused(t, label+"[0].length", files[0]["length"]); got != "1048576" {
		t.Fatalf("%s[0].length = %q, want 1048576", label, got)
	}
	uris := mustJSONMapSliceFocused(t, label+"[0].uris", files[0]["uris"])
	if len(uris) != 2 {
		t.Fatalf("%s[0].uris len = %d, want 2", label, len(uris))
	}
	if got := mustJSONStringFocused(t, label+"[0].uris[0].status", uris[0]["status"]); got != "used" {
		t.Fatalf("%s[0].uris[0].status = %q, want used", label, got)
	}
	if got := mustJSONStringFocused(t, label+"[0].uris[1].status", uris[1]["status"]); got != "waiting" {
		t.Fatalf("%s[0].uris[1].status = %q, want waiting", label, got)
	}
}

func requireStoppedTorrentFilesFocused(t *testing.T, label string, raw json.RawMessage) {
	t.Helper()

	files := mustJSONMapSliceFocused(t, label, raw)
	if len(files) == 0 {
		t.Fatalf("%s empty; want at least one file", label)
	}
	for i, file := range files {
		requireJSONFieldsFocused(t, fmt.Sprintf("%s[%d]", label, i), file, []string{"index", "path", "length", "completedLength", "selected", "uris"})
	}
}

func mustJSONListFocused(t *testing.T, label string, raw json.RawMessage) []json.RawMessage {
	t.Helper()

	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("unmarshal %s list: %v (raw=%s)", label, err, string(raw))
	}
	return values
}

func mustJSONMapSliceFocused(t *testing.T, label string, raw json.RawMessage) []map[string]json.RawMessage {
	t.Helper()

	var values []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("unmarshal %s object list: %v (raw=%s)", label, err, string(raw))
	}
	return values
}

func mustJSONStringFocused(t *testing.T, label string, raw json.RawMessage) string {
	t.Helper()

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("unmarshal %s string: %v (raw=%s)", label, err, string(raw))
	}
	return value
}

func requireJSONMapFieldFocused(t *testing.T, label string, values map[string]json.RawMessage, key string) map[string]json.RawMessage {
	t.Helper()

	raw, ok := values[key]
	if !ok {
		t.Fatalf("%s missing %q key; keys=%v", label, key, mapKeys(values))
	}
	return mustJSONMap(t, label+"."+key, raw)
}

func assertJSONFieldEqualFocused(t *testing.T, label string, ref, impl map[string]json.RawMessage, key string) {
	t.Helper()

	refRaw, ok := ref[key]
	if !ok {
		t.Fatalf("ref %s missing %q key; keys=%v", label, key, mapKeys(ref))
	}
	implRaw, ok := impl[key]
	if !ok {
		t.Fatalf("impl %s missing %q key; keys=%v", label, key, mapKeys(impl))
	}
	compareJSONValueEqual(t, label, refRaw, implRaw)
}

func assertNormalizedJSONFieldEqualFocused(t *testing.T, label string, ref, impl map[string]json.RawMessage, key string, replacements map[string]string) {
	t.Helper()

	refRaw, ok := ref[key]
	if !ok {
		t.Fatalf("ref %s missing %q key; keys=%v", label, key, mapKeys(ref))
	}
	implRaw, ok := impl[key]
	if !ok {
		t.Fatalf("impl %s missing %q key; keys=%v", label, key, mapKeys(impl))
	}
	compareJSONValueEqual(t, label, normalizeJSONFocused(t, "ref "+label, refRaw, replacements), normalizeJSONFocused(t, "impl "+label, implRaw, replacements))
}

func assertOptionalJSONFieldEqualFocused(t *testing.T, label string, ref, impl map[string]json.RawMessage, key string) {
	t.Helper()

	if _, ok := ref[key]; ok {
		assertJSONFieldEqualFocused(t, label+"."+key, ref, impl, key)
	}
}

func rpcParityReplacementsFocused(t *testing.T, refDir, implDir, refGID, implGID string) map[string]string {
	t.Helper()

	replacements := map[string]string{}
	addPathReplacementFocused(t, replacements, refDir, "<DIR>")
	addPathReplacementFocused(t, replacements, implDir, "<DIR>")
	if refGID != "" {
		replacements[refGID] = "<GID>"
	}
	if implGID != "" {
		replacements[implGID] = "<GID>"
	}
	return replacements
}

func addPathReplacementFocused(t *testing.T, replacements map[string]string, path string, value string) {
	t.Helper()

	if path == "" {
		return
	}
	replacements[path] = value
	cleaned := filepath.Clean(path)
	replacements[cleaned] = value
	if evaluated, err := filepath.EvalSymlinks(path); err == nil {
		replacements[evaluated] = value
		replacements[filepath.Clean(evaluated)] = value
	}
}

func normalizeJSONFocused(t *testing.T, label string, raw json.RawMessage, replacements map[string]string) json.RawMessage {
	t.Helper()

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("unmarshal %s for normalization: %v (raw=%s)", label, err, string(raw))
	}
	normalized := normalizeJSONValueFocused(value, replacements)
	out, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("marshal normalized %s: %v", label, err)
	}
	return out
}

func normalizeJSONValueFocused(value any, replacements map[string]string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = normalizeJSONValueFocused(child, replacements)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = normalizeJSONValueFocused(child, replacements)
		}
		return out
	case string:
		out := v
		for old, replacement := range replacements {
			out = strings.ReplaceAll(out, old, replacement)
		}
		return out
	default:
		return v
	}
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
