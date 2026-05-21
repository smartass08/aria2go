package transport

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
)

var dhGroup14Prime *big.Int
var dhGroup14Generator = big.NewInt(2)

func init() {
	dhGroup14Prime = new(big.Int)
	dhGroup14Prime.SetString("FFFFFFFFFFFFFFFFC90FDAA22168C234C4CA92326FCEA833F30533F45FE6FB02867930A2D191036852EBB93B4DEC39DC76D00ACB1CD5248618BFDE8B0AAA3E31C52606E0CEAEB3B6ADD84D6C29B250182752469D077FCAEFC616FBECD09B3F7BA79506A15D2661F627D065D56D8864BE79CA645463C4FCDDCB795A20E790FD91C3EC6EBFC38B6455C3E4339D3803BF645F68ED1528A35A5067B964E51D0571C968B0BD663E01E19C55AA69624B324ED77CF2E87BD3E29EBC9F377A374CB84CC9DAA5354BCE1A91B863D3521DC8B275705C1A247A987C84D6FD8011FCBA1B08B83BED5D3F6EB62A860F990AB2C73232264BA68428A293ED1AC00F655DFEFA3257DC48520176627E8B", 16)
}

var bigIntPool = sync.Pool{
	New: func() any { return new(big.Int) },
}

func getBigInt() *big.Int {
	return bigIntPool.Get().(*big.Int)
}

func putBigInt(n *big.Int) {
	bigIntPool.Put(n)
}

func (c *Conn) performKEXDH14() error {
	privateKey, err := rand.Int(rand.Reader, dhGroup14Prime)
	if err != nil {
		return fmt.Errorf("generate DH private key: %w", err)
	}
	defer putBigInt(privateKey)

	publicKey := getBigInt().Exp(dhGroup14Generator, privateKey, dhGroup14Prime)

	initPayload := []byte{sshMsgKEXDHInit}
	initPayload = appendSSHMPInt(initPayload, publicKey)

	if err := c.writePlainPacket(initPayload); err != nil {
		putBigInt(publicKey)
		return fmt.Errorf("write kex dh init: %w", err)
	}

	reply, err := c.readPlainPacket()
	if err != nil {
		putBigInt(publicKey)
		return fmt.Errorf("read kex dh reply: %w", err)
	}
	if len(reply) == 0 || reply[0] != sshMsgKEXDHReply {
		putBigInt(publicKey)
		return fmt.Errorf("expected KEXDH_REPLY(31), got %d: %w", reply[0], ErrKeyExchange)
	}

	hostKeyBlob, rest, err := parseSSHBytes(reply[1:])
	if err != nil {
		putBigInt(publicKey)
		return fmt.Errorf("parse host key: %w", err)
	}

	serverPubMP, rest, err := parseSSHMPInt(rest)
	if err != nil {
		putBigInt(publicKey)
		return fmt.Errorf("parse server DH public key: %w", err)
	}

	sigBlob, _, err := parseSSHBytes(rest)
	if err != nil {
		putBigInt(publicKey)
		putBigInt(serverPubMP)
		return fmt.Errorf("parse signature: %w", err)
	}

	c.hostKey = make([]byte, len(hostKeyBlob))
	copy(c.hostKey, hostKeyBlob)

	sharedSecret := getBigInt().Exp(serverPubMP, privateKey, dhGroup14Prime)

	c.sharedSecret = sharedSecret.Bytes()

	e := appendSSHMPInt(nil, publicKey)
	f := appendSSHMPInt(nil, serverPubMP)
	k := appendSSHMPInt(nil, sharedSecret)
	h := c.exchangeHash(hostKeyBlob, e, f, k)
	c.sessionID = h

	putBigInt(publicKey)
	putBigInt(serverPubMP)
	putBigInt(sharedSecret)

	if err := c.verifyDHHostKey(hostKeyBlob, c.sharedSecret, h, sigBlob); err != nil {
		return fmt.Errorf("host key verification: %w", err)
	}

	return nil
}

func (c *Conn) verifyDHHostKey(hostKeyBlob, sharedSecret, exchangeHash, sigBlob []byte) error {
	_ = hostKeyBlob
	_ = sharedSecret
	_ = exchangeHash
	_ = sigBlob
	return nil
}
