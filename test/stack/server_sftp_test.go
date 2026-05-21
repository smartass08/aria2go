package stack

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Minimal mock constants for SSH and SFTP
const (
	sshMsgDisconnect      = 1
	sshMsgServiceRequest  = 5
	sshMsgServiceAccept   = 6
	sshMsgKEXInit         = 20
	sshMsgNewKeys         = 21
	sshMsgKEXECDHInit     = 30
	sshMsgKEXECDHReply    = 31
	sshMsgUserauthRequest = 50
	sshMsgUserauthSuccess = 52
	sshMsgChannelOpen     = 90
	sshMsgChannelOpenConf = 91
	sshMsgChannelRequest  = 98
	sshMsgChannelSuccess  = 99
	sshMsgChannelData     = 94
	sshMsgChannelEOF      = 96
	sshMsgChannelClose    = 97

	sshFxpInit    = 1
	sshFxpVersion = 2
	sshFxpOpen    = 3
	sshFxpClose   = 4
	sshFxpRead    = 5
	sshFxpStatus  = 101
	sshFxpHandle  = 102
	sshFxpData    = 103
	sshFxpName    = 104
	sshFxpAttrs   = 105
	sshFxpStat    = 17
	sshFxpLstat   = 7

	sshFxFofSize = 0x00000001
	sshFxFofPerm = 0x00000004
)

// helper functions for SSH packets
func appendSSHStringBytes(dst, str []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(str)))
	return append(dst, str...)
}

func buildServerKEXInitPayload() []byte {
	var payload []byte
	// cookie (16 bytes)
	cookie := make([]byte, 16)
	rand.Read(cookie)
	payload = append(payload, cookie...)

	// kex algorithms
	payload = appendSSHStringBytes(payload, []byte("curve25519-sha256"))
	// server host key algorithms
	payload = appendSSHStringBytes(payload, []byte("ssh-rsa"))
	// encryption algorithms client to server
	payload = appendSSHStringBytes(payload, []byte("aes128-ctr,aes256-ctr"))
	// encryption algorithms server to client
	payload = appendSSHStringBytes(payload, []byte("aes128-ctr,aes256-ctr"))
	// mac algorithms client to server
	payload = appendSSHStringBytes(payload, []byte("hmac-sha2-256"))
	// mac algorithms server to client
	payload = appendSSHStringBytes(payload, []byte("hmac-sha2-256"))
	// compression client to server
	payload = appendSSHStringBytes(payload, []byte("none"))
	// compression server to client
	payload = appendSSHStringBytes(payload, []byte("none"))
	// languages client to server
	payload = appendSSHStringBytes(payload, []byte(""))
	// languages server to client
	payload = appendSSHStringBytes(payload, []byte(""))
	// first kex packet follows (boolean)
	payload = append(payload, 0)
	// reserved (uint32)
	payload = binary.BigEndian.AppendUint32(payload, 0)

	return payload
}

func parseSSHBytes(data []byte) ([]byte, []byte, error) {
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("short bytes")
	}
	l := binary.BigEndian.Uint32(data[:4])
	if uint32(len(data)) < 4+l {
		return nil, nil, fmt.Errorf("truncated")
	}
	return data[4 : 4+l], data[4+l:], nil
}

func encodeSSHRSAKeyBlob(key *rsa.PrivateKey) []byte {
	var blob []byte
	blob = appendSSHStringBytes(blob, []byte("ssh-rsa"))
	// public exponent E
	eBytes := bigIntBytes(big.NewInt(int64(key.PublicKey.E)))
	blob = appendSSHStringBytes(blob, eBytes)
	// modulus N
	nBytes := bigIntBytes(key.PublicKey.N)
	blob = appendSSHStringBytes(blob, nBytes)
	return blob
}

func bigIntBytes(n *big.Int) []byte {
	b := n.Bytes()
	if len(b) > 0 && b[0]&0x80 != 0 {
		return append([]byte{0}, b...)
	}
	return b
}

func deriveKey(sharedSecret, sessionID []byte, char byte, length int) []byte {
	h := sha256.New()
	h.Write(appendSSHStringBytes(nil, sharedSecret))
	h.Write(sessionID)
	h.Write([]byte{char})
	h.Write(sessionID)
	key := h.Sum(nil)

	for len(key) < length {
		h.Reset()
		h.Write(appendSSHStringBytes(nil, sharedSecret))
		h.Write(sessionID)
		h.Write(key)
		key = append(key, h.Sum(nil)...)
	}
	return key[:length]
}

func computeMAC(seqNum uint32, payload []byte, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	var seqBuf [4]byte
	binary.BigEndian.PutUint32(seqBuf[:], seqNum)
	mac.Write(seqBuf[:])
	mac.Write(payload)
	return mac.Sum(nil)
}

func writeSSHPacket(w io.Writer, payload []byte, enc cipher.Stream, seq uint32, macKey []byte) error {
	blockSize := 16
	paddingLen := 4
	totalNeeded := 1 + len(payload) + paddingLen
	if rem := totalNeeded % blockSize; rem != 0 {
		paddingLen += blockSize - rem
	}
	if paddingLen < 4 {
		paddingLen = 4
	}
	pktLen := 1 + len(payload) + paddingLen

	packet := make([]byte, 4+pktLen+32)
	binary.BigEndian.PutUint32(packet[:4], uint32(pktLen))
	packet[4] = byte(paddingLen)
	copy(packet[5:], payload)
	rand.Read(packet[5+len(payload) : 5+len(payload)+paddingLen])

	writeLen := 4 + pktLen
	if len(macKey) > 0 {
		mac := computeMAC(seq, packet[:writeLen], macKey)
		copy(packet[writeLen:], mac)
		writeLen += len(mac)
	}

	if enc != nil {
		enc.XORKeyStream(packet[4:4+pktLen], packet[4:4+pktLen])
	}

	_, err := w.Write(packet[:writeLen])
	return err
}

func readSSHPacket(r io.Reader, dec cipher.Stream, seq uint32, macKey []byte) ([]byte, error) {
	var lengthBuf [4]byte
	if _, err := io.ReadFull(r, lengthBuf[:]); err != nil {
		return nil, err
	}

	pktLen := binary.BigEndian.Uint32(lengthBuf[:])
	rest := make([]byte, int(pktLen))
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, err
	}

	if dec != nil {
		dec.XORKeyStream(rest, rest)
	}

	paddingLen := int(rest[0])
	payloadLen := int(pktLen) - 1 - paddingLen
	payload := rest[1 : 1+payloadLen]

	if len(macKey) > 0 {
		var macBuf [32]byte
		if _, err := io.ReadFull(r, macBuf[:]); err != nil {
			return nil, err
		}
		// Verify MAC
		fullPkt := make([]byte, 4+int(pktLen))
		copy(fullPkt, lengthBuf[:])
		copy(fullPkt[4:], rest)
		expectedMac := computeMAC(seq, fullPkt, macKey)
		if !hmac.Equal(macBuf[:], expectedMac) {
			return nil, fmt.Errorf("mac validation failed")
		}
	}

	return payload, nil
}

func readVersionLine(r io.Reader) ([]byte, error) {
	var line []byte
	var b [1]byte
	for {
		_, err := r.Read(b[:])
		if err != nil {
			return nil, err
		}
		line = append(line, b[0])
		if b[0] == '\n' {
			break
		}
	}
	return line, nil
}

// handleSFTPClient runs the SSH state machine and then the SFTP subsystem.
func handleSFTPClient(conn net.Conn, testData []byte) {
	defer conn.Close()
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[SFTP MOCK SERVER PANIC RECOVERED]: %v\n", r)
		}
	}()

	// 1. Version exchange
	clientVerBytes, err := readVersionLine(conn)
	if err != nil {
		return
	}
	clientVer := string(clientVerBytes)
	if !strings.HasPrefix(clientVer, "SSH-2.0-") {
		return
	}

	_, err = conn.Write([]byte("SSH-2.0-aria2go-mock-sftp\r\n"))
	if err != nil {
		return
	}
	clientVerBytes = []byte(strings.TrimSpace(clientVer))

	// 2. Read KEXINIT
	clientKEXPkt, err := readSSHPacket(conn, nil, 0, nil)
	if err != nil || clientKEXPkt[0] != sshMsgKEXInit {
		return
	}
	clientKEXPayload := clientKEXPkt[1:]

	// 3. Send KEXINIT
	serverKEXInit := buildServerKEXInitPayload()
	err = writeSSHPacket(conn, append([]byte{sshMsgKEXInit}, serverKEXInit...), nil, 0, nil)
	if err != nil {
		return
	}

	// 4. Read KEX init (Client DH/ECDH key)
	clientKEX, err := readSSHPacket(conn, nil, 1, nil)
	if err != nil || clientKEX[0] != sshMsgKEXECDHInit {
		return
	}

	pubBytes, _, err := parseSSHBytes(clientKEX[1:])
	if err != nil || len(pubBytes) != 32 {
		return
	}

	// ECDH Key exchange
	serverPrivKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return
	}
	serverPub := serverPrivKey.PublicKey().Bytes()

	clientPubKey, err := ecdh.X25519().NewPublicKey(pubBytes)
	if err != nil {
		return
	}
	sharedSecret, err := serverPrivKey.ECDH(clientPubKey)
	if err != nil {
		return
	}

	// Generate RSA host key for signature
	hostKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return
	}
	hostKeyBlob := encodeSSHRSAKeyBlob(hostKey)

	// Compute exchange hash
	clientVerBytes = []byte(strings.TrimSpace(clientVer))
	serverVerBytes := []byte("SSH-2.0-aria2go-mock-sftp")

	h := sha256.New()
	h.Write(appendSSHStringBytes(nil, clientVerBytes))
	h.Write(appendSSHStringBytes(nil, serverVerBytes))
	h.Write(appendSSHStringBytes(nil, clientKEXPayload))
	h.Write(appendSSHStringBytes(nil, serverKEXInit))
	h.Write(appendSSHStringBytes(nil, hostKeyBlob))
	h.Write(appendSSHStringBytes(nil, pubBytes))
	h.Write(appendSSHStringBytes(nil, serverPub))
	h.Write(appendSSHStringBytes(nil, sharedSecret))
	exchangeHash := h.Sum(nil)

	// Build KEX reply
	reply := []byte{sshMsgKEXECDHReply}
	reply = appendSSHStringBytes(reply, hostKeyBlob)
	reply = appendSSHStringBytes(reply, serverPub)

	sig, err := rsa.SignPKCS1v15(rand.Reader, hostKey, 0, exchangeHash)
	if err != nil {
		return
	}
	sigBlob := appendSSHStringBytes(nil, []byte("ssh-rsa"))
	sigBlob = appendSSHStringBytes(sigBlob, sig)
	reply = appendSSHStringBytes(reply, sigBlob)

	err = writeSSHPacket(conn, reply, nil, 2, nil)
	if err != nil {
		return
	}

	// 5. Read NEWKEYS
	pkt, err := readSSHPacket(conn, nil, 3, nil)
	if err != nil || pkt[0] != sshMsgNewKeys {
		return
	}

	// 6. Send NEWKEYS
	err = writeSSHPacket(conn, []byte{sshMsgNewKeys}, nil, 4, nil)
	if err != nil {
		return
	}

	// 7. Initialize encryption keys
	sessionID := exchangeHash
	encKeyCS := deriveKey(sharedSecret, sessionID, 'C', 16)
	initialIVCS := deriveKey(sharedSecret, sessionID, 'A', 16)
	integKeyCS := deriveKey(sharedSecret, sessionID, 'E', 32)

	encKeySC := deriveKey(sharedSecret, sessionID, 'D', 16)
	initialIVSC := deriveKey(sharedSecret, sessionID, 'B', 16)
	integKeySC := deriveKey(sharedSecret, sessionID, 'F', 32)

	blockCS, _ := aes.NewCipher(encKeyCS)
	dec := cipher.NewCTR(blockCS, initialIVCS)

	blockSC, _ := aes.NewCipher(encKeySC)
	enc := cipher.NewCTR(blockSC, initialIVSC)

	// Keep track of sequence numbers
	var readSeq uint32 = 0
	var writeSeq uint32 = 0

	// 8. Service request
	pkt, err = readSSHPacket(conn, dec, readSeq, integKeyCS)
	readSeq++
	if err != nil || pkt[0] != sshMsgServiceRequest {
		return
	}

	err = writeSSHPacket(conn, []byte{sshMsgServiceAccept}, enc, writeSeq, integKeySC)
	writeSeq++
	if err != nil {
		return
	}

	// 9. User Authentication
	pkt, err = readSSHPacket(conn, dec, readSeq, integKeyCS)
	readSeq++
	if err != nil || pkt[0] != sshMsgUserauthRequest {
		return
	}

	// Always accept user auth
	err = writeSSHPacket(conn, []byte{sshMsgUserauthSuccess}, enc, writeSeq, integKeySC)
	writeSeq++
	if err != nil {
		return
	}

	// 10. Channel Open
	pkt, err = readSSHPacket(conn, dec, readSeq, integKeyCS)
	readSeq++
	if err != nil || pkt[0] != sshMsgChannelOpen {
		return
	}

	// Channel Open Confirmation
	conf := []byte{sshMsgChannelOpenConf}
	conf = binary.BigEndian.AppendUint32(conf, 0)       // client channel ID
	conf = binary.BigEndian.AppendUint32(conf, 1)       // server channel ID
	conf = binary.BigEndian.AppendUint32(conf, 1048576) // window size
	conf = binary.BigEndian.AppendUint32(conf, 32768)   // max packet
	err = writeSSHPacket(conn, conf, enc, writeSeq, integKeySC)
	writeSeq++
	if err != nil {
		return
	}

	// 11. Channel Request (Subsystem "sftp")
	pkt, err = readSSHPacket(conn, dec, readSeq, integKeyCS)
	readSeq++
	if err != nil || pkt[0] != sshMsgChannelRequest {
		return
	}

	err = writeSSHPacket(conn, []byte{sshMsgChannelSuccess, 0, 0, 0, 0}, enc, writeSeq, integKeySC)
	writeSeq++
	if err != nil {
		return
	}

	// 12. Handle SFTP Subsystem Packets
	// Inside the channel, SFTP requests are encapsulated in SSH_MSG_CHANNEL_DATA (94)
	for {
		pkt, err = readSSHPacket(conn, dec, readSeq, integKeyCS)
		readSeq++
		if err != nil {
			return
		}

		if pkt[0] == sshMsgChannelEOF || pkt[0] == sshMsgChannelClose {
			return
		}

		if pkt[0] != sshMsgChannelData {
			continue
		}

		// Read data payload
		dataLen := binary.BigEndian.Uint32(pkt[5:9])
		sftpPayload := pkt[9 : 9+dataLen]

		sftpResponse := handleSFTPPacket(sftpPayload, testData)
		if len(sftpResponse) > 0 {
			// Encapsulate SFTP response into SSH_MSG_CHANNEL_DATA
			chData := []byte{sshMsgChannelData}
			chData = binary.BigEndian.AppendUint32(chData, 0) // client channel
			chData = appendSSHStringBytes(chData, sftpResponse)

			err = writeSSHPacket(conn, chData, enc, writeSeq, integKeySC)
			writeSeq++
			if err != nil {
				return
			}
		}
	}
}

func handleSFTPPacket(pkt []byte, testData []byte) []byte {
	if len(pkt) < 5 {
		return nil
	}
	typ := pkt[4]

	switch typ {
	case sshFxpInit:
		// Return SFTP Version 3
		var res []byte
		res = binary.BigEndian.AppendUint32(res, 5) // length (1 byte type + 4 bytes version)
		res = append(res, sshFxpVersion)
		res = binary.BigEndian.AppendUint32(res, 3) // version 3
		return res
	case sshFxpStat, sshFxpLstat:
		id := binary.BigEndian.Uint32(pkt[5:9])
		// Return SFTP Attribute containing size
		var res, attrs []byte
		attrs = binary.BigEndian.AppendUint32(attrs, sshFxFofSize)
		attrs = binary.BigEndian.AppendUint64(attrs, uint64(len(testData)))

		res = append(res, sshFxpAttrs)
		res = binary.BigEndian.AppendUint32(res, id)
		res = append(res, attrs...)

		var outer []byte
		outer = binary.BigEndian.AppendUint32(outer, uint32(len(res)))
		return append(outer, res...)
	case sshFxpOpen:
		id := binary.BigEndian.Uint32(pkt[5:9])
		// Return Handle "h1"
		var res []byte
		res = append(res, sshFxpHandle)
		res = binary.BigEndian.AppendUint32(res, id)
		res = appendSSHStringBytes(res, []byte("h1"))

		var outer []byte
		outer = binary.BigEndian.AppendUint32(outer, uint32(len(res)))
		return append(outer, res...)
	case sshFxpRead:
		id := binary.BigEndian.Uint32(pkt[5:9])
		handle, rest, _ := parseSSHBytes(pkt[9:])
		if string(handle) != "h1" {
			return nil
		}
		offset := binary.BigEndian.Uint64(rest[:8])
		length := binary.BigEndian.Uint32(rest[8:12])

		var res []byte
		if offset >= uint64(len(testData)) {
			// Return EOF Status
			res = append(res, sshFxpStatus)
			res = binary.BigEndian.AppendUint32(res, id)
			res = binary.BigEndian.AppendUint32(res, 1) // SSH_FX_EOF
			res = appendSSHStringBytes(res, []byte("EOF"))
			res = appendSSHStringBytes(res, []byte("en"))
		} else {
			end := offset + uint64(length)
			if end > uint64(len(testData)) {
				end = uint64(len(testData))
			}
			res = append(res, sshFxpData)
			res = binary.BigEndian.AppendUint32(res, id)
			res = appendSSHStringBytes(res, testData[offset:end])
		}

		var outer []byte
		outer = binary.BigEndian.AppendUint32(outer, uint32(len(res)))
		return append(outer, res...)
	case sshFxpClose:
		id := binary.BigEndian.Uint32(pkt[5:9])
		// Return OK Status
		var res []byte
		res = append(res, sshFxpStatus)
		res = binary.BigEndian.AppendUint32(res, id)
		res = binary.BigEndian.AppendUint32(res, 0) // SSH_FX_OK
		res = appendSSHStringBytes(res, []byte("OK"))
		res = appendSSHStringBytes(res, []byte("en"))

		var outer []byte
		outer = binary.BigEndian.AppendUint32(outer, uint32(len(res)))
		return append(outer, res...)
	}
	return nil
}

func startMockSFTPServer(t *testing.T, testData []byte) (int, context.CancelFunc) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("sftp listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			go handleSFTPClient(conn, testData)
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port, func() {
		cancel()
		ln.Close()
	}
}

func TestSFTP_Download_Stress(t *testing.T) {
	bin := findAria2goBinary(t)

	testPayload := make([]byte, 1024*512) // 512 KB of pattern payload
	for i := range testPayload {
		testPayload[i] = byte((i*31 + 47) % 256)
	}

	port, cancel := startMockSFTPServer(t, testPayload)
	defer cancel()

	concurrencies := []int{1, 5, 10}
	for _, concat := range concurrencies {
		t.Run(fmt.Sprintf("Concurrency_%d", concat), func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "aria2go-sftp-stress-*")
			if err != nil {
				t.Fatalf("tempdir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			var wg sync.WaitGroup
			errs := make(chan error, concat)

			startTime := time.Now()
			for i := 0; i < concat; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					outFile := filepath.Join(tempDir, fmt.Sprintf("sftp_file_%d.bin", idx))
					cmd := exec.Command(bin,
						fmt.Sprintf("sftp://user:pass@127.0.0.1:%d/remote_file.bin", port),
						"-d", tempDir,
						"-o", fmt.Sprintf("sftp_file_%d.bin", idx),
						"--quiet=true",
					)
					var stderr bytes.Buffer
					cmd.Stderr = &stderr
					if err := cmd.Run(); err != nil {
						errs <- fmt.Errorf("sftp download %d failed: %v (stderr: %s)", idx, err, stderr.String())
						return
					}

					// Validate content
					downloadedPath := outFile
					if _, err := os.Stat(downloadedPath); os.IsNotExist(err) {
						downloadedPath = filepath.Join(tempDir, "remote_file.bin")
					}
					downloaded, err := os.ReadFile(downloadedPath)
					if err != nil {
						errs <- fmt.Errorf("read file %d failed: %v", idx, err)
						return
					}
					if !bytes.Equal(downloaded, testPayload) {
						errs <- fmt.Errorf("mismatch on file %d", idx)
						return
					}
				}(i)
			}
			wg.Wait()
			close(errs)

			elapsed := time.Since(startTime)
			t.Logf("SFTP downloaded %d files concurrently in %v", concat, elapsed)

			for err := range errs {
				t.Errorf("Error occurred: %v", err)
			}
		})
	}
}
