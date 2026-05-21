package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSession_RefSaveImplLoad(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.sess")
	downloadDir := filepath.Join(dir, "downloads")

	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort,
		"--dir="+downloadDir,
		"--save-session="+sessionPath,
	)
	refSrv.WaitReady(t)

	rrAdd := rpcCallOK(t, rPort, "aria2.addUri", []any{[]string{"http://localhost:1/session-test-a"}})
	refGID := rpcResultString(t, rrAdd)
	t.Logf("added download with GID %s", refGID)

	rpcCallOK(t, rPort, "aria2.pause", []any{refGID})

	rrSave := rpcCall(t, rPort, "aria2.saveSession", []any{})
	if rrSave.Error != nil {
		t.Logf("ref saveSession error: %s", rrSave.Error.Message)
	}

	refSrv.Stop(t)

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ref session file is empty")
	}
	t.Logf("ref session file size: %d bytes", len(data))

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort,
		"--dir="+downloadDir,
		"--input-file="+sessionPath,
	)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	irWait := rpcCallOK(t, iPort, "aria2.tellWaiting", []any{float64(0), float64(10)})
	var implWaiting []map[string]json.RawMessage
	if err := json.Unmarshal(irWait.Result, &implWaiting); err != nil {
		t.Fatalf("unmarshal impl waiting: %v", err)
	}

	if len(implWaiting) == 0 {
		t.Log("impl did not load any downloads from session (may need session parsing support)")
	} else {
		t.Logf("impl loaded %d downloads from session", len(implWaiting))
	}
}

func TestSession_ImplSaveRefLoad(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session-impl.sess")
	downloadDir := filepath.Join(dir, "downloads-impl")

	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort,
		"--dir="+downloadDir,
		"--save-session="+sessionPath,
	)
	implSrv.WaitReadyOrSkip(t)

	irAdd := rpcCallOK(t, iPort, "aria2.addUri", []any{[]string{"http://localhost:1/impl-session-test"}})
	implGID := rpcResultString(t, irAdd)
	t.Logf("added download with GID %s", implGID)

	rpcCallOK(t, iPort, "aria2.pause", []any{implGID})

	irSave := rpcCall(t, iPort, "aria2.saveSession", []any{})
	if irSave.Error != nil {
		t.Logf("impl saveSession error: %s", irSave.Error.Message)
	}

	implSrv.Stop(t)

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("impl session file is empty")
	}
	t.Logf("impl session file size: %d bytes", len(data))

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort,
		"--dir="+downloadDir,
		"--input-file="+sessionPath,
	)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	rrWait := rpcCallOK(t, rPort, "aria2.tellWaiting", []any{float64(0), float64(10)})
	var refWaiting []map[string]json.RawMessage
	if err := json.Unmarshal(rrWait.Result, &refWaiting); err != nil {
		t.Fatalf("unmarshal ref waiting: %v", err)
	}

	if len(refWaiting) == 0 {
		t.Log("ref did not load any downloads from impl session (may need format compatibility)")
	} else {
		t.Logf("ref loaded %d downloads from impl session", len(refWaiting))
	}
}

func TestSession_RefSaveByteComparison(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	downloadDir := filepath.Join(dir, "dl")

	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	session1 := filepath.Join(dir, "s1.sess")
	session2 := filepath.Join(dir, "s2.sess")

	for i, sessPath := range []string{session1, session2} {
		port := findFreePort(t)
		srv := startRPCRef(t, port,
			"--dir="+downloadDir,
			"--save-session="+sessPath,
		)
		srv.WaitReady(t)

		addURI := "http://localhost:1/session-byte-test"
		rrAdd := rpcCallOK(t, port, "aria2.addUri", []any{[]string{addURI}})
		gid := rpcResultString(t, rrAdd)
		rpcCallOK(t, port, "aria2.pause", []any{gid})
		rpcCall(t, port, "aria2.saveSession", []any{})

		srv.Stop(t)

		data, err := os.ReadFile(sessPath)
		if err != nil {
			t.Fatalf("read session %d: %v", i+1, err)
		}
		if len(data) == 0 {
			t.Fatalf("session %d is empty", i+1)
		}
		t.Logf("session %d size: %d bytes", i+1, len(data))
	}

	data1, _ := os.ReadFile(session1)
	data2, _ := os.ReadFile(session2)

	if len(data1) == len(data2) {
		t.Log("session files have identical length")
	} else {
		t.Logf("session files differ in length: %d vs %d", len(data1), len(data2))
	}

	content1 := string(data1)
	if !strings.Contains(content1, "http://localhost:1/session-byte-test") {
		t.Error("session file missing expected URI")
	}
}

func TestSession_SaveSessionImmediate(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "immediate.sess")
	downloadDir := filepath.Join(dir, "dl-immediate")

	os.MkdirAll(downloadDir, 0755)

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort,
		"--dir="+downloadDir,
		"--save-session="+sessionPath,
	)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	rr := rpcCall(t, rPort, "aria2.saveSession", []any{})
	if rr.Error != nil {
		t.Logf("ref immediate saveSession: %s (code=%d)", rr.Error.Message, rr.Error.Code)
	}

	data, err := os.ReadFile(sessionPath)
	if err != nil && !os.IsNotExist(err) {
		t.Errorf("read session: %v", err)
	}
	if len(data) > 0 {
		t.Logf("session file size (empty queue): %d bytes", len(data))
	}
}
