// Package keys provides SSH private-key parsing for the key formats accepted
// by OpenSSH and libssh2-backed aria2 sessions.
package keys

import (
	"errors"
	"fmt"

	xssh "golang.org/x/crypto/ssh"
)

var (
	// ErrUnsupported is returned when the key data is malformed or uses a
	// format that the Go SSH stack does not understand.
	ErrUnsupported = errors.New("ssh/keys: unsupported key format")
	// ErrPassphraseRequired indicates that the key is encrypted and no
	// passphrase was supplied.
	ErrPassphraseRequired = errors.New("ssh/keys: passphrase required")
)

// ParsePrivateKey parses a PEM-encoded private key without a passphrase.
func ParsePrivateKey(pemData []byte) (any, error) {
	key, err := xssh.ParseRawPrivateKey(pemData)
	if err != nil {
		return nil, normalizeParseError(err)
	}
	return key, nil
}

// ParsePrivateKeyWithPassphrase parses a PEM-encoded private key, optionally
// decrypting it with passphrase.
func ParsePrivateKeyWithPassphrase(pemData []byte, passphrase []byte) (any, error) {
	if len(passphrase) == 0 {
		return ParsePrivateKey(pemData)
	}
	key, err := xssh.ParseRawPrivateKeyWithPassphrase(pemData, passphrase)
	if err != nil {
		return nil, normalizeParseError(err)
	}
	return key, nil
}

func normalizeParseError(err error) error {
	var missing *xssh.PassphraseMissingError
	if errors.As(err, &missing) {
		return fmt.Errorf("%w", ErrPassphraseRequired)
	}
	return fmt.Errorf("%w: %v", ErrUnsupported, err)
}
