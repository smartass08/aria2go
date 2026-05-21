package mse

import (
	"bytes"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"

	"github.com/smartass08/aria2go/internal/core"
)

type Mode uint8

const (
	Off Mode = iota
	Allow
	Prefer
	Require
)

var (
	ErrHandshakeFailed = core.NewError(core.ExitNetworkProblem, "MSE handshake failed")
	ErrInfoHash        = core.NewError(core.ExitUnknownError, "MSE info hash mismatch")
)

const (
	primeBits            = 768
	keyLength            = 96
	maxPadLength         = 512
	cryptoBitfieldLength = 4
	vcLength             = 8
	infoHashLength       = 20
	maxIALength          = 68
	privateKeyBits       = 160
	rc4Discard           = 1024
)

const (
	cryptoPlainText uint32 = 0x01
	cryptoARC4      uint32 = 0x02
)

var (
	dhPrime       *big.Int
	dhGenerator   = big.NewInt(2)
	maxPrivateKey *big.Int
)

func init() {
	dhPrime = new(big.Int)
	dhPrime.SetString(
		"FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1"+
			"29024E088A67CC74020BBEA63B139B22514A08798E3404DD"+
			"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245"+
			"E485B576625E7EC6F44C42E9A63A36210000000000090563", 16)
	maxPrivateKey = new(big.Int).Lsh(big.NewInt(1), privateKeyBits)
}

var (
	bigIntPool = sync.Pool{
		New: func() any { return new(big.Int) },
	}
	deriveKeyPool = sync.Pool{
		New: func() any {
			b := make([]byte, 4+keyLength+infoHashLength)
			return &b
		},
	}
	bufPool1K = sync.Pool{
		New: func() any {
			b := make([]byte, 1024)
			return &b
		},
	}
	bufPool512 = sync.Pool{
		New: func() any {
			b := make([]byte, 512)
			return &b
		},
	}
	bytesBufPool = sync.Pool{
		New: func() any { return new(bytes.Buffer) },
	}
)

type Conn struct {
	net.Conn
	encrypt *rc4.Cipher
	decrypt *rc4.Cipher
	pending []byte
}

func (c *Conn) Read(b []byte) (int, error) {
	if len(c.pending) > 0 {
		n := copy(b, c.pending)
		copy(c.pending, c.pending[n:])
		c.pending = c.pending[:len(c.pending)-n]
		return n, nil
	}
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.decrypt.XORKeyStream(b[:n], b[:n])
	}
	return n, err
}

func (c *Conn) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	enc := make([]byte, len(b))
	c.encrypt.XORKeyStream(enc, b)
	return c.Conn.Write(enc)
}

func Initiate(c net.Conn, infoHash [20]byte, mode Mode) (*Conn, [20]byte, []byte, error) {
	if mode == Off {
		return nil, [20]byte{}, nil, fmt.Errorf("mse: Initiate called with Mode.Off")
	}
	if infoHash == ([20]byte{}) {
		return nil, [20]byte{}, nil, fmt.Errorf("mse: empty info hash: %w", ErrInfoHash)
	}
	encConn, err := initiateHandshake(c, infoHash)
	if err != nil {
		if mode == Require {
			c.Close()
			return nil, [20]byte{}, nil, core.WrapError(core.ExitNetworkProblem,
				"MSE handshake required but failed", err)
		}
		return nil, [20]byte{}, nil, err
	}
	return encConn, infoHash, nil, nil
}

func Receive(c net.Conn, infoHashes [][20]byte, mode Mode) (*Conn, [20]byte, []byte, error) {
	if mode == Off {
		return nil, [20]byte{}, nil, fmt.Errorf("mse: Receive called with Mode.Off")
	}
	if len(infoHashes) == 0 {
		return nil, [20]byte{}, nil, fmt.Errorf("mse: no candidate info hashes: %w", ErrInfoHash)
	}
	encConn, matchedHash, ia, err := receiveHandshake(c, infoHashes)
	if err != nil {
		if mode == Require {
			c.Close()
			return nil, [20]byte{}, nil, core.WrapError(core.ExitNetworkProblem,
				"MSE handshake required but failed", err)
		}
		return nil, [20]byte{}, nil, err
	}
	return encConn, matchedHash, ia, nil
}

func generateDHKeyPair() (*big.Int, *big.Int, error) {
	priv, err := rand.Int(rand.Reader, maxPrivateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("mse: generate private key: %w", err)
	}
	pub := bigIntPool.Get().(*big.Int)
	pub.Exp(dhGenerator, priv, dhPrime)
	return priv, pub, nil
}

func encodeDHKey(pub *big.Int) []byte {
	b := pub.Bytes()
	out := make([]byte, keyLength)
	if len(b) <= keyLength {
		copy(out[keyLength-len(b):], b)
	} else {
		copy(out, b[len(b)-keyLength:])
	}
	return out
}

func decodeDHKey(raw []byte) *big.Int {
	i := bigIntPool.Get().(*big.Int)
	return i.SetBytes(raw)
}

func validateDHKey(pub *big.Int) error {
	if pub == nil {
		return errors.New("mse: missing peer public key")
	}
	min := big.NewInt(2)
	max := bigIntPool.Get().(*big.Int)
	max.Sub(dhPrime, min)
	defer bigIntPool.Put(max)
	if pub.Cmp(min) < 0 || pub.Cmp(max) > 0 {
		return fmt.Errorf("mse: invalid peer public key")
	}
	return nil
}

func computeSharedSecret(peerPub, priv *big.Int) []byte {
	s := bigIntPool.Get().(*big.Int)
	s.Exp(peerPub, priv, dhPrime)
	b := s.Bytes()
	out := make([]byte, keyLength)
	if len(b) <= keyLength {
		copy(out[keyLength-len(b):], b)
	} else {
		copy(out, b[len(b)-keyLength:])
	}
	bigIntPool.Put(s)
	return out
}

func deriveCipherKeys(secret, infoHash []byte, initiator bool) (localKey, peerKey []byte) {
	var localPrefix, peerPrefix string
	if initiator {
		localPrefix = "keyA"
		peerPrefix = "keyB"
	} else {
		localPrefix = "keyB"
		peerPrefix = "keyA"
	}
	localKey = deriveKey(localPrefix, secret, infoHash)
	peerKey = deriveKey(peerPrefix, secret, infoHash)
	return
}

func deriveKey(prefix string, secret, infoHash []byte) []byte {
	bufp := deriveKeyPool.Get().(*[]byte)
	buf := *bufp
	bufLen := 4 + keyLength + infoHashLength
	copy(buf[:4], prefix)
	copy(buf[4:], secret)
	copy(buf[4+keyLength:], infoHash)
	h := sha1.Sum(buf[:bufLen])
	deriveKeyPool.Put(bufp)
	return h[:]
}

func newRC4Cipher(key []byte) (*rc4.Cipher, error) {
	c, err := rc4.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("mse: rc4: %w", err)
	}
	var discard [rc4Discard]byte
	c.XORKeyStream(discard[:], discard[:])
	return c, nil
}

func computeVC(zeros [vcLength]byte, peerKey []byte) [vcLength]byte {
	c, _ := rc4.NewCipher(peerKey)
	var discard [rc4Discard]byte
	c.XORKeyStream(discard[:], discard[:])
	var out [vcLength]byte
	c.XORKeyStream(out[:], zeros[:])
	return out
}

func computeReq1(secret []byte) [20]byte {
	var buf [4 + keyLength]byte
	copy(buf[:4], "req1")
	copy(buf[4:], secret)
	return sha1.Sum(buf[:])
}

func computeReq23(infoHash, secret []byte) [20]byte {
	var buf2 [4 + infoHashLength]byte
	var buf3 [4 + keyLength]byte
	copy(buf2[:4], "req2")
	copy(buf2[4:], infoHash)
	copy(buf3[:4], "req3")
	copy(buf3[4:], secret)
	xh := sha1.Sum(buf2[:])
	yh := sha1.Sum(buf3[:])

	var out [20]byte
	for i := range out {
		out[i] = xh[i] ^ yh[i]
	}
	return out
}

func initiateHandshake(c net.Conn, infoHash [20]byte) (*Conn, error) {
	priv, pub, err := generateDHKeyPair()
	if err != nil {
		return nil, err
	}

	publicKeyPad, err := writeDHPublicKey(c, pub, "initiator")
	if err != nil {
		return nil, err
	}

	peerKeyRaw, err := readExact(c, keyLength)
	if err != nil {
		return nil, fmt.Errorf("mse: read peer public key: %w", err)
	}
	peerPub := decodeDHKey(peerKeyRaw)
	if err := validateDHKey(peerPub); err != nil {
		return nil, err
	}

	secret := computeSharedSecret(peerPub, priv)
	localKey, peerKey := deriveCipherKeys(secret, infoHash[:], true)

	encCipher, err := newRC4Cipher(localKey)
	if err != nil {
		return nil, err
	}
	decCipher, err := newRC4Cipher(peerKey)
	if err != nil {
		return nil, err
	}

	req1 := computeReq1(secret)
	req23 := computeReq23(infoHash[:], secret)

	vc := [vcLength]byte{}
	padCLen, err := randRange(maxPadLength + 1)
	if err != nil {
		return nil, fmt.Errorf("mse: random padC length: %w", err)
	}
	iaLen := 0

	blobLen := vcLength + cryptoBitfieldLength + 2 + padCLen + 2
	blob := make([]byte, blobLen)
	off := 0
	copy(blob[off:], vc[:])
	off += vcLength
	blob[off+3] = byte(cryptoARC4)
	off += cryptoBitfieldLength
	binary.BigEndian.PutUint16(blob[off:], uint16(padCLen))
	off += 2
	if padCLen > 0 {
		if _, err := io.ReadFull(rand.Reader, blob[off:off+padCLen]); err != nil {
			return nil, fmt.Errorf("mse: random padC: %w", err)
		}
	}
	off += padCLen
	binary.BigEndian.PutUint16(blob[off:], uint16(iaLen))
	encCipher.XORKeyStream(blob, blob)

	step2 := bytesBufPool.Get().(*bytes.Buffer)
	step2.Reset()
	step2.Grow(len(publicKeyPad) + 20 + 20 + len(blob))
	step2.Write(publicKeyPad)
	step2.Write(req1[:])
	step2.Write(req23[:])
	step2.Write(blob)

	if _, err := c.Write(step2.Bytes()); err != nil {
		bytesBufPool.Put(step2)
		return nil, fmt.Errorf("mse: write initiator step2: %w", err)
	}
	bytesBufPool.Put(step2)

	marker := computeVC(vc, peerKey)

	buf := make([]byte, 0, 1024)
	for {
		tmpPtr := bufPool1K.Get().(*[]byte)
		tmp := *tmpPtr
		n, err := c.Read(tmp)
		if err != nil {
			bufPool1K.Put(tmpPtr)
			return nil, fmt.Errorf("mse: read during VC marker search: %w", err)
		}
		buf = append(buf, tmp[:n]...)
		bufPool1K.Put(tmpPtr)
		if idx := bytes.Index(buf, marker[:]); idx >= 0 {
			pending, err := parseInitiatorReceive(decCipher, c, buf, idx)
			if err != nil {
				return nil, err
			}
			return &Conn{Conn: c, encrypt: encCipher, decrypt: decCipher, pending: pending}, nil
		}
		if len(buf) >= 520 {
			return nil, fmt.Errorf("mse: failed to find VC marker")
		}
	}
}

func parseInitiatorReceive(dec *rc4.Cipher, conn io.Reader, buf []byte, pos int) ([]byte, error) {
	need := pos + vcLength + cryptoBitfieldLength + 2
	buf, err := readUntil(conn, buf, need)
	if err != nil {
		return nil, err
	}

	// Verify and consume VC to advance RC4 keystream (8 bytes).
	var vcDec [vcLength]byte
	dec.XORKeyStream(vcDec[:], buf[pos:pos+vcLength])
	if vcDec != ([vcLength]byte{}) {
		return nil, fmt.Errorf("mse: initiator VC verification failed: %x", vcDec)
	}
	pos += vcLength

	dec.XORKeyStream(buf[pos:pos+cryptoBitfieldLength], buf[pos:pos+cryptoBitfieldLength])
	if buf[pos+3]&byte(cryptoARC4) == 0 {
		return nil, fmt.Errorf("mse: no supported crypto type selected")
	}
	pos += cryptoBitfieldLength

	var padDLenBE [2]byte
	dec.XORKeyStream(padDLenBE[:], buf[pos:pos+2])
	padDLen := int(binary.BigEndian.Uint16(padDLenBE[:]))
	pos += 2

	if padDLen > maxPadLength {
		return nil, fmt.Errorf("mse: padD too large: %d", padDLen)
	}

	end := pos + padDLen
	if padDLen > 0 {
		buf, err = readUntil(conn, buf, end)
		if err != nil {
			return nil, err
		}
		dec.XORKeyStream(buf[pos:end], buf[pos:end])
	}
	if len(buf) <= end {
		return nil, nil
	}
	pending := append([]byte(nil), buf[end:]...)
	dec.XORKeyStream(pending, pending)
	return pending, nil
}

func receiveHandshake(c net.Conn, infoHashes [][20]byte) (*Conn, [20]byte, []byte, error) {
	priv, pub, err := generateDHKeyPair()
	if err != nil {
		return nil, [20]byte{}, nil, err
	}

	peerKeyRaw, err := readExact(c, keyLength)
	if err != nil {
		return nil, [20]byte{}, nil, fmt.Errorf("mse: read initiator public key: %w", err)
	}
	peerPub := decodeDHKey(peerKeyRaw)
	if err := validateDHKey(peerPub); err != nil {
		return nil, [20]byte{}, nil, err
	}

	secret := computeSharedSecret(peerPub, priv)

	publicKeyPad, err := writeDHPublicKey(c, pub, "receiver")
	if err != nil {
		return nil, [20]byte{}, nil, err
	}

	req1 := computeReq1(secret)

	buf := make([]byte, 0, 1024)
	for {
		tmpPtr := bufPool1K.Get().(*[]byte)
		tmp := *tmpPtr
		n, err := c.Read(tmp)
		if err != nil {
			bufPool1K.Put(tmpPtr)
			return nil, [20]byte{}, nil, fmt.Errorf("mse: read during hash marker search: %w", err)
		}
		buf = append(buf, tmp[:n]...)
		bufPool1K.Put(tmpPtr)
		if idx := bytes.Index(buf, req1[:]); idx >= 0 {
			return parseReceiverReceive(c, buf, idx, secret, infoHashes, publicKeyPad)
		}
		if len(buf) >= 532 {
			return nil, [20]byte{}, nil, fmt.Errorf("mse: failed to find hash marker")
		}
	}
}

func parseReceiverReceive(conn net.Conn, buf []byte, foundIdx int, secret []byte, infoHashes [][20]byte, publicKeyPad []byte) (*Conn, [20]byte, []byte, error) {
	pos := foundIdx + 20

	headerLen := pos + 20 + vcLength + cryptoBitfieldLength + 2
	buf, err := readUntil(conn, buf, headerLen)
	if err != nil {
		return nil, [20]byte{}, nil, err
	}

	receivedReq23 := buf[pos : pos+20]
	pos += 20

	var matchedHash [20]byte
	var foundHash bool
	for _, ih := range infoHashes {
		if [20]byte(receivedReq23) == computeReq23(ih[:], secret) {
			copy(matchedHash[:], ih[:])
			foundHash = true
			break
		}
	}
	if !foundHash {
		return nil, [20]byte{}, nil, ErrInfoHash
	}

	localKey, peerKey := deriveCipherKeys(secret, matchedHash[:], false)

	encCipher, err := newRC4Cipher(localKey)
	if err != nil {
		return nil, [20]byte{}, nil, err
	}
	decCipher, err := newRC4Cipher(peerKey)
	if err != nil {
		return nil, [20]byte{}, nil, err
	}

	var vcDec [vcLength]byte
	copy(vcDec[:], buf[pos:pos+vcLength])
	decCipher.XORKeyStream(vcDec[:], vcDec[:])
	if vcDec != ([vcLength]byte{}) {
		return nil, [20]byte{}, nil, fmt.Errorf("mse: VC verification failed: %x", vcDec)
	}
	pos += vcLength

	decCipher.XORKeyStream(buf[pos:pos+cryptoBitfieldLength], buf[pos:pos+cryptoBitfieldLength])
	if buf[pos+3]&byte(cryptoARC4) == 0 {
		return nil, [20]byte{}, nil, fmt.Errorf("mse: no supported crypto type provided")
	}
	pos += cryptoBitfieldLength

	var padCLenBE [2]byte
	decCipher.XORKeyStream(padCLenBE[:], buf[pos:pos+2])
	padCLen := int(binary.BigEndian.Uint16(padCLenBE[:]))
	pos += 2

	if padCLen > maxPadLength {
		return nil, [20]byte{}, nil, fmt.Errorf("mse: padC too large: %d", padCLen)
	}

	need := pos + padCLen + 2
	buf, err = readUntil(conn, buf, need)
	if err != nil {
		return nil, [20]byte{}, nil, err
	}

	if padCLen > 0 {
		decCipher.XORKeyStream(buf[pos:pos+padCLen], buf[pos:pos+padCLen])
	}
	pos += padCLen

	var iaLenBE [2]byte
	decCipher.XORKeyStream(iaLenBE[:], buf[pos:pos+2])
	iaLen := int(binary.BigEndian.Uint16(iaLenBE[:]))
	pos += 2
	if iaLen > maxIALength {
		return nil, [20]byte{}, nil, fmt.Errorf("mse: IA too large: %d", iaLen)
	}

	var ia []byte
	if iaLen > 0 {
		buf, err = readUntil(conn, buf, pos+iaLen)
		if err != nil {
			return nil, [20]byte{}, nil, err
		}
		ia = make([]byte, iaLen)
		copy(ia, buf[pos:pos+iaLen])
		decCipher.XORKeyStream(ia, ia)
		pos += iaLen
	}

	var pending []byte
	if len(buf) > pos {
		pending = append([]byte(nil), buf[pos:]...)
		decCipher.XORKeyStream(pending, pending)
	}

	vcBlob := [vcLength]byte{}
	padDLen, err := randRange(maxPadLength + 1)
	if err != nil {
		return nil, [20]byte{}, nil, fmt.Errorf("mse: random padD length: %w", err)
	}

	step2Len := vcLength + cryptoBitfieldLength + 2 + padDLen
	step2Ptr := bufPool1K.Get().(*[]byte)
	step2 := (*step2Ptr)[:step2Len]
	off := 0
	copy(step2[off:], vcBlob[:])
	off += vcLength
	step2[off+3] = byte(cryptoARC4)
	off += cryptoBitfieldLength
	binary.BigEndian.PutUint16(step2[off:], uint16(padDLen))
	off += 2
	if padDLen > 0 {
		if _, err := io.ReadFull(rand.Reader, step2[off:]); err != nil {
			bufPool1K.Put(step2Ptr)
			return nil, [20]byte{}, nil, fmt.Errorf("mse: random padD: %w", err)
		}
	}

	encCipher.XORKeyStream(step2, step2)
	if len(publicKeyPad) > 0 {
		if _, err := conn.Write(publicKeyPad); err != nil {
			bufPool1K.Put(step2Ptr)
			return nil, [20]byte{}, nil, fmt.Errorf("mse: write receiver public key pad: %w", err)
		}
	}
	if _, err := conn.Write(step2); err != nil {
		bufPool1K.Put(step2Ptr)
		return nil, [20]byte{}, nil, fmt.Errorf("mse: write receiver step2: %w", err)
	}
	bufPool1K.Put(step2Ptr)

	return &Conn{Conn: conn, encrypt: encCipher, decrypt: decCipher, pending: pending}, matchedHash, ia, nil
}

func writeDHPublicKey(w io.Writer, pub *big.Int, role string) ([]byte, error) {
	padLen, err := randRange(maxPadLength + 1)
	if err != nil {
		return nil, fmt.Errorf("mse: random %s public key pad length: %w", role, err)
	}
	buf := encodeDHKey(pub)
	if padLen > 0 {
		pad := make([]byte, padLen)
		if _, err := io.ReadFull(rand.Reader, pad); err != nil {
			return nil, fmt.Errorf("mse: random %s public key pad: %w", role, err)
		}
		if _, err := w.Write(buf); err != nil {
			return nil, fmt.Errorf("mse: write %s step1: %w", role, err)
		}
		return pad, nil
	}
	if _, err := w.Write(buf); err != nil {
		return nil, fmt.Errorf("mse: write %s step1: %w", role, err)
	}
	return nil, nil
}

func readExact(r io.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	return b, nil
}

func readUntil(r io.Reader, buf []byte, need int) ([]byte, error) {
	for len(buf) < need {
		tmpPtr := bufPool512.Get().(*[]byte)
		tmp := *tmpPtr
		want := need - len(buf)
		if want > len(tmp) {
			want = len(tmp)
		}
		n, err := r.Read(tmp[:want])
		if err != nil {
			bufPool512.Put(tmpPtr)
			return nil, fmt.Errorf("mse: read: %w", err)
		}
		buf = append(buf, tmp[:n]...)
		bufPool512.Put(tmpPtr)
	}
	return buf, nil
}

func randRange(max int) (int, error) {
	if max <= 1 {
		return 0, nil
	}
	var b [8]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return 0, err
	}
	return int(binary.BigEndian.Uint64(b[:]) % uint64(max)), nil
}
