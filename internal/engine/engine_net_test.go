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
		AsyncDNS:             true,
		EnableAsyncDNS6:      true,
		AsyncDNSServer:       "8.8.8.8,1.1.1.1:54",
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
	if !got.PreferIPv4 {
		t.Fatal("PreferIPv4 = false, want true when disable-ipv6 is set")
	}
	if !got.AsyncDNS {
		t.Fatal("AsyncDNS = false, want true")
	}
	if !got.EnableAsyncDNS6 {
		t.Fatal("EnableAsyncDNS6 = false, want true")
	}
	if got.AsyncDNSServer != "8.8.8.8,1.1.1.1:54" {
		t.Fatalf("AsyncDNSServer = %q, want %q", got.AsyncDNSServer, "8.8.8.8,1.1.1.1:54")
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

func TestHTTPClientTLSConfigIgnoresPrivateKeyWithoutCertificate(t *testing.T) {
	got, err := httpClientTLSConfig(&config.Options{
		PrivateKey: filepath.Join(t.TempDir(), "missing-key.pem"),
	})
	if err != nil {
		t.Fatalf("httpClientTLSConfig() error = %v", err)
	}
	if len(got.Certificates) != 0 {
		t.Fatalf("Certificates = %d, want 0", len(got.Certificates))
	}
}
