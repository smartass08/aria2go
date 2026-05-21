package mse

import (
	"bytes"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net"
	"testing"

	"github.com/smartass08/aria2go/internal/core"
)

func TestHandshakeRoundTrip(t *testing.T) {
	infoHash := randomInfoHash()
	infoHashes := [][20]byte{infoHash}

	aConn, bConn := pipe()

	var clientConn, serverConn *Conn
	var clientHash, serverHash [20]byte
	var clientPeerID, serverPeerID []byte
	var clientErr, serverErr error

	done := make(chan struct{})
	go func() {
		defer close(done)
		clientConn, clientHash, clientPeerID, clientErr = Initiate(aConn, infoHash, Require)
	}()

	serverConn, serverHash, serverPeerID, serverErr = Receive(bConn, infoHashes, Require)
	<-done

	if clientErr != nil {
		t.Fatalf("Initiate error: %v", clientErr)
	}
	if serverErr != nil {
		t.Fatalf("Receive error: %v", serverErr)
	}
	if clientConn == nil || serverConn == nil {
		t.Fatal("connections should be non-nil")
	}

	if clientHash != infoHash {
		t.Errorf("Initiate received hash %x, want %x", clientHash, infoHash)
	}
	if serverHash != infoHash {
		t.Errorf("Receive matched hash %x, want %x", serverHash, infoHash)
	}
	if len(clientPeerID) != 0 {
		t.Errorf("Initiate IA should be empty (aria2 initiator sends iaLength=0), got %d bytes", len(clientPeerID))
	}
	if len(serverPeerID) != 0 {
		t.Errorf("Receive IA should be empty (aria2 initiator sends iaLength=0), got %d bytes", len(serverPeerID))
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	infoHash := randomInfoHash()
	aConn, bConn := pipe()

	var clientConn, serverConn *Conn
	var clientErr, serverErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		clientConn, _, _, clientErr = Initiate(aConn, infoHash, Require)
	}()

	serverConn, _, _, serverErr = Receive(bConn, [][20]byte{infoHash}, Require)
	<-done

	if clientErr != nil || serverErr != nil {
		t.Fatalf("handshake: client=%v server=%v", clientErr, serverErr)
	}

	want := []byte("hello encrypted world")
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		if _, err := clientConn.Write(want); err != nil {
			t.Errorf("write: %v", err)
		}
	}()

	got := make([]byte, len(want))
	if _, err := io.ReadFull(serverConn, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	<-writeDone

	if !bytes.Equal(got, want) {
		t.Errorf("round-trip: got %q, want %q", got, want)
	}
}

func TestEncryptDecryptBidirectional(t *testing.T) {
	infoHash := randomInfoHash()
	aConn, bConn := pipe()

	var clientConn, serverConn *Conn
	var clientErr, serverErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		clientConn, _, _, clientErr = Initiate(aConn, infoHash, Require)
	}()

	serverConn, _, _, serverErr = Receive(bConn, [][20]byte{infoHash}, Require)
	<-done

	if clientErr != nil || serverErr != nil {
		t.Fatalf("handshake: client=%v server=%v", clientErr, serverErr)
	}

	c2s := []byte("client to server message")
	s2c := []byte("server to client response")

	errCh := make(chan error, 2)
	go func() {
		_, e := clientConn.Write(c2s)
		errCh <- e
	}()
	go func() {
		_, e := serverConn.Write(s2c)
		errCh <- e
	}()

	readC2s := make([]byte, len(c2s))
	readS2c := make([]byte, len(s2c))
	if _, err := io.ReadFull(serverConn, readC2s); err != nil {
		t.Fatalf("server read: %v", err)
	}
	if _, err := io.ReadFull(clientConn, readS2c); err != nil {
		t.Fatalf("client read: %v", err)
	}

	for i := 0; i < 2; i++ {
		if e := <-errCh; e != nil {
			t.Errorf("write error: %v", e)
		}
	}

	if !bytes.Equal(readC2s, c2s) {
		t.Errorf("c2s: got %q, want %q", readC2s, c2s)
	}
	if !bytes.Equal(readS2c, s2c) {
		t.Errorf("s2c: got %q, want %q", readS2c, s2c)
	}
}

func TestModeOff(t *testing.T) {
	infoHash := randomInfoHash()

	t.Run("Initiate", func(t *testing.T) {
		c, _ := pipe()
		conn, _, _, err := Initiate(c, infoHash, Off)
		if err == nil {
			t.Error("Initiate with Mode.Off should return error")
		}
		if conn != nil {
			t.Error("Initiate with Mode.Off should return nil conn")
		}
	})

	t.Run("Receive", func(t *testing.T) {
		c, _ := pipe()
		conn, _, _, err := Receive(c, nil, Off)
		if err == nil {
			t.Error("Receive with Mode.Off should return error")
		}
		if conn != nil {
			t.Error("Receive with Mode.Off should return nil conn")
		}
	})
}

func TestModeRequireFailure(t *testing.T) {
	a, b := pipe()
	b.Close()

	_, _, _, err := Initiate(a, randomInfoHash(), Require)
	if err == nil {
		t.Fatal("Initiate with Require should fail when peer is gone")
	}
	var ce *core.Error
	if !errors.As(err, &ce) {
		t.Errorf("error should wrap core.Error, got %T: %v", err, err)
	}
}

func TestModeAllowFallback(t *testing.T) {
	a, b := pipe()
	b.Close()

	_, _, _, err := Initiate(a, randomInfoHash(), Allow)
	if err == nil {
		t.Fatal("Initiate with Allow should still return error when handshake fails")
	}
	if errors.Is(err, ErrHandshakeFailed) {
		t.Error("Allow should not return wrapped ErrHandshakeFailed; should be raw handshake error")
	}
}

func TestModePreferFallback(t *testing.T) {
	a, b := pipe()
	b.Close()

	_, _, _, err := Initiate(a, randomInfoHash(), Prefer)
	if err == nil {
		t.Fatal("Initiate with Prefer should return error when handshake fails")
	}
}

func TestInfoHashMismatch(t *testing.T) {
	aConn, bConn := pipe()

	clientHash := randomInfoHash()
	serverHashes := [][20]byte{randomInfoHash()}

	var clientErr, serverErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		_, _, _, clientErr = Initiate(aConn, clientHash, Require)
	}()

	_, _, _, serverErr = Receive(bConn, serverHashes, Require)
	<-done

	if !errors.Is(serverErr, ErrInfoHash) {
		t.Errorf("Receive should return ErrInfoHash, got: %v", serverErr)
	}
	if clientErr == nil {
		t.Error("Initiate should fail when peer disconnects after hash mismatch")
	}
}

func TestMultipleInfoHashes(t *testing.T) {
	aConn, bConn := pipe()

	hash1 := randomInfoHash()
	hash2 := randomInfoHash()
	hash3 := randomInfoHash()
	serverHashes := [][20]byte{hash1, hash2, hash3}

	var serverConn *Conn
	var serverHash [20]byte
	var serverErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		_, _, _, _ = Initiate(aConn, hash2, Require)
	}()

	serverConn, serverHash, _, serverErr = Receive(bConn, serverHashes, Require)
	<-done

	if serverErr != nil {
		t.Fatalf("Receive error: %v", serverErr)
	}
	if serverHash != hash2 {
		t.Errorf("Receive matched %x, want %x", serverHash, hash2)
	}
	if serverConn == nil {
		t.Fatal("Receive should return non-nil Conn")
	}
}

func TestZeroInfoHash(t *testing.T) {
	var zeroHash [20]byte

	aConn, bConn := pipe()
	defer aConn.Close()
	defer bConn.Close()

	if _, _, _, err := Initiate(aConn, zeroHash, Require); !errors.Is(err, ErrInfoHash) {
		t.Errorf("Initiate should reject empty info hash, got: %v", err)
	}
}

func TestReceiveRequiresInfoHashes(t *testing.T) {
	aConn, bConn := pipe()
	defer aConn.Close()
	defer bConn.Close()

	if _, _, _, err := Receive(bConn, nil, Require); !errors.Is(err, ErrInfoHash) {
		t.Errorf("Receive should reject empty info hash set, got: %v", err)
	}
}

func TestLargeDataTransfer(t *testing.T) {
	infoHash := randomInfoHash()
	aConn, bConn := pipe()

	var clientConn, serverConn *Conn
	var clientErr, serverErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		clientConn, _, _, clientErr = Initiate(aConn, infoHash, Require)
	}()

	serverConn, _, _, serverErr = Receive(bConn, [][20]byte{infoHash}, Require)
	<-done

	if clientErr != nil || serverErr != nil {
		t.Fatalf("handshake: client=%v server=%v", clientErr, serverErr)
	}

	size := 64 * 1024
	want := make([]byte, size)
	if _, err := rand.Read(want); err != nil {
		t.Fatalf("rand: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, e := clientConn.Write(want)
		errCh <- e
	}()

	got := make([]byte, size)
	if _, err := io.ReadFull(serverConn, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if e := <-errCh; e != nil {
		t.Fatalf("write: %v", e)
	}

	if !bytes.Equal(got, want) {
		hGot := sha1.Sum(got)
		hWant := sha1.Sum(want)
		t.Errorf("large transfer mismatch: got sha1=%x, want sha1=%x", hGot, hWant)
	}
}

func TestReadUntilHandlesNeedsLargerThanScratchBuffer(t *testing.T) {
	want := bytes.Repeat([]byte{0x5a}, 514)
	got, err := readUntil(bytes.NewReader(want), nil, len(want))
	if err != nil {
		t.Fatalf("readUntil: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("readUntil returned %d bytes, want %d", len(got), len(want))
	}
}

func TestParseInitiatorReceivePreservesOverreadPayload(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, sha1.Size)
	payload := []byte("next bittorrent frame")
	plain := make([]byte, vcLength+cryptoBitfieldLength+2+len(payload))
	plain[vcLength+cryptoBitfieldLength-1] = byte(cryptoARC4)
	copy(plain[vcLength+cryptoBitfieldLength+2:], payload)

	wire := append([]byte(nil), plain...)
	enc := mustRC4(t, key)
	enc.XORKeyStream(wire, wire)

	dec := mustRC4(t, key)
	pending, err := parseInitiatorReceive(dec, bytes.NewReader(nil), wire, 0)
	if err != nil {
		t.Fatalf("parseInitiatorReceive: %v", err)
	}
	if !bytes.Equal(pending, payload) {
		t.Fatalf("pending payload = %q, want %q", pending, payload)
	}
}

func TestParseReceiverRejectsOversizeIA(t *testing.T) {
	infoHash := randomInfoHash()
	secret := bytes.Repeat([]byte{0x33}, keyLength)
	_, peerKey := deriveCipherKeys(secret, infoHash[:], false)

	req1 := computeReq1(secret)
	req23 := computeReq23(infoHash[:], secret)
	plain := make([]byte, vcLength+cryptoBitfieldLength+2+2)
	plain[vcLength+cryptoBitfieldLength-1] = byte(cryptoARC4)
	binary.BigEndian.PutUint16(plain[vcLength+cryptoBitfieldLength+2:], uint16(maxIALength+1))

	enc := mustRC4(t, peerKey)
	enc.XORKeyStream(plain, plain)

	buf := make([]byte, 0, len(req1)+len(req23)+len(plain))
	buf = append(buf, req1[:]...)
	buf = append(buf, req23[:]...)
	buf = append(buf, plain...)

	_, _, _, err := parseReceiverReceive(nil, buf, 0, secret, [][20]byte{infoHash}, nil)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("IA too large")) {
		t.Fatalf("parseReceiverReceive error = %v, want IA too large", err)
	}
}

func TestValidateDHKeyRejectsInvalidPublicKeys(t *testing.T) {
	for _, pub := range []*big.Int{big.NewInt(0), big.NewInt(1), new(big.Int).Set(dhPrime)} {
		if err := validateDHKey(pub); err == nil {
			t.Fatalf("validateDHKey(%s) returned nil", pub)
		}
	}
}

func randomInfoHash() [20]byte {
	var h [20]byte
	if _, err := rand.Read(h[:]); err != nil {
		panic(err)
	}
	return h
}

func pipe() (net.Conn, net.Conn) {
	return net.Pipe()
}

func mustRC4(t *testing.T, key []byte) *rc4.Cipher {
	t.Helper()
	c, err := rc4.NewCipher(key)
	if err != nil {
		t.Fatalf("rc4: %v", err)
	}
	var discard [rc4Discard]byte
	c.XORKeyStream(discard[:], discard[:])
	return c
}
