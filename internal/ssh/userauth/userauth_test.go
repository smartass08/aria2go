package userauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"sync"
	"testing"

	"github.com/smartass08/aria2go/internal/ssh/wire"
)

type mockTransport struct {
	mu       sync.Mutex
	incoming [][]byte
	outgoing [][]byte
	outIdx   int
}

func newMockTransport() *mockTransport {
	return &mockTransport{}
}

func (m *mockTransport) Send(payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	m.incoming = append(m.incoming, cp)
	return nil
}

func (m *mockTransport) Receive() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.outIdx >= len(m.outgoing) {
		return nil, io.EOF
	}
	p := m.outgoing[m.outIdx]
	m.outIdx++
	return p, nil
}

func (m *mockTransport) SessionID() []byte {
	return make([]byte, 32)
}

func (m *mockTransport) Close() error {
	return nil
}

func (m *mockTransport) queueResponse(payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	m.outgoing = append(m.outgoing, cp)
}

func buildPKOK(alg string, pubBlob []byte) []byte {
	b := wire.NewBuilder()
	b.PutByte(SSH_MSG_USERAUTH_PK_OK)
	b.WriteString(alg)
	b.WriteBytes(pubBlob)
	return b.Payload()
}

func buildAuthFailure(methods string) []byte {
	b := wire.NewBuilder()
	b.PutByte(SSH_MSG_USERAUTH_FAILURE)
	b.WriteString(methods)
	b.WriteBool(false)
	return b.Payload()
}

func TestAuthenticatePassword_Success(t *testing.T) {
	conn := newMockTransport()

	sessionID := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionID); err != nil {
		t.Fatal(err)
	}

	conn.queueResponse([]byte{SSH_MSG_USERAUTH_SUCCESS})

	client := NewClient(conn)
	err := client.Authenticate(sessionID, []AuthMethod{
		&PasswordAuth{Username: "testuser", Password: "secret"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(conn.incoming) != 1 {
		t.Fatalf("expected 1 incoming packet, got %d", len(conn.incoming))
	}

	p := conn.incoming[0]
	if len(p) < 1 || p[0] != SSH_MSG_USERAUTH_REQUEST {
		t.Fatal("packet not SSH_MSG_USERAUTH_REQUEST")
	}
	r := &wire.Reader{Buf: p}
	_ = r.GetByte()
	user := r.ReadString()
	svc := r.ReadString()
	method := r.ReadString()
	if user != "testuser" || svc != "ssh-connection" || method != "password" {
		t.Fatalf("password auth request: user=%q svc=%q method=%q", user, svc, method)
	}
}

func TestAuthenticatePassword_Banner(t *testing.T) {
	conn := newMockTransport()

	sessionID := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionID); err != nil {
		t.Fatal(err)
	}

	// Banner before success — must be skipped.
	banner := wire.NewBuilder()
	banner.PutByte(SSH_MSG_USERAUTH_BANNER)
	banner.WriteString("Welcome to test server")
	banner.WriteString("")
	conn.queueResponse(banner.Payload())
	conn.queueResponse([]byte{SSH_MSG_USERAUTH_SUCCESS})

	client := NewClient(conn)
	err := client.Authenticate(sessionID, []AuthMethod{
		&PasswordAuth{Username: "testuser", Password: "secret"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthenticatePassword_Failure(t *testing.T) {
	conn := newMockTransport()

	sessionID := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionID); err != nil {
		t.Fatal(err)
	}

	conn.queueResponse(buildAuthFailure("publickey"))

	client := NewClient(conn)
	err := client.Authenticate(sessionID, []AuthMethod{
		&PasswordAuth{Username: "testuser", Password: "wrong"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAuthenticatePublicKey_RSA(t *testing.T) {
	conn := newMockTransport()

	sessionID := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionID); err != nil {
		t.Fatal(err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	_, pubBlob, _, _ := extractKey(key)
	conn.queueResponse(buildPKOK("ssh-rsa", pubBlob))
	conn.queueResponse([]byte{SSH_MSG_USERAUTH_SUCCESS})

	client := NewClient(conn)
	err = client.Authenticate(sessionID, []AuthMethod{
		&PublicKeyAuth{Username: "rsauser", Key: key},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(conn.incoming) != 2 {
		t.Fatalf("expected 2 incoming packets (probe + sign), got %d", len(conn.incoming))
	}

	// Verify probe packet.
	p1 := conn.incoming[0]
	r := &wire.Reader{Buf: p1}
	_ = r.GetByte()
	user := r.ReadString()
	svc := r.ReadString()
	method := r.ReadString()
	hasSig := r.ReadBool()
	if user != "rsauser" || svc != "ssh-connection" || method != "publickey" || hasSig {
		t.Fatal("invalid probe packet")
	}

	// Verify sign packet.
	p2 := conn.incoming[1]
	r2 := &wire.Reader{Buf: p2}
	_ = r2.GetByte()
	user2 := r2.ReadString()
	_ = r2.ReadString()
	method2 := r2.ReadString()
	hasSig2 := r2.ReadBool()
	if user2 != "rsauser" || method2 != "publickey" || !hasSig2 {
		t.Fatal("invalid sign packet")
	}
	// Verify signature algorithm is rsa-sha2-256 (not ssh-rsa) for the signature blob.
	alg2 := r2.ReadString() // key alg
	if alg2 != "ssh-rsa" {
		t.Fatalf("key alg: got %q, want ssh-rsa", alg2)
	}
	_ = r2.ReadBytes() // pub key blob
	sigBlob := r2.ReadBytes()
	sr := &wire.Reader{Buf: sigBlob}
	sigAlg := sr.ReadString()
	if sigAlg != "rsa-sha2-256" {
		t.Fatalf("signature alg: got %q, want rsa-sha2-256", sigAlg)
	}
}

func TestAuthenticatePublicKey_BannerDuringProbe(t *testing.T) {
	conn := newMockTransport()

	sessionID := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionID); err != nil {
		t.Fatal(err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	banner := wire.NewBuilder()
	banner.PutByte(SSH_MSG_USERAUTH_BANNER)
	banner.WriteString("Welcome")
	banner.WriteString("")

	_, pubBlob, _, _ := extractKey(key)
	conn.queueResponse(banner.Payload())
	conn.queueResponse(buildPKOK("ssh-rsa", pubBlob))
	conn.queueResponse([]byte{SSH_MSG_USERAUTH_SUCCESS})

	client := NewClient(conn)
	err = client.Authenticate(sessionID, []AuthMethod{
		&PublicKeyAuth{Username: "rsauser", Key: key},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthenticatePublicKey_Ed25519(t *testing.T) {
	conn := newMockTransport()

	sessionID := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionID); err != nil {
		t.Fatal(err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = pub

	// Build correct PK_OK with echoed algorithm + public key.
	pkOK := wire.NewBuilder()
	pkOK.PutByte(SSH_MSG_USERAUTH_PK_OK)
	pkOK.WriteString("ssh-ed25519")
	keyBlob := wire.NewBuilder()
	keyBlob.WriteString("ssh-ed25519")
	keyBlob.WriteString(string(pub))
	pkOK.WriteBytes(keyBlob.Payload())

	conn.queueResponse(pkOK.Payload())
	conn.queueResponse([]byte{SSH_MSG_USERAUTH_SUCCESS})

	client := NewClient(conn)
	err = client.Authenticate(sessionID, []AuthMethod{
		&PublicKeyAuth{Username: "eduser", Key: priv},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthenticateKeyboardInteractive(t *testing.T) {
	conn := newMockTransport()

	sessionID := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionID); err != nil {
		t.Fatal(err)
	}

	// Build SSH_MSG_USERAUTH_INFO_REQUEST.
	infoReq := wire.NewBuilder()
	infoReq.PutByte(SSH_MSG_USERAUTH_INFO_REQUEST)
	infoReq.WriteString("Test Server")
	infoReq.WriteString("Please answer")
	infoReq.WriteString("")
	infoReq.WriteUint32(2)
	infoReq.WriteString("Password:")
	infoReq.WriteBool(false)
	infoReq.WriteString("OTP:")
	infoReq.WriteBool(false)
	conn.queueResponse(infoReq.Payload())
	conn.queueResponse([]byte{SSH_MSG_USERAUTH_SUCCESS})

	client := NewClient(conn)
	err := client.Authenticate(sessionID, []AuthMethod{
		&KeyboardInteractiveAuth{
			Username: "kbduser",
			Challenge: func(name, instruction string, questions []string) ([]string, error) {
				if name != "Test Server" {
					t.Fatalf("unexpected name: %q", name)
				}
				if len(questions) != 2 {
					t.Fatalf("expected 2 questions, got %d", len(questions))
				}
				return []string{"mypassword", "123456"}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the kbd-int request was sent.
	if len(conn.incoming) < 2 {
		t.Fatal("expected at least 2 incoming packets")
	}

	// Check response packet.
	respPkt := conn.incoming[len(conn.incoming)-1]
	p := &wire.Reader{Buf: respPkt}
	msgType := p.GetByte()
	if msgType != SSH_MSG_USERAUTH_INFO_RESPONSE {
		t.Fatalf("expected info response (%d), got %d", SSH_MSG_USERAUTH_INFO_RESPONSE, msgType)
	}
	numAnswers := p.ReadUint32()
	if numAnswers != 2 {
		t.Fatalf("expected 2 answers, got %d", numAnswers)
	}
	a1 := p.ReadString()
	a2 := p.ReadString()
	if a1 != "mypassword" || a2 != "123456" {
		t.Fatalf("wrong answers: %q, %q", a1, a2)
	}
}

func TestAuthenticateKeyboardInteractive_Banner(t *testing.T) {
	conn := newMockTransport()

	sessionID := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionID); err != nil {
		t.Fatal(err)
	}

	banner := wire.NewBuilder()
	banner.PutByte(SSH_MSG_USERAUTH_BANNER)
	banner.WriteString("Please read instructions")
	banner.WriteString("")

	infoReq := wire.NewBuilder()
	infoReq.PutByte(SSH_MSG_USERAUTH_INFO_REQUEST)
	infoReq.WriteString("Server")
	infoReq.WriteString("")
	infoReq.WriteString("")
	infoReq.WriteUint32(1)
	infoReq.WriteString("Answer:")
	infoReq.WriteBool(true)
	conn.queueResponse(banner.Payload())
	conn.queueResponse(infoReq.Payload())
	conn.queueResponse([]byte{SSH_MSG_USERAUTH_SUCCESS})

	client := NewClient(conn)
	err := client.Authenticate(sessionID, []AuthMethod{
		&KeyboardInteractiveAuth{
			Username: "kbduser",
			Challenge: func(name, instruction string, questions []string) ([]string, error) {
				return []string{"ok"}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthenticateMethodSequence(t *testing.T) {
	conn := newMockTransport()

	sessionID := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionID); err != nil {
		t.Fatal(err)
	}

	// First method fails, second succeeds.
	conn.queueResponse(buildAuthFailure("publickey,keyboard-interactive"))
	conn.queueResponse([]byte{SSH_MSG_USERAUTH_SUCCESS})

	client := NewClient(conn)
	err := client.Authenticate(sessionID, []AuthMethod{
		&PasswordAuth{Username: "u1", Password: "wrong"},
		&PasswordAuth{Username: "u1", Password: "right"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthenticate_AllMethodsFail(t *testing.T) {
	conn := newMockTransport()

	sessionID := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionID); err != nil {
		t.Fatal(err)
	}

	conn.queueResponse(buildAuthFailure("publickey"))
	conn.queueResponse(buildAuthFailure("password"))

	client := NewClient(conn)
	err := client.Authenticate(sessionID, []AuthMethod{
		&PasswordAuth{Username: "u1", Password: "wrong1"},
		&PasswordAuth{Username: "u1", Password: "wrong2"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAuthMethodNames(t *testing.T) {
	if (&PasswordAuth{}).Name() != "password" {
		t.Error("wrong PasswordAuth name")
	}
	if (&PublicKeyAuth{}).Name() != "publickey" {
		t.Error("wrong PublicKeyAuth name")
	}
	if (&KeyboardInteractiveAuth{}).Name() != "keyboard-interactive" {
		t.Error("wrong KeyboardInteractiveAuth name")
	}
}

func TestBuildSignatureBlob(t *testing.T) {
	sessionID := make([]byte, 32)
	blob := buildSignatureBlob(sessionID, "user", "ssh-rsa", []byte("fakeblob"))

	r := &wire.Reader{Buf: blob}
	sid := r.ReadBytes()
	msgType := r.GetByte()
	user := r.ReadString()
	svc := r.ReadString()
	method := r.ReadString()
	hasSig := r.ReadBool()
	alg := r.ReadString()
	pk := r.ReadBytes()

	if len(sid) != 32 {
		t.Errorf("session ID length: %d", len(sid))
	}
	if msgType != SSH_MSG_USERAUTH_REQUEST {
		t.Errorf("msg type: %d", msgType)
	}
	if user != "user" || svc != "ssh-connection" || method != "publickey" || !hasSig {
		t.Fatal("invalid signature blob header")
	}
	if alg != "ssh-rsa" {
		t.Errorf("alg: %q", alg)
	}
	if string(pk) != "fakeblob" {
		t.Errorf("pub key: %q", pk)
	}
}

func BenchmarkPasswordAuth(b *testing.B) {
	conn := newMockTransport()
	conn.queueResponse([]byte{SSH_MSG_USERAUTH_SUCCESS})

	sessionID := make([]byte, 32)
	client := NewClient(conn)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.outIdx = 0
		conn.incoming = conn.incoming[:0]
		conn.queueResponse([]byte{SSH_MSG_USERAUTH_SUCCESS})
		_ = client.Authenticate(sessionID, []AuthMethod{
			&PasswordAuth{Username: "user", Password: "pass"},
		})
	}
}
