package engine

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testRPCBackend struct{}

func (testRPCBackend) Call(context.Context, string, string, []any) (any, error) {
	return nil, nil
}

func (testRPCBackend) SubscribeNotifications(func(string, map[string]any)) func() {
	return func() {}
}

func TestRPCTransportConfigWiresRuntimeOptions(t *testing.T) {
	opts := testOpts()
	opts.RPCListenPort = 12345
	opts.RPCListenAll = true
	opts.RPCAllowOriginAll = true
	opts.RPCSecret = "secret"
	opts.RPCUser = "user"
	opts.RPCPasswd = "passwd"
	opts.RPCMaxRequestSize = "3M"

	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	cfg, err := e.rpcTransportConfig(testRPCBackend{})
	if err != nil {
		t.Fatalf("rpcTransportConfig() error = %v", err)
	}
	if cfg.Listen != ":12345" {
		t.Errorf("Listen = %q, want :12345", cfg.Listen)
	}
	if !cfg.ListenAll {
		t.Error("ListenAll = false, want true")
	}
	if len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "*" {
		t.Fatalf("AllowedOrigins = %#v, want [*]", cfg.AllowedOrigins)
	}
	if cfg.Secret != "secret" || cfg.RPCUser != "user" || cfg.RPCPasswd != "passwd" {
		t.Fatalf("auth fields not wired: secret=%q user=%q passwd=%q", cfg.Secret, cfg.RPCUser, cfg.RPCPasswd)
	}
	if cfg.MaxRequestSize != 3*1024*1024 {
		t.Errorf("MaxRequestSize = %d, want %d", cfg.MaxRequestSize, 3*1024*1024)
	}
	if cfg.TLS != nil {
		t.Fatal("TLS config set when RPCSecure is false")
	}
	if cfg.Dispatcher == nil {
		t.Fatal("Dispatcher is nil")
	}
}

func TestRPCTransportConfigSecureLoadsTLSCertificate(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTestCertificate(t, dir)
	opts := testOpts()
	opts.RPCSecure = true
	opts.RPCCertificate = certFile
	opts.RPCPrivateKey = keyFile
	opts.RPCMaxRequestSize = "1K"

	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	cfg, err := e.rpcTransportConfig(testRPCBackend{})
	if err != nil {
		t.Fatalf("rpcTransportConfig() error = %v", err)
	}
	if cfg.TLS == nil {
		t.Fatal("TLS config is nil")
	}
	if len(cfg.TLS.Certificates) != 1 {
		t.Fatalf("TLS certificates = %d, want 1", len(cfg.TLS.Certificates))
	}
	if cfg.TLS.MinVersion != tls.VersionTLS12 {
		t.Errorf("TLS MinVersion = %v, want tls.VersionTLS12", cfg.TLS.MinVersion)
	}
	if cfg.MaxRequestSize != 1024 {
		t.Errorf("MaxRequestSize = %d, want 1024", cfg.MaxRequestSize)
	}
}

func TestRPCTransportConfigSecureReportsCertificateError(t *testing.T) {
	opts := testOpts()
	opts.RPCSecure = true
	opts.RPCCertificate = filepath.Join(t.TempDir(), "missing-cert.pem")
	opts.RPCPrivateKey = filepath.Join(t.TempDir(), "missing-key.pem")

	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = e.rpcTransportConfig(testRPCBackend{})
	if err == nil {
		t.Fatal("rpcTransportConfig() error = nil, want certificate error")
	}
	if !strings.Contains(err.Error(), "engine: rpc tls") {
		t.Fatalf("rpcTransportConfig() error = %v, want rpc tls context", err)
	}
}

func writeTestCertificate(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "aria2go rpc test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}
