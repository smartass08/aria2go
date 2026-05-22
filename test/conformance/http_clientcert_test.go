package conformance

// TestHTTPS_ClientCertMutualTLS: Stand up an mTLS httptest.Server requiring client auth.
// Run both aria2c and aria2go with --certificate/--private-key/--ca-certificate.
// On macOS/AppleTLS builds, aria2c does not support custom CA or client certs via
// --ca-certificate/--certificate; detect and skip gracefully.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// clientCertMTLSPayload is the fixed payload served by the mTLS test server.
var clientCertMTLSPayload = []byte("mtls-client-cert-parity-payload\n")

// mtlsFixture holds a running httptest.Server with mutual TLS and the
// cert/key files written to disk for consumption by aria2c/aria2go flags.
type mtlsFixture struct {
	server        *httptest.Server
	caCertFile    string
	clientCrtFile string
	clientKeyFile string
}

// newMTLSFixture generates a self-signed CA, server cert, and client cert,
// writes the PEM files under dir, and starts an httptest TLS server that
// requires client certificate authentication (tls.RequireAndVerifyClientCert).
func newMTLSFixture(t *testing.T, dir string) *mtlsFixture {
	t.Helper()

	// ── CA ──────────────────────────────────────────────────────────────────
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("mtls gen CA key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "aria2go-test-CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("mtls create CA cert: %v", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("mtls parse CA cert: %v", err)
	}

	caCertFile := filepath.Join(dir, "ca.pem")
	if err := mtlsWritePEMFile(caCertFile, "CERTIFICATE", caDER); err != nil {
		t.Fatalf("write CA cert: %v", err)
	}

	// ── Server cert ─────────────────────────────────────────────────────────
	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("mtls gen server key: %v", err)
	}
	srvTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTemplate, caCert, &srvKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("mtls create server cert: %v", err)
	}
	srvTLSCert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvDER}),
		mtlsECKeyToPEM(srvKey),
	)
	if err != nil {
		t.Fatalf("mtls X509KeyPair server: %v", err)
	}

	// ── Client cert ─────────────────────────────────────────────────────────
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("mtls gen client key: %v", err)
	}
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "aria2go-test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("mtls create client cert: %v", err)
	}

	clientCrtFile := filepath.Join(dir, "client.pem")
	if err := mtlsWritePEMFile(clientCrtFile, "CERTIFICATE", clientDER); err != nil {
		t.Fatalf("write client cert: %v", err)
	}
	clientKeyFile := filepath.Join(dir, "client.key")
	if err := os.WriteFile(clientKeyFile, mtlsECKeyToPEM(clientKey), 0o600); err != nil {
		t.Fatalf("write client key: %v", err)
	}

	// ── TLS server ──────────────────────────────────────────────────────────
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(clientCertMTLSPayload)))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Method != http.MethodHead {
			_, _ = w.Write(clientCertMTLSPayload)
		}
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{srvTLSCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	return &mtlsFixture{
		server:        srv,
		caCertFile:    caCertFile,
		clientCrtFile: clientCrtFile,
		clientKeyFile: clientKeyFile,
	}
}

// TestHTTPS_ClientCertMutualTLS verifies that both aria2c and aria2go can
// successfully download from a server that requires mutual TLS.
func TestHTTPS_ClientCertMutualTLS(t *testing.T) {
	SkipIfNoRef(t)

	// AppleTLS on macOS does not honour --ca-certificate or --certificate/
	// --private-key for custom CAs. Detect and skip.
	if refUsesAppleTLS(t) {
		t.Skip("reference aria2c uses AppleTLS: custom CA and client-cert flags " +
			"are not supported (AppleTLSContext ignores --ca-certificate / " +
			"--certificate in favour of the OS trust store)")
	}

	refDir := t.TempDir()
	implDir := t.TempDir()
	refFix := newMTLSFixture(t, refDir)
	implFix := newMTLSFixture(t, implDir)

	mtlsArgs := func(fix *mtlsFixture, dir string) []string {
		args := httpEdgeBaseArgs(dir, "mtls.bin")
		args = append(args,
			"--certificate="+fix.clientCrtFile,
			"--private-key="+fix.clientKeyFile,
			"--ca-certificate="+fix.caCertFile,
			fix.server.URL+"/mtls.bin",
		)
		return args
	}

	ref, err := RunRefWithOptions(t, mtlsArgs(refFix, refDir), "", RunOptions{
		Env:     httpEdgeProxylessEnv(),
		Timeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("run ref mTLS: %v\nstdout=%s\nstderr=%s", err, ref.Stdout, ref.Stderr)
	}

	impl, err := RunImplWithOptions(t, mtlsArgs(implFix, implDir), "", RunOptions{
		Env:     httpEdgeProxylessEnv(),
		Timeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("run impl mTLS: %v\nstdout=%s\nstderr=%s", err, impl.Stdout, impl.Stderr)
	}

	AssertEqualExit(t, ref, impl)
	requireExitSuccess(t, "ref mTLS client cert", ref)
	requireExitSuccess(t, "impl mTLS client cert", impl)
	AssertFileBytes(t, filepath.Join(refDir, "mtls.bin"), clientCertMTLSPayload)
	AssertFileBytes(t, filepath.Join(implDir, "mtls.bin"), clientCertMTLSPayload)
}

// TestHTTPS_ClientCertMissingFails verifies that connecting to a mTLS server
// WITHOUT a client cert fails on both binaries with the same exit code.
func TestHTTPS_ClientCertMissingFails(t *testing.T) {
	SkipIfNoRef(t)

	if refUsesAppleTLS(t) {
		t.Skip("reference aria2c uses AppleTLS: mTLS handshake rejection without " +
			"client cert is not observable through these CLI flags")
	}

	sharedDir := t.TempDir()
	fix := newMTLSFixture(t, sharedDir)

	// Provide only the CA cert so the server cert is trusted, but no client cert.
	noClientArgs := func(outDir string) []string {
		args := httpEdgeBaseArgs(outDir, "no-client.bin")
		args = append(args,
			"--ca-certificate="+fix.caCertFile,
			fix.server.URL+"/no-client.bin",
		)
		return args
	}

	refDir, implDir := t.TempDir(), t.TempDir()

	ref, err := RunRefWithOptions(t, noClientArgs(refDir), "", RunOptions{
		Env:     httpEdgeProxylessEnv(),
		Timeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("run ref no-client-cert: %v", err)
	}

	impl, err := RunImplWithOptions(t, noClientArgs(implDir), "", RunOptions{
		Env:     httpEdgeProxylessEnv(),
		Timeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("run impl no-client-cert: %v", err)
	}

	if ref.ExitCode == 0 {
		// Server accepted without client cert – cannot assert failure parity.
		t.Skip("reference did not reject missing client cert (server may not have " +
			"enforced mTLS); skipping failure parity assertion")
	}

	AssertEqualExit(t, ref, impl)
}

// ── private helpers ──────────────────────────────────────────────────────────

func mtlsWritePEMFile(path, blockType string, der []byte) error {
	data := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	return os.WriteFile(path, data, 0o644)
}

func mtlsECKeyToPEM(key *ecdsa.PrivateKey) []byte {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		panic("marshal EC private key: " + err.Error())
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}
