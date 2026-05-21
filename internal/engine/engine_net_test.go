package engine

import (
	"crypto/tls"
	"path/filepath"
	"testing"

	"github.com/smartass08/aria2go/internal/config"
)

func TestEngineDialerConfigWiresNetworkOptions(t *testing.T) {
	opts := &config.Options{
		ConnectTimeout:       "7",
		Interface:            "eth0",
		DisableIPv6:          true,
		SocketRecvBufferSize: "4096",
		DSCP:                 "32",
		MultipleInterface:    "eth0,eth1",
	}

	got := engineDialerConfig(opts)
	if got.Timeout.String() != "7s" {
		t.Fatalf("Timeout = %s, want 7s", got.Timeout)
	}
	if got.Interface != "eth0" {
		t.Fatalf("Interface = %q, want eth0", got.Interface)
	}
	if got.SocketRecvBufferSize != 4096 {
		t.Fatalf("SocketRecvBufferSize = %d, want 4096", got.SocketRecvBufferSize)
	}
	if got.DSCP != 32 {
		t.Fatalf("DSCP = %d, want 32", got.DSCP)
	}
	if got.Interfaces != "eth0,eth1" {
		t.Fatalf("Interfaces = %q, want eth0,eth1", got.Interfaces)
	}
}

func TestHTTPClientTLSConfigWiresCertificateOptions(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTestCertificate(t, dir)
	opts := &config.Options{
		CACertificate: certFile,
		Certificate:   certFile,
		PrivateKey:    keyFile,
		MinTLSVersion: "TLSv1.3",
	}

	got, err := httpClientTLSConfig(opts)
	if err != nil {
		t.Fatalf("httpClientTLSConfig() error = %v", err)
	}
	if got.MinVersion != tls.VersionTLS13 {
		t.Fatalf("MinVersion = %v, want tls.VersionTLS13", got.MinVersion)
	}
	if got.RootCAs == nil {
		t.Fatal("RootCAs is nil")
	}
	if len(got.Certificates) != 1 {
		t.Fatalf("Certificates = %d, want 1", len(got.Certificates))
	}
}

func TestHTTPClientTLSConfigReportsReadErrors(t *testing.T) {
	_, err := httpClientTLSConfig(&config.Options{
		CACertificate: filepath.Join(t.TempDir(), "missing.pem"),
	})
	if err == nil {
		t.Fatal("httpClientTLSConfig() error = nil, want read error")
	}
}
