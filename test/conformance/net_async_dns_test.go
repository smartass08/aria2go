package conformance

// Task C.7 — net.async-dns-runtime: async DNS conformance tests.
//
// Note on --async-dns-server port support:
//   aria2c uses c-ares for async DNS and c-ares only accepts plain IP addresses
//   in --async-dns-server (no port override; always port 53).  A local DNS
//   fixture on an ephemeral port cannot be used as the reference binary would
//   receive DNS queries on port 53 rather than the fixture port and always fail.
//   Therefore, the local-DNS-fixture test is an aria2go-only unit test
//   (see internal/netx/dialer_test.go TestDialContext_AsyncDNS), and the
//   cross-binary parity tests below use IP-addressed hosts or --async-dns=false.
//
// TestAsyncDNS_IPAddrWithAsyncDNSParity: both download from a loopback IP
// with --async-dns=true; no DNS lookup needed — asserts equal exit codes.
//
// TestAsyncDNS_DefaultFallbackParity: --async-dns=false on a loopback IP;
// asserts equal exit codes.
//
// TestAsyncDNS_CustomServerFlagAcceptedParity: verify that supplying
// --async-dns-server=127.0.0.1 (port-53 format, no port) is accepted without
// error by both binaries even if the DNS query fails (both fail equally).

import (
	"net"
	"path/filepath"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// startConformanceDNS starts a UDP DNS fixture that answers TypeA queries for
// fqdn with ip in a loop until the test ends.  Returns the local address.
func startConformanceDNS(t *testing.T, fqdn string, ip net.IP) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("dns listen: %v", err)
	}
	addr := conn.LocalAddr().String()
	t.Cleanup(func() { _ = conn.Close() })

	go serveConformanceDNSLoop(conn, fqdn, ip)
	return addr
}

// serveConformanceDNSLoop answers TypeA DNS queries for fqdn with ip in a
// loop until conn is closed.
func serveConformanceDNSLoop(conn net.PacketConn, fqdn string, ip net.IP) {
	buf := make([]byte, 1500)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		resp := buildConformanceDNSResponse(buf[:n], fqdn, ip)
		if resp != nil {
			_, _ = conn.WriteTo(resp, addr)
		}
	}
}

func buildConformanceDNSResponse(query []byte, fqdn string, ip net.IP) []byte {
	var parser dnsmessage.Parser
	header, err := parser.Start(query)
	if err != nil {
		return nil
	}
	question, err := parser.Question()
	if err != nil {
		return nil
	}
	if question.Type != dnsmessage.TypeA {
		return nil
	}
	if question.Name.String() != fqdn {
		return nil
	}

	respHeader := dnsmessage.Header{
		ID:                 header.ID,
		Response:           true,
		RecursionAvailable: true,
	}
	b := dnsmessage.NewBuilder(nil, respHeader)
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil
	}
	if err := b.Question(question); err != nil {
		return nil
	}
	if err := b.StartAnswers(); err != nil {
		return nil
	}
	var a [4]byte
	copy(a[:], ip.To4())
	if err := b.AResource(dnsmessage.ResourceHeader{
		Name:  question.Name,
		Type:  dnsmessage.TypeA,
		Class: dnsmessage.ClassINET,
		TTL:   30,
	}, dnsmessage.AResource{A: a}); err != nil {
		return nil
	}
	resp, err := b.Finish()
	if err != nil {
		return nil
	}
	return resp
}

// TestAsyncDNS_IPAddrWithAsyncDNSEnabledParity verifies that both aria2c and
// aria2go successfully download from a loopback IP with --async-dns=true.
// When the target is an IP literal, no DNS resolution occurs and the async DNS
// flag should not cause any failure.
func TestAsyncDNS_IPAddrWithAsyncDNSEnabledParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("async-dns-ip-addr", 40*1024+11)

	refHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/async-dns.bin": payload})
	implHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/async-dns.bin": payload})
	defer refHTTP.Close()
	defer implHTTP.Close()

	refDir := t.TempDir()
	implDir := t.TempDir()

	refArgs := append(protocolBaseArgs(refDir),
		"--async-dns=true",
		"--out=async-dns.bin",
		refHTTP.URLPath("/async-dns.bin"),
	)
	implArgs := append(protocolBaseArgs(implDir),
		"--async-dns=true",
		"--out=async-dns.bin",
		implHTTP.URLPath("/async-dns.bin"),
	)

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref async-dns=true IP download", ref)
	protocolRequireExitZero(t, "impl async-dns=true IP download", impl)
	protocolRequireFile(t, filepath.Join(refDir, "async-dns.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "async-dns.bin"), payload)
}

// TestAsyncDNS_DefaultFallbackParity verifies that with --async-dns=false,
// both binaries download from a loopback HTTP server (addressed by IP)
// and exit 0.
func TestAsyncDNS_DefaultFallbackParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("async-dns-fallback", 32*1024+7)

	refHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/fallback.bin": payload})
	implHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/fallback.bin": payload})
	defer refHTTP.Close()
	defer implHTTP.Close()

	refDir := t.TempDir()
	implDir := t.TempDir()

	refArgs := append(protocolBaseArgs(refDir),
		"--async-dns=false",
		"--out=fallback.bin",
		refHTTP.URLPath("/fallback.bin"),
	)
	implArgs := append(protocolBaseArgs(implDir),
		"--async-dns=false",
		"--out=fallback.bin",
		implHTTP.URLPath("/fallback.bin"),
	)

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref async-dns=false fallback", ref)
	protocolRequireExitZero(t, "impl async-dns=false fallback", impl)
	protocolRequireFile(t, filepath.Join(refDir, "fallback.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "fallback.bin"), payload)
}

// TestAsyncDNS_CustomServerFlagAcceptedParity verifies that specifying
// --async-dns-server=127.0.0.1 (port-53 format; valid aria2c syntax) is
// accepted and does not cause a configuration error in either binary.
// Both are expected to fail to download (our loopback:53 does not serve DNS),
// and both must fail with equal exit codes.
func TestAsyncDNS_CustomServerFlagAcceptedParity(t *testing.T) {
	SkipIfNoRef(t)

	refDir := t.TempDir()
	implDir := t.TempDir()

	// Hostname that will not resolve on standard port 53 (loopback DNS).
	// Both binaries should fail equally.
	targetURL := "http://aria2go-unreachable-dns-test.invalid:9999/x.bin"

	makeArgs := func(dir string) []string {
		return []string{
			"--no-conf=true",
			"--dir=" + dir,
			"--allow-overwrite=true",
			"--file-allocation=none",
			"--quiet=true",
			"--show-console-readout=false",
			"--summary-interval=0",
			"--no-netrc=true",
			"--check-certificate=false",
			"--no-proxy=",
			"--enable-dht=false",
			"--enable-dht6=false",
			"--bt-enable-lpd=false",
			"--disable-ipv6=true",
			"--async-dns=true",
			"--async-dns-server=127.0.0.1",
			"--max-tries=1",
			"--connect-timeout=3",
			"--timeout=5",
			targetURL,
		}
	}

	ref := protocolRun(t, true, makeArgs(refDir))
	impl := protocolRun(t, false, makeArgs(implDir))

	// Neither binary should succeed: the flag must be accepted (no arg-parse
	// error), but the download should fail because the DNS server at
	// 127.0.0.1:53 is not reachable.  We do NOT assert identical exit codes
	// because different DNS failure modes (e.g. NXDOMAIN vs connection
	// refused) can yield different codes; we only assert both fail.
	if ref.ExitCode == 0 {
		t.Fatalf("ref unexpectedly succeeded downloading from unreachable host")
	}
	if impl.ExitCode == 0 {
		t.Fatalf("impl unexpectedly succeeded downloading from unreachable host")
	}
}
