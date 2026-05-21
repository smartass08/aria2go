package tlsx

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClientConfigMinimal(t *testing.T) {
	cfg, err := ClientConfig(ClientOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %v, want tls.VersionTLS12", cfg.MinVersion)
	}
	if cfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be false by default")
	}
	if cfg.RootCAs != nil {
		t.Error("RootCAs should be nil when no CACerts provided")
	}
	if len(cfg.Certificates) != 0 {
		t.Error("Certificates should be empty when no client cert provided")
	}
}

func TestClientConfigWithCACerts(t *testing.T) {
	_, caDer, _ := generateCAPEM(t)
	opts := ClientOpts{
		CACerts: [][]byte{caDer},
	}
	cfg, err := ClientConfig(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Fatal("RootCAs should be set")
	}
	// Verify the CA is in the pool by checking the cert count
	pool := cfg.RootCAs
	if pool == nil || len(pool.Subjects()) == 0 {
		t.Error("CA pool should contain at least one subject")
	}
}

func TestClientConfigWithClientCert(t *testing.T) {
	_, certPEM, keyPEM := generateCertKeyPair(t)
	opts := ClientOpts{
		ClientCert: certPEM,
		ClientKey:  keyPEM,
	}
	cfg, err := ClientConfig(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("Certificates length = %d, want 1", len(cfg.Certificates))
	}
}

func TestClientConfigWithALPN(t *testing.T) {
	opts := ClientOpts{
		ALPN: []string{"h2", "http/1.1"},
	}
	cfg, err := ClientConfig(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.NextProtos) != 2 {
		t.Fatalf("NextProtos length = %d, want 2", len(cfg.NextProtos))
	}
	if cfg.NextProtos[0] != "h2" || cfg.NextProtos[1] != "http/1.1" {
		t.Errorf("NextProtos = %v, want [h2 http/1.1]", cfg.NextProtos)
	}
}

func TestClientConfigSkipVerify(t *testing.T) {
	opts := ClientOpts{
		SkipVerify: true,
	}
	cfg, err := ClientConfig(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true")
	}
}

func TestClientConfigWithPinnedCert(t *testing.T) {
	_, caPEM, caKey := generateCAPEM(t)
	_, leafPEM, leafKey := generateCertKeySignedBy(t, caPEM, caKey)

	leaf, err := tls.X509KeyPair(leafPEM, leafKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := tls.X509KeyPair(caPEM, caKey)
	if err != nil {
		t.Fatal(err)
	}

	pinnedDER := leaf.Certificate[0]

	opts := ClientOpts{
		CACerts: [][]byte{caPEM},
		Pinned:  pinnedDER,
	}
	cfg, err := ClientConfig(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.VerifyConnection == nil {
		t.Fatal("VerifyConnection should be set when Pinned is configured")
	}

	// Good match: leaf matches pinned
	leafCert, err := x509.ParseCertificate(leaf.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(ca.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{leafCert, caCert}})
	if err != nil {
		t.Errorf("expected verify to succeed for pinned cert, got: %v", err)
	}

	// Bad match: wrong cert
	_, wrongPEM, wrongKey := generateCertKeyPair(t)
	wrong, err := tls.X509KeyPair(wrongPEM, wrongKey)
	if err != nil {
		t.Fatal(err)
	}
	wrongCert, err := x509.ParseCertificate(wrong.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{wrongCert, caCert}})
	if err == nil {
		t.Error("expected verify to fail for non-matching pinned cert")
	}
}

func TestClientConfigInvalidCert(t *testing.T) {
	opts := ClientOpts{
		ClientCert: []byte("not-a-cert"),
		ClientKey:  []byte("not-a-key"),
	}
	_, err := ClientConfig(opts)
	if err == nil {
		t.Fatal("expected error for invalid cert/key")
	}
}

func TestClientConfigClientCertRequiresKeyPair(t *testing.T) {
	_, certPEM, keyPEM := generateCertKeyPair(t)
	if _, err := ClientConfig(ClientOpts{ClientCert: certPEM}); err == nil {
		t.Fatal("expected error when client cert is provided without key")
	}
	if _, err := ClientConfig(ClientOpts{ClientKey: keyPEM}); err == nil {
		t.Fatal("expected error when client key is provided without cert")
	}
}

func TestClientConfigInvalidCACert(t *testing.T) {
	opts := ClientOpts{
		CACerts: [][]byte{[]byte("not-a-cert")},
	}
	_, err := ClientConfig(opts)
	if err == nil {
		t.Fatal("expected error for invalid CA cert")
	}
}

func TestClientConfigCustomMinVersion(t *testing.T) {
	opts := ClientOpts{
		MinVersion: tls.VersionTLS13,
	}
	cfg, err := ClientConfig(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %v, want tls.VersionTLS13", cfg.MinVersion)
	}
}

func TestTLSVersionMapping(t *testing.T) {
	tests := []struct {
		input string
		want  uint16
	}{
		{"", 0},
		{"TLSv1.1", tls.VersionTLS11},
		{" TLSv1.2 ", tls.VersionTLS12},
		{"tlsv1.3", tls.VersionTLS13},
	}
	for _, tt := range tests {
		got, err := TLSVersion(tt.input)
		if err != nil {
			t.Fatalf("TLSVersion(%q) unexpected error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("TLSVersion(%q) = %x, want %x", tt.input, got, tt.want)
		}
	}
	if _, err := TLSVersion("TLSv1.0"); err == nil {
		t.Fatal("expected unsupported TLS version error")
	}
}

func TestServerConfigHappyPath(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	_, certPEM, keyPEM := generateCertKeyPair(t)
	writeFile(t, certFile, certPEM)
	writeFile(t, keyFile, keyPEM)

	cfg, err := ServerConfig(ServerOpts{
		CertFile: certFile,
		KeyFile:  keyFile,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("Certificates length = %d, want 1", len(cfg.Certificates))
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %v, want tls.VersionTLS12", cfg.MinVersion)
	}
}

func TestServerConfigWithClientAuth(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	_, certPEM, keyPEM := generateCertKeyPair(t)
	writeFile(t, certFile, certPEM)
	writeFile(t, keyFile, keyPEM)

	cfg, err := ServerConfig(ServerOpts{
		CertFile:   certFile,
		KeyFile:    keyFile,
		ClientAuth: tls.RequireAndVerifyClientCert,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want tls.RequireAndVerifyClientCert", cfg.ClientAuth)
	}
}

func TestServerConfigMissingCertFile(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	_, _, keyPEM := generateCertKeyPair(t)
	writeFile(t, keyFile, keyPEM)

	_, err := ServerConfig(ServerOpts{
		CertFile: certFile,
		KeyFile:  keyFile,
	})
	if err == nil {
		t.Fatal("expected error for missing cert file")
	}
}

func TestServerConfigMissingKeyFile(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	_, certPEM, _ := generateCertKeyPair(t)
	writeFile(t, certFile, certPEM)

	_, err := ServerConfig(ServerOpts{
		CertFile: certFile,
		KeyFile:  keyFile,
	})
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestServerConfigInvalidCert(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	writeFile(t, certFile, []byte("not-a-cert"))
	writeFile(t, keyFile, []byte("not-a-key"))

	_, err := ServerConfig(ServerOpts{
		CertFile: certFile,
		KeyFile:  keyFile,
	})
	if err == nil {
		t.Fatal("expected error for invalid cert/key")
	}
}

func TestClientConfigZeroMinVersionDefault(t *testing.T) {
	cfg, err := ClientConfig(ClientOpts{MinVersion: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %v, want tls.VersionTLS12", cfg.MinVersion)
	}
}

func TestClientConfigWithCipherSuites(t *testing.T) {
	suites := []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384}
	cfg, err := ClientConfig(ClientOpts{CipherSuites: suites})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.CipherSuites) != 2 {
		t.Fatalf("CipherSuites length = %d, want 2", len(cfg.CipherSuites))
	}
	if cfg.CipherSuites[0] != suites[0] || cfg.CipherSuites[1] != suites[1] {
		t.Errorf("CipherSuites = %v, want %v", cfg.CipherSuites, suites)
	}
	// Verify the copy is independent
	suites[0] = 0xFFFF
	if cfg.CipherSuites[0] == 0xFFFF {
		t.Error("CipherSuites should be a copy, not a reference")
	}
}

func TestServerConfigWithCipherSuites(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	_, certPEM, keyPEM := generateCertKeyPair(t)
	writeFile(t, certFile, certPEM)
	writeFile(t, keyFile, keyPEM)

	suites := []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}
	cfg, err := ServerConfig(ServerOpts{
		CertFile:     certFile,
		KeyFile:      keyFile,
		CipherSuites: suites,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.CipherSuites) != 1 {
		t.Fatalf("CipherSuites length = %d, want 1", len(cfg.CipherSuites))
	}
	if cfg.CipherSuites[0] != suites[0] {
		t.Errorf("CipherSuites[0] = %v, want %v", cfg.CipherSuites[0], suites[0])
	}
}

func TestClientConfigSystemCAsWithCustomCerts(t *testing.T) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		t.Skip("system cert pool not available")
	}
	systemCount := len(pool.Subjects())

	_, caDer, _ := generateCAPEM(t)
	cfg, err := ClientConfig(ClientOpts{
		CACerts:   [][]byte{caDer},
		SystemCAs: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Fatal("RootCAs should not be nil")
	}
	// Should have system + 1 custom CA
	customCount := len(cfg.RootCAs.Subjects())
	if customCount <= systemCount {
		t.Errorf("RootCAs subjects = %d, want > system count %d", customCount, systemCount)
	}
}

func TestClientConfigSystemCAsOnly(t *testing.T) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		t.Skip("system cert pool not available")
	}
	systemCount := len(pool.Subjects())

	cfg, err := ClientConfig(ClientOpts{SystemCAs: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Fatal("RootCAs should be set when SystemCAs is true")
	}
	if len(cfg.RootCAs.Subjects()) != systemCount {
		t.Errorf("RootCAs subjects = %d, want system count %d", len(cfg.RootCAs.Subjects()), systemCount)
	}
}

func TestServerConfigClientCAsWithoutAuthGate(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	_, certPEM, keyPEM := generateCertKeyPair(t)
	writeFile(t, certFile, certPEM)
	writeFile(t, keyFile, keyPEM)

	_, caDer, _ := generateCAPEM(t)
	// ClientAuth below VerifyClientCertIfGiven — gate was removed, so ClientCAs should be set
	cfg, err := ServerConfig(ServerOpts{
		CertFile:   certFile,
		KeyFile:    keyFile,
		ClientAuth: tls.NoClientCert,
		ClientCAs:  [][]byte{caDer},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientCAs == nil {
		t.Fatal("ClientCAs should be set regardless of ClientAuth mode")
	}
	if len(cfg.ClientCAs.Subjects()) == 0 {
		t.Error("ClientCAs pool should contain at least one subject")
	}
}

func TestServerConfigSystemCAsWithCustomClientCAs(t *testing.T) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		t.Skip("system cert pool not available")
	}
	systemCount := len(pool.Subjects())

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	_, certPEM, keyPEM := generateCertKeyPair(t)
	writeFile(t, certFile, certPEM)
	writeFile(t, keyFile, keyPEM)

	_, caDer, _ := generateCAPEM(t)
	cfg, err := ServerConfig(ServerOpts{
		CertFile:   certFile,
		KeyFile:    keyFile,
		ClientCAs:  [][]byte{caDer},
		SystemCAs:  true,
		ClientAuth: tls.RequireAndVerifyClientCert,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientCAs == nil {
		t.Fatal("ClientCAs should not be nil")
	}
	customCount := len(cfg.ClientCAs.Subjects())
	if customCount <= systemCount {
		t.Errorf("ClientCAs subjects = %d, want > system count %d", customCount, systemCount)
	}
}

func TestServerConfigSystemCAsOnly(t *testing.T) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		t.Skip("system cert pool not available")
	}
	systemCount := len(pool.Subjects())

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	_, certPEM, keyPEM := generateCertKeyPair(t)
	writeFile(t, certFile, certPEM)
	writeFile(t, keyFile, keyPEM)

	cfg, err := ServerConfig(ServerOpts{
		CertFile:   certFile,
		KeyFile:    keyFile,
		SystemCAs:  true,
		ClientAuth: tls.RequireAndVerifyClientCert,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientCAs == nil {
		t.Fatal("ClientCAs should be set to system pool when SystemCAs is true")
	}
	if len(cfg.ClientCAs.Subjects()) != systemCount {
		t.Errorf("ClientCAs subjects = %d, want system count %d", len(cfg.ClientCAs.Subjects()), systemCount)
	}
}

func TestClientConfigNoCipherSuites(t *testing.T) {
	cfg, err := ClientConfig(ClientOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CipherSuites != nil {
		t.Error("CipherSuites should be nil when not configured (let Go use defaults)")
	}
}

func generateCAPEM(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return der, pemCert, pemKey
}

func generateCertKeyPair(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "Test Cert"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return der, pemCert, pemKey
}

func generateCertKeySignedBy(t *testing.T, caPEM, caKey []byte) ([]byte, []byte, []byte) {
	t.Helper()
	caBlock, _ := pem.Decode(caPEM)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	caKeyBlock, _ := pem.Decode(caKey)
	caPrivKey, err := x509.ParsePKCS1PrivateKey(caKeyBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(99),
		Subject:      pkix.Name{CommonName: "Leaf Cert"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &leafKey.PublicKey, caPrivKey)
	if err != nil {
		t.Fatal(err)
	}
	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)})
	return der, pemCert, pemKey
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
