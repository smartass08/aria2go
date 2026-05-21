package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestRPC_SystemListMethods(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "system.listMethods", []any{})
	ir := rpcCallOK(t, iPort, "system.listMethods", []any{})

	var refMethods, implMethods []string
	if err := json.Unmarshal(rr.Result, &refMethods); err != nil {
		t.Fatalf("unmarshal ref: %v", err)
	}
	if err := json.Unmarshal(ir.Result, &implMethods); err != nil {
		t.Fatalf("unmarshal impl: %v", err)
	}

	compareStringSet(t, "system.listMethods", refMethods, implMethods)
	t.Logf("ref methods: %d, impl methods: %d", len(refMethods), len(implMethods))
}

func TestRPC_SystemListNotifications(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "system.listNotifications", []any{})
	ir := rpcCallOK(t, iPort, "system.listNotifications", []any{})

	var refNots, implNots []string
	if err := json.Unmarshal(rr.Result, &refNots); err != nil {
		t.Fatalf("unmarshal ref notifications: %v", err)
	}
	if err := json.Unmarshal(ir.Result, &implNots); err != nil {
		t.Fatalf("unmarshal impl notifications: %v", err)
	}
	compareStringSet(t, "system.listNotifications", refNots, implNots)
}

func TestRPC_AddUri(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	// addUri returns a GID string.
	rr := rpcCall(t, rPort, "aria2.addUri", []any{[]string{"http://localhost:1/nonexistent"}})
	ir := rpcCall(t, iPort, "aria2.addUri", []any{[]string{"http://localhost:1/nonexistent"}})

	if rr.Error != nil && ir.Error != nil {
		t.Logf("both returned error (expected for unreachable URI): ref=%s, impl=%s", rr.Error.Message, ir.Error.Message)
		return
	}

	refGID := rpcResultString(t, rr)
	implGID := rpcResultString(t, ir)

	if refGID == "" {
		t.Error("ref addUri returned empty GID")
	}
	if implGID == "" {
		t.Error("impl addUri returned empty GID")
	}
	t.Logf("addUri GID: ref=%s impl=%s", refGID, implGID)
}

func TestRPC_GetVersion(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getVersion", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getVersion", []any{})

	compareJSONObjectKeysExact(t, rr.Result, ir.Result)

	compareJSONStringFields(t, "aria2.getVersion", rr.Result, ir.Result, []string{"version"})
}

func TestRPC_GetSessionInfo(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getSessionInfo", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getSessionInfo", []any{})

	compareJSONObjectKeysExact(t, rr.Result, ir.Result)
}

func TestRPC_GetGlobalStat(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getGlobalStat", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getGlobalStat", []any{})

	compareJSONObjectKeysExact(t, rr.Result, ir.Result)
	compareStringMapValues(t, "aria2.getGlobalStat", rr.Result, ir.Result, []string{
		"downloadSpeed",
		"uploadSpeed",
		"numActive",
		"numWaiting",
		"numStopped",
		"numStoppedTotal",
	})
}

func TestRPC_GetGlobalOption(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getGlobalOption", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getGlobalOption", []any{})

	compareStringMapValues(t, "aria2.getGlobalOption defaults", rr.Result, ir.Result, []string{
		"max-concurrent-downloads",
		"max-connection-per-server",
		"split",
		"continue",
		"allow-overwrite",
		"check-certificate",
	})
}

func TestRPC_GetGlobalOptionKeySetExact(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getGlobalOption", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getGlobalOption", []any{})
	compareGlobalOptionKeysExact(t, rr.Result, ir.Result)
}

func TestRPC_GetGlobalOptionSecretRedactionAndValues(t *testing.T) {
	SkipIfNoRef(t)

	rPort, iPort := startPairedRPCServers(t,
		"--dir=/tmp",
		"--rpc-secret=conformance-secret",
		"--max-concurrent-downloads=7",
		"--split=3",
		"--continue=false",
	)

	params := []any{"token:conformance-secret"}
	rr := rpcCallOK(t, rPort, "aria2.getGlobalOption", params)
	ir := rpcCallOK(t, iPort, "aria2.getGlobalOption", params)

	compareGlobalOptionKeysExact(t, rr.Result, ir.Result)
	compareStringMapValues(t, "aria2.getGlobalOption configured", rr.Result, ir.Result, []string{
		"dir",
		"max-concurrent-downloads",
		"split",
		"continue",
	})
	expected := map[string]string{
		"dir":                      "/tmp",
		"max-concurrent-downloads": "7",
		"split":                    "3",
		"continue":                 "false",
	}
	requireStringMapValues(t, "ref getGlobalOption configured", rr.Result, expected)
	requireStringMapValues(t, "impl getGlobalOption configured", ir.Result, expected)
	requireStringMapAbsent(t, "ref getGlobalOption", rr.Result, []string{"rpc-secret"})
	requireStringMapAbsent(t, "impl getGlobalOption", ir.Result, []string{"rpc-secret"})
}

func TestRPC_TellActive(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.tellActive", []any{})
	ir := rpcCallOK(t, iPort, "aria2.tellActive", []any{})

	compareJSONShapeSlice(t, rr.Result, ir.Result)
}

func TestRPC_TellWaiting(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.tellWaiting", []any{float64(0), float64(10)})
	ir := rpcCallOK(t, iPort, "aria2.tellWaiting", []any{float64(0), float64(10)})

	compareJSONShapeSlice(t, rr.Result, ir.Result)
}

func TestRPC_TellStopped(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.tellStopped", []any{float64(0), float64(10)})
	ir := rpcCallOK(t, iPort, "aria2.tellStopped", []any{float64(0), float64(10)})

	compareJSONShapeSlice(t, rr.Result, ir.Result)
}

func TestRPC_AddAndTellStatus(t *testing.T) {
	SkipIfNoRef(t)

	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")
	const gid = "00000000000000a1"
	addPausedURI(t, rPort, gid, "http://127.0.0.1:1/add-and-status")
	addPausedURI(t, iPort, gid, "http://127.0.0.1:1/add-and-status")

	// Call tellStatus for each.
	rr := rpcCallOK(t, rPort, "aria2.tellStatus", []any{gid})
	ir := rpcCallOK(t, iPort, "aria2.tellStatus", []any{gid})

	compareJSONObjectKeysExact(t, rr.Result, ir.Result)

	// Verify key fields exist in ref.
	var refStatus map[string]json.RawMessage
	if err := json.Unmarshal(rr.Result, &refStatus); err != nil {
		t.Fatalf("unmarshal ref status: %v", err)
	}

	requiredKeys := []string{"gid", "status", "totalLength", "completedLength", "downloadSpeed", "uploadSpeed", "connections"}
	for _, k := range requiredKeys {
		if _, ok := refStatus[k]; !ok {
			t.Errorf("key %q missing from ref tellStatus", k)
		}
	}
	implStatus := mustJSONMap(t, "impl tellStatus", ir.Result)
	for _, k := range requiredKeys {
		if _, ok := implStatus[k]; !ok {
			t.Errorf("key %q missing from impl tellStatus", k)
		}
	}
}

func TestRPC_AddUriFixedGIDPauseAndTellStatus(t *testing.T) {
	SkipIfNoRef(t)

	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")
	const gid = "00000000000000a1"
	uri := "http://127.0.0.1:1/fixed-gid"

	refGID := addPausedURI(t, rPort, gid, uri)
	implGID := addPausedURI(t, iPort, gid, uri)
	if refGID != implGID {
		t.Fatalf("fixed GID mismatch: ref=%s impl=%s", refGID, implGID)
	}

	rr := rpcCallOK(t, rPort, "aria2.tellStatus", []any{gid, []string{"gid", "status"}})
	ir := rpcCallOK(t, iPort, "aria2.tellStatus", []any{gid, []string{"gid", "status"}})
	compareJSONValueEqual(t, "paused fixed-gid tellStatus", rr.Result, ir.Result)
	requireStringMapValues(t, "paused fixed-gid tellStatus", ir.Result, map[string]string{
		"gid":    gid,
		"status": "paused",
	})
}

func TestRPC_AddUriDefaultAppendAndExplicitPosition(t *testing.T) {
	SkipIfNoRef(t)

	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")
	const gidA = "00000000000000a1"
	const gidB = "00000000000000b2"
	const gidC = "00000000000000c3"

	addPausedURI(t, rPort, gidA, "http://127.0.0.1:1/a")
	addPausedURI(t, rPort, gidB, "http://127.0.0.1:1/b")
	addPausedURIAt(t, rPort, gidC, "http://127.0.0.1:1/c", 1)

	addPausedURI(t, iPort, gidA, "http://127.0.0.1:1/a")
	addPausedURI(t, iPort, gidB, "http://127.0.0.1:1/b")
	addPausedURIAt(t, iPort, gidC, "http://127.0.0.1:1/c", 1)

	params := []any{float64(0), float64(10), []string{"gid"}}
	rr := rpcCallOK(t, rPort, "aria2.tellWaiting", params)
	ir := rpcCallOK(t, iPort, "aria2.tellWaiting", params)
	compareJSONValueEqual(t, "addUri append and explicit position", rr.Result, ir.Result)
	requireGIDSequence(t, "ref addUri append and explicit position", rr.Result, []string{gidA, gidC, gidB})
	requireGIDSequence(t, "impl addUri append and explicit position", ir.Result, []string{gidA, gidC, gidB})
}

func TestRPC_TellWaitingKeyFilterAndNegativeOffset(t *testing.T) {
	SkipIfNoRef(t)

	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")
	const gidA = "00000000000000a1"
	const gidB = "00000000000000b2"
	addPausedURI(t, rPort, gidA, "http://127.0.0.1:1/a")
	addPausedURI(t, rPort, gidB, "http://127.0.0.1:1/b")
	addPausedURI(t, iPort, gidA, "http://127.0.0.1:1/a")
	addPausedURI(t, iPort, gidB, "http://127.0.0.1:1/b")

	params := []any{float64(0), float64(10), []string{"gid", "status"}}
	rr := rpcCallOK(t, rPort, "aria2.tellWaiting", params)
	ir := rpcCallOK(t, iPort, "aria2.tellWaiting", params)
	compareJSONValueEqual(t, "tellWaiting filtered", rr.Result, ir.Result)
	requireStringMapSliceKeysExact(t, "ref tellWaiting filtered", rr.Result, []string{"gid", "status"})
	requireStringMapSliceKeysExact(t, "impl tellWaiting filtered", ir.Result, []string{"gid", "status"})

	reverseParams := []any{float64(-1), float64(2), []string{"gid", "status"}}
	rrRev := rpcCallOK(t, rPort, "aria2.tellWaiting", reverseParams)
	irRev := rpcCallOK(t, iPort, "aria2.tellWaiting", reverseParams)
	compareJSONValueEqual(t, "tellWaiting negative offset", rrRev.Result, irRev.Result)
	requireStringMapSliceKeysExact(t, "ref tellWaiting negative offset", rrRev.Result, []string{"gid", "status"})
	requireStringMapSliceKeysExact(t, "impl tellWaiting negative offset", irRev.Result, []string{"gid", "status"})
	refRev := mustStringMapSlice(t, "ref tellWaiting negative offset", rrRev.Result)
	implRev := mustStringMapSlice(t, "impl tellWaiting negative offset", irRev.Result)
	wantRev := []string{gidB, gidA}
	if len(refRev) != len(wantRev) {
		t.Fatalf("ref tellWaiting negative offset got %d entries want %d: %#v", len(refRev), len(wantRev), refRev)
	}
	if len(implRev) != len(wantRev) {
		t.Fatalf("impl tellWaiting negative offset got %d entries want %d: %#v", len(implRev), len(wantRev), implRev)
	}
	for i, wantGID := range wantRev {
		if got := refRev[i]["gid"]; got != wantGID {
			t.Errorf("ref tellWaiting negative offset[%d] gid got %q want %q", i, got, wantGID)
		}
		if got := implRev[i]["gid"]; got != wantGID {
			t.Errorf("impl tellWaiting negative offset[%d] gid got %q want %q", i, got, wantGID)
		}
	}
}

func TestRPC_ChangePositionSuccess(t *testing.T) {
	SkipIfNoRef(t)

	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")
	const gidA = "00000000000000a1"
	const gidB = "00000000000000b2"
	addPausedURI(t, rPort, gidA, "http://127.0.0.1:1/a")
	addPausedURI(t, rPort, gidB, "http://127.0.0.1:1/b")
	addPausedURI(t, iPort, gidA, "http://127.0.0.1:1/a")
	addPausedURI(t, iPort, gidB, "http://127.0.0.1:1/b")

	rr := rpcCallOK(t, rPort, "aria2.changePosition", []any{gidB, float64(0), "POS_SET"})
	ir := rpcCallOK(t, iPort, "aria2.changePosition", []any{gidB, float64(0), "POS_SET"})
	compareJSONValueEqual(t, "changePosition result", rr.Result, ir.Result)
	requireNumberValue(t, "ref changePosition result", rr.Result, 0)
	requireNumberValue(t, "impl changePosition result", ir.Result, 0)

	params := []any{float64(0), float64(10), []string{"gid"}}
	rrWait := rpcCallOK(t, rPort, "aria2.tellWaiting", params)
	irWait := rpcCallOK(t, iPort, "aria2.tellWaiting", params)
	compareJSONValueEqual(t, "waiting order after changePosition", rrWait.Result, irWait.Result)
	requireGIDSequence(t, "ref waiting order after changePosition", rrWait.Result, []string{gidB, gidA})
	requireGIDSequence(t, "impl waiting order after changePosition", irWait.Result, []string{gidB, gidA})
}

func TestRPC_PauseUnpauseRemove(t *testing.T) {
	SkipIfNoRef(t)

	fileSrv := newBlockingDownloadServer(t)
	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")

	// Add downloads.
	rrAdd := rpcCallOK(t, rPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/pause-ref"}})
	irAdd := rpcCallOK(t, iPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/pause-impl"}})

	refGID := rpcResultString(t, rrAdd)
	implGID := rpcResultString(t, irAdd)
	waitForRPCStatus(t, rPort, refGID, "active")
	waitForRPCStatus(t, iPort, implGID, "active")

	// pause
	rrPause := rpcCallOK(t, rPort, "aria2.pause", []any{refGID})
	irPause := rpcCallOK(t, iPort, "aria2.pause", []any{implGID})
	waitForRPCStatus(t, rPort, refGID, "paused")
	waitForRPCStatus(t, iPort, implGID, "paused")

	pauseRefGID := rpcResultString(t, rrPause)
	pauseImplGID := rpcResultString(t, irPause)
	if pauseRefGID != refGID {
		t.Errorf("pause ref GID mismatch: got %s want %s", pauseRefGID, refGID)
	}
	if pauseImplGID != implGID {
		t.Errorf("pause impl GID mismatch: got %s want %s", pauseImplGID, implGID)
	}

	// unpause
	rrUnpause := rpcCallOK(t, rPort, "aria2.unpause", []any{refGID})
	irUnpause := rpcCallOK(t, iPort, "aria2.unpause", []any{implGID})
	_ = rpcResultString(t, rrUnpause)
	_ = rpcResultString(t, irUnpause)
	waitForRPCStatus(t, rPort, refGID, "active")
	waitForRPCStatus(t, iPort, implGID, "active")

	// forcePause
	rpcCallOK(t, rPort, "aria2.forcePause", []any{refGID})
	rpcCallOK(t, iPort, "aria2.forcePause", []any{implGID})
	waitForRPCStatus(t, rPort, refGID, "paused")
	waitForRPCStatus(t, iPort, implGID, "paused")

	// unpause again
	rpcCallOK(t, rPort, "aria2.unpause", []any{refGID})
	rpcCallOK(t, iPort, "aria2.unpause", []any{implGID})
	waitForRPCStatus(t, rPort, refGID, "active")
	waitForRPCStatus(t, iPort, implGID, "active")

	// remove
	rrRem := rpcCallOK(t, rPort, "aria2.remove", []any{refGID})
	irRem := rpcCallOK(t, iPort, "aria2.remove", []any{implGID})

	remRefGID := rpcResultString(t, rrRem)
	remImplGID := rpcResultString(t, irRem)
	if remRefGID != refGID {
		t.Errorf("remove ref GID mismatch: got %s want %s", remRefGID, refGID)
	}
	if remImplGID != implGID {
		t.Errorf("remove impl GID mismatch: got %s want %s", remImplGID, implGID)
	}
}

func TestRPC_PauseAllUnpauseAll(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	// Add a download first.
	rpcCallOK(t, rPort, "aria2.addUri", []any{[]string{"http://localhost:1/b"}})
	rpcCallOK(t, iPort, "aria2.addUri", []any{[]string{"http://localhost:1/b"}})

	// pauseAll
	rr := rpcCallOK(t, rPort, "aria2.pauseAll", []any{})
	ir := rpcCallOK(t, iPort, "aria2.pauseAll", []any{})

	refRes := rpcResultString(t, rr)
	implRes := rpcResultString(t, ir)
	if refRes != "OK" {
		t.Errorf("pauseAll ref: got %q want OK", refRes)
	}
	if implRes != "OK" {
		t.Errorf("pauseAll impl: got %q want OK", implRes)
	}

	// forcePauseAll
	rpcCallOK(t, rPort, "aria2.unpauseAll", []any{})
	rpcCallOK(t, iPort, "aria2.unpauseAll", []any{})
	rpcCallOK(t, rPort, "aria2.pauseAll", []any{})
	rpcCallOK(t, iPort, "aria2.pauseAll", []any{})

	rr2 := rpcCallOK(t, rPort, "aria2.forcePauseAll", []any{})
	ir2 := rpcCallOK(t, iPort, "aria2.forcePauseAll", []any{})

	refRes2 := rpcResultString(t, rr2)
	implRes2 := rpcResultString(t, ir2)
	if refRes2 != "OK" {
		t.Errorf("forcePauseAll ref: got %q want OK", refRes2)
	}
	if implRes2 != "OK" {
		t.Errorf("forcePauseAll impl: got %q want OK", implRes2)
	}
}

func TestRPC_GetUris(t *testing.T) {
	SkipIfNoRef(t)

	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")
	const gid = "00000000000000a1"
	addPausedURI(t, rPort, gid, "http://127.0.0.1:1/uris-test")
	addPausedURI(t, iPort, gid, "http://127.0.0.1:1/uris-test")

	rr := rpcCallOK(t, rPort, "aria2.getUris", []any{gid})
	ir := rpcCallOK(t, iPort, "aria2.getUris", []any{gid})

	compareJSONShapeSlice(t, rr.Result, ir.Result)

	// Verify the URI array contains at least one entry.
	var refUris []map[string]json.RawMessage
	json.Unmarshal(rr.Result, &refUris)
	if len(refUris) == 0 {
		t.Error("ref getUris returned empty array")
	} else {
		keys := []string{"uri", "status"}
		for _, k := range keys {
			if _, ok := refUris[0][k]; !ok {
				t.Errorf("key %q missing from ref getUris entry", k)
			}
		}
	}
}

func TestRPC_ChangeUriMutation(t *testing.T) {
	SkipIfNoRef(t)

	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")
	const gid = "00000000000000a1"
	oldURI := "http://127.0.0.1:1/a"
	newURI := "http://127.0.0.1:1/c"
	addPausedURI(t, rPort, gid, oldURI)
	addPausedURI(t, iPort, gid, oldURI)

	params := []any{gid, float64(1), []string{oldURI}, []string{newURI}, float64(0)}
	rr := rpcCallOK(t, rPort, "aria2.changeUri", params)
	ir := rpcCallOK(t, iPort, "aria2.changeUri", params)
	compareJSONValueEqual(t, "changeUri result", rr.Result, ir.Result)
	requireNumberList(t, "ref changeUri result", rr.Result, []int{1, 1})
	requireNumberList(t, "impl changeUri result", ir.Result, []int{1, 1})

	rrUris := rpcCallOK(t, rPort, "aria2.getUris", []any{gid})
	irUris := rpcCallOK(t, iPort, "aria2.getUris", []any{gid})
	compareJSONValueEqual(t, "getUris after changeUri", rrUris.Result, irUris.Result)
	requireURISet(t, "ref getUris after changeUri", rrUris.Result, []string{newURI}, []string{oldURI})
	requireURISet(t, "impl getUris after changeUri", irUris.Result, []string{newURI}, []string{oldURI})
}

func TestRPC_GetFiles(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rrAdd := rpcCallOK(t, rPort, "aria2.addUri", []any{[]string{"http://localhost:1/getfiles-test"}})
	irAdd := rpcCallOK(t, iPort, "aria2.addUri", []any{[]string{"http://localhost:1/getfiles-test"}})

	refGID := rpcResultString(t, rrAdd)
	implGID := rpcResultString(t, irAdd)

	rr := rpcCallOK(t, rPort, "aria2.getFiles", []any{refGID})
	ir := rpcCallOK(t, iPort, "aria2.getFiles", []any{implGID})

	compareJSONShapeSlice(t, rr.Result, ir.Result)
}

func TestRPC_GetOptionAndChangeOption(t *testing.T) {
	SkipIfNoRef(t)

	releaseDownload := make(chan struct{})
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-releaseDownload:
		case <-r.Context().Done():
		}
	}))
	defer fileSrv.Close()
	defer close(releaseDownload)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rrAdd := rpcCallOK(t, rPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/opt-test"}})
	irAdd := rpcCallOK(t, iPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/opt-test"}})

	refGID := rpcResultString(t, rrAdd)
	implGID := rpcResultString(t, irAdd)

	// getOption
	rr := rpcCallOK(t, rPort, "aria2.getOption", []any{refGID})
	ir := rpcCallOK(t, iPort, "aria2.getOption", []any{implGID})

	compareJSONObjectKeysExact(t, rr.Result, ir.Result)

	// changeOption - set max-download-limit (safe, no restart needed).
	rrCh := rpcCallOK(t, rPort, "aria2.changeOption", []any{refGID, map[string]string{"max-download-limit": "1M"}})
	irCh := rpcCallOK(t, iPort, "aria2.changeOption", []any{implGID, map[string]string{"max-download-limit": "1M"}})

	refCh := rpcResultString(t, rrCh)
	implCh := rpcResultString(t, irCh)
	if refCh != "OK" {
		t.Errorf("changeOption ref: got %q want OK", refCh)
	}
	if implCh != "OK" {
		t.Errorf("changeOption impl: got %q want OK", implCh)
	}
}

func TestRPC_ChangeGlobalOption(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.changeGlobalOption", []any{map[string]string{"max-concurrent-downloads": "3"}})
	ir := rpcCallOK(t, iPort, "aria2.changeGlobalOption", []any{map[string]string{"max-concurrent-downloads": "3"}})

	ref := rpcResultString(t, rr)
	impl := rpcResultString(t, ir)
	if ref != "OK" {
		t.Errorf("changeGlobalOption ref: got %q want OK", ref)
	}
	if impl != "OK" {
		t.Errorf("changeGlobalOption impl: got %q want OK", impl)
	}
}

func TestRPC_ForceRemove(t *testing.T) {
	SkipIfNoRef(t)

	fileSrv := newBlockingDownloadServer(t)
	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")

	rrAdd := rpcCallOK(t, rPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/force-remove-ref"}})
	irAdd := rpcCallOK(t, iPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/force-remove-impl"}})

	refGID := rpcResultString(t, rrAdd)
	implGID := rpcResultString(t, irAdd)
	waitForRPCStatus(t, rPort, refGID, "active")
	waitForRPCStatus(t, iPort, implGID, "active")

	// forceRemove returns the GID.
	rrRem := rpcCallOK(t, rPort, "aria2.forceRemove", []any{refGID})
	irRem := rpcCallOK(t, iPort, "aria2.forceRemove", []any{implGID})

	remRef := rpcResultString(t, rrRem)
	remImpl := rpcResultString(t, irRem)
	if remRef != refGID {
		t.Errorf("forceRemove ref: got %q want %q", remRef, refGID)
	}
	if remImpl != implGID {
		t.Errorf("forceRemove impl: got %q want %q", remImpl, implGID)
	}
}

func TestRPC_RemoveDownloadResult(t *testing.T) {
	SkipIfNoRef(t)

	fileSrv := newBlockingDownloadServer(t)
	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")

	// Add and then remove a download so it goes to stopped state.
	rrAdd := rpcCallOK(t, rPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/rdr-ref"}})
	irAdd := rpcCallOK(t, iPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/rdr-impl"}})

	refGID := rpcResultString(t, rrAdd)
	implGID := rpcResultString(t, irAdd)
	waitForRPCStatus(t, rPort, refGID, "active")
	waitForRPCStatus(t, iPort, implGID, "active")

	rpcCallOK(t, rPort, "aria2.remove", []any{refGID})
	rpcCallOK(t, iPort, "aria2.remove", []any{implGID})
	waitForRPCStatus(t, rPort, refGID, "removed")
	waitForRPCStatus(t, iPort, implGID, "removed")

	// Now remove from download result.
	rr := rpcCallOK(t, rPort, "aria2.removeDownloadResult", []any{refGID})
	ir := rpcCallOK(t, iPort, "aria2.removeDownloadResult", []any{implGID})

	ref := rpcResultString(t, rr)
	impl := rpcResultString(t, ir)
	if ref != "OK" {
		t.Errorf("removeDownloadResult ref: got %q want OK", ref)
	}
	if impl != "OK" {
		t.Errorf("removeDownloadResult impl: got %q want OK", impl)
	}
}

func TestRPC_RemoveDownloadResultActiveError(t *testing.T) {
	SkipIfNoRef(t)

	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")
	const gid = "00000000000000a1"
	addPausedURI(t, rPort, gid, "http://127.0.0.1:1/a")
	addPausedURI(t, iPort, gid, "http://127.0.0.1:1/a")

	refErr := rpcCallExpectError(t, rPort, "aria2.removeDownloadResult", []any{gid})
	implErr := rpcCallExpectError(t, iPort, "aria2.removeDownloadResult", []any{gid})
	if refErr.Code != 1 {
		t.Errorf("removeDownloadResult active ref code got %d want 1", refErr.Code)
	}
	if implErr.Code != 1 {
		t.Errorf("removeDownloadResult active impl code got %d want 1", implErr.Code)
	}
	if refErr.Code != implErr.Code {
		t.Errorf("removeDownloadResult active code mismatch: ref=%d impl=%d", refErr.Code, implErr.Code)
	}
	if refErr.Message != implErr.Message {
		t.Errorf("removeDownloadResult active message mismatch: ref=%q impl=%q", refErr.Message, implErr.Message)
	}
}

func TestRPC_PurgeDownloadResult(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.purgeDownloadResult", []any{})
	ir := rpcCallOK(t, iPort, "aria2.purgeDownloadResult", []any{})

	ref := rpcResultString(t, rr)
	impl := rpcResultString(t, ir)
	if ref != "OK" {
		t.Errorf("purgeDownloadResult ref: got %q want OK", ref)
	}
	if impl != "OK" {
		t.Errorf("purgeDownloadResult impl: got %q want OK", impl)
	}
}

func TestRPC_GetPeers(t *testing.T) {
	SkipIfNoRef(t)

	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")

	// getPeers on non-BT download returns empty array.
	const gid = "00000000000000a1"
	addPausedURI(t, rPort, gid, "http://127.0.0.1:1/peers-test")
	addPausedURI(t, iPort, gid, "http://127.0.0.1:1/peers-test")

	rr := rpcCallOK(t, rPort, "aria2.getPeers", []any{gid})
	ir := rpcCallOK(t, iPort, "aria2.getPeers", []any{gid})

	compareJSONShapeSlice(t, rr.Result, ir.Result)
}

func TestRPC_GetServers(t *testing.T) {
	SkipIfNoRef(t)

	fileSrv := newBlockingDownloadServer(t)
	rPort, iPort := startPairedRPCServers(t, "--dir=/tmp")

	rrAdd := rpcCallOK(t, rPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/servers-ref"}})
	irAdd := rpcCallOK(t, iPort, "aria2.addUri", []any{[]string{fileSrv.URL + "/servers-impl"}})

	refGID := rpcResultString(t, rrAdd)
	implGID := rpcResultString(t, irAdd)
	waitForRPCStatus(t, rPort, refGID, "active")
	waitForRPCStatus(t, iPort, implGID, "active")

	rr := rpcCallOK(t, rPort, "aria2.getServers", []any{refGID})
	ir := rpcCallOK(t, iPort, "aria2.getServers", []any{implGID})

	compareJSONShapeSlice(t, rr.Result, ir.Result)
}

func TestRPC_ChangePosition(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rrAdd := rpcCallOK(t, rPort, "aria2.addUri", []any{[]string{"http://localhost:1/pos-test"}})
	irAdd := rpcCallOK(t, iPort, "aria2.addUri", []any{[]string{"http://localhost:1/pos-test"}})

	refGID := rpcResultString(t, rrAdd)
	implGID := rpcResultString(t, irAdd)

	// changePosition(gid, delta, how)
	// Note: When max-concurrent-downloads (default 5) has capacity,
	// the download may be promoted to active before changePosition is
	// called. changePosition only works on waiting downloads. We check
	// that both ref and impl behave consistently (same error parity).
	rr := rpcCall(t, rPort, "aria2.changePosition", []any{refGID, float64(0), "POS_SET"})
	ir := rpcCall(t, iPort, "aria2.changePosition", []any{implGID, float64(0), "POS_SET"})

	if (rr.Error == nil) != (ir.Error == nil) {
		t.Errorf("changePosition error mismatch: ref.err=%v impl.err=%v",
			rr.Error != nil, ir.Error != nil)
	}
	if rr.Error != nil {
		t.Logf("changePosition both error (download not in waiting): ref=%s impl=%s",
			rr.Error.Message, ir.Error.Message)
	}

	// Returns an integer position on success.
	if rr.Result != nil {
		var refPos, implPos float64
		json.Unmarshal(rr.Result, &refPos)
		json.Unmarshal(ir.Result, &implPos)
		t.Logf("changePosition result: ref=%v impl=%v", refPos, implPos)
	}
}

func TestRPC_ChangeUri(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rrAdd := rpcCallOK(t, rPort, "aria2.addUri", []any{[]string{"http://localhost:1/changeuri-test"}})
	irAdd := rpcCallOK(t, iPort, "aria2.addUri", []any{[]string{"http://localhost:1/changeuri-test"}})

	refGID := rpcResultString(t, rrAdd)
	implGID := rpcResultString(t, irAdd)

	// changeUri(gid, fileIndex, delUris, addUris, [position])
	rr := rpcCall(t, rPort, "aria2.changeUri", []any{refGID, float64(1), []string{}, []string{"http://localhost:1/added-uri"}})
	ir := rpcCall(t, iPort, "aria2.changeUri", []any{implGID, float64(1), []string{}, []string{"http://localhost:1/added-uri"}})

	if rr.Error != nil {
		t.Logf("ref changeUri returned error: %s", rr.Error.Message)
	}
	if ir.Error != nil {
		t.Logf("impl changeUri returned error: %s", ir.Error.Message)
	}

	// Verify return type is array [deleted, added].
	if rr.Result != nil {
		var refResult []float64
		if err := json.Unmarshal(rr.Result, &refResult); err != nil {
			t.Errorf("ref changeUri result not array: %v", err)
		}
	}
	if ir.Result != nil {
		var implResult []float64
		if err := json.Unmarshal(ir.Result, &implResult); err != nil {
			t.Errorf("impl changeUri result not array: %v", err)
		}
	}
}

func TestRPC_SaveSession(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	// saveSession requires --save-session to be set at launch, so it may error.
	// We just verify same error behavior.
	rr := rpcCall(t, rPort, "aria2.saveSession", []any{})
	ir := rpcCall(t, iPort, "aria2.saveSession", []any{})

	if rr.Error != nil && ir.Error != nil {
		t.Logf("both errors (expected without --save-session): ref=%s impl=%s", rr.Error.Message, ir.Error.Message)
		return
	}
	if rr.Error != nil && ir.Error == nil {
		t.Errorf("ref error but impl success: ref=%s", rr.Error.Message)
	}
	if rr.Error == nil && ir.Error != nil {
		t.Errorf("ref success but impl error: %s", ir.Error.Message)
	}
}

func TestRPC_Shutdown(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	// Don't defer stop; shutdown will kill it.

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")

	refSrv.WaitReady(t)
	implSrv.WaitReadyOrSkip(t)

	// shutdown returns "OK" then the server exits.
	// Note: aria2 may not respond before shutting down.
	rr := rpcCall(t, rPort, "aria2.shutdown", []any{})
	ir := rpcCall(t, iPort, "aria2.shutdown", []any{})

	if rr.Error == nil {
		ref := rpcResultString(t, rr)
		t.Logf("ref shutdown returned: %s", ref)
	}
	if ir.Error == nil {
		impl := rpcResultString(t, ir)
		t.Logf("impl shutdown returned: %s", impl)
	}

	// Both servers should exit.
	refSrv.Stop(t)
	implSrv.Stop(t)
}

func TestRPC_ForceShutdown(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")

	refSrv.WaitReady(t)
	implSrv.WaitReadyOrSkip(t)

	rpcCall(t, rPort, "aria2.forceShutdown", []any{})
	rpcCall(t, iPort, "aria2.forceShutdown", []any{})

	refSrv.Stop(t)
	implSrv.Stop(t)
}

func TestRPC_SystemMulticall(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	methods := []map[string]any{
		{"methodName": "aria2.getVersion", "params": []any{}},
		{"methodName": "aria2.getGlobalStat", "params": []any{}},
		{"methodName": "system.listMethods", "params": []any{}},
	}

	rr := rpcCallOK(t, rPort, "system.multicall", []any{methods})
	ir := rpcCallOK(t, iPort, "system.multicall", []any{methods})

	// Each result is [[result], ...] for success, or {faultCode, faultString} for failure.
	var refResults, implResults []json.RawMessage
	json.Unmarshal(rr.Result, &refResults)
	json.Unmarshal(ir.Result, &implResults)

	if len(refResults) != len(methods) {
		t.Errorf("ref multicall: got %d results, want %d", len(refResults), len(methods))
	}
	if len(implResults) != len(methods) {
		t.Errorf("impl multicall: got %d results, want %d", len(implResults), len(methods))
	}
}

func TestRPC_SystemMulticallNestedErrorShape(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	methods := []map[string]any{
		{"methodName": "aria2.getGlobalStat", "params": []any{}},
		{"methodName": "aria2.tellStatus", "params": []any{"00000000000000ff", []string{"gid", "status"}}},
		{"methodName": "system.listNotifications", "params": []any{}},
	}

	rr := rpcCallOK(t, rPort, "system.multicall", []any{methods})
	ir := rpcCallOK(t, iPort, "system.multicall", []any{methods})
	compareMulticallSuccessShapes(t, "system.multicall nested error", rr.Result, ir.Result, []int{0, 2})
	requireMulticallNestedErrorShape(t, "ref system.multicall nested error", rr.Result, 1)
	requireMulticallNestedErrorShape(t, "impl system.multicall nested error", ir.Result, 1)
}

func TestRPC_InvalidUploadedMetadataErrors(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort, "--dir=/tmp")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort, "--dir=/tmp")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	for _, method := range []string{"aria2.addTorrent", "aria2.addMetalink"} {
		t.Run(method, func(t *testing.T) {
			params := []any{"ZHVtbXk="}
			refErr := rpcCallExpectError(t, rPort, method, params)
			implErr := rpcCallExpectError(t, iPort, method, params)
			if refErr.Code != 1 {
				t.Errorf("%s ref code got %d want 1", method, refErr.Code)
			}
			if implErr.Code != 1 {
				t.Errorf("%s impl code got %d want 1", method, implErr.Code)
			}
			if refErr.Code != implErr.Code {
				t.Errorf("%s code mismatch: ref=%d impl=%d", method, refErr.Code, implErr.Code)
			}
			if refErr.Message != implErr.Message {
				t.Errorf("%s message mismatch: ref=%q impl=%q", method, refErr.Message, implErr.Message)
			}
		})
	}
}

func TestRPC_JSONRPCBatch(t *testing.T) {
	SkipIfNoRef(t)

	rPort := findFreePort(t)
	iPort := findFreePort(t)

	refSrv := startRPCRef(t, rPort)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	implSrv := startRPCImpl(t, iPort)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	// Test JSON-RPC batch (array of request objects).
	batch := []rpcRequest{
		{JSONRPC: "2.0", ID: "1", Method: "aria2.getVersion", Params: []any{}},
		{JSONRPC: "2.0", ID: "2", Method: "system.listMethods", Params: []any{}},
	}

	body, _ := json.Marshal(batch)
	refResp := httpPostJSON(t, rPort, body)
	implResp := httpPostJSON(t, iPort, body)

	// Both should return an array.
	if len(refResp) == 0 || refResp[0] != '[' {
		t.Errorf("ref batch response not an array: %s", string(refResp))
	}
	if len(implResp) == 0 || implResp[0] != '[' {
		t.Errorf("impl batch response not an array: %s", string(implResp))
	}

	var refArr, implArr []rpcResponse
	json.Unmarshal(refResp, &refArr)
	json.Unmarshal(implResp, &implArr)

	if len(refArr) != 2 {
		t.Errorf("ref batch: got %d responses want 2", len(refArr))
	}
	if len(implArr) != 2 {
		t.Errorf("impl batch: got %d responses want 2", len(implArr))
	}

	for i, r := range refArr {
		if r.Error != nil {
			t.Errorf("ref batch[%d]: error code=%d msg=%s", i, r.Error.Code, r.Error.Message)
		}
	}
	for i, r := range implArr {
		if r.Error != nil {
			t.Errorf("impl batch[%d]: error code=%d msg=%s", i, r.Error.Code, r.Error.Message)
		}
	}
}

func startPairedRPCServers(t *testing.T, extraArgs ...string) (refPort, implPort int) {
	t.Helper()

	refPort = findFreePort(t)
	refSrv := startRPCRef(t, refPort, extraArgs...)
	t.Cleanup(func() { refSrv.Stop(t) })
	refSrv.WaitReady(t)

	implPort = findFreePort(t)
	implSrv := startRPCImpl(t, implPort, extraArgs...)
	t.Cleanup(func() { implSrv.Stop(t) })
	implSrv.WaitReadyOrSkip(t)

	return refPort, implPort
}

func addPausedURI(t *testing.T, port int, gid string, uri string) string {
	t.Helper()
	return addPausedURIParams(t, port, gid, uri, nil)
}

func addPausedURIAt(t *testing.T, port int, gid string, uri string, position int) string {
	t.Helper()
	return addPausedURIParams(t, port, gid, uri, &position)
}

func addPausedURIParams(t *testing.T, port int, gid string, uri string, position *int) string {
	t.Helper()
	params := []any{
		[]string{uri},
		map[string]string{
			"gid":   gid,
			"pause": "true",
		},
	}
	if position != nil {
		params = append(params, float64(*position))
	}
	rr := rpcCallOK(t, port, "aria2.addUri", params)
	got := rpcResultString(t, rr)
	if got != gid {
		t.Fatalf("addUri fixed gid: got %s want %s", got, gid)
	}
	return got
}

func requireURISet(t *testing.T, label string, raw json.RawMessage, wantPresent, wantAbsent []string) {
	t.Helper()

	values := mustStringMapSlice(t, label, raw)
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if uri := value["uri"]; uri != "" {
			seen[uri] = struct{}{}
		}
	}
	for _, uri := range wantPresent {
		if _, ok := seen[uri]; !ok {
			t.Errorf("%s: URI %q missing from getUris result %#v", label, uri, values)
		}
	}
	for _, uri := range wantAbsent {
		if _, ok := seen[uri]; ok {
			t.Errorf("%s: URI %q still present in getUris result %#v", label, uri, values)
		}
	}
}

func requireMulticallNestedErrorShape(t *testing.T, label string, raw json.RawMessage, index int) {
	t.Helper()

	var results []json.RawMessage
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatalf("%s: unmarshal multicall results: %v (raw=%s)", label, err, string(raw))
	}
	if index >= len(results) {
		t.Fatalf("%s: result index %d out of range for %d results", label, index, len(results))
	}
	errObj := mustJSONMap(t, label, results[index])
	if _, ok := errObj["code"]; !ok {
		t.Errorf("%s: nested error missing code: %s", label, string(results[index]))
	}
	if _, ok := errObj["message"]; !ok {
		t.Errorf("%s: nested error missing message: %s", label, string(results[index]))
	}
	var code int
	if err := json.Unmarshal(errObj["code"], &code); err != nil {
		t.Errorf("%s: nested error code is not an integer: %v", label, err)
	} else if code != 1 {
		t.Errorf("%s: nested error code got %d want 1", label, code)
	}
	var message string
	if err := json.Unmarshal(errObj["message"], &message); err != nil {
		t.Errorf("%s: nested error message is not a string: %v", label, err)
	} else if message == "" {
		t.Errorf("%s: nested error message is empty", label)
	}
	if _, ok := errObj["faultCode"]; ok {
		t.Errorf("%s: nested JSON-RPC error unexpectedly used faultCode: %s", label, string(results[index]))
	}
	if _, ok := errObj["faultString"]; ok {
		t.Errorf("%s: nested JSON-RPC error unexpectedly used faultString: %s", label, string(results[index]))
	}
}

func compareMulticallSuccessShapes(t *testing.T, label string, refRaw, implRaw json.RawMessage, indexes []int) {
	t.Helper()

	var refResults, implResults []json.RawMessage
	if err := json.Unmarshal(refRaw, &refResults); err != nil {
		t.Fatalf("%s: unmarshal ref multicall results: %v (raw=%s)", label, err, string(refRaw))
	}
	if err := json.Unmarshal(implRaw, &implResults); err != nil {
		t.Fatalf("%s: unmarshal impl multicall results: %v (raw=%s)", label, err, string(implRaw))
	}
	if len(refResults) != len(implResults) {
		t.Fatalf("%s: ref has %d results, impl has %d", label, len(refResults), len(implResults))
	}
	for _, index := range indexes {
		if index >= len(refResults) {
			t.Fatalf("%s: success index %d out of range for %d results", label, index, len(refResults))
		}
		compareJSONValueEqual(t, fmt.Sprintf("%s success[%d]", label, index), refResults[index], implResults[index])
	}
}

func httpPostJSON(t *testing.T, port int, body []byte) []byte {
	t.Helper()
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/jsonrpc"
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return data
}
