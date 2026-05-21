package transport

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
)

type nameList []string

func parseNameList(data []byte) []nameList {
	if len(data) < 4 {
		return nil
	}
	fields := make([]nameList, 10)

	pos := 0
	for i := 0; i < 10 && pos+4 <= len(data); i++ {
		l := binary.BigEndian.Uint32(data[pos:])
		pos += 4
		if l == 0 {
			continue
		}
		if int(l) > len(data)-pos {
			break
		}
		s := string(data[pos : pos+int(l)])
		pos += int(l)
		if s == "" {
			continue
		}
		fields[i] = splitComma(s)
	}
	return fields
}

func (nl nameList) Bytes() []byte {
	s := ""
	for i, n := range nl {
		if i > 0 {
			s += ","
		}
		s += n
	}
	return []byte(s)
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func (c *Conn) deriveKeys() error {
	switch c.kexAlgo {
	case "curve25519-sha256", "curve25519-sha256@libssh.org", "diffie-hellman-group14-sha256":
		return c.deriveKeysSHA256()
	default:
		return fmt.Errorf("unsupported kex for key derivation %q: %w", c.kexAlgo, ErrKeyExchange)
	}
}

func (c *Conn) deriveKeysSHA256() error {
	algo := c.cipherCS
	keyLen := 16
	if algo == "aes256-ctr" {
		keyLen = 32
	}
	ivLen := 16
	macKeyLen := 32

	c.initialIVCS = deriveKey(c.sharedSecret, c.sessionID, 'A', ivLen)
	c.initialIVSC = deriveKey(c.sharedSecret, c.sessionID, 'B', ivLen)
	c.encKeyCS = deriveKey(c.sharedSecret, c.sessionID, 'C', keyLen)
	c.encKeySC = deriveKey(c.sharedSecret, c.sessionID, 'D', keyLen)
	c.integKeyCS = deriveKey(c.sharedSecret, c.sessionID, 'E', macKeyLen)
	c.integKeySC = deriveKey(c.sharedSecret, c.sessionID, 'F', macKeyLen)

	return nil
}

func deriveKey(sharedSecret, sessionID []byte, char byte, length int) []byte {
	h := sha256.New()
	h.Write(appendSSHStringBytes(nil, sharedSecret))
	h.Write(sessionID)
	h.Write([]byte{char})
	h.Write(sessionID)
	return h.Sum(nil)[:length]
}

func (c *Conn) exchangeHash(hostKeyBlob, e, f, k []byte) []byte {
	h := sha256.New()
	h.Write(appendSSHStringBytes(nil, c.clientVersion))
	h.Write(appendSSHStringBytes(nil, c.serverVersion))
	h.Write(appendSSHStringBytes(nil, c.clientKEXInit))
	h.Write(appendSSHStringBytes(nil, c.serverKEXInit))
	h.Write(appendSSHStringBytes(nil, hostKeyBlob))
	h.Write(e)
	h.Write(f)
	h.Write(k)
	return h.Sum(nil)
}

func computeMAC(seq uint32, data []byte, hs *hmacState) []byte {
	hs.mac.Reset()
	var seqBuf [4]byte
	binary.BigEndian.PutUint32(seqBuf[:], seq)
	hs.mac.Write(seqBuf[:])
	hs.mac.Write(data)
	return hs.mac.Sum(nil)
}

func hmacEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

func newAESCTR(key, iv []byte) (encryptor, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	return cipher.NewCTR(block, iv), nil
}

func randSource() io.Reader {
	return rand.Reader
}
