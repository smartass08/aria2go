package conformance

// Task C.6 — download.proxy-routing: ftp-proxy specific test.
//
// Unlike the existing --all-proxy tests, these use --ftp-proxy= which routes
// only ftp:// URIs through the proxy.  Both aria2c and aria2go must send the
// request to the proxy fixture.

import (
	"net/http"
	"path/filepath"
	"testing"
)

// TestProtocol_FTPProxySpecificGETParity verifies that --ftp-proxy routes
// ftp:// requests through the designated proxy (rather than requiring
// --all-proxy).
func TestProtocol_FTPProxySpecificGETParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("ftp-proxy-specific-get", 56*1024+19)
	refProxy := startProtocolFTPProxyFixture(t, map[string][]byte{"/ftp-specific.bin": payload})
	implProxy := startProtocolFTPProxyFixture(t, map[string][]byte{"/ftp-specific.bin": payload})

	target := "ftp://example.invalid/ftp-specific.bin"
	refDir := t.TempDir()
	implDir := t.TempDir()

	refArgs := append(protocolBaseArgs(refDir),
		"--out=ftp-specific.bin",
		"--no-proxy=",
		"--ftp-proxy="+refProxy.URL(),
		target,
	)
	implArgs := append(protocolBaseArgs(implDir),
		"--out=ftp-specific.bin",
		"--no-proxy=",
		"--ftp-proxy="+implProxy.URL(),
		target,
	)

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref FTP ftp-proxy GET", ref)
	protocolRequireExitZero(t, "impl FTP ftp-proxy GET", impl)
	protocolRequireFile(t, filepath.Join(refDir, "ftp-specific.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "ftp-specific.bin"), payload)

	// Verify that the proxy received the request for the FTP URL.
	protocolRequireProxyFTPURL(t, "ref FTP ftp-proxy GET", refProxy.snapshotRequests(), http.MethodGet, target)
	protocolRequireProxyFTPURL(t, "impl FTP ftp-proxy GET", implProxy.snapshotRequests(), http.MethodGet, target)
}

// TestProtocol_FTPProxySpecificIsolationParity verifies that --ftp-proxy does
// NOT route HTTP traffic — only ftp:// URIs are affected.  We download an
// HTTP URL directly (bypassing --ftp-proxy) and confirm the proxy does NOT
// receive a request for it.
func TestProtocol_FTPProxySpecificIsolationParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("ftp-proxy-isolation", 32*1024+5)
	refHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/iso.bin": payload})
	implHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/iso.bin": payload})
	defer refHTTP.Close()
	defer implHTTP.Close()

	// FTP proxy fixtures with NO files — should never be contacted for HTTP.
	refProxy := startProtocolFTPProxyFixture(t, nil)
	implProxy := startProtocolFTPProxyFixture(t, nil)

	refDir := t.TempDir()
	implDir := t.TempDir()

	refArgs := append(protocolBaseArgs(refDir),
		"--out=iso.bin",
		"--ftp-proxy="+refProxy.URL(),
		refHTTP.URLPath("/iso.bin"),
	)
	implArgs := append(protocolBaseArgs(implDir),
		"--out=iso.bin",
		"--ftp-proxy="+implProxy.URL(),
		implHTTP.URLPath("/iso.bin"),
	)

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref HTTP direct (ftp-proxy isolation)", ref)
	protocolRequireExitZero(t, "impl HTTP direct (ftp-proxy isolation)", impl)
	protocolRequireFile(t, filepath.Join(refDir, "iso.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "iso.bin"), payload)

	// The ftp-proxy must not have received anything.
	if len(refProxy.snapshotRequests()) != 0 {
		t.Errorf("ref: ftp-proxy received %d requests for HTTP download, want 0", len(refProxy.snapshotRequests()))
	}
	if len(implProxy.snapshotRequests()) != 0 {
		t.Errorf("impl: ftp-proxy received %d requests for HTTP download, want 0", len(implProxy.snapshotRequests()))
	}
}
