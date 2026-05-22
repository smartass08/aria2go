package conformance

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"path"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	sftpSSHMsgDisconnect      = 1
	sftpSSHMsgServiceRequest  = 5
	sftpSSHMsgServiceAccept   = 6
	sftpSSHMsgKEXInit         = 20
	sftpSSHMsgNewKeys         = 21
	sftpSSHMsgKEXDHInit       = 30
	sftpSSHMsgKEXDHReply      = 31
	sftpSSHMsgUserauthRequest = 50
	sftpSSHMsgUserauthFailure = 51
	sftpSSHMsgUserauthSuccess = 52
	sftpSSHMsgChannelOpen     = 90
	sftpSSHMsgChannelOpenConf = 91
	sftpSSHMsgChannelOpenFail = 92
	sftpSSHMsgChannelWindow   = 93
	sftpSSHMsgChannelData     = 94
	sftpSSHMsgChannelEOF      = 96
	sftpSSHMsgChannelClose    = 97
	sftpSSHMsgChannelRequest  = 98
	sftpSSHMsgChannelSuccess  = 99
	sftpSSHMsgChannelFailure  = 100

	sftpFXPInit     = 1
	sftpFXPVersion  = 2
	sftpFXPOpen     = 3
	sftpFXPClose    = 4
	sftpFXPRead     = 5
	sftpFXPFstat    = 8
	sftpFXPLstat    = 7
	sftpFXPRealpath = 16
	sftpFXPStat     = 17
	sftpFXPStatus   = 101
	sftpFXPHandle   = 102
	sftpFXPData     = 103
	sftpFXPName     = 104
	sftpFXPAttrs    = 105

	sftpFXOK          = 0
	sftpFXEOF         = 1
	sftpFXNoSuchFile  = 2
	sftpFXFailure     = 4
	sftpAttrSize      = 0x00000001
	sftpAttrPerms     = 0x00000004
	sftpAttrACModTime = 0x00000008
	sftpFixtureMTime  = 1_700_000_000
)

type sftpAuthAttempt struct {
	User     string
	Password string
	Method   string
}

type sftpFixture struct {
	ln     net.Listener
	cancel context.CancelFunc

	user string
	pass string

	hostKey     *rsa.PrivateKey
	hostKeyBlob []byte
	files       map[string][]byte

	mu          sync.Mutex
	auths       []sftpAuthAttempt
	paths       []string
	bytesServed int64
}

func startSFTPFixture(t *testing.T, files map[string][]byte) *sftpFixture {
	t.Helper()

	key, keyBlob := sftpFixtureHostKey(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("sftp listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &sftpFixture{
		ln:          ln,
		cancel:      cancel,
		user:        "sftp-user",
		pass:        "sftp-pass",
		hostKey:     key,
		hostKeyBlob: keyBlob,
		files:       make(map[string][]byte, len(files)),
	}
	for name, data := range files {
		f.files[sftpCleanPath(name)] = append([]byte(nil), data...)
	}

	go f.serve(ctx)
	t.Cleanup(f.Close)
	return f
}

func (f *sftpFixture) URL(name string) string {
	u := url.URL{
		Scheme: "sftp",
		User:   url.UserPassword(f.user, f.pass),
		Host:   f.ln.Addr().String(),
		Path:   sftpCleanPath(name),
	}
	return u.String()
}

func (f *sftpFixture) HostKeyDigest(hashType string) string {
	switch hashType {
	case "sha-1":
		sum := sha1.Sum(f.hostKeyBlob)
		return hex.EncodeToString(sum[:])
	case "md5":
		sum := md5.Sum(f.hostKeyBlob)
		return hex.EncodeToString(sum[:])
	default:
		return ""
	}
}

func TestSFTPFixtureEncryptedPacketAlignment(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 16)
	iv := bytes.Repeat([]byte{0x22}, aes.BlockSize)
	macKey := bytes.Repeat([]byte{0x33}, sha256.Size)

	for payloadLen := 0; payloadLen < 128; payloadLen++ {
		payload := bytes.Repeat([]byte{byte(payloadLen)}, payloadLen)
		encBlock, err := aes.NewCipher(key)
		if err != nil {
			t.Fatalf("aes enc: %v", err)
		}
		var buf bytes.Buffer
		if err := sftpWriteEncryptedPacket(&buf, payload, cipher.NewCTR(encBlock, iv), macKey, 7, false); err != nil {
			t.Fatalf("write len=%d: %v", payloadLen, err)
		}

		wire := buf.Bytes()
		encryptedLen := len(wire) - sha256.Size
		if encryptedLen <= 0 || encryptedLen%aes.BlockSize != 0 {
			t.Fatalf("encrypted packet len=%d for payload len=%d is not block aligned", encryptedLen, payloadLen)
		}

		decBlock, err := aes.NewCipher(key)
		if err != nil {
			t.Fatalf("aes dec: %v", err)
		}
		got, err := sftpReadEncryptedPacket(bytes.NewReader(wire), cipher.NewCTR(decBlock, iv), macKey, 7, false)
		if err != nil {
			t.Fatalf("read len=%d: %v", payloadLen, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("payload len=%d round trip mismatch", payloadLen)
		}
	}
}

func (f *sftpFixture) Close() {
	f.cancel()
	_ = f.ln.Close()
}

func (f *sftpFixture) AuthAttempts() []sftpAuthAttempt {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sftpAuthAttempt(nil), f.auths...)
}

func (f *sftpFixture) RequestedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

func (f *sftpFixture) BytesServed() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bytesServed
}

func (f *sftpFixture) recordAuth(a sftpAuthAttempt) {
	f.mu.Lock()
	f.auths = append(f.auths, a)
	f.mu.Unlock()
}

func (f *sftpFixture) recordPath(name string) {
	f.mu.Lock()
	f.paths = append(f.paths, name)
	f.mu.Unlock()
}

func (f *sftpFixture) recordBytes(n int) {
	f.mu.Lock()
	f.bytesServed += int64(n)
	f.mu.Unlock()
}

func (f *sftpFixture) serve(ctx context.Context) {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		go f.handleConn(ctx, conn)
	}
}

func (f *sftpFixture) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	clientVersion, err := sftpReadSSHVersion(conn)
	if err != nil {
		return
	}
	compatAria2go := bytes.Contains(clientVersion, []byte("aria2go"))
	serverVersion := []byte("SSH-2.0-aria2go-conformance-sftp")
	if _, err := conn.Write(append(append([]byte(nil), serverVersion...), '\r', '\n')); err != nil {
		return
	}

	var plainReadSeq, plainWriteSeq uint32
	clientKEXPacket, err := sftpReadPlainPacket(conn)
	plainReadSeq++
	if err != nil || len(clientKEXPacket) == 0 || clientKEXPacket[0] != sftpSSHMsgKEXInit {
		return
	}
	clientKEXInit := clientKEXPacket[1:]
	offer := sftpAlgorithmOffer{
		KEX:         []string{"curve25519-sha256", "curve25519-sha256@libssh.org", "diffie-hellman-group14-sha256"},
		HostKey:     []string{"ssh-rsa"},
		Cipher:      []string{"aes128-ctr"},
		MAC:         []string{"hmac-sha2-256"},
		Compression: []string{"none"},
	}
	choice, err := sftpChooseAlgorithms(clientKEXInit, offer)
	if err != nil {
		return
	}

	serverKEXInit := sftpBuildKEXInit(offer)
	if err := sftpWritePlainPacket(conn, append([]byte{sftpSSHMsgKEXInit}, serverKEXInit...)); err != nil {
		return
	}
	plainWriteSeq++
	if sftpFirstKEXGuessWrong(clientKEXInit, choice) {
		if _, err := sftpReadPlainPacket(conn); err != nil {
			return
		}
		plainReadSeq++
	}

	clientKEX, err := sftpReadPlainPacket(conn)
	plainReadSeq++
	if err != nil || len(clientKEX) == 0 || clientKEX[0] != sftpSSHMsgKEXDHInit {
		return
	}
	keyState, err := f.handleKEX(clientVersion, serverVersion, clientKEXInit, serverKEXInit, clientKEX[1:], choice, compatAria2go, conn)
	if err != nil {
		return
	}
	plainWriteSeq++

	pkt, err := sftpReadPlainPacket(conn)
	plainReadSeq++
	if err != nil || len(pkt) == 0 || pkt[0] != sftpSSHMsgNewKeys {
		return
	}
	if err := sftpWritePlainPacket(conn, []byte{sftpSSHMsgNewKeys}); err != nil {
		return
	}
	plainWriteSeq++

	var readSeq, writeSeq uint32
	if !compatAria2go {
		readSeq = plainReadSeq
		writeSeq = plainWriteSeq
	}
	pkt, err = sftpReadEncryptedPacket(conn, keyState.dec, keyState.macCS, readSeq, compatAria2go)
	readSeq++
	if err != nil || len(pkt) == 0 || pkt[0] != sftpSSHMsgServiceRequest {
		return
	}
	serviceAccept := []byte{sftpSSHMsgServiceAccept}
	serviceAccept = sftpAppendStringBytes(serviceAccept, []byte("ssh-userauth"))
	if err := sftpWriteEncryptedPacket(conn, serviceAccept, keyState.enc, keyState.macSC, writeSeq, compatAria2go); err != nil {
		return
	}
	writeSeq++

	for authTries := 0; authTries < 5; authTries++ {
		pkt, err = sftpReadEncryptedPacket(conn, keyState.dec, keyState.macCS, readSeq, compatAria2go)
		readSeq++
		if err != nil || len(pkt) == 0 || pkt[0] != sftpSSHMsgUserauthRequest {
			return
		}
		ok, attempt := f.checkUserauth(pkt)
		f.recordAuth(attempt)
		if ok {
			if err := sftpWriteEncryptedPacket(conn, []byte{sftpSSHMsgUserauthSuccess}, keyState.enc, keyState.macSC, writeSeq, compatAria2go); err != nil {
				return
			}
			writeSeq++
			break
		}
		fail := []byte{sftpSSHMsgUserauthFailure}
		fail = sftpAppendStringBytes(fail, []byte("password"))
		fail = append(fail, 0)
		if err := sftpWriteEncryptedPacket(conn, fail, keyState.enc, keyState.macSC, writeSeq, compatAria2go); err != nil {
			return
		}
		writeSeq++
		if authTries == 4 {
			return
		}
	}

	pkt, err = sftpReadEncryptedPacket(conn, keyState.dec, keyState.macCS, readSeq, compatAria2go)
	readSeq++
	if err != nil || len(pkt) == 0 || pkt[0] != sftpSSHMsgChannelOpen {
		return
	}
	clientChan, ok := sftpParseChannelOpen(pkt)
	if !ok {
		_ = sftpWriteEncryptedPacket(conn, []byte{sftpSSHMsgChannelOpenFail, 0, 0, 0, 0, 0, 0, 0, 2}, keyState.enc, keyState.macSC, writeSeq, compatAria2go)
		return
	}
	const serverChan uint32 = 1
	conf := []byte{sftpSSHMsgChannelOpenConf}
	conf = binary.BigEndian.AppendUint32(conf, clientChan)
	conf = binary.BigEndian.AppendUint32(conf, serverChan)
	conf = binary.BigEndian.AppendUint32(conf, 1<<20)
	conf = binary.BigEndian.AppendUint32(conf, 32768)
	if err := sftpWriteEncryptedPacket(conn, conf, keyState.enc, keyState.macSC, writeSeq, compatAria2go); err != nil {
		return
	}
	writeSeq++

	for {
		pkt, err = sftpReadEncryptedPacket(conn, keyState.dec, keyState.macCS, readSeq, compatAria2go)
		readSeq++
		if err != nil || len(pkt) == 0 {
			return
		}
		if pkt[0] != sftpSSHMsgChannelRequest {
			continue
		}
		request, wantReply, subsystem, ok := sftpParseChannelRequest(pkt)
		if !ok {
			return
		}
		success := request == "subsystem" && subsystem == "sftp"
		if wantReply {
			resp := []byte{sftpSSHMsgChannelFailure}
			if success {
				resp[0] = sftpSSHMsgChannelSuccess
			}
			resp = binary.BigEndian.AppendUint32(resp, clientChan)
			if err := sftpWriteEncryptedPacket(conn, resp, keyState.enc, keyState.macSC, writeSeq, compatAria2go); err != nil {
				return
			}
			writeSeq++
		}
		if success {
			break
		}
	}

	var sftpIn []byte
	for {
		pkt, err = sftpReadEncryptedPacket(conn, keyState.dec, keyState.macCS, readSeq, compatAria2go)
		readSeq++
		if err != nil || len(pkt) == 0 {
			return
		}
		switch pkt[0] {
		case sftpSSHMsgChannelEOF, sftpSSHMsgChannelClose, sftpSSHMsgDisconnect:
			return
		case sftpSSHMsgChannelWindow:
			continue
		case sftpSSHMsgChannelData:
			data, ok := sftpParseChannelData(pkt)
			if !ok {
				return
			}
			sftpIn = append(sftpIn, data...)
			for {
				if len(sftpIn) < 4 {
					break
				}
				n := int(binary.BigEndian.Uint32(sftpIn[:4])) + 4
				if n < 4 || len(sftpIn) < n {
					break
				}
				req := append([]byte(nil), sftpIn[:n]...)
				sftpIn = sftpIn[n:]
				resp := f.handleSFTPPacket(req)
				if len(resp) == 0 {
					continue
				}
				chData := []byte{sftpSSHMsgChannelData}
				chData = binary.BigEndian.AppendUint32(chData, clientChan)
				chData = sftpAppendStringBytes(chData, resp)
				if err := sftpWriteEncryptedPacket(conn, chData, keyState.enc, keyState.macSC, writeSeq, compatAria2go); err != nil {
					return
				}
				writeSeq++
			}
		}
	}
}

type sftpAlgorithmOffer struct {
	KEX         []string
	HostKey     []string
	Cipher      []string
	MAC         []string
	Compression []string
}

type sftpAlgorithmChoice struct {
	KEX     string
	HostKey string
	Cipher  string
	MAC     string
}

type sftpKeyState struct {
	enc   cipher.Stream
	dec   cipher.Stream
	macCS []byte
	macSC []byte
}

func (f *sftpFixture) handleKEX(clientVersion, serverVersion, clientKEXInit, serverKEXInit, payload []byte, choice sftpAlgorithmChoice, compat bool, w io.Writer) (sftpKeyState, error) {
	switch choice.KEX {
	case "curve25519-sha256", "curve25519-sha256@libssh.org":
		return f.handleCurveKEX(clientVersion, serverVersion, clientKEXInit, serverKEXInit, payload, choice, compat, w)
	case "diffie-hellman-group14-sha256":
		return f.handleDHKEX(clientVersion, serverVersion, clientKEXInit, serverKEXInit, payload, choice, compat, w)
	default:
		return sftpKeyState{}, fmt.Errorf("unsupported kex %q", choice.KEX)
	}
}

func (f *sftpFixture) handleCurveKEX(clientVersion, serverVersion, clientKEXInit, serverKEXInit, payload []byte, choice sftpAlgorithmChoice, compat bool, w io.Writer) (sftpKeyState, error) {
	clientPub, _, err := sftpParseBytes(payload)
	if err != nil {
		return sftpKeyState{}, err
	}
	serverPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return sftpKeyState{}, err
	}
	serverPub := serverPriv.PublicKey().Bytes()
	clientKey, err := ecdh.X25519().NewPublicKey(clientPub)
	if err != nil {
		return sftpKeyState{}, err
	}
	shared, err := serverPriv.ECDH(clientKey)
	if err != nil {
		return sftpKeyState{}, err
	}

	clientKEXHash := clientKEXInit
	serverKEXHash := serverKEXInit
	if !compat {
		clientKEXHash = append([]byte{sftpSSHMsgKEXInit}, clientKEXInit...)
		serverKEXHash = append([]byte{sftpSSHMsgKEXInit}, serverKEXInit...)
	}
	e := sftpAppendStringBytes(nil, clientPub)
	serverPubEncoded := sftpAppendStringBytes(nil, serverPub)
	kMaterial := sftpAppendStringBytes(nil, shared)
	kHash := kMaterial
	if !compat {
		kHash = sftpAppendMPInt(nil, new(big.Int).SetBytes(shared))
		kMaterial = kHash
	}
	h := sftpExchangeHash(clientVersion, serverVersion, clientKEXHash, serverKEXHash, f.hostKeyBlob, e, serverPubEncoded, kHash)
	sigBlob, err := sftpSignHostKey(f.hostKey, choice.HostKey, h)
	if err != nil {
		return sftpKeyState{}, err
	}

	reply := []byte{sftpSSHMsgKEXDHReply}
	reply = sftpAppendStringBytes(reply, f.hostKeyBlob)
	reply = sftpAppendStringBytes(reply, serverPub)
	reply = sftpAppendStringBytes(reply, sigBlob)
	if err := sftpWritePlainPacket(w, reply); err != nil {
		return sftpKeyState{}, err
	}
	return sftpDeriveKeyState(kMaterial, h, choice.Cipher)
}

func (f *sftpFixture) handleDHKEX(clientVersion, serverVersion, clientKEXInit, serverKEXInit, payload []byte, choice sftpAlgorithmChoice, compat bool, w io.Writer) (sftpKeyState, error) {
	clientPub, _, err := sftpParseMPInt(payload)
	if err != nil {
		return sftpKeyState{}, err
	}
	clientPubEncodedLen := 4 + int(binary.BigEndian.Uint32(payload[:4]))
	clientPubEncoded := append([]byte(nil), payload[:clientPubEncodedLen]...)
	priv, err := rand.Int(rand.Reader, sftpDHGroup14Prime())
	if err != nil {
		return sftpKeyState{}, err
	}
	serverPub := new(big.Int).Exp(big.NewInt(2), priv, sftpDHGroup14Prime())
	shared := new(big.Int).Exp(clientPub, priv, sftpDHGroup14Prime())

	e := clientPubEncoded
	serverPubEncoded := sftpAppendMPInt(nil, serverPub)
	kSpec := sftpAppendMPInt(nil, shared)
	clientKEXHash := clientKEXInit
	serverKEXHash := serverKEXInit
	if !compat {
		clientKEXHash = append([]byte{sftpSSHMsgKEXInit}, clientKEXInit...)
		serverKEXHash = append([]byte{sftpSSHMsgKEXInit}, serverKEXInit...)
	}
	hostKeyBlob := f.hostKeyBlob
	if choice.HostKey != "ssh-rsa" {
		hostKeyBlob = sftpEncodeRSAHostKeyWithName(f.hostKey, choice.HostKey)
	}
	h := sftpExchangeHash(clientVersion, serverVersion, clientKEXHash, serverKEXHash, hostKeyBlob, e, serverPubEncoded, kSpec)

	sigBlob, err := sftpSignHostKey(f.hostKey, choice.HostKey, h)
	if err != nil {
		return sftpKeyState{}, err
	}
	reply := []byte{sftpSSHMsgKEXDHReply}
	reply = sftpAppendStringBytes(reply, hostKeyBlob)
	reply = append(reply, serverPubEncoded...)
	reply = sftpAppendStringBytes(reply, sigBlob)
	if err := sftpWritePlainPacket(w, reply); err != nil {
		return sftpKeyState{}, err
	}

	kMaterial := kSpec
	if compat {
		kMaterial = sftpAppendStringBytes(nil, shared.Bytes())
	}
	return sftpDeriveKeyState(kMaterial, h, choice.Cipher)
}

func (f *sftpFixture) checkUserauth(pkt []byte) (bool, sftpAuthAttempt) {
	user, rest, err := sftpParseString(pkt[1:])
	if err != nil {
		return false, sftpAuthAttempt{}
	}
	_, rest, err = sftpParseString(rest)
	if err != nil {
		return false, sftpAuthAttempt{User: user}
	}
	method, rest, err := sftpParseString(rest)
	if err != nil {
		return false, sftpAuthAttempt{User: user}
	}
	attempt := sftpAuthAttempt{User: user, Method: method}
	if method != "password" {
		return false, attempt
	}
	if len(rest) < 1 {
		return false, attempt
	}
	pass, _, err := sftpParseString(rest[1:])
	if err != nil {
		return false, attempt
	}
	attempt.Password = pass
	return user == f.user && pass == f.pass, attempt
}

func (f *sftpFixture) handleSFTPPacket(pkt []byte) []byte {
	if len(pkt) < 5 {
		return nil
	}
	typ := pkt[4]
	switch typ {
	case sftpFXPInit:
		body := []byte{sftpFXPVersion}
		body = binary.BigEndian.AppendUint32(body, 3)
		return sftpFrame(body)
	case sftpFXPRealpath:
		id := binary.BigEndian.Uint32(pkt[5:9])
		name, _, err := sftpParseString(pkt[9:])
		if err != nil {
			return sftpStatus(id, sftpFXFailure, "bad path")
		}
		clean := sftpCleanPath(name)
		return sftpName(id, clean)
	case sftpFXPStat, sftpFXPLstat:
		id := binary.BigEndian.Uint32(pkt[5:9])
		name, _, err := sftpParseString(pkt[9:])
		if err != nil {
			return sftpStatus(id, sftpFXFailure, "bad path")
		}
		clean := sftpCleanPath(name)
		f.recordPath(clean)
		data, ok := f.files[clean]
		if !ok {
			return sftpStatus(id, sftpFXNoSuchFile, "No such file")
		}
		return sftpAttrs(id, int64(len(data)))
	case sftpFXPOpen:
		id := binary.BigEndian.Uint32(pkt[5:9])
		name, _, err := sftpParseString(pkt[9:])
		if err != nil {
			return sftpStatus(id, sftpFXFailure, "bad path")
		}
		clean := sftpCleanPath(name)
		f.recordPath(clean)
		if _, ok := f.files[clean]; !ok {
			return sftpStatus(id, sftpFXNoSuchFile, "No such file")
		}
		body := []byte{sftpFXPHandle}
		body = binary.BigEndian.AppendUint32(body, id)
		body = sftpAppendStringBytes(body, []byte("h:"+clean))
		return sftpFrame(body)
	case sftpFXPFstat:
		id := binary.BigEndian.Uint32(pkt[5:9])
		handle, _, err := sftpParseBytes(pkt[9:])
		if err != nil {
			return sftpStatus(id, sftpFXFailure, "bad handle")
		}
		data, ok := f.files[sftpHandlePath(handle)]
		if !ok {
			return sftpStatus(id, sftpFXFailure, "bad handle")
		}
		return sftpAttrs(id, int64(len(data)))
	case sftpFXPRead:
		id := binary.BigEndian.Uint32(pkt[5:9])
		handle, rest, err := sftpParseBytes(pkt[9:])
		if err != nil || len(rest) < 12 {
			return sftpStatus(id, sftpFXFailure, "bad read")
		}
		data, ok := f.files[sftpHandlePath(handle)]
		if !ok {
			return sftpStatus(id, sftpFXFailure, "bad handle")
		}
		offset := binary.BigEndian.Uint64(rest[:8])
		length := binary.BigEndian.Uint32(rest[8:12])
		if offset >= uint64(len(data)) {
			return sftpStatus(id, sftpFXEOF, "EOF")
		}
		end := offset + uint64(length)
		if end > uint64(len(data)) {
			end = uint64(len(data))
		}
		chunk := data[offset:end]
		f.recordBytes(len(chunk))
		body := []byte{sftpFXPData}
		body = binary.BigEndian.AppendUint32(body, id)
		body = sftpAppendStringBytes(body, chunk)
		return sftpFrame(body)
	case sftpFXPClose:
		id := binary.BigEndian.Uint32(pkt[5:9])
		return sftpStatus(id, sftpFXOK, "OK")
	default:
		return nil
	}
}

func sftpBuildKEXInit(offer sftpAlgorithmOffer) []byte {
	payload := make([]byte, 16)
	_, _ = rand.Read(payload)
	for _, names := range [][]string{
		offer.KEX,
		offer.HostKey,
		offer.Cipher,
		offer.Cipher,
		offer.MAC,
		offer.MAC,
		offer.Compression,
		offer.Compression,
		nil,
		nil,
	} {
		payload = sftpAppendNameList(payload, names)
	}
	payload = append(payload, 0)
	payload = binary.BigEndian.AppendUint32(payload, 0)
	return payload
}

func sftpChooseAlgorithms(clientKEX []byte, offer sftpAlgorithmOffer) (sftpAlgorithmChoice, error) {
	lists := sftpParseKEXNameLists(clientKEX)
	if len(lists) < 8 {
		return sftpAlgorithmChoice{}, fmt.Errorf("short kexinit")
	}
	kex, ok := sftpChooseClientFirst(lists[0], offer.KEX)
	if !ok {
		return sftpAlgorithmChoice{}, fmt.Errorf("no kex match")
	}
	hostKey, ok := sftpChooseClientFirst(lists[1], offer.HostKey)
	if !ok {
		return sftpAlgorithmChoice{}, fmt.Errorf("no host key match")
	}
	cipher, ok := sftpChooseClientFirst(lists[2], offer.Cipher)
	if !ok {
		return sftpAlgorithmChoice{}, fmt.Errorf("no cipher match")
	}
	mac, ok := sftpChooseClientFirst(lists[4], offer.MAC)
	if !ok {
		return sftpAlgorithmChoice{}, fmt.Errorf("no mac match")
	}
	if _, ok := sftpChooseClientFirst(lists[6], offer.Compression); !ok {
		return sftpAlgorithmChoice{}, fmt.Errorf("no compression match")
	}
	return sftpAlgorithmChoice{KEX: kex, HostKey: hostKey, Cipher: cipher, MAC: mac}, nil
}

func sftpParseKEXNameLists(payload []byte) [][]string {
	if len(payload) < 16 {
		return nil
	}
	payload = payload[16:]
	lists := make([][]string, 0, 10)
	for i := 0; i < 10 && len(payload) >= 4; i++ {
		n := int(binary.BigEndian.Uint32(payload[:4]))
		payload = payload[4:]
		if len(payload) < n {
			return lists
		}
		if n == 0 {
			lists = append(lists, nil)
		} else {
			lists = append(lists, strings.Split(string(payload[:n]), ","))
		}
		payload = payload[n:]
	}
	return lists
}

func sftpFirstKEXGuessWrong(payload []byte, choice sftpAlgorithmChoice) bool {
	if len(payload) < 16 {
		return false
	}
	rest := payload[16:]
	var firstKEX, firstHostKey string
	for i := 0; i < 10 && len(rest) >= 4; i++ {
		n := int(binary.BigEndian.Uint32(rest[:4]))
		rest = rest[4:]
		if len(rest) < n {
			return false
		}
		if n > 0 {
			name := string(rest[:n])
			if comma := strings.IndexByte(name, ','); comma >= 0 {
				name = name[:comma]
			}
			if i == 0 {
				firstKEX = name
			} else if i == 1 {
				firstHostKey = name
			}
		}
		rest = rest[n:]
	}
	if len(rest) < 1 || rest[0] == 0 {
		return false
	}
	return firstKEX != choice.KEX || firstHostKey != choice.HostKey
}

func sftpChooseClientFirst(client, server []string) (string, bool) {
	allowed := make(map[string]struct{}, len(server))
	for _, name := range server {
		allowed[name] = struct{}{}
	}
	for _, name := range client {
		if _, ok := allowed[name]; ok {
			return name, true
		}
	}
	return "", false
}

func sftpDeriveKeyState(kMaterial, sessionID []byte, cipherName string) (sftpKeyState, error) {
	keyLen := 16
	if cipherName == "aes256-ctr" {
		keyLen = 32
	}
	initialIVCS := sftpDeriveKey(kMaterial, sessionID, 'A', 16)
	initialIVSC := sftpDeriveKey(kMaterial, sessionID, 'B', 16)
	encKeyCS := sftpDeriveKey(kMaterial, sessionID, 'C', keyLen)
	encKeySC := sftpDeriveKey(kMaterial, sessionID, 'D', keyLen)
	macCS := sftpDeriveKey(kMaterial, sessionID, 'E', 32)
	macSC := sftpDeriveKey(kMaterial, sessionID, 'F', 32)

	clientBlock, err := aes.NewCipher(encKeyCS)
	if err != nil {
		return sftpKeyState{}, err
	}
	serverBlock, err := aes.NewCipher(encKeySC)
	if err != nil {
		return sftpKeyState{}, err
	}
	return sftpKeyState{
		dec:   cipher.NewCTR(clientBlock, initialIVCS),
		enc:   cipher.NewCTR(serverBlock, initialIVSC),
		macCS: macCS,
		macSC: macSC,
	}, nil
}

func sftpDeriveKey(kMaterial, sessionID []byte, letter byte, length int) []byte {
	h := sha256.New()
	h.Write(kMaterial)
	h.Write(sessionID)
	h.Write([]byte{letter})
	h.Write(sessionID)
	out := h.Sum(nil)
	for len(out) < length {
		h.Reset()
		h.Write(kMaterial)
		h.Write(sessionID)
		h.Write(out)
		out = append(out, h.Sum(nil)...)
	}
	return out[:length]
}

func sftpExchangeHash(clientVersion, serverVersion, clientKEXInit, serverKEXInit, hostKey, e, f, k []byte) []byte {
	h := sha256.New()
	h.Write(sftpAppendStringBytes(nil, clientVersion))
	h.Write(sftpAppendStringBytes(nil, serverVersion))
	h.Write(sftpAppendStringBytes(nil, clientKEXInit))
	h.Write(sftpAppendStringBytes(nil, serverKEXInit))
	h.Write(sftpAppendStringBytes(nil, hostKey))
	h.Write(e)
	h.Write(f)
	h.Write(k)
	return h.Sum(nil)
}

func sftpSignHostKey(key *rsa.PrivateKey, algo string, exchangeHash []byte) ([]byte, error) {
	var (
		hash crypto.Hash
		sum  []byte
	)
	switch algo {
	case "rsa-sha2-512":
		digest := sha512.Sum512(exchangeHash)
		hash = crypto.SHA512
		sum = digest[:]
	case "rsa-sha2-256":
		digest := sha256.Sum256(exchangeHash)
		hash = crypto.SHA256
		sum = digest[:]
	case "ssh-rsa":
		digest := sha1.Sum(exchangeHash)
		hash = crypto.SHA1
		sum = digest[:]
	default:
		return nil, fmt.Errorf("unsupported host key algorithm %q", algo)
	}
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, hash, sum)
	if err != nil {
		return nil, err
	}
	blob := sftpAppendStringBytes(nil, []byte(algo))
	blob = sftpAppendStringBytes(blob, sig)
	return blob, nil
}

func sftpReadSSHVersion(r io.Reader) ([]byte, error) {
	for i := 0; i < 8; i++ {
		line, err := sftpReadLine(r)
		if err != nil {
			return nil, err
		}
		line = bytes.TrimRight(line, "\r\n")
		if bytes.HasPrefix(line, []byte("SSH-2.0-")) {
			return append([]byte(nil), line...), nil
		}
	}
	return nil, fmt.Errorf("ssh version line not found")
}

func sftpReadLine(r io.Reader) ([]byte, error) {
	var out []byte
	var b [1]byte
	for len(out) <= 255 {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		out = append(out, b[0])
		if b[0] == '\n' {
			return out, nil
		}
	}
	return nil, fmt.Errorf("line too long")
}

func sftpReadPlainPacket(r io.Reader) ([]byte, error) {
	return sftpReadPacket(r, nil, nil, 0, false, false)
}

func sftpWritePlainPacket(w io.Writer, payload []byte) error {
	return sftpWritePacket(w, payload, nil, nil, 0, false, false)
}

func sftpReadEncryptedPacket(r io.Reader, dec cipher.Stream, macKey []byte, seq uint32, compat bool) ([]byte, error) {
	return sftpReadPacket(r, dec, macKey, seq, true, compat)
}

func sftpWriteEncryptedPacket(w io.Writer, payload []byte, enc cipher.Stream, macKey []byte, seq uint32, compat bool) error {
	return sftpWritePacket(w, payload, enc, macKey, seq, true, compat)
}

func sftpReadPacket(r io.Reader, dec cipher.Stream, macKey []byte, seq uint32, encrypted, compat bool) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	plainLen := lenBuf
	if encrypted && !compat {
		dec.XORKeyStream(plainLen[:], plainLen[:])
	}
	pktLen := binary.BigEndian.Uint32(plainLen[:])
	if pktLen == 0 || pktLen > 1<<20 {
		return nil, fmt.Errorf("bad packet length %d", pktLen)
	}

	rest := make([]byte, pktLen)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, err
	}
	if encrypted {
		dec.XORKeyStream(rest, rest)
	}

	packetPlain := make([]byte, 4+len(rest))
	copy(packetPlain[:4], plainLen[:])
	copy(packetPlain[4:], rest)
	if encrypted {
		var macBuf [32]byte
		if _, err := io.ReadFull(r, macBuf[:]); err != nil {
			return nil, err
		}
		if !hmac.Equal(macBuf[:], sftpMAC(seq, packetPlain, macKey)) {
			return nil, fmt.Errorf("bad mac")
		}
	}

	paddingLen := int(rest[0])
	if paddingLen < 4 || paddingLen > len(rest)-1 {
		return nil, fmt.Errorf("bad padding length %d", paddingLen)
	}
	payload := make([]byte, len(rest)-1-paddingLen)
	copy(payload, rest[1:len(rest)-paddingLen])
	return payload, nil
}

func sftpWritePacket(w io.Writer, payload []byte, enc cipher.Stream, macKey []byte, seq uint32, encrypted, compat bool) error {
	blockSize := 8
	if encrypted {
		blockSize = 16
	}
	paddingLen := 4
	for (4+1+len(payload)+paddingLen)%blockSize != 0 {
		paddingLen++
	}
	packetLen := 1 + len(payload) + paddingLen
	packet := make([]byte, 4+packetLen)
	binary.BigEndian.PutUint32(packet[:4], uint32(packetLen))
	packet[4] = byte(paddingLen)
	copy(packet[5:], payload)
	_, _ = rand.Read(packet[5+len(payload):])

	var mac []byte
	if encrypted {
		mac = sftpMAC(seq, packet, macKey)
		if compat {
			enc.XORKeyStream(packet[4:], packet[4:])
		} else {
			enc.XORKeyStream(packet, packet)
		}
	}
	if _, err := w.Write(packet); err != nil {
		return err
	}
	if len(mac) > 0 {
		_, err := w.Write(mac)
		return err
	}
	return nil
}

func sftpMAC(seq uint32, packet []byte, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	var seqBuf [4]byte
	binary.BigEndian.PutUint32(seqBuf[:], seq)
	mac.Write(seqBuf[:])
	mac.Write(packet)
	return mac.Sum(nil)
}

func sftpParseChannelOpen(pkt []byte) (uint32, bool) {
	typ, rest, err := sftpParseString(pkt[1:])
	if err != nil || typ != "session" || len(rest) < 12 {
		return 0, false
	}
	return binary.BigEndian.Uint32(rest[:4]), true
}

func sftpParseChannelRequest(pkt []byte) (request string, wantReply bool, subsystem string, ok bool) {
	if len(pkt) < 5 {
		return "", false, "", false
	}
	rest := pkt[5:]
	var err error
	request, rest, err = sftpParseString(rest)
	if err != nil || len(rest) < 1 {
		return "", false, "", false
	}
	wantReply = rest[0] != 0
	rest = rest[1:]
	if request == "subsystem" {
		subsystem, _, err = sftpParseString(rest)
		if err != nil {
			return "", false, "", false
		}
	}
	return request, wantReply, subsystem, true
}

func sftpParseChannelData(pkt []byte) ([]byte, bool) {
	if len(pkt) < 9 {
		return nil, false
	}
	data, _, err := sftpParseBytes(pkt[5:])
	if err != nil {
		return nil, false
	}
	return data, true
}

func sftpFrame(body []byte) []byte {
	out := make([]byte, 0, 4+len(body))
	out = binary.BigEndian.AppendUint32(out, uint32(len(body)))
	return append(out, body...)
}

func sftpStatus(id uint32, code uint32, msg string) []byte {
	body := []byte{sftpFXPStatus}
	body = binary.BigEndian.AppendUint32(body, id)
	body = binary.BigEndian.AppendUint32(body, code)
	body = sftpAppendStringBytes(body, []byte(msg))
	body = sftpAppendStringBytes(body, []byte("en"))
	return sftpFrame(body)
}

func sftpAttrs(id uint32, size int64) []byte {
	attrs := make([]byte, 0, 24)
	attrs = binary.BigEndian.AppendUint32(attrs, sftpAttrSize|sftpAttrPerms|sftpAttrACModTime)
	attrs = binary.BigEndian.AppendUint64(attrs, uint64(size))
	attrs = binary.BigEndian.AppendUint32(attrs, 0o100644)
	attrs = binary.BigEndian.AppendUint32(attrs, sftpFixtureMTime)
	attrs = binary.BigEndian.AppendUint32(attrs, sftpFixtureMTime)

	body := []byte{sftpFXPAttrs}
	body = binary.BigEndian.AppendUint32(body, id)
	body = append(body, attrs...)
	return sftpFrame(body)
}

func sftpName(id uint32, name string) []byte {
	attrs := make([]byte, 0, 4)
	attrs = binary.BigEndian.AppendUint32(attrs, 0)
	body := []byte{sftpFXPName}
	body = binary.BigEndian.AppendUint32(body, id)
	body = binary.BigEndian.AppendUint32(body, 1)
	body = sftpAppendStringBytes(body, []byte(name))
	body = sftpAppendStringBytes(body, []byte(name))
	body = append(body, attrs...)
	return sftpFrame(body)
}

func sftpAppendNameList(dst []byte, names []string) []byte {
	return sftpAppendStringBytes(dst, []byte(strings.Join(names, ",")))
}

func sftpAppendStringBytes(dst, s []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(s)))
	return append(dst, s...)
}

func sftpAppendMPInt(dst []byte, n *big.Int) []byte {
	if n.Sign() == 0 {
		return binary.BigEndian.AppendUint32(dst, 0)
	}
	b := n.Bytes()
	if b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return sftpAppendStringBytes(dst, b)
}

func sftpParseString(data []byte) (string, []byte, error) {
	b, rest, err := sftpParseBytes(data)
	if err != nil {
		return "", nil, err
	}
	return string(b), rest, nil
}

func sftpParseBytes(data []byte) ([]byte, []byte, error) {
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("short string")
	}
	n := int(binary.BigEndian.Uint32(data[:4]))
	if n < 0 || len(data) < 4+n {
		return nil, nil, fmt.Errorf("truncated string")
	}
	return data[4 : 4+n], data[4+n:], nil
}

func sftpParseMPInt(data []byte) (*big.Int, []byte, error) {
	b, rest, err := sftpParseBytes(data)
	if err != nil {
		return nil, nil, err
	}
	return new(big.Int).SetBytes(b), rest, nil
}

func sftpCleanPath(name string) string {
	clean := path.Clean("/" + strings.TrimPrefix(name, "/"))
	if clean == "." {
		return "/"
	}
	return clean
}

func sftpHandlePath(handle []byte) string {
	return strings.TrimPrefix(string(handle), "h:")
}

var (
	sftpFixtureHostKeyOnce sync.Once
	sftpFixtureKey         *rsa.PrivateKey
	sftpFixtureKeyBlob     []byte
	sftpFixtureKeyErr      error
)

func sftpFixtureHostKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	sftpFixtureHostKeyOnce.Do(func() {
		sftpFixtureKey, sftpFixtureKeyErr = rsa.GenerateKey(rand.Reader, 2048)
		if sftpFixtureKeyErr != nil {
			return
		}
		sftpFixtureKeyBlob = sftpEncodeRSAHostKey(sftpFixtureKey)
	})
	if sftpFixtureKeyErr != nil {
		t.Fatalf("generate sftp host key: %v", sftpFixtureKeyErr)
	}
	return sftpFixtureKey, append([]byte(nil), sftpFixtureKeyBlob...)
}

func sftpEncodeRSAHostKey(key *rsa.PrivateKey) []byte {
	return sftpEncodeRSAHostKeyWithName(key, "ssh-rsa")
}

func sftpEncodeRSAHostKeyWithName(key *rsa.PrivateKey, name string) []byte {
	var blob []byte
	blob = sftpAppendStringBytes(blob, []byte(name))
	blob = sftpAppendStringBytes(blob, sftpPositiveMPIntBytes(big.NewInt(int64(key.PublicKey.E))))
	blob = sftpAppendStringBytes(blob, sftpPositiveMPIntBytes(key.PublicKey.N))
	return blob
}

func sftpPositiveMPIntBytes(n *big.Int) []byte {
	b := n.Bytes()
	if len(b) > 0 && b[0]&0x80 != 0 {
		return append([]byte{0}, b...)
	}
	return b
}

var (
	sftpDHPrimeOnce sync.Once
	sftpDHPrime     *big.Int
)

func sftpDHGroup14Prime() *big.Int {
	sftpDHPrimeOnce.Do(func() {
		sftpDHPrime = new(big.Int)
		sftpDHPrime.SetString("FFFFFFFFFFFFFFFFC90FDAA22168C234C4CA92326FCEA833F30533F45FE6FB02867930A2D191036852EBB93B4DEC39DC76D00ACB1CD5248618BFDE8B0AAA3E31C52606E0CEAEB3B6ADD84D6C29B250182752469D077FCAEFC616FBECD09B3F7BA79506A15D2661F627D065D56D8864BE79CA645463C4FCDDCB795A20E790FD91C968B0BD663E01E19C55AA69624B324ED77CF2E87BD3E29EBC9F377A374CB84CC9DAA5354BCE1A91B863D3521DC8B275705C1A247A987C84D6FD8011FCBA1B08B83BED5D3F6EB62A860F990AB2C73232264BA68428A293ED1AC00F655DFEFA3257DC48520176627E8B", 16)
	})
	return sftpDHPrime
}
