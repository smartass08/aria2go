package transport

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/smartass08/aria2go/internal/core"
)

const (
	versionPrefix = "SSH-2.0-"
	maxVersionLen = 255
)

var sshVersionBytes = []byte("SSH-2.0-aria2go\r\n")

const (
	sshMsgDisconnect     = 1
	sshMsgIgnore         = 2
	sshMsgUnimplemented  = 3
	sshMsgDebug          = 4
	sshMsgServiceRequest = 5
	sshMsgServiceAccept  = 6
	sshMsgKEXInit        = 20
	sshMsgNewKeys        = 21
	sshMsgKEXDHInit      = 30
	sshMsgKEXDHReply     = 31
	sshMsgKEXECDHInit    = 30
	sshMsgKEXECDHReply   = 31
)

const maxPacketSize = 262144

var (
	ErrProtocolError   = core.NewError(core.ExitNetworkProblem, "SSH protocol error")
	ErrKeyExchange     = core.NewError(core.ExitNetworkProblem, "SSH key exchange failed")
	ErrVersionMismatch = core.NewError(core.ExitNetworkProblem, "SSH version mismatch")
	ErrNoCommonAlgo    = core.NewError(core.ExitNetworkProblem, "SSH no common algorithm")
)

var pktBufPool atomic.Pointer[sync.Pool]

func init() {
	pktBufPool.Store(newPktBufPool())
}

func newPktBufPool() *sync.Pool {
	return &sync.Pool{
		New: func() any {
			buf := make([]byte, maxPacketSize+4+32)
			return buf
		},
	}
}

func getPktBuf(size int) []byte {
	buf := pktBufPool.Load().Get().([]byte)
	if cap(buf) < size {
		return make([]byte, size)
	}
	return buf[:size]
}

func putPktBuf(buf []byte) {
	if cap(buf) >= maxPacketSize+4+32 {
		pktBufPool.Load().Put(buf)
	}
}

type Conn struct {
	c net.Conn

	sessionID []byte

	readSeqNum  uint32
	writeSeqNum uint32

	clientVersion []byte
	serverVersion []byte
	clientKEXInit []byte
	serverKEXInit []byte

	kexAlgo     string
	hostKeyAlgo string
	cipherCS    string
	cipherSC    string
	macCS       string
	macSC       string
	compCS      string
	compSC      string
	hostKey     []byte

	encDirection, decDirection encryptor
	macEnc, macDec             *hmacState

	initialIVCS []byte
	initialIVSC []byte
	encKeyCS    []byte
	encKeySC    []byte
	integKeyCS  []byte
	integKeySC  []byte

	sharedSecret []byte
}

type encryptor interface {
	XORKeyStream(dst, src []byte)
}

type hmacState struct {
	mac hash.Hash
}

func newHMACState(key []byte) *hmacState {
	return &hmacState{mac: hmac.New(sha256.New, key)}
}

type ClientConfig struct {
	KEXAlgorithms     []string
	HostKeyAlgorithms []string
	Ciphers           []string
	MACs              []string
	Compression       []string
}

func (c *Conn) Close() error {
	return c.c.Close()
}

func (c *Conn) Send(payload []byte) error {
	return c.writeEncryptedPacket(payload)
}

func (c *Conn) Receive() ([]byte, error) {
	return c.readEncryptedPacket()
}

func (c *Conn) SessionID() []byte {
	return c.sessionID
}

// HostKey returns the raw host key blob received during key exchange.
func (c *Conn) HostKey() []byte {
	return c.hostKey
}

// HostKeyFingerprint returns the fingerprint of the server's host key in the
// format specified by hashType. Supported values:
//
//	"md5"    — hex-encoded MD5 hash with colon separators (e.g. 7a:b0:4e:...)
//	"sha-1"  — base64-encoded SHA-1 hash
//	"sha-256"— base64-encoded SHA-256 hash
//
// Returns an empty string if hashType is unrecognized or the host key is not
// available.
func (c *Conn) HostKeyFingerprint(hashType string) string {
	if len(c.hostKey) == 0 {
		return ""
	}
	switch hashType {
	case "md5":
		h := md5.Sum(c.hostKey)
		var parts []string
		for _, b := range h {
			parts = append(parts, fmt.Sprintf("%02x", b))
		}
		return strings.Join(parts, ":")
	case "sha-1":
		h := sha1.Sum(c.hostKey)
		return base64.StdEncoding.EncodeToString(h[:])
	case "sha-256":
		h := sha256.Sum256(c.hostKey)
		return base64.StdEncoding.EncodeToString(h[:])
	default:
		return ""
	}
}

func ClientHandshake(c net.Conn, cfg ClientConfig) (*Conn, []byte, error) {
	conn := &Conn{c: c}
	if err := conn.exchangeVersions(); err != nil {
		return nil, nil, fmt.Errorf("transport: version exchange: %w", err)
	}
	if err := conn.exchangeKEXInit(cfg); err != nil {
		return nil, nil, fmt.Errorf("transport: kexinit: %w", err)
	}
	if err := conn.negotiateAlgorithms(cfg); err != nil {
		return nil, nil, fmt.Errorf("transport: algorithm negotiation: %w", err)
	}
	if err := conn.performKEX(); err != nil {
		return nil, nil, fmt.Errorf("transport: key exchange: %w", err)
	}
	h := conn.sessionID
	if err := conn.deriveKeys(); err != nil {
		return nil, nil, fmt.Errorf("transport: key derivation: %w", err)
	}
	if err := conn.exchangeNewKeys(); err != nil {
		return nil, nil, fmt.Errorf("transport: newkeys: %w", err)
	}
	if err := conn.installKeys(); err != nil {
		return nil, nil, fmt.Errorf("transport: install keys: %w", err)
	}
	if err := conn.requestService("ssh-userauth"); err != nil {
		return nil, nil, fmt.Errorf("transport: service request: %w", err)
	}
	return conn, h, nil
}

func (c *Conn) exchangeVersions() error {
	if _, err := c.c.Write(sshVersionBytes); err != nil {
		return fmt.Errorf("write version: %w", err)
	}
	c.clientVersion = []byte(sshVersionBytes[:len(sshVersionBytes)-2])

	buf := make([]byte, maxVersionLen+1)
	n := 0
	for n < maxVersionLen {
		var b [1]byte
		if _, err := io.ReadFull(c.c, b[:]); err != nil {
			return fmt.Errorf("read version: %w", err)
		}
		buf[n] = b[0]
		n++
		if buf[n-1] == '\n' {
			break
		}
	}
	if n == 0 {
		return fmt.Errorf("empty version string: %w", ErrVersionMismatch)
	}
	line := buf[:n]
	if len(line) < 2 || line[len(line)-2] != '\r' || line[len(line)-1] != '\n' {
		return fmt.Errorf("version line without CRLF: %w", ErrVersionMismatch)
	}

	ver := line[:len(line)-2]

	if len(ver) < len(versionPrefix) || string(ver[:len(versionPrefix)]) != versionPrefix {
		return fmt.Errorf("bad version prefix %q: %w", string(ver), ErrVersionMismatch)
	}

	c.serverVersion = ver
	return nil
}

func (c *Conn) exchangeKEXInit(cfg ClientConfig) error {
	clientInit := c.buildKEXInit(cfg)
	c.clientKEXInit = clientInit[1:] // payload without message type byte

	err := c.writePlainPacket(clientInit)
	if err != nil {
		return fmt.Errorf("write kexinit: %w", err)
	}

	serverPayload, err := c.readPlainPacket()
	if err != nil {
		return fmt.Errorf("read server kexinit: %w", err)
	}

	if len(serverPayload) == 0 || serverPayload[0] != sshMsgKEXInit {
		return fmt.Errorf("expected KEXINIT(20), got %d: %w", serverPayload[0], ErrProtocolError)
	}

	c.serverKEXInit = serverPayload[1:]
	return nil
}

func (c *Conn) buildKEXInit(cfg ClientConfig) []byte {
	payload := []byte{sshMsgKEXInit}
	// 16 bytes of random cookie
	cookie := make([]byte, 16)
	_, _ = io.ReadFull(randSource(), cookie)
	payload = append(payload, cookie...)

	nameLists := [][]string{
		cfg.KEXAlgorithms,
		cfg.HostKeyAlgorithms,
		cfg.Ciphers,
		cfg.Ciphers,
		cfg.MACs,
		cfg.MACs,
		cfg.Compression,
		cfg.Compression,
		{},
		{},
	}

	for _, nl := range nameLists {
		payload = appendNameListBytes(payload, nl)
	}

	payload = append(payload, 0, 0, 0, 0, 0)
	return payload
}

func appendNameListBytes(payload []byte, names []string) []byte {
	var nl nameList
	for _, n := range names {
		nl = append(nl, n)
	}
	b := nl.Bytes()
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(b)))
	payload = append(payload, b...)
	return payload
}

func (c *Conn) negotiateAlgorithms(cfg ClientConfig) error {
	serverKEX := parseNameList(c.serverKEXInit[16:])

	kex, err := agreeFirst(serverKEX[0], cfg.KEXAlgorithms)
	if err != nil {
		return fmt.Errorf("kex algorithms: %w", err)
	}
	c.kexAlgo = kex

	hostKey, err := agreeFirst(serverKEX[1], cfg.HostKeyAlgorithms)
	if err != nil {
		return fmt.Errorf("host key algorithms: %w", err)
	}
	c.hostKeyAlgo = hostKey

	cipherAlgosCS := serverKEX[2]
	cipherAlgosSC := serverKEX[3]
	macAlgosCS := serverKEX[4]
	macAlgosSC := serverKEX[5]
	compAlgosCS := serverKEX[6]
	compAlgosSC := serverKEX[7]

	cipherCS, err := agreeFirst(cipherAlgosCS, cfg.Ciphers)
	if err != nil {
		return fmt.Errorf("encryption cs: %w", err)
	}
	c.cipherCS = cipherCS

	cipherSC, err := agreeFirst(cipherAlgosSC, cfg.Ciphers)
	if err != nil {
		return fmt.Errorf("encryption sc: %w", err)
	}
	c.cipherSC = cipherSC

	macCS, err := agreeFirst(macAlgosCS, cfg.MACs)
	if err != nil {
		return fmt.Errorf("mac cs: %w", err)
	}
	c.macCS = macCS

	macSC, err := agreeFirst(macAlgosSC, cfg.MACs)
	if err != nil {
		return fmt.Errorf("mac sc: %w", err)
	}
	c.macSC = macSC

	compCS, err := agreeFirst(compAlgosCS, cfg.Compression)
	if err != nil {
		return fmt.Errorf("compression cs: %w", err)
	}
	c.compCS = compCS

	compSC, err := agreeFirst(compAlgosSC, cfg.Compression)
	if err != nil {
		return fmt.Errorf("compression sc: %w", err)
	}
	c.compSC = compSC

	return nil
}

func agreeFirst(serverList, clientPref []string) (string, error) {
	serverSet := make(map[string]bool, len(serverList))
	for _, a := range serverList {
		serverSet[a] = true
	}
	for _, c := range clientPref {
		if serverSet[c] {
			return c, nil
		}
	}
	return "", fmt.Errorf("no match: server=%v client=%v: %w", serverList, clientPref, ErrNoCommonAlgo)
}

func (c *Conn) performKEX() error {
	switch c.kexAlgo {
	case "curve25519-sha256", "curve25519-sha256@libssh.org":
		return c.performKEXCurve25519()
	case "diffie-hellman-group14-sha256":
		return c.performKEXDH14()
	default:
		return fmt.Errorf("unsupported kex algorithm %q: %w", c.kexAlgo, ErrKeyExchange)
	}
}

func (c *Conn) exchangeNewKeys() error {
	err := c.writePlainPacket([]byte{sshMsgNewKeys})
	if err != nil {
		return fmt.Errorf("write newkeys: %w", err)
	}

	pkt, err := c.readPlainPacket()
	if err != nil {
		return fmt.Errorf("read server newkeys: %w", err)
	}
	if len(pkt) == 0 || pkt[0] != sshMsgNewKeys {
		return fmt.Errorf("expected NEWKEYS(21), got %v: %w", pkt, ErrProtocolError)
	}

	c.readSeqNum = 0
	c.writeSeqNum = 0
	return nil
}

func (c *Conn) installKeys() error {
	enc, err := newCTRCipher(c.cipherCS, c.encKeyCS, c.initialIVCS)
	if err != nil {
		return fmt.Errorf("encrypt cipher: %w", err)
	}
	c.encDirection = enc

	dec, err := newCTRCipher(c.cipherSC, c.encKeySC, c.initialIVSC)
	if err != nil {
		return fmt.Errorf("decrypt cipher: %w", err)
	}
	c.decDirection = dec

	if c.macCS == "hmac-sha2-256" {
		c.macEnc = newHMACState(c.integKeyCS)
	}
	if c.macSC == "hmac-sha2-256" {
		c.macDec = newHMACState(c.integKeySC)
	}

	return nil
}

func (c *Conn) requestService(name string) error {
	payload := []byte{sshMsgServiceRequest}
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(name)))
	payload = append(payload, []byte(name)...)

	err := c.writeEncryptedPacket(payload)
	if err != nil {
		return err
	}

	pkt, err := c.readEncryptedPacket()
	if err != nil {
		return fmt.Errorf("read service accept: %w", err)
	}
	if len(pkt) == 0 || pkt[0] != sshMsgServiceAccept {
		return fmt.Errorf("expected SERVICE_ACCEPT(6), got %d: %w", pkt[0], ErrProtocolError)
	}
	return nil
}

func (c *Conn) writePlainPacket(payload []byte) error {
	return writeSSHPacket(c.c, payload, nil, 0)
}

func (c *Conn) readPlainPacket() ([]byte, error) {
	return readSSHPacket(c.c, nil, 0)
}

func (c *Conn) writeEncryptedPacket(payload []byte) error {
	seq := c.writeSeqNum
	c.writeSeqNum++
	return writeSSHPacket(c.c, payload, c.encDirection, seq, c.macEnc)
}

func (c *Conn) readEncryptedPacket() ([]byte, error) {
	seq := c.readSeqNum
	c.readSeqNum++
	pkt, err := readSSHPacket(c.c, c.decDirection, seq, c.macDec)
	if err != nil {
		return nil, err
	}
	return pkt, nil
}

func writeSSHPacket(w io.Writer, payload []byte, enc encryptor, seq uint32, macs ...*hmacState) error {
	blockSize := 16
	if enc == nil {
		blockSize = 8
	}

	paddingLen := 4
	totalNeeded := 1 + len(payload) + paddingLen
	if rem := totalNeeded % blockSize; rem != 0 {
		paddingLen += blockSize - rem
	}
	if paddingLen < 4 {
		paddingLen = 4
	}
	if paddingLen > 255 {
		return fmt.Errorf("padding too large: %d", paddingLen)
	}

	pktLen := 1 + len(payload) + paddingLen

	packet := getPktBuf(4 + pktLen + 32)
	binary.BigEndian.PutUint32(packet[:4], uint32(pktLen))
	packet[4] = byte(paddingLen)
	copy(packet[5:], payload)
	_, _ = io.ReadFull(randSource(), packet[5+len(payload):])

	writeLen := 4 + pktLen
	if len(macs) > 0 && macs[0] != nil {
		mac := computeMAC(seq, packet[:writeLen], macs[0])
		copy(packet[writeLen:], mac)
		writeLen += len(mac)
	}

	if enc != nil {
		enc.XORKeyStream(packet[4:4+pktLen], packet[4:4+pktLen])
	}

	_, err := w.Write(packet[:writeLen])
	putPktBuf(packet)
	return err
}

func readSSHPacket(r io.Reader, dec encryptor, seq uint32, macs ...*hmacState) ([]byte, error) {
	var lengthBuf [4]byte
	if _, err := io.ReadFull(r, lengthBuf[:]); err != nil {
		return nil, fmt.Errorf("read length: %w", err)
	}

	pktLen := binary.BigEndian.Uint32(lengthBuf[:])
	if pktLen > maxPacketSize {
		return nil, fmt.Errorf("packet too large: %d: %w", pktLen, ErrProtocolError)
	}

	hasMAC := len(macs) > 0 && macs[0] != nil
	macLen := 0
	if hasMAC {
		macLen = 32
	}

	rest := getPktBuf(int(pktLen) + macLen)
	defer putPktBuf(rest)
	if _, err := io.ReadFull(r, rest[:int(pktLen)+macLen]); err != nil {
		return nil, fmt.Errorf("read packet body: %w", err)
	}

	if dec != nil {
		dec.XORKeyStream(rest[:pktLen], rest[:pktLen])
	}

	if hasMAC {
		plaintext := getPktBuf(4 + int(pktLen))
		copy(plaintext, lengthBuf[:])
		copy(plaintext[4:], rest[:pktLen])
		macExpected := computeMAC(seq, plaintext[:4+int(pktLen)], macs[0])
		putPktBuf(plaintext)
		if !hmacEqual(rest[pktLen:pktLen+uint32(macLen)], macExpected) {
			return nil, fmt.Errorf("bad mac: %w", ErrProtocolError)
		}
	}

	paddingLen := int(rest[0])
	if paddingLen < 4 || paddingLen > int(pktLen)-1 {
		return nil, fmt.Errorf("bad padding length %d: %w", paddingLen, ErrProtocolError)
	}

	payloadLen := int(pktLen) - 1 - paddingLen
	payload := make([]byte, payloadLen)
	copy(payload, rest[1:1+payloadLen])

	return payload, nil
}

var (
	errBadKeyLen = errors.New("bad key length for cipher")
)

func newCTRCipher(algo string, key, iv []byte) (encryptor, error) {
	switch algo {
	case "aes128-ctr":
		if len(key) != 16 {
			return nil, fmt.Errorf("aes128-ctr wants 16-byte key, got %d: %w", len(key), errBadKeyLen)
		}
		return newAESCTR(key, iv)
	case "aes256-ctr":
		if len(key) != 32 {
			return nil, fmt.Errorf("aes256-ctr wants 32-byte key, got %d: %w", len(key), errBadKeyLen)
		}
		return newAESCTR(key, iv)
	default:
		return nil, fmt.Errorf("unknown cipher %q", algo)
	}
}
