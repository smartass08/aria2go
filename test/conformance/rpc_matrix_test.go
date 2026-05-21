package conformance

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

var rpcGIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func TestRPC_MethodAvailabilityMatrix(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPair(t, []string{"--no-conf"}, []string{"--no-conf"})

	cases := []struct {
		name     string
		method   string
		expected []string
	}{
		{
			name:   "methods",
			method: "system.listMethods",
			expected: []string{
				"aria2.addMetalink",
				"aria2.addTorrent",
				"aria2.addUri",
				"aria2.changeGlobalOption",
				"aria2.changeOption",
				"aria2.changePosition",
				"aria2.changeUri",
				"aria2.forcePause",
				"aria2.forcePauseAll",
				"aria2.forceRemove",
				"aria2.forceShutdown",
				"aria2.getFiles",
				"aria2.getGlobalOption",
				"aria2.getGlobalStat",
				"aria2.getOption",
				"aria2.getPeers",
				"aria2.getServers",
				"aria2.getSessionInfo",
				"aria2.getUris",
				"aria2.getVersion",
				"aria2.pause",
				"aria2.pauseAll",
				"aria2.purgeDownloadResult",
				"aria2.remove",
				"aria2.removeDownloadResult",
				"aria2.saveSession",
				"aria2.shutdown",
				"aria2.tellActive",
				"aria2.tellStopped",
				"aria2.tellStatus",
				"aria2.tellWaiting",
				"aria2.unpause",
				"aria2.unpauseAll",
				"system.listMethods",
				"system.listNotifications",
				"system.multicall",
			},
		},
		{
			name:   "notifications",
			method: "system.listNotifications",
			expected: []string{
				"aria2.onDownloadStart",
				"aria2.onDownloadPause",
				"aria2.onDownloadStop",
				"aria2.onDownloadComplete",
				"aria2.onDownloadError",
				"aria2.onBtDownloadComplete",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := rpcStringListResult(t, rpcCallOK(t, refPort, tc.method, []any{}))
			impl := rpcStringListResult(t, rpcCallOK(t, implPort, tc.method, []any{}))

			compareStringSet(t, tc.method, ref, impl)
			requireStringSetEqual(t, "ref "+tc.method, ref, tc.expected)
			requireStringSetEqual(t, "impl "+tc.method, impl, tc.expected)
		})
	}
}

func TestRPC_AuthAndReadOnlyMatrix(t *testing.T) {
	SkipIfNoRef(t)

	const secret = "matrix-secret"
	refPort, implPort := startRPCPair(t,
		[]string{"--no-conf", "--rpc-secret=" + secret},
		[]string{"--no-conf", "--rpc-secret=" + secret},
	)

	cases := []struct {
		name     string
		method   string
		params   []any
		wantOK   bool
		readOnly bool
	}{
		{
			name:     "listMethods without token",
			method:   "system.listMethods",
			params:   []any{},
			wantOK:   true,
			readOnly: true,
		},
		{
			name:     "listNotifications without token",
			method:   "system.listNotifications",
			params:   []any{},
			wantOK:   true,
			readOnly: true,
		},
		{
			name:   "getVersion with correct token",
			method: "aria2.getVersion",
			params: []any{"token:" + secret},
			wantOK: true,
		},
		{
			name:   "getVersion missing token",
			method: "aria2.getVersion",
			params: []any{},
		},
		{
			name:   "getVersion wrong token",
			method: "aria2.getVersion",
			params: []any{"token:wrong"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refStatus, ref := rpcCallWithHTTPStatus(t, refPort, tc.method, tc.params)
			implStatus, impl := rpcCallWithHTTPStatus(t, implPort, tc.method, tc.params)

			if tc.wantOK {
				if refStatus != http.StatusOK {
					t.Errorf("ref HTTP status got %d want %d", refStatus, http.StatusOK)
				}
				if implStatus != refStatus {
					t.Errorf("impl HTTP status got %d want ref status %d", implStatus, refStatus)
				}
				requireRPCSuccess(t, "ref "+tc.method, ref)
				requireRPCSuccess(t, "impl "+tc.method, impl)
				if tc.readOnly {
					refValues := rpcStringListResult(t, ref)
					implValues := rpcStringListResult(t, impl)
					compareStringSet(t, tc.method, refValues, implValues)
				}
				return
			}
			if refStatus == http.StatusOK {
				t.Errorf("ref HTTP status got %d, want error status", refStatus)
			}
			if implStatus != refStatus {
				t.Errorf("impl HTTP status got %d want ref status %d", implStatus, refStatus)
			}
			requireRPCErrorEqual(t, tc.method, ref, impl)
			requireRPCErrorCode(t, "ref "+tc.method, ref, 1)
			requireRPCErrorCode(t, "impl "+tc.method, impl, 1)
		})
	}

	t.Run("multicall nested tokens", func(t *testing.T) {
		methods := []map[string]any{
			{"methodName": "aria2.getGlobalStat", "params": []any{}},
			{"methodName": "aria2.getGlobalStat", "params": []any{"token:" + secret}},
			{"methodName": "system.listNotifications", "params": []any{}},
		}
		refStatus, ref := rpcCallWithHTTPStatus(t, refPort, "system.multicall", []any{methods})
		implStatus, impl := rpcCallWithHTTPStatus(t, implPort, "system.multicall", []any{methods})
		if refStatus != implStatus {
			t.Errorf("HTTP status mismatch: ref=%d impl=%d", refStatus, implStatus)
		}
		requireRPCSuccess(t, "ref system.multicall", ref)
		requireRPCSuccess(t, "impl system.multicall", impl)
		compareMulticallSuccessShapes(t, "system.multicall nested tokens", ref.Result, impl.Result, []int{1, 2})
		requireMulticallNestedErrorShape(t, "ref system.multicall nested auth error", ref.Result, 0)
		requireMulticallNestedErrorShape(t, "impl system.multicall nested auth error", impl.Result, 0)
	})
}

func TestRPC_QueueControlMatrix(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	refDir := filepath.Join(dir, "ref")
	implDir := filepath.Join(dir, "impl")
	refSession := filepath.Join(dir, "ref.session")
	implSession := filepath.Join(dir, "impl.session")
	refPort, implPort := startRPCPair(t,
		[]string{"--no-conf", "--dir=" + refDir, "--save-session=" + refSession},
		[]string{"--no-conf", "--dir=" + implDir, "--save-session=" + implSession},
	)

	const gidA = "00000000000000a1"
	const gidB = "00000000000000b2"
	addPausedURI(t, refPort, gidA, "http://127.0.0.1:1/a")
	addPausedURI(t, refPort, gidB, "http://127.0.0.1:1/b")
	addPausedURI(t, implPort, gidA, "http://127.0.0.1:1/a")
	addPausedURI(t, implPort, gidB, "http://127.0.0.1:1/b")

	cases := []struct {
		name   string
		method string
		params []any
		assert func(t *testing.T, label string, ref, impl rpcResponse)
	}{
		{
			name:   "addUri rejects negative position",
			method: "aria2.addUri",
			params: []any{[]string{"http://127.0.0.1:1/negative"}, map[string]string{}, float64(-1)},
			assert: assertRPCErrorSame,
		},
		{
			name:   "tellStatus key filter",
			method: "aria2.tellStatus",
			params: []any{gidA, []string{"gid", "status"}},
			assert: assertRPCSameJSON,
		},
		{
			name:   "tellWaiting first page",
			method: "aria2.tellWaiting",
			params: []any{float64(0), float64(2), []string{"gid", "status"}},
			assert: assertRPCSameJSON,
		},
		{
			name:   "tellWaiting reverse page",
			method: "aria2.tellWaiting",
			params: []any{float64(-1), float64(2), []string{"gid", "status"}},
			assert: assertRPCSameJSON,
		},
		{
			name:   "tellStopped empty page",
			method: "aria2.tellStopped",
			params: []any{float64(0), float64(5), []string{"gid", "status"}},
			assert: assertRPCSameJSON,
		},
		{
			name:   "getUris",
			method: "aria2.getUris",
			params: []any{gidA},
			assert: assertRPCShapeSlice,
		},
		{
			name:   "getFiles",
			method: "aria2.getFiles",
			params: []any{gidA},
			assert: assertRPCShapeSlice,
		},
		{
			name:   "getPeers non-bittorrent",
			method: "aria2.getPeers",
			params: []any{gidA},
			assert: assertRPCSameJSON,
		},
		{
			name:   "getOption",
			method: "aria2.getOption",
			params: []any{gidA},
			assert: assertRPCSameObjectKeys,
		},
		{
			name:   "changeOption",
			method: "aria2.changeOption",
			params: []any{gidA, map[string]string{"max-download-limit": "1M"}},
			assert: assertRPCStringResult("OK"),
		},
		{
			name:   "changeGlobalOption",
			method: "aria2.changeGlobalOption",
			params: []any{map[string]string{"max-concurrent-downloads": "2"}},
			assert: assertRPCStringResult("OK"),
		},
		{
			name:   "getGlobalOption",
			method: "aria2.getGlobalOption",
			params: []any{},
			assert: assertRPCSameGlobalOptionKeys,
		},
		{
			name:   "getGlobalStat",
			method: "aria2.getGlobalStat",
			params: []any{},
			assert: assertRPCSameObjectKeys,
		},
		{
			name:   "getVersion",
			method: "aria2.getVersion",
			params: []any{},
			assert: assertRPCSameObjectKeys,
		},
		{
			name:   "getSessionInfo",
			method: "aria2.getSessionInfo",
			params: []any{},
			assert: assertRPCSameObjectKeys,
		},
		{
			name:   "saveSession",
			method: "aria2.saveSession",
			params: []any{},
			assert: assertRPCStringResult("OK"),
		},
		{
			name:   "pauseAll",
			method: "aria2.pauseAll",
			params: []any{},
			assert: assertRPCStringResult("OK"),
		},
		{
			name:   "unpauseAll",
			method: "aria2.unpauseAll",
			params: []any{},
			assert: assertRPCStringResult("OK"),
		},
		{
			name:   "forcePauseAll",
			method: "aria2.forcePauseAll",
			params: []any{},
			assert: assertRPCStringResult("OK"),
		},
		{
			name:   "purgeDownloadResult",
			method: "aria2.purgeDownloadResult",
			params: []any{},
			assert: assertRPCStringResult("OK"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := rpcCall(t, refPort, tc.method, tc.params)
			impl := rpcCall(t, implPort, tc.method, tc.params)
			tc.assert(t, tc.method, ref, impl)
		})
	}

	t.Run("pause unpause remove force variants", func(t *testing.T) {
		release := make(chan struct{})
		fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "1048576")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(bytes.Repeat([]byte("x"), 1024))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}))
		defer fileSrv.Close()
		defer close(release)

		const gidC = "00000000000000c3"
		const gidD = "00000000000000d4"
		addURIWithGID(t, refPort, gidC, fileSrv.URL+"/c-ref")
		addURIWithGID(t, implPort, gidC, fileSrv.URL+"/c-impl")
		addURIWithGID(t, refPort, gidD, fileSrv.URL+"/d-ref")
		addURIWithGID(t, implPort, gidD, fileSrv.URL+"/d-impl")
		waitForRPCStatus(t, refPort, gidC, "active")
		waitForRPCStatus(t, implPort, gidC, "active")
		waitForRPCStatus(t, refPort, gidD, "active")
		waitForRPCStatus(t, implPort, gidD, "active")

		sequence := []struct {
			method string
			gid    string
			wait   bool
		}{
			{method: "aria2.pause", gid: gidC},
			{method: "aria2.unpause", gid: gidC, wait: true},
			{method: "aria2.forcePause", gid: gidC},
			{method: "aria2.remove", gid: gidC},
			{method: "aria2.forceRemove", gid: gidD},
		}
		for _, step := range sequence {
			t.Run(step.method, func(t *testing.T) {
				ref := rpcCallOK(t, refPort, step.method, []any{step.gid})
				impl := rpcCallOK(t, implPort, step.method, []any{step.gid})
				if got := rpcResultString(t, ref); got != step.gid {
					t.Errorf("ref result got %q want %q", got, step.gid)
				}
				if got := rpcResultString(t, impl); got != step.gid {
					t.Errorf("impl result got %q want %q", got, step.gid)
				}
				if step.wait {
					waitForRPCStatus(t, refPort, step.gid, "active")
					waitForRPCStatus(t, implPort, step.gid, "active")
				}
			})
		}
	})

	if _, err := os.Stat(refSession); err != nil {
		t.Errorf("ref saveSession did not create %s: %v", refSession, err)
	}
	if _, err := os.Stat(implSession); err != nil {
		t.Errorf("impl saveSession did not create %s: %v", implSession, err)
	}
}

func TestRPC_ActiveHTTPFixtureMatrix(t *testing.T) {
	SkipIfNoRef(t)

	release := make(chan struct{})
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 1024))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer fileSrv.Close()
	defer close(release)

	dir := t.TempDir()
	refPort, implPort := startRPCPair(t,
		[]string{"--no-conf", "--dir=" + filepath.Join(dir, "ref")},
		[]string{"--no-conf", "--dir=" + filepath.Join(dir, "impl")},
	)
	refGID := rpcResultString(t, rpcCallOK(t, refPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/active-ref"}}))
	implGID := rpcResultString(t, rpcCallOK(t, implPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/active-impl"}}))

	waitForRPCStatus(t, refPort, refGID, "active")
	waitForRPCStatus(t, implPort, implGID, "active")

	cases := []struct {
		name       string
		method     string
		refParams  []any
		implParams []any
		assert     func(t *testing.T, label string, ref, impl rpcResponse)
	}{
		{
			name:       "tellActive key filter",
			method:     "aria2.tellActive",
			refParams:  []any{[]string{"gid", "status"}},
			implParams: []any{[]string{"gid", "status"}},
			assert:     assertRPCShapeSlice,
		},
		{
			name:       "getServers active download",
			method:     "aria2.getServers",
			refParams:  []any{refGID},
			implParams: []any{implGID},
			assert:     assertRPCShapeSlice,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := rpcCallOK(t, refPort, tc.method, tc.refParams)
			impl := rpcCallOK(t, implPort, tc.method, tc.implParams)
			tc.assert(t, tc.method, ref, impl)
		})
	}
}

func TestRPC_UploadedMetadataMatrix(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	refPort, implPort := startRPCPair(t,
		[]string{
			"--no-conf",
			"--dir=" + filepath.Join(dir, "ref"),
			"--pause=true",
			"--rpc-save-upload-metadata=false",
			"--enable-dht=false",
			"--bt-enable-lpd=false",
		},
		[]string{
			"--no-conf",
			"--dir=" + filepath.Join(dir, "impl"),
			"--pause=true",
			"--rpc-save-upload-metadata=false",
			"--enable-dht=false",
			"--bt-enable-lpd=false",
		},
	)

	cases := []struct {
		name       string
		method     string
		fixtureRel string
		params     func(payload string) []any
		assert     func(t *testing.T, ref, impl rpcResponse) (refGIDs, implGIDs []string)
	}{
		{
			name:       "addTorrent valid fixture",
			method:     "aria2.addTorrent",
			fixtureRel: "internal/torrent/testdata/single.torrent",
			params: func(payload string) []any {
				return []any{payload, []string{}, map[string]string{"pause": "true"}}
			},
			assert: func(t *testing.T, ref, impl rpcResponse) ([]string, []string) {
				refGID := requireRPCGIDResult(t, "ref addTorrent", ref)
				implGID := requireRPCGIDResult(t, "impl addTorrent", impl)
				return []string{refGID}, []string{implGID}
			},
		},
		{
			name:       "addMetalink valid fixture",
			method:     "aria2.addMetalink",
			fixtureRel: "internal/protocol/metalink/testdata/basic.meta4",
			params: func(payload string) []any {
				return []any{payload, map[string]string{"pause": "true"}}
			},
			assert: func(t *testing.T, ref, impl rpcResponse) ([]string, []string) {
				refGIDs := requireRPCGIDListResult(t, "ref addMetalink", ref)
				implGIDs := requireRPCGIDListResult(t, "impl addMetalink", impl)
				if len(refGIDs) != len(implGIDs) {
					t.Fatalf("addMetalink GID count mismatch: ref=%d impl=%d", len(refGIDs), len(implGIDs))
				}
				return refGIDs, implGIDs
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := base64.StdEncoding.EncodeToString(readFixture(t, tc.fixtureRel))
			ref := rpcCall(t, refPort, tc.method, tc.params(payload))
			impl := rpcCall(t, implPort, tc.method, tc.params(payload))
			refGIDs, implGIDs := tc.assert(t, ref, impl)

			for i := range refGIDs {
				refStatus := rpcCallOK(t, refPort, "aria2.tellStatus", []any{refGIDs[i], []string{"status"}})
				implStatus := rpcCallOK(t, implPort, "aria2.tellStatus", []any{implGIDs[i], []string{"status"}})
				compareJSONValueEqual(t, tc.method+" uploaded status", refStatus.Result, implStatus.Result)
				requireStringMapValues(t, "ref uploaded status", refStatus.Result, map[string]string{"status": "paused"})
				requireStringMapValues(t, "impl uploaded status", implStatus.Result, map[string]string{"status": "paused"})
			}
		})
	}

	t.Run("invalid uploaded metadata", func(t *testing.T) {
		for _, method := range []string{"aria2.addTorrent", "aria2.addMetalink"} {
			t.Run(method, func(t *testing.T) {
				ref := rpcCall(t, refPort, method, []any{"ZHVtbXk="})
				impl := rpcCall(t, implPort, method, []any{"ZHVtbXk="})
				requireRPCErrorEqual(t, method, ref, impl)
				requireRPCErrorCode(t, "ref "+method, ref, 1)
				requireRPCErrorCode(t, "impl "+method, impl, 1)
			})
		}
	})
}

func startRPCPair(t *testing.T, refArgs, implArgs []string) (refPort, implPort int) {
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

func rpcCallWithHTTPStatus(t *testing.T, port int, method string, params []any) (int, rpcResponse) {
	t.Helper()

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/jsonrpc"
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", method, err)
	}
	defer resp.Body.Close()

	var rr rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		t.Fatalf("decode response for %s: %v (status=%d)", method, err, resp.StatusCode)
	}
	return resp.StatusCode, rr
}

func rpcStringListResult(t *testing.T, rr rpcResponse) []string {
	t.Helper()

	var values []string
	if err := json.Unmarshal(rr.Result, &values); err != nil {
		t.Fatalf("unmarshal string list: %v (raw=%s)", err, string(rr.Result))
	}
	return values
}

func requireStringSetEqual(t *testing.T, label string, got, want []string) {
	t.Helper()

	gotSet := make(map[string]int, len(got))
	for _, value := range got {
		gotSet[value]++
	}
	wantSet := make(map[string]int, len(want))
	for _, value := range want {
		wantSet[value]++
	}
	for value, wantCount := range wantSet {
		if gotSet[value] != wantCount {
			t.Errorf("%s: %q count got %d want %d", label, value, gotSet[value], wantCount)
		}
	}
	for value, gotCount := range gotSet {
		if wantSet[value] != gotCount {
			t.Errorf("%s: unexpected %q count %d", label, value, gotCount)
		}
	}
}

func requireRPCSuccess(t *testing.T, label string, rr rpcResponse) {
	t.Helper()

	if rr.Error != nil {
		t.Fatalf("%s returned error: code=%d message=%s", label, rr.Error.Code, rr.Error.Message)
	}
}

func requireRPCErrorEqual(t *testing.T, label string, ref, impl rpcResponse) {
	t.Helper()

	if ref.Error == nil {
		t.Fatalf("%s ref succeeded, want error result=%s", label, string(ref.Result))
	}
	if impl.Error == nil {
		t.Fatalf("%s impl succeeded, want error result=%s", label, string(impl.Result))
	}
	if ref.Error.Code != impl.Error.Code {
		t.Errorf("%s error code mismatch: ref=%d impl=%d", label, ref.Error.Code, impl.Error.Code)
	}
	if ref.Error.Message != impl.Error.Message {
		t.Errorf("%s error message mismatch: ref=%q impl=%q", label, ref.Error.Message, impl.Error.Message)
	}
}

func requireRPCErrorCode(t *testing.T, label string, rr rpcResponse, want int) {
	t.Helper()

	if rr.Error == nil {
		t.Fatalf("%s succeeded, want error", label)
	}
	if rr.Error.Code != want {
		t.Errorf("%s error code got %d want %d", label, rr.Error.Code, want)
	}
}

func assertRPCErrorSame(t *testing.T, label string, ref, impl rpcResponse) {
	t.Helper()
	requireRPCErrorEqual(t, label, ref, impl)
}

func assertRPCSameJSON(t *testing.T, label string, ref, impl rpcResponse) {
	t.Helper()
	requireRPCSuccess(t, "ref "+label, ref)
	requireRPCSuccess(t, "impl "+label, impl)
	compareJSONValueEqual(t, label, ref.Result, impl.Result)
}

func assertRPCSameObjectKeys(t *testing.T, label string, ref, impl rpcResponse) {
	t.Helper()
	requireRPCSuccess(t, "ref "+label, ref)
	requireRPCSuccess(t, "impl "+label, impl)
	compareJSONObjectKeysExact(t, ref.Result, impl.Result)
}

func assertRPCSameGlobalOptionKeys(t *testing.T, label string, ref, impl rpcResponse) {
	t.Helper()
	requireRPCSuccess(t, "ref "+label, ref)
	requireRPCSuccess(t, "impl "+label, impl)
	compareGlobalOptionKeysExact(t, ref.Result, impl.Result)
}

func assertRPCShapeSlice(t *testing.T, label string, ref, impl rpcResponse) {
	t.Helper()
	requireRPCSuccess(t, "ref "+label, ref)
	requireRPCSuccess(t, "impl "+label, impl)
	compareJSONShapeSlice(t, ref.Result, impl.Result)
}

func assertRPCStringResult(want string) func(*testing.T, string, rpcResponse, rpcResponse) {
	return func(t *testing.T, label string, ref, impl rpcResponse) {
		t.Helper()
		requireRPCSuccess(t, "ref "+label, ref)
		requireRPCSuccess(t, "impl "+label, impl)
		if got := rpcResultString(t, ref); got != want {
			t.Errorf("ref %s result got %q want %q", label, got, want)
		}
		if got := rpcResultString(t, impl); got != want {
			t.Errorf("impl %s result got %q want %q", label, got, want)
		}
	}
}

func requireRPCGIDResult(t *testing.T, label string, rr rpcResponse) string {
	t.Helper()
	requireRPCSuccess(t, label, rr)
	gid := rpcResultString(t, rr)
	if !rpcGIDPattern.MatchString(gid) {
		t.Fatalf("%s GID got %q, want 16-char lowercase hex", label, gid)
	}
	return gid
}

func requireRPCGIDListResult(t *testing.T, label string, rr rpcResponse) []string {
	t.Helper()
	requireRPCSuccess(t, label, rr)

	var gids []string
	if err := json.Unmarshal(rr.Result, &gids); err != nil {
		t.Fatalf("%s unmarshal GID list: %v (raw=%s)", label, err, string(rr.Result))
	}
	if len(gids) == 0 {
		t.Fatalf("%s returned empty GID list", label)
	}
	for i, gid := range gids {
		if !rpcGIDPattern.MatchString(gid) {
			t.Fatalf("%s[%d] GID got %q, want 16-char lowercase hex", label, i, gid)
		}
	}
	return gids
}

func addURIWithGID(t *testing.T, port int, gid string, uri string) string {
	t.Helper()

	rr := rpcCallOK(t, port, "aria2.addUri", []any{
		[]string{uri},
		map[string]string{"gid": gid},
	})
	got := rpcResultString(t, rr)
	if got != gid {
		t.Fatalf("addUri fixed gid: got %s want %s", got, gid)
	}
	return got
}

func readFixture(t *testing.T, rel string) []byte {
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

func waitForRPCStatus(t *testing.T, port int, gid string, want string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rr := rpcCall(t, port, "aria2.tellStatus", []any{gid, []string{"status"}})
		if rr.Error == nil {
			values := mustStringMap(t, "tellStatus status", rr.Result)
			if values["status"] == want {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("GID %s on port %d did not reach status %q", gid, port, want)
}
