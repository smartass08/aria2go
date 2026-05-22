package agent

import (
	"crypto/rand"
	"crypto/rsa"
	"net"
	"path/filepath"
	"testing"

	xssh "golang.org/x/crypto/ssh"
	xagent "golang.org/x/crypto/ssh/agent"
)

func TestAgentListAndSign(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa generate: %v", err)
	}

	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()

	keyring := xagent.NewKeyring()
	if err := keyring.Add(xagent.AddedKey{PrivateKey: key, Comment: "rsa@test"}); err != nil {
		t.Fatalf("keyring.Add: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = xagent.ServeAgent(keyring, c)
			}(conn)
		}
	}()

	ag := &Agent{}
	if err := ag.Connect(socketPath); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer ag.Close()

	ids, err := ag.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("List() = %d identities, want 1", len(ids))
	}
	if ids[0].KeyType != xssh.KeyAlgoRSA {
		t.Fatalf("KeyType = %q, want %q", ids[0].KeyType, xssh.KeyAlgoRSA)
	}

	signer := NewSigner(ag, ids[0])
	sig, err := signer.SignWithAlgorithm(nil, []byte("payload"), xssh.KeyAlgoRSASHA256)
	if err != nil {
		t.Fatalf("SignWithAlgorithm: %v", err)
	}
	if sig.Format != xssh.KeyAlgoRSASHA256 {
		t.Fatalf("sig.Format = %q, want %q", sig.Format, xssh.KeyAlgoRSASHA256)
	}
	if err := signer.PublicKey().Verify([]byte("payload"), sig); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}
