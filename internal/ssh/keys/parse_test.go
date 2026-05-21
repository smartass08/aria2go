package keys

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func generateRSAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa generate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func generatePKCS8PEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa generate: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})
}

func generateECPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa generate: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal ec: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: der,
	})
}

func TestParseRSAPrivateKey(t *testing.T) {
	pemData := generateRSAPEM(t)
	key, err := ParsePrivateKey(pemData)
	if err != nil {
		t.Fatalf("ParsePrivateKey RSA: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
	_, ok := key.(*rsa.PrivateKey)
	if !ok {
		t.Fatalf("expected *rsa.PrivateKey, got %T", key)
	}
}

func TestParsePKCS8PrivateKey(t *testing.T) {
	pemData := generatePKCS8PEM(t)
	key, err := ParsePrivateKey(pemData)
	if err != nil {
		t.Fatalf("ParsePrivateKey PKCS8: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestParseECPrivateKey(t *testing.T) {
	pemData := generateECPEM(t)
	key, err := ParsePrivateKey(pemData)
	if err != nil {
		t.Fatalf("ParsePrivateKey EC: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
	_, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("expected *ecdsa.PrivateKey, got %T", key)
	}
}

func TestParseOpenSSHKeyNotSupported(t *testing.T) {
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: []byte("dummy-data"),
	})
	_, err := ParsePrivateKey(pemData)
	if err == nil {
		t.Error("expected error for OpenSSH key format")
	}
	if !errorsIs(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
}

func TestParseEncryptedKeyNoPassphrase(t *testing.T) {
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "ENCRYPTED PRIVATE KEY",
		Bytes: []byte("dummy-data"),
	})
	_, err := ParsePrivateKey(pemData)
	if err == nil {
		t.Error("expected error for encrypted key without passphrase")
	}
}

func TestParseEncryptedKeyWithPassphrase(t *testing.T) {
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "ENCRYPTED PRIVATE KEY",
		Bytes: []byte("dummy-data"),
	})
	_, err := ParsePrivateKeyWithPassphrase(pemData, []byte("password"))
	if err == nil {
		t.Error("expected not-yet-implemented error for encrypted key decryption")
	}
}

func TestParsePrivateKeyWithPassphraseRSANoPass(t *testing.T) {
	pemData := generateRSAPEM(t)
	key, err := ParsePrivateKeyWithPassphrase(pemData, nil)
	if err != nil {
		t.Fatalf("ParsePrivateKeyWithPassphrase RSA (nil pass): %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestParseInvalidPEM(t *testing.T) {
	_, err := ParsePrivateKey([]byte("not valid PEM data"))
	if err == nil {
		t.Error("expected error for invalid PEM data")
	}
}

func TestParseUnknownPEMType(t *testing.T) {
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: []byte("dummy-data"),
	})
	_, err := ParsePrivateKey(pemData)
	if err == nil {
		t.Error("expected error for unknown PEM type")
	}
}

func TestParseEmptyPEM(t *testing.T) {
	_, err := ParsePrivateKey([]byte{})
	if err == nil {
		t.Error("expected error for empty PEM data")
	}
}

func TestParseEd25519PKCS8(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 generate: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})
	result, err := ParsePrivateKey(pemData)
	if err != nil {
		t.Fatalf("ParsePrivateKey Ed25519 PKCS8: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestRoundTripRSASign(t *testing.T) {
	orig, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa generate: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(orig),
	})
	parsed, err := ParsePrivateKey(pemData)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		t.Fatalf("expected *rsa.PrivateKey, got %T", parsed)
	}
	msg := []byte("aria2 round-trip test signature")
	hash := sha256.Sum256(msg)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15: %v", err)
	}
	if err := rsa.VerifyPKCS1v15(&orig.PublicKey, crypto.SHA256, hash[:], sig); err != nil {
		t.Fatalf("VerifyPKCS1v15: %v", err)
	}
}

func TestRoundTripECDSASign(t *testing.T) {
	orig, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa generate: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(orig)
	if err != nil {
		t.Fatalf("marshal ec: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: der,
	})
	parsed, err := ParsePrivateKey(pemData)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("expected *ecdsa.PrivateKey, got %T", parsed)
	}
	msg := []byte("aria2 ecdsa round-trip test")
	hash := sha256.Sum256(msg)
	sig, err := ecdsa.SignASN1(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatalf("SignASN1: %v", err)
	}
	if !ecdsa.VerifyASN1(&orig.PublicKey, hash[:], sig) {
		t.Fatal("ASN1 signature verification failed")
	}
}

func TestRoundTripEd25519Sign(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 generate: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})
	parsed, err := ParsePrivateKey(pemData)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		t.Fatalf("expected ed25519.PrivateKey, got %T", parsed)
	}
	msg := []byte("aria2 ed25519 round-trip test")
	sig := ed25519.Sign(key, msg)
	if !ed25519.Verify(pub, msg, sig) {
		t.Fatal("ed25519 signature verification failed")
	}
}

func TestRoundTripPKCS8ECDSASign(t *testing.T) {
	orig, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa generate: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(orig)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})
	parsed, err := ParsePrivateKey(pemData)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("expected *ecdsa.PrivateKey, got %T", parsed)
	}
	msg := []byte("aria2 pkcs8 ecdsa round-trip")
	hash := sha256.Sum256(msg)
	sig, err := ecdsa.SignASN1(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatalf("SignASN1: %v", err)
	}
	if !ecdsa.VerifyASN1(&orig.PublicKey, hash[:], sig) {
		t.Fatal("PKCS8 round-trip signature verification failed")
	}
}

func errorsIs(err, target error) bool {
	for {
		if err == target {
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
}
