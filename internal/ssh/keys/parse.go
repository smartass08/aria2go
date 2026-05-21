// Package keys provides SSH private key parsing for the formats used by
// OpenSSH and standard PKCS#8, matching the key-handling behavior of aria2's
// libssh2-backed SSH session (SSHSession.cc).
//
// Supported formats:
//   - PEM-encoded unencrypted PKCS#8 (standard Go x509 fallback)
//   - OpenSSH "OPENSSH PRIVATE KEY" format detection (not yet implemented)
//   - Encrypted PKCS#8 pass-through (not yet decrypted)
package keys

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// ErrUnsupported is returned when a key format is recognized but not yet
// implemented.
var ErrUnsupported = fmt.Errorf("ssh/keys: key format not yet supported")

// ParsePrivateKey parses a PEM-encoded private key. It attempts to detect and
// handle:
//   - "RSA PRIVATE KEY" / "EC PRIVATE KEY" → standard x509 fallback
//   - "PRIVATE KEY" → PKCS#8 (unencrypted)
//   - "ENCRYPTED PRIVATE KEY" → pass-through error (needs passphrase)
//   - "OPENSSH PRIVATE KEY" → recognized but not yet implemented
func ParsePrivateKey(pemData []byte) (any, error) {
	return ParsePrivateKeyWithPassphrase(pemData, nil)
}

// ParsePrivateKeyWithPassphrase parses a PEM-encoded private key, optionally
// decrypting with the given passphrase.
func ParsePrivateKeyWithPassphrase(pemData []byte, passphrase []byte) (any, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("ssh/keys: failed to decode PEM block")
	}

	switch block.Type {
	case "OPENSSH PRIVATE KEY":
		return nil, fmt.Errorf("ssh/keys: OpenSSH private key format not yet supported: %w", ErrUnsupported)

	case "ENCRYPTED PRIVATE KEY":
		if passphrase != nil && len(passphrase) > 0 {
			return nil, fmt.Errorf("ssh/keys: encrypted PKCS#8 key decryption not yet implemented: %w", ErrUnsupported)
		}
		return nil, fmt.Errorf("ssh/keys: encrypted PKCS#8 key requires passphrase: %w", ErrUnsupported)

	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("ssh/keys: PKCS#8 parse: %w", err)
		}
		return key, nil

	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("ssh/keys: PKCS#1 RSA parse: %w", err)
		}
		return key, nil

	case "EC PRIVATE KEY":
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("ssh/keys: EC private key parse: %w", err)
		}
		return key, nil

	default:
		return nil, fmt.Errorf("ssh/keys: unknown PEM type %q", block.Type)
	}
}
