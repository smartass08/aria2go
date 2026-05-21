package dispatcher

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	ariabase64 "github.com/smartass08/aria2go/internal/encoding/base64"
	"github.com/smartass08/aria2go/internal/engine"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func testOpts() *config.Options {
	return &config.Options{
		Dir:                    "/tmp/aria2go-test",
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      100,
		SaveSession:            "/tmp/aria2go-test-session",
	}
}

func newTestDispatcher(t *testing.T, cfg Config) (*Dispatcher, *engine.Engine) {
	t.Helper()
	e, err := engine.New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("engine.New failed: %v", err)
	}
	return New(e, cfg), e
}

func requireMulticallJSONFault(t *testing.T, elem interface{}, wantMessage string) {
	t.Helper()
	fault, ok := elem.(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested fault map, got %T", elem)
	}
	if _, ok := fault["faultCode"]; ok {
		t.Fatalf("nested fault used XML-RPC key faultCode; JSON-RPC aria2 uses code/message: %#v", fault)
	}
	if _, ok := fault["faultString"]; ok {
		t.Fatalf("nested fault used XML-RPC key faultString; JSON-RPC aria2 uses code/message: %#v", fault)
	}
	switch got := fault["code"].(type) {
	case int:
		if got != 1 {
			t.Fatalf("fault code = %d, want 1", got)
		}
	case int64:
		if got != 1 {
			t.Fatalf("fault code = %d, want 1", got)
		}
	case float64:
		if got != 1 {
			t.Fatalf("fault code = %v, want 1", got)
		}
	default:
		t.Fatalf("fault code = %T(%v), want numeric 1", fault["code"], fault["code"])
	}
	msg, ok := fault["message"].(string)
	if !ok {
		t.Fatalf("fault message = %T(%v), want string", fault["message"], fault["message"])
	}
	if wantMessage != "" && msg != wantMessage {
		t.Fatalf("fault message = %q, want %q", msg, wantMessage)
	}
}

func requireInt64Pair(t *testing.T, got interface{}, want0, want1 int64) {
	t.Helper()
	arr, ok := got.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", got)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 elements, got %d: %v", len(arr), arr)
	}
	if arr[0] != want0 || arr[1] != want1 {
		t.Fatalf("result = %v, want [%d %d]", arr, want0, want1)
	}
}

func TestNew(t *testing.T) {
	e, err := engine.New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	d := New(e, Config{})
	if d == nil {
		t.Fatal("New returned nil")
	}
	if d.engine != e {
		t.Error("engine not set")
	}
}

func TestListMethods(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	methods := d.ListMethods()
	if len(methods) == 0 {
		t.Fatal("ListMethods returned empty")
	}
	// Must include key methods.
	hasAddUri := false
	hasSystemMulticall := false
	hasSystemListMethods := false
	hasSystemListNotifications := false
	for _, m := range methods {
		switch m {
		case "aria2.addUri":
			hasAddUri = true
		case "system.multicall":
			hasSystemMulticall = true
		case "system.listMethods":
			hasSystemListMethods = true
		case "system.listNotifications":
			hasSystemListNotifications = true
		}
	}
	if !hasAddUri {
		t.Error("ListMethods missing aria2.addUri")
	}
	if !hasSystemMulticall {
		t.Error("ListMethods missing system.multicall")
	}
	if !hasSystemListMethods {
		t.Error("ListMethods missing system.listMethods")
	}
	if !hasSystemListNotifications {
		t.Error("ListMethods missing system.listNotifications")
	}
}

func TestListNotifications(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	notifs := d.ListNotifications()
	if len(notifs) == 0 {
		t.Fatal("ListNotifications returned empty")
	}
	expected := map[string]bool{
		"aria2.onDownloadStart":      true,
		"aria2.onDownloadPause":      true,
		"aria2.onDownloadStop":       true,
		"aria2.onDownloadComplete":   true,
		"aria2.onDownloadError":      true,
		"aria2.onBtDownloadComplete": true,
	}
	for _, n := range notifs {
		if !expected[n] {
			t.Errorf("unexpected notification: %s", n)
		}
	}
	for n := range expected {
		found := false
		for _, m := range notifs {
			if m == n {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing notification: %s", n)
		}
	}
}

// ---- Auth tests ----

func TestAuth_NoSecret_Passes(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.getVersion", []interface{}{})
	if err != nil {
		t.Fatalf("unexpected auth error: %v", err)
	}
}

func TestAuth_WithSecret_ValidToken(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{Secret: "mysecret"})
	_, err := d.Call(context.Background(), "mysecret", "aria2.getVersion", []interface{}{})
	if err != nil {
		t.Fatalf("unexpected auth error: %v", err)
	}
}

func TestAuth_WithSecret_InvalidToken(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{Secret: "mysecret"})
	_, err := d.Call(context.Background(), "wrongsecret", "aria2.getVersion", []interface{}{})
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
}

func TestAuth_WithSecret_EmptyToken(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{Secret: "mysecret"})
	_, err := d.Call(context.Background(), "", "aria2.getVersion", []interface{}{})
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
}

func TestAuth_SystemListMethodsDoNotRequireToken(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{Secret: "mysecret"})
	_, err := d.Call(context.Background(), "", "system.listMethods", []interface{}{})
	if err != nil {
		t.Fatalf("unexpected auth error for listMethods without token: %v", err)
	}
	_, err = d.Call(context.Background(), "mysecret", "system.listMethods", []interface{}{})
	if err != nil {
		t.Fatalf("unexpected auth error for listMethods with token: %v", err)
	}
	_, err = d.Call(context.Background(), "", "system.listNotifications", []interface{}{})
	if err != nil {
		t.Fatalf("unexpected auth error for listNotifications without token: %v", err)
	}
	_, err = d.Call(context.Background(), "mysecret", "system.listNotifications", []interface{}{})
	if err != nil {
		t.Fatalf("unexpected auth error for listNotifications with token: %v", err)
	}
}

// ---- Read-only tests ----

func TestReadOnly_MutatingMethodBlocked(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{ReadOnly: true})
	_, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file"},
	})
	if err == nil {
		t.Fatal("expected read-only error, got nil")
	}
}

func TestReadOnly_NonMutatingMethodAllowed(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{ReadOnly: true})
	_, err := d.Call(context.Background(), "", "aria2.getVersion", []interface{}{})
	if err != nil {
		t.Fatalf("unexpected error in read-only mode: %v", err)
	}
}

// ---- Method tests ----

func TestGetVersion(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.getVersion", []interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["version"] != "1.37.0" {
		t.Errorf("version = %v, want 1.37.0", m["version"])
	}
	features, ok := m["enabledFeatures"].([]string)
	if !ok {
		t.Fatalf("expected []string enabledFeatures, got %T", m["enabledFeatures"])
	}
	if len(features) == 0 {
		t.Error("enabledFeatures is empty")
	}
}

func TestGetSessionInfo(t *testing.T) {
	d, e := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.getSessionInfo", []interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["sessionId"] != e.SessionID() {
		t.Errorf("sessionId = %v, want %v", m["sessionId"], e.SessionID())
	}
}

func TestAddUri(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gidStr, ok := result.(string)
	if !ok {
		t.Fatalf("expected string GID, got %T", result)
	}
	if len(gidStr) != 16 {
		t.Errorf("GID = %q, want 16-char hex", gidStr)
	}
}

func TestAddUri_WithOptions(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
		map[string]interface{}{
			"dir": "/tmp/aria2go-test",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.(string); !ok {
		t.Fatalf("expected string GID, got %T", result)
	}
}

func TestAddUri_WithPosition(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})

	add := func(uri string, position int64) string {
		t.Helper()
		result, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
			[]interface{}{uri},
			map[string]interface{}{},
			position,
		})
		if err != nil {
			t.Fatalf("addUri(%q, %d): %v", uri, position, err)
		}
		gid, ok := result.(string)
		if !ok {
			t.Fatalf("expected string GID, got %T", result)
		}
		return gid
	}

	first := add("http://example.com/a", 0)
	second := add("http://example.com/b", 1)
	inserted := add("http://example.com/c", 1)
	appended := add("http://example.com/d", 99)

	result, err := d.Call(context.Background(), "", "aria2.tellWaiting", []interface{}{int64(0), int64(10)})
	if err != nil {
		t.Fatalf("tellWaiting: %v", err)
	}
	waiting, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	want := []string{first, inserted, second, appended}
	if len(waiting) != len(want) {
		t.Fatalf("waiting len = %d, want %d: %v", len(waiting), len(want), waiting)
	}
	for i, wantGID := range want {
		if waiting[i]["gid"] != wantGID {
			t.Fatalf("waiting[%d].gid = %v, want %s; full waiting=%v", i, waiting[i]["gid"], wantGID, waiting)
		}
	}

	_, err = d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/negative-position"},
		map[string]interface{}{},
		int64(-1),
	})
	if err == nil {
		t.Fatal("expected error for negative position")
	}
}

func validMetalinkData(name string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="` + name + `">
    <size>12345</size>
    <url priority="1">http://example.com/` + name + `</url>
  </file>
</metalink>`)
}

func TestAddTorrent_InvalidUploadedMetadataErrors(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	torrentData := []byte("dummy torrent data")
	_, err := d.Call(context.Background(), "", "aria2.addTorrent", []interface{}{
		ariabase64.Encode(torrentData),
		nil,
	})
	if err == nil {
		t.Fatal("expected error for invalid torrent metadata")
	}
	if err.Error() != "Bencode decoding failed" {
		t.Fatalf("error = %q, want Bencode decoding failed", err.Error())
	}
}

func TestAddMetalink(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	metalinkData := validMetalinkData("a.bin")
	result, err := d.Call(context.Background(), "", "aria2.addMetalink", []interface{}{
		ariabase64.Encode(metalinkData),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Returns []string for metalink (array of GIDs).
	arr, ok := result.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", result)
	}
	if len(arr) == 0 {
		t.Error("expected at least one GID from addMetalink")
	}
}

func TestPauseAndResume(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	// Add a download.
	addResult, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	if err != nil {
		t.Fatalf("addUri: %v", err)
	}
	gid := addResult.(string)

	// Pause it.
	pauseResult, err := d.Call(context.Background(), "", "aria2.forcePause", []interface{}{gid})
	if err != nil {
		t.Fatalf("forcePause: %v", err)
	}
	if pauseResult != gid {
		t.Errorf("forcePause returned %v, want %v", pauseResult, gid)
	}

	// Unpause it.
	unpauseResult, err := d.Call(context.Background(), "", "aria2.unpause", []interface{}{gid})
	if err != nil {
		t.Fatalf("unpause: %v", err)
	}
	if unpauseResult != gid {
		t.Errorf("unpause returned %v, want %v", unpauseResult, gid)
	}
}

func TestPauseAll_ForcePauseAll(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	// Add two downloads.
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/a"},
	})
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/b"},
	})

	result, err := d.Call(context.Background(), "", "aria2.forcePauseAll", []interface{}{})
	if err != nil {
		t.Fatalf("forcePauseAll: %v", err)
	}
	if result != "OK" {
		t.Errorf("forcePauseAll returned %v, want OK", result)
	}

	result, err = d.Call(context.Background(), "", "aria2.unpauseAll", []interface{}{})
	if err != nil {
		t.Fatalf("unpauseAll: %v", err)
	}
	if result != "OK" {
		t.Errorf("unpauseAll returned %v, want OK", result)
	}
}

func TestRemove(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	addResult, _ := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	gid := addResult.(string)

	result, err := d.Call(context.Background(), "", "aria2.forceRemove", []interface{}{gid})
	if err != nil {
		t.Fatalf("forceRemove: %v", err)
	}
	if result != gid {
		t.Errorf("forceRemove returned %v, want %v", result, gid)
	}
}

func TestTellStatus(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	addResult, _ := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	gid := addResult.(string)

	result, err := d.Call(context.Background(), "", "aria2.tellStatus", []interface{}{gid})
	if err != nil {
		t.Fatalf("tellStatus: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["gid"] != gid {
		t.Errorf("gid = %v, want %v", m["gid"], gid)
	}
	if m["status"] == "" {
		t.Error("status is empty")
	}
}

func TestTellStatus_WithKeys(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	addResult, _ := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	gid := addResult.(string)

	result, err := d.Call(context.Background(), "", "aria2.tellStatus", []interface{}{
		gid,
		[]interface{}{"gid", "status"},
	})
	if err != nil {
		t.Fatalf("tellStatus with keys: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if len(m) != 2 {
		t.Errorf("expected 2 keys, got %d", len(m))
	}
	if m["gid"] != gid {
		t.Errorf("gid = %v, want %v", m["gid"], gid)
	}
}

func TestTellActive(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.tellActive", []interface{}{})
	if err != nil {
		t.Fatalf("tellActive: %v", err)
	}
	arr, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	if len(arr) != 0 {
		t.Logf("tellActive: %d active downloads (expected 0 in skeleton engine)", len(arr))
	}
}

func TestTellWaiting(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	// Add a download.
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})

	result, err := d.Call(context.Background(), "", "aria2.tellWaiting", []interface{}{
		int64(0), int64(10),
	})
	if err != nil {
		t.Fatalf("tellWaiting: %v", err)
	}
	arr, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	if len(arr) == 0 {
		t.Error("tellWaiting returned empty array")
	}
}

func TestTellWaiting_NegativeOffset(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/a"},
	})
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/b"},
	})

	result, err := d.Call(context.Background(), "", "aria2.tellWaiting", []interface{}{
		int64(-1), int64(2),
	})
	if err != nil {
		t.Fatalf("tellWaiting with negative offset: %v", err)
	}
	arr, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	if len(arr) == 0 {
		t.Error("tellWaiting with negative offset returned empty")
	}
}

func TestTellStopped(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.tellStopped", []interface{}{
		int64(0), int64(10),
	})
	if err != nil {
		t.Fatalf("tellStopped: %v", err)
	}
	arr, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	// Stopped queue may be empty.
	_ = arr
}

func TestGetUris(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	addResult, _ := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	gid := addResult.(string)

	result, err := d.Call(context.Background(), "", "aria2.getUris", []interface{}{gid})
	if err != nil {
		t.Fatalf("getUris: %v", err)
	}
	uris, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	// Skeleton engine returns empty; full engine fills URI data.
	// Verify shape per aria2 spec: each item has "uri" and "status".
	for i, u := range uris {
		if _, ok := u["uri"]; !ok {
			t.Errorf("getUris[%d] missing 'uri' key", i)
		}
		if _, ok := u["status"]; !ok {
			t.Errorf("getUris[%d] missing 'status' key", i)
		}
	}
}

func TestGetFiles(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	addResult, _ := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	gid := addResult.(string)

	result, err := d.Call(context.Background(), "", "aria2.getFiles", []interface{}{gid})
	if err != nil {
		t.Fatalf("getFiles: %v", err)
	}
	files, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	// Skeleton engine returns empty; full engine fills file data.
	// Verify shape per aria2 spec: index, path, length, completedLength, selected, uris.
	for i, f := range files {
		requiredKeys := []string{"index", "path", "length", "completedLength", "selected", "uris"}
		for _, k := range requiredKeys {
			if _, ok := f[k]; !ok {
				t.Errorf("getFiles[%d] missing %q key", i, k)
			}
		}
		if f["selected"] != "true" && f["selected"] != "false" {
			t.Errorf("getFiles[%d] selected = %q, want 'true' or 'false'", i, f["selected"])
		}
		// Verify uris sub-shape.
		fileUris, ok := f["uris"].([]map[string]interface{})
		if ok {
			for _, u := range fileUris {
				if _, ok := u["uri"]; !ok {
					t.Error("file uri missing 'uri' key")
				}
				if _, ok := u["status"]; !ok {
					t.Error("file uri missing 'status' key")
				}
			}
		}
	}
}

func TestGetPeers(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	if err != nil {
		t.Fatalf("addUri: %v", err)
	}
	gid := result.(string)
	peers, err := d.Call(context.Background(), "", "aria2.getPeers", []interface{}{gid})
	if err != nil {
		t.Fatalf("getPeers: %v", err)
	}
	arr, ok := peers.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", peers)
	}
	// Skeleton engine returns empty. Full engine fills with peer dicts
	// containing: peerId, ip, port, bitfield, amChoking, peerChoking,
	// downloadSpeed, uploadSpeed, seeder.
	if len(arr) > 0 {
		for i, p := range arr {
			peerMap, ok := p.(map[string]interface{})
			if !ok {
				t.Fatalf("getPeers[%d]: expected map[string]interface{}, got %T", i, p)
			}
			peerKeys := []string{"peerId", "ip", "port", "bitfield", "amChoking", "peerChoking", "downloadSpeed", "uploadSpeed", "seeder"}
			for _, k := range peerKeys {
				if _, ok := peerMap[k]; !ok {
					t.Errorf("getPeers[%d] missing %q key", i, k)
				}
			}
		}
	}
}

func TestGetServers(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	if err != nil {
		t.Fatalf("addUri: %v", err)
	}
	gid := result.(string)
	_, err = d.Call(context.Background(), "", "aria2.getServers", []interface{}{gid})
	if err == nil {
		t.Fatal("getServers on non-active download returned success, want error")
	}
	if got, want := err.Error(), fmt.Sprintf("No active download for GID#%s", gid); got != want {
		t.Fatalf("getServers error got %q want %q", got, want)
	}
}

func TestChangeOption(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	addResult, _ := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	gid := addResult.(string)

	result, err := d.Call(context.Background(), "", "aria2.changeOption", []interface{}{
		gid,
		map[string]interface{}{
			"max-download-limit": "1M",
		},
	})
	if err != nil {
		t.Fatalf("changeOption: %v", err)
	}
	if result != "OK" {
		t.Errorf("changeOption returned %v, want OK", result)
	}
}

func TestGetGlobalOption(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.getGlobalOption", []interface{}{})
	if err != nil {
		t.Fatalf("getGlobalOption: %v", err)
	}
	opts, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}
	if _, ok := opts["dir"]; !ok {
		t.Fatal("getGlobalOption missing dir")
	}
}

func TestChangeGlobalOption(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.changeGlobalOption", []interface{}{
		map[string]interface{}{
			"max-concurrent-downloads": "3",
		},
	})
	if err != nil {
		t.Fatalf("changeGlobalOption: %v", err)
	}
	if result != "OK" {
		t.Errorf("changeGlobalOption returned %v, want OK", result)
	}
}

func TestChangePosition(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	if err != nil {
		t.Fatalf("addUri: %v", err)
	}
	gid := result.(string)

	posResult, err := d.Call(context.Background(), "", "aria2.changePosition", []interface{}{
		gid, int64(3), "POS_SET",
	})
	if err != nil {
		t.Fatalf("changePosition: %v", err)
	}
	_, ok := posResult.(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", posResult)
	}
}

func TestChangeUri(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip", "http://backup.example.com/file.zip"},
	})
	if err != nil {
		t.Fatalf("addUri: %v", err)
	}
	gid := result.(string)

	uriResult, err := d.Call(context.Background(), "", "aria2.changeUri", []interface{}{
		gid,
		int64(1),
		[]interface{}{"http://backup.example.com/file.zip"},
		[]interface{}{"http://mirror.example.com/file.zip", "http://cdn.example.com/file.zip"},
		int64(0),
	})
	if err != nil {
		t.Fatalf("changeUri: %v", err)
	}
	requireInt64Pair(t, uriResult, 1, 2)

	urisResult, err := d.Call(context.Background(), "", "aria2.getUris", []interface{}{gid})
	if err != nil {
		t.Fatalf("getUris: %v", err)
	}
	uris, ok := urisResult.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", urisResult)
	}
	want := []string{
		"http://mirror.example.com/file.zip",
		"http://cdn.example.com/file.zip",
		"http://example.com/file.zip",
	}
	if len(uris) != len(want) {
		t.Fatalf("uris len = %d, want %d: %v", len(uris), len(want), uris)
	}
	for i, wantURI := range want {
		if uris[i]["uri"] != wantURI {
			t.Fatalf("uris[%d].uri = %v, want %s; full uris=%v", i, uris[i]["uri"], wantURI, uris)
		}
	}
}

func TestGetGlobalStat(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.getGlobalStat", []interface{}{})
	if err != nil {
		t.Fatalf("getGlobalStat: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}
	keys := []string{"downloadSpeed", "uploadSpeed", "numActive", "numWaiting", "numStopped", "numStoppedTotal"}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in globalStat", k)
		}
	}
}

func TestPurgeDownloadResult(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.purgeDownloadResult", []interface{}{})
	if err != nil {
		t.Fatalf("purgeDownloadResult: %v", err)
	}
	if result != "OK" {
		t.Errorf("expected OK, got %v", result)
	}
}

func TestRemoveDownloadResult(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	addResult, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	if err != nil {
		t.Fatalf("addUri: %v", err)
	}
	gid := addResult.(string)

	_, err = d.Call(context.Background(), "", "aria2.removeDownloadResult", []interface{}{gid})
	if err == nil {
		t.Fatal("expected error removing result for active/waiting download")
	}

	// Force-remove so it enters stopped queue.
	d.Call(context.Background(), "", "aria2.forceRemove", []interface{}{gid})

	result, err := d.Call(context.Background(), "", "aria2.removeDownloadResult", []interface{}{gid})
	if err != nil {
		t.Fatalf("removeDownloadResult: %v", err)
	}
	if result != "OK" {
		t.Errorf("expected OK, got %v", result)
	}
}

func TestShutdown(t *testing.T) {
	e, err := engine.New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = e.Run(ctx) }()
	time.Sleep(10 * time.Millisecond) // let Run set e.ctx

	d := New(e, Config{})
	result, err := d.Call(context.Background(), "", "aria2.shutdown", []interface{}{})
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if result != "OK" {
		t.Errorf("expected OK, got %v", result)
	}
}

func TestForceShutdown(t *testing.T) {
	e, err := engine.New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = e.Run(ctx) }()
	time.Sleep(10 * time.Millisecond)

	d := New(e, Config{})
	result, err := d.Call(context.Background(), "", "aria2.forceShutdown", []interface{}{})
	if err != nil {
		t.Fatalf("forceShutdown: %v", err)
	}
	if result != "OK" {
		t.Errorf("expected OK, got %v", result)
	}
}

func TestSaveSession(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "aria2.saveSession", []interface{}{})
	if err != nil {
		t.Fatalf("saveSession: %v", err)
	}
	if result != "OK" {
		t.Errorf("expected OK, got %v", result)
	}
}

func TestGetOption(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	addResult, _ := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	gid := addResult.(string)

	result, err := d.Call(context.Background(), "", "aria2.getOption", []interface{}{gid})
	if err != nil {
		t.Fatalf("getOption: %v", err)
	}
	opts, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}
	if _, ok := opts["dir"]; !ok {
		t.Fatal("getOption missing per-download dir")
	}
	if _, ok := opts["max-download-limit"]; !ok {
		t.Fatal("getOption missing per-download max-download-limit")
	}
	if _, ok := opts["enable-rpc"]; ok {
		t.Fatal("getOption included global-only enable-rpc")
	}
}

// ---- system.multicall tests ----

func TestSystemMulticall_Empty(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "system.multicall", []interface{}{
		[]interface{}{},
	})
	if err != nil {
		t.Fatalf("multicall: %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 0 {
		t.Errorf("expected empty, got %d elements", len(arr))
	}
}

func TestSystemMulticall_Success(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "system.multicall", []interface{}{
		[]interface{}{
			map[string]interface{}{
				"methodName": "aria2.getVersion",
				"params":     []interface{}{},
			},
		},
	})
	if err != nil {
		t.Fatalf("multicall: %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 result, got %d", len(arr))
	}
	// Success: wrapped in single-element array.
	inner, ok := arr[0].([]interface{})
	if !ok {
		t.Fatalf("expected []interface{} wrapper, got %T", arr[0])
	}
	if len(inner) != 1 {
		t.Fatalf("expected 1 element in wrapper, got %d", len(inner))
	}
	versionMap, ok := inner[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", inner[0])
	}
	if versionMap["version"] != "1.37.0" {
		t.Errorf("version = %v", versionMap["version"])
	}
}

func TestSystemMulticall_Fault(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "system.multicall", []interface{}{
		[]interface{}{
			map[string]interface{}{
				"methodName": "aria2.remove",
				"params":     []interface{}{"nonexistent_gid"},
			},
		},
	})
	if err != nil {
		t.Fatalf("multicall: %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 result, got %d", len(arr))
	}
	requireMulticallJSONFault(t, arr[0], "")
}

func TestSystemMulticall_Recursive_Forbidden(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "system.multicall", []interface{}{
		[]interface{}{
			map[string]interface{}{
				"methodName": "system.multicall",
				"params":     []interface{}{},
			},
		},
	})
	if err != nil {
		t.Fatalf("multicall: %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	requireMulticallJSONFault(t, arr[0], "Recursive system.multicall forbidden.")
}

func TestSystemMulticall_Mixed(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "system.multicall", []interface{}{
		[]interface{}{
			map[string]interface{}{
				"methodName": "aria2.getVersion",
				"params":     []interface{}{},
			},
			map[string]interface{}{
				"methodName": "aria2.remove",
				"params":     []interface{}{"bad_gid"},
			},
		},
	})
	if err != nil {
		t.Fatalf("multicall: %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 results, got %d", len(arr))
	}
	// First should be success (wrapped in []).
	_, ok = arr[0].([]interface{})
	if !ok {
		t.Errorf("first result should be success array, got %T", arr[0])
	}
	// Second should be fault.
	requireMulticallJSONFault(t, arr[1], "")
}

func TestSystemMulticall_WithSecretAuthorizesEachSubcall(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{Secret: "mysecret"})
	result, err := d.Call(context.Background(), "", "system.multicall", []interface{}{
		[]interface{}{
			map[string]interface{}{
				"methodName": "aria2.getVersion",
				"params":     []interface{}{"token:mysecret"},
			},
			map[string]interface{}{
				"methodName": "aria2.getVersion",
				"params":     []interface{}{},
			},
		},
	})
	if err != nil {
		t.Fatalf("top-level multicall should not require token: %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("result = %T, want []interface{} for mixed subcall auth", result)
	}
	if len(arr) != 2 {
		t.Fatalf("results len = %d, want 2", len(arr))
	}
	if _, ok := arr[0].([]interface{}); !ok {
		t.Fatalf("first subcall = %T, want success wrapper", arr[0])
	}
	requireMulticallJSONFault(t, arr[1], "Unauthorized")
}

// ---- system.listMethods / system.listNotifications ----

func TestSystemListMethods_RPC(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "system.listMethods", []interface{}{})
	if err != nil {
		t.Fatalf("listMethods: %v", err)
	}
	arr, ok := result.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", result)
	}
	if len(arr) == 0 {
		t.Error("listMethods returned empty")
	}
}

func TestSystemListNotifications_RPC(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "system.listNotifications", []interface{}{})
	if err != nil {
		t.Fatalf("listNotifications: %v", err)
	}
	arr, ok := result.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", result)
	}
	if len(arr) == 0 {
		t.Error("listNotifications returned empty")
	}
}

// ---- Error cases ----

func TestUnknownMethod(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.nonexistent", []interface{}{})
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
	if err.Error() != "No such method: aria2.nonexistent" {
		t.Errorf("error message = %q", err.Error())
	}
}

func TestMissingRequiredParam(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.remove", []interface{}{})
	if err == nil {
		t.Fatal("expected error for missing param")
	}
}

func TestWrongParamType(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.remove", []interface{}{
		int64(12345), // GID should be string
	})
	if err == nil {
		t.Fatal("expected error for wrong param type")
	}
}

func TestTellStatus_NotFound(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.tellStatus", []interface{}{
		"ffffffffffffffff",
	})
	if err == nil {
		t.Fatal("expected error for not-found GID")
	}
}

// ---- Notifications ----

func TestSubscribeNotifications(t *testing.T) {
	d, e := newTestDispatcher(t, Config{})

	var mu sync.Mutex
	var received []string
	sink := func(name string, params map[string]interface{}) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, name)
	}

	cancel := d.SubscribeNotifications(sink)
	defer cancel()

	// Add a download to generate an event.
	_, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	if err != nil {
		t.Fatalf("addUri: %v", err)
	}

	// Get active downloads to trigger pause events.
	active := e.TellActive()
	if len(active) > 0 {
		_ = e.Pause(active[0].GID, false)
		_ = e.Resume(active[0].GID)
	}

	// We should at least have some events.
	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Log("no notifications received (engine skeleton may not emit all events)")
	}
	for _, name := range received {
		t.Logf("received notification: %s", name)
	}
}

func TestSubscribeNotifications_Cancel(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})

	var mu sync.Mutex
	count := 0
	sink := func(name string, params map[string]interface{}) {
		mu.Lock()
		defer mu.Unlock()
		count++
	}

	cancel := d.SubscribeNotifications(sink)
	cancel()

	// Add a download.
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})

	// Cancelled sink should not receive events. Nil sinks are skipped.
	mu.Lock()
	defer mu.Unlock()
	// After cancellation, the sink is set to nil and should not be called.
	// We can't fully verify this, but the OnEvent should not panic.
	t.Logf("events after cancel: %d", count)
}

// ---- Extracted params ----

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name      string
		params    []interface{}
		wantToken string
		wantLen   int
	}{
		{"nil params", nil, "", 0},
		{"no token", []interface{}{"http://example.com"}, "", 1},
		{"with token", []interface{}{"token:mysecret", "arg1"}, "mysecret", 1},
		{"empty token value", []interface{}{"token:"}, "", 1},
		{"only token", []interface{}{"token:mysecret"}, "mysecret", 0},
		{"non-string first", []interface{}{int64(42), "arg1"}, "", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, remaining := extractToken(tt.params)
			if token != tt.wantToken {
				t.Errorf("token = %q, want %q", token, tt.wantToken)
			}
			if len(remaining) != tt.wantLen {
				t.Errorf("remaining len = %d, want %d", len(remaining), tt.wantLen)
			}
		})
	}
}

// ---- Param helpers ----

func TestParamString(t *testing.T) {
	p := []interface{}{"hello", int64(42)}
	s, err := paramString(p, 0)
	if err != nil {
		t.Fatalf("paramString index 0: %v", err)
	}
	if s != "hello" {
		t.Errorf("s = %q, want hello", s)
	}
	_, err = paramString(p, 1)
	if err == nil {
		t.Error("expected error for non-string param")
	}
	_, err = paramString(p, 2)
	if err == nil {
		t.Error("expected error for out-of-range param")
	}
}

func TestParamInt(t *testing.T) {
	p := []interface{}{int64(42), "hello"}
	v, err := paramInt(p, 0)
	if err != nil {
		t.Fatalf("paramInt index 0: %v", err)
	}
	if v != 42 {
		t.Errorf("v = %d, want 42", v)
	}
	_, err = paramInt(p, 1)
	if err == nil {
		t.Error("expected error for non-int param")
	}
}

func TestParamStringArray(t *testing.T) {
	p := []interface{}{[]interface{}{"a", "b"}}
	arr, err := paramStringArray(p, 0)
	if err != nil {
		t.Fatalf("paramStringArray: %v", err)
	}
	if len(arr) != 2 || arr[0] != "a" || arr[1] != "b" {
		t.Errorf("arr = %v", arr)
	}
}

func TestParamMap(t *testing.T) {
	p := []interface{}{map[string]interface{}{"key": "val"}}
	m, err := paramMap(p, 0)
	if err != nil {
		t.Fatalf("paramMap: %v", err)
	}
	if m["key"] != "val" {
		t.Errorf("m[key] = %v, want val", m["key"])
	}
}

// ---- Options conversion ----

func TestMapToOptions(t *testing.T) {
	m := map[string]interface{}{
		"dir":                      "/downloads",
		"max-concurrent-downloads": "5",
		"max-download-limit":       "1M",
	}
	opts := mapToOptions(m)
	if opts == nil {
		t.Fatal("mapToOptions returned nil")
	}
	if opts.Dir != "/downloads" {
		t.Errorf("Dir = %q, want /downloads", opts.Dir)
	}
	if opts.MaxConcurrentDownloads != 5 {
		t.Errorf("MaxConcurrentDownloads = %d, want 5", opts.MaxConcurrentDownloads)
	}
	if opts.MaxDownloadLimit != "1M" {
		t.Errorf("MaxDownloadLimit = %q, want 1M", opts.MaxDownloadLimit)
	}
}

func TestOptionsToMap(t *testing.T) {
	o := &config.Options{
		Dir:                    "/downloads",
		MaxConcurrentDownloads: 5,
		Split:                  3,
		Header:                 []string{"X-Matrix: config"},
	}
	m := optionsToMap(o)
	if m["dir"] != "/downloads" {
		t.Errorf("dir = %v", m["dir"])
	}
	if m["max-concurrent-downloads"] != "5" {
		t.Errorf("max-concurrent-downloads = %v", m["max-concurrent-downloads"])
	}
	if m["split"] != "3" {
		t.Errorf("split = %v", m["split"])
	}
	if m["header"] != "X-Matrix: config\n" {
		t.Errorf("header = %v, want X-Matrix: config\\n", m["header"])
	}
	for _, key := range []string{
		"allow-overwrite",
		"continue",
		"dry-run",
		"http-accept-gzip",
		"rpc-listen-all",
	} {
		if m[key] != "false" {
			t.Errorf("%s = %v, want false", key, m[key])
		}
	}
	if m["help"] != "#basic" {
		t.Errorf("help = %v, want #basic", m["help"])
	}
	if _, ok := m["out"]; ok {
		t.Error("undefined zero-valued out should be omitted")
	}
	if _, ok := m["rpc-secret"]; ok {
		t.Error("rpc-secret should be omitted")
	}
}

// ---- Dispatcher implements Subscriber ----

func TestDispatcherImplementsSubscriber(t *testing.T) {
	// Compile-time check is in dispatcher.go via var _ engine.Subscriber = (*Dispatcher)(nil)
	// This test ensures the method is callable.
	d, _ := newTestDispatcher(t, Config{})
	d.OnEvent(core.Event{
		Kind: core.EvComplete,
		GID:  1,
	})
	// Should not panic.
}

// ---- Concurrent access ----

func TestConcurrentCall(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = d.Call(context.Background(), "", "aria2.getVersion", []interface{}{})
		}()
	}
	wg.Wait()
}

func TestAddUri_WithoutUri(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{})
	if err == nil {
		t.Fatal("expected error for missing uris param")
	}
	_, err = d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error for empty uris list")
	}
}

func TestAddUri_WithBadOption(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file"},
		map[string]interface{}{
			"max-concurrent-downloads": "not_a_number",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddTorrent_WithoutTorrent(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.addTorrent", []interface{}{})
	if err == nil {
		t.Fatal("expected error for missing torrent param")
	}
}

func TestAddTorrent_NotBase64(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.addTorrent", []interface{}{
		"!!!not-valid-base64!!!",
		nil,
	})
	if err == nil {
		t.Fatal("expected error for invalid base64 torrent")
	}
}

func TestAddMetalink_WithoutMetalink(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.addMetalink", []interface{}{})
	if err == nil {
		t.Fatal("expected error for missing metalink param")
	}
}

func TestAddMetalink_NotBase64(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.addMetalink", []interface{}{
		"!!!not-valid-base64!!!",
	})
	if err == nil {
		t.Fatal("expected error for invalid base64 metalink")
	}
}

func TestAddMetalink_DecodesBase64BeforeSavingMetadata(t *testing.T) {
	dir := t.TempDir()
	opts := testOpts()
	opts.Dir = dir
	opts.RPCSaveUploadMetadata = true
	e, err := engine.New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("engine.New failed: %v", err)
	}
	d := New(e, Config{})

	metalinkData := validMetalinkData("from-rpc.bin")
	result, err := d.Call(context.Background(), "", "aria2.addMetalink", []interface{}{
		ariabase64.Encode(metalinkData),
	})
	if err != nil {
		t.Fatalf("addMetalink: %v", err)
	}
	gids, ok := result.([]string)
	if !ok || len(gids) != 1 {
		t.Fatalf("result = %#v, want one GID string", result)
	}

	hash := sha1.Sum(metalinkData)
	path := filepath.Join(dir, fmt.Sprintf("%x.meta4", hash))
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved metalink metadata: %v", err)
	}
	if !bytes.Equal(got, metalinkData) {
		t.Fatalf("saved metadata = %q, want decoded metalink bytes", got)
	}
}

func TestChangeOption_WithBadOption(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	addResult, _ := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	gid := addResult.(string)

	_, err := d.Call(context.Background(), "", "aria2.changeOption", []interface{}{
		gid,
		map[string]interface{}{
			"not-a-real-option": "value",
		},
	})
	if err != nil {
		t.Logf("got error (expected): %v", err)
	}
}

func TestChangeGlobalOption_WithBadOption(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.changeGlobalOption", []interface{}{
		map[string]interface{}{
			"max-concurrent-downloads": "not_a_number",
		},
	})
	if err != nil {
		t.Logf("got error (expected): %v", err)
	}
}

func TestTellStatus_WithoutGID(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.tellStatus", []interface{}{})
	if err == nil {
		t.Fatal("expected error for missing GID")
	}
}

func TestTellWaiting_Int32MaxOffset(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/a"},
	})
	result, err := d.Call(context.Background(), "", "aria2.tellWaiting", []interface{}{
		int64(2147483647), int64(10),
	})
	if err != nil {
		t.Fatalf("tellWaiting INT32_MAX offset: %v", err)
	}
	arr := result.([]map[string]interface{})
	if len(arr) != 0 {
		t.Logf("tellWaiting with large offset returned %d elements", len(arr))
	}
}

func TestTellWaiting_NegativeOffsetBeyond(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/a"},
	})
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/b"},
	})
	result, err := d.Call(context.Background(), "", "aria2.tellWaiting", []interface{}{
		int64(-100), int64(2),
	})
	if err != nil {
		t.Fatalf("tellWaiting large negative offset: %v", err)
	}
	arr := result.([]map[string]interface{})
	if len(arr) != 0 {
		t.Logf("tellWaiting with large negative offset returned %d elements", len(arr))
	}
}

func TestTellWaiting_Int32MinOffset(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/a"},
	})
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/b"},
	})
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/c"},
	})
	d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/d"},
	})
	// INT32_MIN offset is so negative that normalized offset < 0 -> empty.
	result, err := d.Call(context.Background(), "", "aria2.tellWaiting", []interface{}{
		int64(-2147483648), int64(100),
	})
	if err != nil {
		t.Fatalf("tellWaiting INT32_MIN offset: %v", err)
	}
	arr := result.([]map[string]interface{})
	if len(arr) != 0 {
		t.Errorf("expected empty, got %d", len(arr))
	}
}

func TestTellWaiting_Int32MaxOffsetAndNum(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	for i := 0; i < 4; i++ {
		d.Call(context.Background(), "", "aria2.addUri", []interface{}{
			[]interface{}{"http://example.com/file" + string(rune('a'+i))},
		})
	}
	// Both offset and num are INT32_MAX.
	result, err := d.Call(context.Background(), "", "aria2.tellWaiting", []interface{}{
		int64(2147483647), int64(2147483647),
	})
	if err != nil {
		t.Fatalf("tellWaiting INT32_MAX offset+num: %v", err)
	}
	arr := result.([]map[string]interface{})
	if len(arr) != 0 {
		t.Errorf("expected empty with INT32_MAX offset, got %d", len(arr))
	}
}

func TestTellWaiting_NegativeOffsetNormalizedBoundary(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	// Add exactly 4 downloads.
	for i := 0; i < 4; i++ {
		d.Call(context.Background(), "", "aria2.addUri", []interface{}{
			[]interface{}{"http://example.com/file" + string(rune('a'+i))},
		})
	}
	// offset=-5: normalized = 4 + (-5) = -1 < 0 → empty.
	result, err := d.Call(context.Background(), "", "aria2.tellWaiting", []interface{}{
		int64(-5), int64(100),
	})
	if err != nil {
		t.Fatalf("tellWaiting offset -5: %v", err)
	}
	arr := result.([]map[string]interface{})
	if len(arr) != 0 {
		t.Errorf("expected 0 with offset=-5 (normalized < 0), got %d", len(arr))
	}
	// offset=-4: normalized = 4 + (-4) = 0 → exactly first element.
	result, err = d.Call(context.Background(), "", "aria2.tellWaiting", []interface{}{
		int64(-4), int64(100),
	})
	if err != nil {
		t.Fatalf("tellWaiting offset -4: %v", err)
	}
	arr = result.([]map[string]interface{})
	if len(arr) != 1 {
		t.Errorf("expected 1 with offset=-4 (normalized == 0), got %d", len(arr))
	}
}

func TestChangePosition_MissingParams(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.changePosition", []interface{}{})
	if err == nil {
		t.Fatal("expected error for missing params")
	}
	_, err = d.Call(context.Background(), "", "aria2.changePosition", []interface{}{
		"ffffffffffffffff",
	})
	if err == nil {
		t.Fatal("expected error for missing position param")
	}
	_, err = d.Call(context.Background(), "", "aria2.changePosition", []interface{}{
		"ffffffffffffffff", int64(1),
	})
	if err == nil {
		t.Fatal("expected error for missing how param")
	}
	_, err = d.Call(context.Background(), "", "aria2.changePosition", []interface{}{
		"ffffffffffffffff", int64(1), "INVALID_HOW",
	})
	if err == nil {
		t.Fatal("expected error for invalid how value")
	}
}

func TestChangeUri_BadGID(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	_, err := d.Call(context.Background(), "", "aria2.changeUri", []interface{}{
		"ffffffffffffffff",
		int64(1),
		[]interface{}{},
		[]interface{}{"http://mirror.example.com/file.zip"},
	})
	if err == nil {
		t.Fatal("expected error for non-existent GID")
	}
}

func TestChangeUri_FileIndexOutOfRange(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	addResult, _ := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	gid := addResult.(string)
	// fileIndex=0: aria2 requires >=1, per C++ IntegerGE(1).
	_, err := d.Call(context.Background(), "", "aria2.changeUri", []interface{}{
		gid,
		int64(0),
		[]interface{}{},
		[]interface{}{"http://mirror.example.com/file.zip"},
	})
	if err == nil {
		t.Fatal("expected error for fileIndex 0")
	}
	_, err = d.Call(context.Background(), "", "aria2.changeUri", []interface{}{
		gid,
		int64(100),
		[]interface{}{},
		[]interface{}{"http://mirror.example.com/file.zip"},
	})
	if err == nil {
		t.Fatal("expected error for out-of-range fileIndex")
	}
}

func TestChangeUri_IndexEdgeCases(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	addResult, _ := d.Call(context.Background(), "", "aria2.addUri", []interface{}{
		[]interface{}{"http://example.com/file.zip"},
	})
	gid := addResult.(string)

	_, err := d.Call(context.Background(), "", "aria2.changeUri", []interface{}{
		gid,
		int64(0),
		[]interface{}{},
		[]interface{}{"http://mirror.example.com/file.zip"},
	})
	if err == nil {
		t.Error("expected error for fileIndex 0")
	}

	_, err = d.Call(context.Background(), "", "aria2.changeUri", []interface{}{
		gid,
		int64(1),
		"not_an_array",
		[]interface{}{"http://mirror.example.com/file.zip"},
	})
	if err == nil {
		t.Error("expected error for non-array delete URIs")
	}

	_, err = d.Call(context.Background(), "", "aria2.changeUri", []interface{}{
		gid,
		int64(1),
		[]interface{}{},
		"not_an_array",
	})
	if err == nil {
		t.Error("expected error for non-array add URIs")
	}
}

func TestSystemMulticall_MissingMethodName(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "system.multicall", []interface{}{
		[]interface{}{
			map[string]interface{}{
				"params": []interface{}{},
			},
		},
	})
	if err != nil {
		t.Fatalf("multicall: %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 result, got %d", len(arr))
	}
	requireMulticallJSONFault(t, arr[0], "Missing methodName.")
}

func TestSystemMulticall_MissingParams(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "system.multicall", []interface{}{
		[]interface{}{
			map[string]interface{}{
				"methodName": "aria2.getVersion",
			},
		},
	})
	if err != nil {
		t.Fatalf("multicall: %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 result, got %d", len(arr))
	}
	// Per C++: missing params maps to empty params, so getVersion should succeed.
	inner, ok := arr[0].([]interface{})
	if !ok {
		t.Fatalf("expected success wrapper []interface{}, got %T", arr[0])
	}
	if len(inner) != 1 {
		t.Fatalf("expected 1 result, got %d", len(inner))
	}
	versionMap, ok := inner[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected version map, got %T", inner[0])
	}
	if versionMap["version"] != "1.37.0" {
		t.Errorf("version = %v", versionMap["version"])
	}
}

func TestSystemMulticall_NonStructElements(t *testing.T) {
	d, _ := newTestDispatcher(t, Config{})
	result, err := d.Call(context.Background(), "", "system.multicall", []interface{}{
		[]interface{}{
			"not_a_struct",
			int64(42),
			3.14,
		},
	})
	if err != nil {
		t.Fatalf("multicall: %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 results, got %d", len(arr))
	}
	for _, elem := range arr {
		requireMulticallJSONFault(t, elem, "system.multicall expected struct.")
	}
}
