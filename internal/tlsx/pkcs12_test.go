package tlsx

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

const testPKCS12Base64 = "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"

func TestClientConfigWithPKCS12ClientCert(t *testing.T) {
	p12 := decodeTestPKCS12(t)

	cfg, err := ClientConfig(ClientOpts{
		ClientCert: p12,
	})
	if err != nil {
		t.Fatalf("ClientConfig(PKCS12) error = %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("Certificates length = %d, want 1", len(cfg.Certificates))
	}
	if len(cfg.Certificates[0].Certificate) == 0 {
		t.Fatal("PKCS12 certificate chain is empty")
	}
}

func TestServerConfigWithPKCS12Certificate(t *testing.T) {
	p12 := decodeTestPKCS12(t)
	path := filepath.Join(t.TempDir(), "server.p12")
	if err := os.WriteFile(path, p12, 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}

	cfg, err := ServerConfig(ServerOpts{CertFile: path})
	if err != nil {
		t.Fatalf("ServerConfig(PKCS12) error = %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("Certificates length = %d, want 1", len(cfg.Certificates))
	}
}

func decodeTestPKCS12(t *testing.T) []byte {
	t.Helper()

	data, err := base64.StdEncoding.DecodeString(testPKCS12Base64)
	if err != nil {
		t.Fatalf("DecodeString(testPKCS12Base64): %v", err)
	}
	return data
}
