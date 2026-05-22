package knownhosts

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	xssh "golang.org/x/crypto/ssh"
	xknownhosts "golang.org/x/crypto/ssh/knownhosts"
)

type stubAddr string

func (a stubAddr) Network() string { return "tcp" }
func (a stubAddr) String() string  { return string(a) }

func TestParseAndMatchExactHost(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa generate: %v", err)
	}
	pub, err := xssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}

	data := []byte(xknownhosts.Line([]string{HostPort("example.com", 22), "example.com"}, pub) + "\n")
	file, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	cb := NewHostKeyCallback(file)
	if err := cb(HostPort("example.com", 22), stubAddr("203.0.113.10:22"), pub.Type(), pub.Marshal()); err != nil {
		t.Fatalf("HostKeyCallback: %v", err)
	}
}

func TestParseAndMatchHashedHost(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa generate: %v", err)
	}
	pub, err := xssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}

	host := HostPort("example.com", 22)
	line := fmt.Sprintf("%s %s %s\n", xknownhosts.HashHostname(host), pub.Type(), base64.StdEncoding.EncodeToString(pub.Marshal()))
	file, err := Parse([]byte(line))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	cb := NewHostKeyCallback(file)
	if err := cb(host, stubAddr("203.0.113.10:22"), pub.Type(), pub.Marshal()); err != nil {
		t.Fatalf("HostKeyCallback: %v", err)
	}
}

func TestHostKeyMismatch(t *testing.T) {
	keyA, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa generate A: %v", err)
	}
	keyB, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa generate B: %v", err)
	}
	pubA, _ := xssh.NewPublicKey(&keyA.PublicKey)
	pubB, _ := xssh.NewPublicKey(&keyB.PublicKey)

	file, err := Parse([]byte(xknownhosts.Line([]string{HostPort("example.com", 22)}, pubA) + "\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	err = NewHostKeyCallback(file)(HostPort("example.com", 22), stubAddr("203.0.113.10:22"), pubB.Type(), pubB.Marshal())
	if !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("callback error = %v, want ErrKeyMismatch", err)
	}
}

func TestNoMatch(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa generate: %v", err)
	}
	pub, _ := xssh.NewPublicKey(&key.PublicKey)

	file, err := Parse([]byte(xknownhosts.Line([]string{HostPort("example.com", 22)}, pub) + "\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	err = NewHostKeyCallback(file)(HostPort("other.example", 22), stubAddr("203.0.113.10:22"), pub.Type(), pub.Marshal())
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("callback error = %v, want ErrNoMatch", err)
	}
}
