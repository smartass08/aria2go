package sftp

// Tests for encrypted-key passphrase support (aria2go extension — aria2c
// only supports password-only SFTP auth via libssh2).

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"testing"

	sshkeys "github.com/smartass08/aria2go/internal/ssh/keys"
	xssh "golang.org/x/crypto/ssh"
)

// generateEncryptedKeyPEM produces an OpenSSH private key PEM encrypted with
// passphrase.
func generateEncryptedKeyPEM(t *testing.T, passphrase []byte) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	block, err := xssh.MarshalPrivateKeyWithPassphrase(key, "", passphrase)
	if err != nil {
		t.Fatalf("marshal encrypted key: %v", err)
	}
	return pem.EncodeToMemory(block)
}

// generatePlainKeyPEM produces an unencrypted OpenSSH private key PEM.
func generatePlainKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	block, err := xssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatalf("marshal plain key: %v", err)
	}
	return pem.EncodeToMemory(block)
}

// TestSFTPEncryptedKeyCorrectPassphrase verifies that
// parsePrivateKeyMaybeEncrypted succeeds when the correct passphrase is
// provided for an encrypted key.
func TestSFTPEncryptedKeyCorrectPassphrase(t *testing.T) {
	const passphrase = "correct-horse-battery-staple"
	pemData := generateEncryptedKeyPEM(t, []byte(passphrase))

	key, err := parsePrivateKeyMaybeEncrypted(pemData, passphrase)
	if err != nil {
		t.Fatalf("parsePrivateKeyMaybeEncrypted() error = %v, want nil", err)
	}
	if key == nil {
		t.Fatal("parsePrivateKeyMaybeEncrypted() returned nil key")
	}
}

// TestSFTPEncryptedKeyWrongPassphrase verifies that
// parsePrivateKeyMaybeEncrypted returns a clear error when the wrong
// passphrase is provided for an encrypted key.
func TestSFTPEncryptedKeyWrongPassphrase(t *testing.T) {
	const correctPassphrase = "correct-horse-battery-staple"
	const wrongPassphrase = "wrong-passphrase"
	pemData := generateEncryptedKeyPEM(t, []byte(correctPassphrase))

	_, err := parsePrivateKeyMaybeEncrypted(pemData, wrongPassphrase)
	if err == nil {
		t.Fatal("parsePrivateKeyMaybeEncrypted() expected error with wrong passphrase, got nil")
	}
	// Must not be the passphrase-required sentinel; the error should indicate
	// a decryption/format failure, not a missing passphrase.
	if errors.Is(err, sshkeys.ErrPassphraseRequired) {
		t.Fatalf("wrong-passphrase case returned ErrPassphraseRequired, want a decryption error; err=%v", err)
	}
}

// TestSFTPEncryptedKeyMissingPassphrase verifies that
// parsePrivateKeyMaybeEncrypted returns ErrPassphraseRequired when the key is
// encrypted but no passphrase is provided.
func TestSFTPEncryptedKeyMissingPassphrase(t *testing.T) {
	const passphrase = "secret"
	pemData := generateEncryptedKeyPEM(t, []byte(passphrase))

	_, err := parsePrivateKeyMaybeEncrypted(pemData, "")
	if err == nil {
		t.Fatal("parsePrivateKeyMaybeEncrypted() expected error with missing passphrase, got nil")
	}
	if !errors.Is(err, sshkeys.ErrPassphraseRequired) {
		t.Fatalf("expected ErrPassphraseRequired, got %v", err)
	}
}

// TestSFTPPlainKeyNoPassphrase verifies that parsePrivateKeyMaybeEncrypted
// correctly handles a plain (unencrypted) key when no passphrase is given.
func TestSFTPPlainKeyNoPassphrase(t *testing.T) {
	pemData := generatePlainKeyPEM(t)

	key, err := parsePrivateKeyMaybeEncrypted(pemData, "")
	if err != nil {
		t.Fatalf("parsePrivateKeyMaybeEncrypted() error = %v, want nil", err)
	}
	if key == nil {
		t.Fatal("parsePrivateKeyMaybeEncrypted() returned nil key")
	}
}

// TestSFTPAuthMethodsPassphraseField ensures that the KeyPassphrase field is
// present in AuthMethods and wired correctly through Opts.
func TestSFTPAuthMethodsPassphraseField(t *testing.T) {
	opts := Opts{
		Auth: AuthMethods{
			KeyFile:       "/path/to/key",
			KeyPassphrase: "my-passphrase",
		},
	}
	if opts.Auth.KeyPassphrase != "my-passphrase" {
		t.Fatalf("KeyPassphrase field = %q, want %q", opts.Auth.KeyPassphrase, "my-passphrase")
	}
}
