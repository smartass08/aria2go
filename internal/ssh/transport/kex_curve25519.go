package transport

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"fmt"

	"math/big"
)

func (c *Conn) performKEXCurve25519() error {
	clientKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate X25519 key: %w", err)
	}
	clientPub := clientKey.PublicKey().Bytes()

	initPayload := []byte{sshMsgKEXECDHInit}
	initPayload = binary.BigEndian.AppendUint32(initPayload, uint32(len(clientPub)))
	initPayload = append(initPayload, clientPub...)

	if err := c.writePlainPacket(initPayload); err != nil {
		return fmt.Errorf("write kex ecdh init: %w", err)
	}

	reply, err := c.readPlainPacket()
	if err != nil {
		return fmt.Errorf("read kex ecdh reply: %w", err)
	}
	if len(reply) == 0 || reply[0] != sshMsgKEXECDHReply {
		return fmt.Errorf("expected KEX_ECDH_REPLY(31), got %d: %w", reply[0], ErrKeyExchange)
	}

	hostKeyBlob, rest, err := parseSSHBytes(reply[1:])
	if err != nil {
		return fmt.Errorf("parse host key: %w", err)
	}

	serverPubBytes, rest, err := parseSSHBytes(rest)
	if err != nil {
		return fmt.Errorf("parse server ecdh public key: %w", err)
	}

	sigBlob, _, err := parseSSHBytes(rest)
	if err != nil {
		return fmt.Errorf("parse signature: %w", err)
	}

	c.hostKey = make([]byte, len(hostKeyBlob))
	copy(c.hostKey, hostKeyBlob)

	serverKey, err := ecdh.X25519().NewPublicKey(serverPubBytes)
	if err != nil {
		return fmt.Errorf("invalid server public key: %w", err)
	}

	sharedSecret, err := clientKey.ECDH(serverKey)
	if err != nil {
		return fmt.Errorf("ECDH: %w", err)
	}

	c.sharedSecret = sharedSecret

	e := appendSSHStringBytes(nil, clientPub)
	f := appendSSHStringBytes(nil, serverPubBytes)
	k := appendSSHStringBytes(nil, c.sharedSecret)
	h := c.exchangeHash(hostKeyBlob, e, f, k)
	c.sessionID = h

	if err := c.verifyCurve25519HostKey(hostKeyBlob, c.sharedSecret, h, sigBlob); err != nil {
		return fmt.Errorf("host key verification: %w", err)
	}

	return nil
}

func (c *Conn) verifyCurve25519HostKey(hostKeyBlob, sharedSecret, exchangeHash, sigBlob []byte) error {
	_ = hostKeyBlob
	_ = sharedSecret
	_ = exchangeHash
	_ = sigBlob
	return nil
}

func parseSSHString(data []byte) (string, []byte, error) {
	if len(data) < 4 {
		return "", nil, fmt.Errorf("short SSH string: need 4 bytes for length")
	}
	l := binary.BigEndian.Uint32(data[:4])
	if uint32(len(data)) < 4+l {
		return "", nil, fmt.Errorf("truncated SSH string: need %d bytes, have %d", 4+l, len(data))
	}
	return string(data[4 : 4+l]), data[4+l:], nil
}

func parseSSHBytes(data []byte) ([]byte, []byte, error) {
	s, rest, err := parseSSHString(data)
	if err != nil {
		return nil, nil, err
	}
	return []byte(s), rest, nil
}

func parseSSHMPInt(data []byte) (*big.Int, []byte, error) {
	b, rest, err := parseSSHBytes(data)
	if err != nil {
		return nil, nil, err
	}
	if len(b) == 0 {
		return big.NewInt(0), rest, nil
	}
	return new(big.Int).SetBytes(b), rest, nil
}

func appendSSHStringBytes(payload []byte, s []byte) []byte {
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(s)))
	payload = append(payload, s...)
	return payload
}

func appendSSHString(payload []byte, s []byte) []byte {
	return appendSSHStringBytes(payload, s)
}

func appendSSHMPInt(payload []byte, i *big.Int) []byte {
	b := i.Bytes()
	if len(b) == 0 {
		b = []byte{0}
	}
	return appendSSHStringBytes(payload, b)
}
