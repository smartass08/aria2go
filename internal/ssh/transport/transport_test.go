package transport

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
)

func pipe() (net.Conn, net.Conn) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	type result struct {
		c   net.Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := l.Accept()
		ch <- result{c: c, err: err}
	}()

	c1, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		l.Close()
		panic(err)
	}

	r := <-ch
	l.Close()
	if r.err != nil {
		c1.Close()
		panic(r.err)
	}
	return c1, r.c
}

func TestWriteReadPlainPacket(t *testing.T) {
	c1, c2 := pipe()
	defer c1.Close()
	defer c2.Close()

	go func() {
		payload := []byte{20, 1, 2, 3, 4, 5}
		writeSSHPacket(c1, payload, nil, 0)
	}()

	pkt, err := readSSHPacket(c2, nil, 0)
	if err != nil {
		t.Fatalf("readSSHPacket: %v", err)
	}
	if !bytes.Equal(pkt, []byte{20, 1, 2, 3, 4, 5}) {
		t.Errorf("got %v, want %v", pkt, []byte{20, 1, 2, 3, 4, 5})
	}
}

func TestReadSSHPacketReusesPacketBuffer(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte{20, 1, 2, 3, 4, 5}
	if err := writeSSHPacket(&buf, payload, nil, 0); err != nil {
		t.Fatalf("writeSSHPacket: %v", err)
	}
	packet := append([]byte(nil), buf.Bytes()...)

	oldPool := pktBufPool.Load()
	newCalls := 0
	pktBufPool.Store(&sync.Pool{New: func() any {
		newCalls++
		return make([]byte, maxPacketSize+4+32)
	}})
	defer pktBufPool.Store(oldPool)

	var r bytes.Reader
	r.Reset(packet)
	got, err := readSSHPacket(&r, nil, 0)
	if err != nil {
		t.Fatalf("readSSHPacket: %v", err)
	}
	if newCalls == 0 {
		t.Fatal("readSSHPacket did not use pktBufPool for the raw packet body")
	}

	recycled := pktBufPool.Load().Get().([]byte)
	for i := range recycled {
		recycled[i] = 0xff
	}
	pktBufPool.Load().Put(recycled)
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload changed after pooled buffer reuse: got %v want %v", got, payload)
	}
}

func TestWriteReadEncryptedPacket(t *testing.T) {
	c1, c2 := pipe()
	defer c1.Close()
	defer c2.Close()

	key := make([]byte, 16)
	iv := make([]byte, 16)
	_, _ = rand.Read(key)
	_, _ = rand.Read(iv)

	enc, _ := newAESCTR(key, iv)
	dec, _ := newAESCTR(key, iv)

	go func() {
		payload := []byte{5, 1, 2, 3, 4, 5, 6, 7}
		writeSSHPacket(c1, payload, enc, 0)
	}()

	pkt, err := readSSHPacket(c2, dec, 0)
	if err != nil {
		t.Fatalf("readSSHPacket encrypted: %v", err)
	}
	if !bytes.Equal(pkt, []byte{5, 1, 2, 3, 4, 5, 6, 7}) {
		t.Errorf("got %v, want %v", pkt, []byte{5, 1, 2, 3, 4, 5, 6, 7})
	}
}

func TestWriteReadEncryptedWithMAC(t *testing.T) {
	c1, c2 := pipe()
	defer c1.Close()
	defer c2.Close()

	key := make([]byte, 16)
	iv := make([]byte, 16)
	macKey := make([]byte, 32)
	_, _ = rand.Read(key)
	_, _ = rand.Read(iv)
	_, _ = rand.Read(macKey)

	enc, _ := newAESCTR(key, iv)
	dec, _ := newAESCTR(key, iv)
	macState := newHMACState(macKey)

	go func() {
		payload := []byte{6, 10, 20, 30}
		writeSSHPacket(c1, payload, enc, 0, macState)
	}()

	pkt, err := readSSHPacket(c2, dec, 0, macState)
	if err != nil {
		t.Fatalf("readSSHPacket encrypted+MAC: %v", err)
	}
	if !bytes.Equal(pkt, []byte{6, 10, 20, 30}) {
		t.Errorf("got %v, want %v", pkt, []byte{6, 10, 20, 30})
	}
}

func TestMACRejectsTamperedPacket(t *testing.T) {
	c1, c2 := pipe()
	defer c1.Close()
	defer c2.Close()

	key := make([]byte, 16)
	iv := make([]byte, 16)
	macKey := make([]byte, 32)
	_, _ = rand.Read(key)
	_, _ = rand.Read(iv)
	_, _ = rand.Read(macKey)

	enc, _ := newAESCTR(key, iv)
	dec, _ := newAESCTR(key, iv)
	macEnc := newHMACState(macKey)
	macDec := newHMACState(macKey)

	payload := []byte{6, 10, 20, 30}

	go func() {
		writeSSHPacket(c1, payload, enc, 0, macEnc)

		// Tamper with data: write junk after the real packet
		c1.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	}()

	_, err := readSSHPacket(c2, dec, 0, macDec)
	if err != nil {
		t.Fatalf("first read should succeed: %v", err)
	}

	// Second read should fail because we wrote junk
	_, err = readSSHPacket(c2, dec, 1, macDec)
	if err == nil {
		t.Error("expected error reading tampered data, got nil")
	}
}

func TestKEXInitParsing(t *testing.T) {
	cfg := ClientConfig{
		KEXAlgorithms:     []string{"curve25519-sha256", "diffie-hellman-group14-sha256"},
		HostKeyAlgorithms: []string{"ssh-rsa"},
		Ciphers:           []string{"aes128-ctr", "aes256-ctr"},
		MACs:              []string{"hmac-sha2-256"},
		Compression:       []string{"none"},
	}

	conn := &Conn{}
	kexInit := conn.buildKEXInit(cfg)

	if kexInit[0] != sshMsgKEXInit {
		t.Errorf("first byte should be KEXINIT(20), got %d", kexInit[0])
	}

	// Parse the KEX init payload
	parsed := parseNameList(kexInit[17:])
	if len(parsed) != 10 {
		t.Fatalf("expected 10 name-list fields, got %d", len(parsed))
	}

	if len(parsed[0]) < 2 {
		t.Errorf("expected at least 2 kex algorithms, got %d", len(parsed[0]))
	}
	if parsed[0][0] != "curve25519-sha256" {
		t.Errorf("first kex algorithm should be curve25519-sha256, got %q", parsed[0][0])
	}

	if len(parsed[1]) > 0 && parsed[1][0] != "ssh-rsa" {
		t.Errorf("first host key algorithm should be ssh-rsa, got %q", parsed[1][0])
	}
}

func TestAlgorithmNegotiation(t *testing.T) {
	conn := &Conn{}
	cfg := ClientConfig{
		KEXAlgorithms:     []string{"curve25519-sha256", "diffie-hellman-group14-sha256"},
		HostKeyAlgorithms: []string{"ssh-rsa"},
		Ciphers:           []string{"aes256-ctr", "aes128-ctr"},
		MACs:              []string{"hmac-sha2-256"},
		Compression:       []string{"none"},
	}

	// Build server KEX init with specific algorithms
	clientInit := conn.buildKEXInit(cfg)
	clientPayload := clientInit[1:]
	conn.clientKEXInit = clientPayload

	serverInit := buildServerKEXInitPayload([8]string{
		"curve25519-sha256,diffie-hellman-group14-sha256",
		"ssh-rsa",
		"aes128-ctr,aes256-ctr",
		"aes128-ctr,aes256-ctr",
		"hmac-sha2-256",
		"hmac-sha2-256",
		"none",
		"none",
	})
	conn.serverKEXInit = serverInit

	err := conn.negotiateAlgorithms(cfg)
	if err != nil {
		t.Fatalf("negotiateAlgorithms: %v", err)
	}

	if conn.kexAlgo != "curve25519-sha256" {
		t.Errorf("kex: got %q, want curve25519-sha256", conn.kexAlgo)
	}
	if conn.cipherCS != "aes256-ctr" {
		t.Errorf("cipherCS: got %q, want aes256-ctr", conn.cipherCS)
	}
	if conn.macCS != "hmac-sha2-256" {
		t.Errorf("macCS: got %q, want hmac-sha2-256", conn.macCS)
	}
	if conn.compCS != "none" {
		t.Errorf("compCS: got %q, want none", conn.compCS)
	}
}

func buildServerKEXInitPayload(nameLists [8]string) []byte {
	payload := make([]byte, 16)
	_, _ = rand.Read(payload)

	for _, nl := range nameLists {
		payload = binary.BigEndian.AppendUint32(payload, uint32(len(nl)))
		payload = append(payload, []byte(nl)...)
	}

	payload = append(payload, 0, 0, 0, 0, 0)
	return payload
}

func TestAlgorithmNegotiationNoCommon(t *testing.T) {
	conn := &Conn{}
	cfg := ClientConfig{
		KEXAlgorithms: []string{"sntrup761x25519-sha512@openssh.com"},
		Ciphers:       []string{"chacha20-poly1305@openssh.com"},
		MACs:          []string{"hmac-sha2-256"},
		Compression:   []string{"none"},
	}
	conn.clientKEXInit = conn.buildKEXInit(cfg)[1:]
	serverInit := buildServerKEXInitPayload([8]string{
		"ecdh-sha2-nistp256,ecdh-sha2-nistp384",
		"ssh-rsa",
		"aes128-ctr",
		"aes128-ctr",
		"hmac-sha2-256",
		"hmac-sha2-256",
		"none",
		"none",
	})
	conn.serverKEXInit = serverInit

	err := conn.negotiateAlgorithms(cfg)
	if err == nil {
		t.Error("expected error for no common algorithms, got nil")
	}
}

func TestDeriveKey(t *testing.T) {
	shared := []byte("shared-secret-value")
	session := []byte("session-id-value")

	keyA := deriveKey(shared, session, 'A', 16)
	keyB := deriveKey(shared, session, 'B', 16)
	keyC := deriveKey(shared, session, 'C', 16)

	if len(keyA) != 16 {
		t.Errorf("keyA length: got %d, want 16", len(keyA))
	}
	if len(keyB) != 16 {
		t.Errorf("keyB length: got %d, want 16", len(keyB))
	}
	if len(keyC) != 16 {
		t.Errorf("keyC length: got %d, want 16", len(keyC))
	}

	if bytes.Equal(keyA, keyB) {
		t.Error("keys for different chars should not be equal")
	}
	if bytes.Equal(keyA, keyC) {
		t.Error("keys for different chars should not be equal")
	}

	keyA2 := deriveKey(shared, session, 'A', 16)
	if !bytes.Equal(keyA, keyA2) {
		t.Error("key derivation should be deterministic")
	}
}

func TestExchangeHash(t *testing.T) {
	conn := &Conn{
		clientVersion: []byte("SSH-2.0-aria2go"),
		serverVersion: []byte("SSH-2.0-test"),
		clientKEXInit: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		serverKEXInit: []byte{15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
	}

	hostKey := appendSSHStringBytes(nil, []byte("host-key"))
	e := appendSSHStringBytes(nil, []byte("client-ephemeral"))
	f := appendSSHStringBytes(nil, []byte("server-ephemeral"))
	shared := appendSSHStringBytes(nil, []byte("shared-secret"))

	h1 := conn.exchangeHash(hostKey, e, f, shared)
	h2 := conn.exchangeHash(hostKey, e, f, shared)

	if len(h1) != sha256.Size {
		t.Errorf("exchange hash length: got %d, want %d", len(h1), sha256.Size)
	}
	if !bytes.Equal(h1, h2) {
		t.Error("exchange hash should be deterministic")
	}

	h3 := conn.exchangeHash(hostKey, f, e, shared)
	if bytes.Equal(h1, h3) {
		t.Error("different e/f order should produce different hash")
	}
}

func TestVersionExchange(t *testing.T) {
	c1, c2 := pipe()
	defer c1.Close()
	defer c2.Close()

	conn := &Conn{c: c1}

	go func() {
		// Read client version
		buf := make([]byte, 256)
		n, _ := c2.Read(buf)
		if string(buf[:n]) != string(sshVersionBytes) {
			t.Errorf("server got version %q, want %q", string(buf[:n]), string(sshVersionBytes))
		}
		// Send server version
		c2.Write([]byte("SSH-2.0-OpenSSH_8.9\r\n"))
	}()

	err := conn.exchangeVersions()
	if err != nil {
		t.Fatalf("exchangeVersions: %v", err)
	}
	if !bytes.Equal(conn.clientVersion, []byte("SSH-2.0-aria2go")) {
		t.Errorf("clientVersion: got %q", conn.clientVersion)
	}
	if !bytes.Equal(conn.serverVersion, []byte("SSH-2.0-OpenSSH_8.9")) {
		t.Errorf("serverVersion: got %q", conn.serverVersion)
	}
}

func TestServiceRequest(t *testing.T) {
	c1, c2 := pipe()
	defer c1.Close()
	defer c2.Close()

	key := make([]byte, 16)
	iv := make([]byte, 16)
	_, _ = rand.Read(key)
	_, _ = rand.Read(iv)

	enc, _ := newAESCTR(key, iv)
	dec, _ := newAESCTR(key, iv)

	conn := &Conn{c: c1, encDirection: enc, decDirection: dec}

	go func() {
		// Read encrypted service request
		pkt, err := readSSHPacket(c2, dec, 0)
		if err != nil {
			t.Errorf("server read service request: %v", err)
			return
		}
		if pkt[0] != sshMsgServiceRequest {
			t.Errorf("expected SERVICE_REQUEST(5), got %d", pkt[0])
			return
		}

		// Parse the service name directly (pkt[1:] is the 4-byte length + name)
		serviceName, _, _ := parseSSHString(pkt[1:])
		if serviceName != "ssh-userauth" {
			t.Errorf("service name: got %q, want ssh-userauth", serviceName)
		}

		// Send SERVICE_ACCEPT
		reply := []byte{sshMsgServiceAccept}
		writeSSHPacket(c2, reply, enc, 0)
	}()

	err := conn.requestService("ssh-userauth")
	if err != nil {
		t.Fatalf("requestService: %v", err)
	}
}

func TestFullHandshakeCurve25519(t *testing.T) {
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	clientConn, serverConn := pipe()

	done := make(chan error, 1)
	go func() {
		done <- runMockServer(serverConn, serverKey, "curve25519-sha256")
	}()

	conn, sessionID, err := ClientHandshake(clientConn, ClientConfig{
		KEXAlgorithms:     []string{"curve25519-sha256"},
		HostKeyAlgorithms: []string{"ssh-rsa"},
		Ciphers:           []string{"aes128-ctr"},
		MACs:              []string{"hmac-sha2-256"},
		Compression:       []string{"none"},
	})
	if err != nil {
		t.Fatalf("ClientHandshake: %v", err)
	}
	if conn == nil {
		t.Fatal("conn is nil")
	}
	if len(sessionID) != sha256.Size {
		t.Errorf("sessionID length: got %d, want %d", len(sessionID), sha256.Size)
	}

	if serverErr := <-done; serverErr != nil {
		t.Fatalf("server error: %v", serverErr)
	}
}

func TestFullHandshakeDH14(t *testing.T) {
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	clientConn, serverConn := pipe()

	done := make(chan error, 1)
	go func() {
		done <- runMockServerDH(serverConn, serverKey)
	}()

	conn, sessionID, err := ClientHandshake(clientConn, ClientConfig{
		KEXAlgorithms:     []string{"diffie-hellman-group14-sha256"},
		HostKeyAlgorithms: []string{"ssh-rsa"},
		Ciphers:           []string{"aes128-ctr"},
		MACs:              []string{"hmac-sha2-256"},
		Compression:       []string{"none"},
	})
	if err != nil {
		t.Fatalf("ClientHandshake DH14: %v", err)
	}
	if conn == nil {
		t.Fatal("conn is nil")
	}
	if len(sessionID) != sha256.Size {
		t.Errorf("sessionID length: got %d, want %d", len(sessionID), sha256.Size)
	}

	if serverErr := <-done; serverErr != nil {
		t.Fatalf("server error: %v", serverErr)
	}
}

func TestClientHandshakeBadVersion(t *testing.T) {
	c1, c2 := pipe()
	defer c1.Close()
	defer c2.Close()

	go func() {
		buf := make([]byte, 256)
		c2.Read(buf)
		c2.Write([]byte("HTTP/1.1 200 OK\r\n"))
	}()

	_, _, err := ClientHandshake(c1, ClientConfig{
		KEXAlgorithms: []string{"curve25519-sha256"},
	})
	if err == nil {
		t.Error("expected error for bad version, got nil")
	}
}

// runMockServer implements a minimal SSH-2 server for testing
func runMockServer(conn net.Conn, hostKey *rsa.PrivateKey, kexAlgo string) error {
	// 1. Version exchange - read byte by byte until \n
	clientVer, err := readVersionLine(conn)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(clientVer, []byte("SSH-2.0-")) {
		return nil
	}

	_, err = conn.Write([]byte("SSH-2.0-aria2go-mock\r\n"))
	if err != nil {
		return err
	}
	clientVerBytes := clientVer[:len(clientVer)-2]

	// 2. Read KEXINIT
	clientKEXPkt, err := readSSHPacket(conn, nil, 0)
	if err != nil {
		return err
	}
	if clientKEXPkt[0] != sshMsgKEXInit {
		return nil
	}
	clientKEXPayload := clientKEXPkt[1:]

	// 3. Send KEXINIT
	serverKEXInit := buildServerKEXInitPayload([8]string{
		"curve25519-sha256,diffie-hellman-group14-sha256",
		"ssh-rsa",
		"aes128-ctr,aes256-ctr",
		"aes128-ctr,aes256-ctr",
		"hmac-sha2-256",
		"hmac-sha2-256",
		"none",
		"none",
	})

	err = writeSSHPacket(conn, append([]byte{sshMsgKEXInit}, serverKEXInit...), nil, 0)
	if err != nil {
		return err
	}

	// 4. Read KEX init
	clientKEX, err := readSSHPacket(conn, nil, 0)
	if err != nil {
		return err
	}

	var serverPubDER []byte
	pubDER, err := x509.MarshalPKIXPublicKey(&hostKey.PublicKey)
	if err != nil {
		return err
	}
	serverPubDER = pubDER

	_ = clientKEXPayload
	_ = clientVerBytes
	_ = serverPubDER

	if kexAlgo == "curve25519-sha256" || kexAlgo == "curve25519-sha256@libssh.org" {
		return runServerECDH(conn, hostKey, clientKEX, clientVerBytes, clientKEXPayload, serverKEXInit)
	}

	return nil
}

func runServerECDH(conn net.Conn, hostKey *rsa.PrivateKey, clientKEX, clientVer, clientKEXPayload, serverKEXPayload []byte) error {
	// Client KEX payload: [msg_type=30][4-byte-len][32-byte-pubkey]
	pubBytes, _, err := parseSSHBytes(clientKEX[1:])
	if err != nil {
		return err
	}
	if len(pubBytes) != 32 {
		return nil
	}
	clientPubBytes := pubBytes

	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	serverPub := serverKey.PublicKey().Bytes()

	clientKey, err := ecdh.X25519().NewPublicKey(clientPubBytes)
	if err != nil {
		return err
	}
	serverECDH, err := serverKey.ECDH(clientKey)
	if err != nil {
		return err
	}

	hostKeyBlob := encodeSSHRSAKeyBlob(hostKey)

	// Compute exchange hash with proper SSH wire encoding
	serverVer := []byte("SSH-2.0-aria2go-mock")
	h := sha256.New()
	h.Write(appendSSHStringBytes(nil, clientVer))
	h.Write(appendSSHStringBytes(nil, serverVer))
	h.Write(appendSSHStringBytes(nil, clientKEXPayload))
	h.Write(appendSSHStringBytes(nil, serverKEXPayload))
	h.Write(appendSSHStringBytes(nil, hostKeyBlob))
	h.Write(appendSSHStringBytes(nil, clientPubBytes))
	h.Write(appendSSHStringBytes(nil, serverPub))
	h.Write(appendSSHStringBytes(nil, serverECDH))
	exchangeHash := h.Sum(nil)

	// Build KEX reply
	reply := []byte{sshMsgKEXECDHReply}
	reply = appendSSHStringBytes(reply, hostKeyBlob)
	reply = appendSSHStringBytes(reply, serverPub)

	sig, err := rsa.SignPKCS1v15(rand.Reader, hostKey, 0, exchangeHash)
	if err != nil {
		return err
	}
	sigBlob := appendSSHStringBytes(nil, []byte("ssh-rsa"))
	sigBlob = appendSSHStringBytes(sigBlob, sig)

	reply = appendSSHStringBytes(reply, sigBlob)

	err = writeSSHPacket(conn, reply, nil, 0)
	if err != nil {
		return err
	}

	// 5. Read NEWKEYS
	pkt, err := readSSHPacket(conn, nil, 0)
	if err != nil {
		return err
	}
	if pkt[0] != sshMsgNewKeys {
		return nil
	}

	// 6. Send NEWKEYS
	err = writeSSHPacket(conn, []byte{sshMsgNewKeys}, nil, 0)
	if err != nil {
		return err
	}

	// 7. Read SERVICE_REQUEST (encrypted)
	// Derive keys on server side
	sessionID := exchangeHash
	keyLen := 16
	ivLen := 16

	// Client->server: encryption key C, IV A, MAC key E
	encKeyCS := deriveKey(serverECDH, sessionID, 'C', keyLen)
	initialIVCS := deriveKey(serverECDH, sessionID, 'A', ivLen)
	integKeyCS := deriveKey(serverECDH, sessionID, 'E', 32)

	// Server->client: encryption key D, IV B, MAC key F
	encKeySC := deriveKey(serverECDH, sessionID, 'D', keyLen)
	initialIVSC := deriveKey(serverECDH, sessionID, 'B', ivLen)
	integKeySC := deriveKey(serverECDH, sessionID, 'F', 32)

	decServer, _ := newAESCTR(encKeyCS, initialIVCS)
	encServer, _ := newAESCTR(encKeySC, initialIVSC)
	macRead := newHMACState(integKeyCS)
	macWrite := newHMACState(integKeySC)

	servicePkt, err := readSSHPacket(conn, decServer, 0, macRead)
	if err != nil {
		return err
	}
	if servicePkt[0] != sshMsgServiceRequest {
		return nil
	}

	// 8. Send SERVICE_ACCEPT (encrypted)
	err = writeSSHPacket(conn, []byte{sshMsgServiceAccept}, encServer, 0, macWrite)
	if err != nil {
		return err
	}

	return nil
}

func runMockServerDH(conn net.Conn, hostKey *rsa.PrivateKey) error {
	// 1. Version exchange - read byte by byte until \n
	clientVer, err := readVersionLine(conn)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(clientVer, []byte("SSH-2.0-")) {
		return nil
	}

	_, err = conn.Write([]byte("SSH-2.0-aria2go-mock\r\n"))
	if err != nil {
		return err
	}
	clientVerBytes := clientVer[:len(clientVer)-2]
	serverVerBytes := []byte("SSH-2.0-aria2go-mock")

	// 2. Read KEXINIT
	clientKEXPkt, err := readSSHPacket(conn, nil, 0)
	if err != nil {
		return err
	}
	if clientKEXPkt[0] != sshMsgKEXInit {
		return nil
	}
	clientKEXPayload := clientKEXPkt[1:]

	// 3. Send KEXINIT
	serverKEXPayload := buildServerKEXInitPayload([8]string{
		"diffie-hellman-group14-sha256,curve25519-sha256",
		"ssh-rsa",
		"aes128-ctr,aes256-ctr",
		"aes128-ctr,aes256-ctr",
		"hmac-sha2-256",
		"hmac-sha2-256",
		"none",
		"none",
	})

	err = writeSSHPacket(conn, append([]byte{sshMsgKEXInit}, serverKEXPayload...), nil, 0)
	if err != nil {
		return err
	}

	// 4. Read DH init
	clientDH, err := readSSHPacket(conn, nil, 0)
	if err != nil {
		return err
	}
	if clientDH[0] != sshMsgKEXDHInit {
		return nil
	}
	clientDHValue, _, err := parseSSHMPInt(clientDH[1:])
	if err != nil {
		return err
	}

	// Generate server DH key pair
	privateKey, err := rand.Int(rand.Reader, dhGroup14Prime)
	if err != nil {
		return err
	}
	publicKey := new(big.Int).Exp(dhGroup14Generator, privateKey, dhGroup14Prime)
	sharedSecret := new(big.Int).Exp(clientDHValue, privateKey, dhGroup14Prime).Bytes()

	hostKeyBlob := encodeSSHRSAKeyBlob(hostKey)

	// Compute exchange hash with proper SSH wire encoding
	h := sha256.New()
	h.Write(appendSSHStringBytes(nil, clientVerBytes))
	h.Write(appendSSHStringBytes(nil, serverVerBytes))
	h.Write(appendSSHStringBytes(nil, clientKEXPayload))
	h.Write(appendSSHStringBytes(nil, serverKEXPayload))
	h.Write(appendSSHStringBytes(nil, hostKeyBlob))
	h.Write(appendSSHMPInt(nil, clientDHValue))
	h.Write(appendSSHMPInt(nil, publicKey))
	h.Write(appendSSHMPInt(nil, new(big.Int).SetBytes(sharedSecret)))
	exchangeHash := h.Sum(nil)

	// Build KEX reply
	reply := []byte{sshMsgKEXDHReply}
	reply = appendSSHStringBytes(reply, hostKeyBlob)
	reply = appendSSHMPInt(reply, publicKey)

	sig, err := rsa.SignPKCS1v15(rand.Reader, hostKey, 0, exchangeHash)
	if err != nil {
		return err
	}
	sigBlob := appendSSHStringBytes(nil, []byte("ssh-rsa"))
	sigBlob = appendSSHStringBytes(sigBlob, sig)
	reply = appendSSHStringBytes(reply, sigBlob)

	err = writeSSHPacket(conn, reply, nil, 0)
	if err != nil {
		return err
	}

	// 5. Read NEWKEYS
	pkt, err := readSSHPacket(conn, nil, 0)
	if err != nil {
		return err
	}
	if pkt[0] != sshMsgNewKeys {
		return nil
	}

	// 6. Send NEWKEYS
	err = writeSSHPacket(conn, []byte{sshMsgNewKeys}, nil, 0)
	if err != nil {
		return err
	}

	// 7. Read SERVICE_REQUEST (encrypted)
	sessionID := exchangeHash
	keyLen := 16
	ivLen := 16

	encKeyCS := deriveKey(sharedSecret, sessionID, 'C', keyLen)
	initialIVCS := deriveKey(sharedSecret, sessionID, 'A', ivLen)
	integKeyCS := deriveKey(sharedSecret, sessionID, 'E', 32)
	encKeySC := deriveKey(sharedSecret, sessionID, 'D', keyLen)
	initialIVSC := deriveKey(sharedSecret, sessionID, 'B', ivLen)
	integKeySC := deriveKey(sharedSecret, sessionID, 'F', 32)

	decServer, _ := newAESCTR(encKeyCS, initialIVCS)
	encServer, _ := newAESCTR(encKeySC, initialIVSC)
	macRead := newHMACState(integKeyCS)
	macWrite := newHMACState(integKeySC)

	servicePkt, err := readSSHPacket(conn, decServer, 0, macRead)
	if err != nil {
		return err
	}
	if servicePkt[0] != sshMsgServiceRequest {
		return nil
	}

	err = writeSSHPacket(conn, []byte{sshMsgServiceAccept}, encServer, 0, macWrite)
	if err != nil {
		return err
	}

	return nil
}

func readVersionLine(conn net.Conn) ([]byte, error) {
	var buf [256]byte
	n := 0
	for n < 255 {
		if _, err := io.ReadFull(conn, buf[n:n+1]); err != nil {
			return nil, err
		}
		n++
		if buf[n-1] == '\n' {
			break
		}
	}
	return buf[:n], nil
}

func encodeSSHRSAKeyBlob(key *rsa.PrivateKey) []byte {
	b := appendSSHStringBytes(nil, []byte("ssh-rsa"))
	b = appendSSHMPInt(b, big.NewInt(int64(key.PublicKey.E)))
	b = appendSSHMPInt(b, key.PublicKey.N)
	return b
}
