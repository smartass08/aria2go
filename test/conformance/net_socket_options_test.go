package conformance

// Task C.8 — download.network-socket-options: --disable-ipv6 conformance parity.
//
// TestNetworkSocketOptions_DisableIPv6Parity: HTTP fixture bound to 127.0.0.1
// only; both binaries run with --disable-ipv6=true; assert exit 0.
//
// Note: DSCP and socket-recv-buffer-size are infeasible to assert offline
// (no portable way to observe SO_DSCP / SO_RCVBUF from the application layer)
// and are therefore NOT included here.

import (
	"path/filepath"
	"testing"
)

// TestNetworkSocketOptions_DisableIPv6Parity verifies that both aria2c and
// aria2go successfully download from an IPv4-only (127.0.0.1) HTTP server when
// --disable-ipv6=true is specified.
func TestNetworkSocketOptions_DisableIPv6Parity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("disable-ipv6-loopback", 48*1024+5)

	// HTTP fixture is bound to 127.0.0.1 — no IPv6 involved.
	refHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/ipv6.bin": payload})
	implHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/ipv6.bin": payload})
	defer refHTTP.Close()
	defer implHTTP.Close()

	refDir := t.TempDir()
	implDir := t.TempDir()

	refArgs := append(protocolBaseArgs(refDir),
		"--disable-ipv6=true",
		"--out=ipv6.bin",
		refHTTP.URLPath("/ipv6.bin"),
	)
	implArgs := append(protocolBaseArgs(implDir),
		"--disable-ipv6=true",
		"--out=ipv6.bin",
		implHTTP.URLPath("/ipv6.bin"),
	)

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref disable-ipv6", ref)
	protocolRequireExitZero(t, "impl disable-ipv6", impl)
	protocolRequireFile(t, filepath.Join(refDir, "ipv6.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "ipv6.bin"), payload)
}

// TestNetworkSocketOptions_DisableIPv6FTPParity verifies --disable-ipv6=true
// works for FTP downloads as well (the FTP passive-mode negotiation should
// not attempt IPv6 even when the fixture might advertise it).
func TestNetworkSocketOptions_DisableIPv6FTPParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("disable-ipv6-ftp", 56*1024+11)

	refFTP := startProtocolFTPFixture(t, map[string][]byte{"/ipv6ftp.bin": payload})
	implFTP := startProtocolFTPFixture(t, map[string][]byte{"/ipv6ftp.bin": payload})

	refDir := t.TempDir()
	implDir := t.TempDir()

	refArgs := append(protocolBaseArgs(refDir),
		"--disable-ipv6=true",
		"--ftp-pasv=true",
		"--out=ipv6ftp.bin",
		refFTP.URL("/ipv6ftp.bin"),
	)
	implArgs := append(protocolBaseArgs(implDir),
		"--disable-ipv6=true",
		"--ftp-pasv=true",
		"--out=ipv6ftp.bin",
		implFTP.URL("/ipv6ftp.bin"),
	)

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref disable-ipv6 FTP", ref)
	protocolRequireExitZero(t, "impl disable-ipv6 FTP", impl)
	protocolRequireFile(t, filepath.Join(refDir, "ipv6ftp.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "ipv6ftp.bin"), payload)
}
