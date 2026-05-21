package peer

import (
	"fmt"
)

const (
	pstr         = "BitTorrent protocol"
	pstrLen      = 19
	reservedLen  = 8
	infoHashLen  = 20
	peerIDLen    = 20
	handshakeLen = 1 + pstrLen + reservedLen + infoHashLen + peerIDLen // 68

	// reserved[5] bit 0x10 — BEP 10 extended messaging
	reservedExtMsg = 0x10
	// reserved[7] bit 0x04 — BEP 6 fast extension
	reservedFastExt = 0x04
	// reserved[7] bit 0x01 — BEP 5 DHT
	reservedDHT = 0x01
)

func marshalHandshake(infoHash [20]byte, peerID [20]byte, reserved [8]byte) [handshakeLen]byte {
	var h [handshakeLen]byte
	h[0] = pstrLen
	copy(h[1:20], pstr)
	copy(h[20:28], reserved[:])
	copy(h[28:48], infoHash[:])
	copy(h[48:68], peerID[:])
	return h
}

type Handshake struct {
	InfoHash [20]byte
	PeerID   [20]byte
	Reserved [8]byte
}

func parseHandshake(data []byte) (Handshake, error) {
	if len(data) < handshakeLen {
		return Handshake{}, fmt.Errorf("peer: handshake too short: %d bytes", len(data))
	}
	var h Handshake
	if data[0] != pstrLen {
		return h, fmt.Errorf("peer: invalid pstrlen: %d", data[0])
	}
	if string(data[1:20]) != pstr {
		return h, fmt.Errorf("peer: invalid protocol string")
	}
	copy(h.Reserved[:], data[20:28])
	copy(h.InfoHash[:], data[28:48])
	copy(h.PeerID[:], data[48:68])
	return h, nil
}

func hasFastExtension(reserved [8]byte) bool {
	return reserved[7]&reservedFastExt != 0
}

func hasExtensionMessaging(reserved [8]byte) bool {
	return reserved[5]&reservedExtMsg != 0
}

func hasDHT(reserved [8]byte) bool {
	return reserved[7]&reservedDHT != 0
}

func MakeReserved(fastExt, extMsg, dht bool) [8]byte {
	var r [8]byte
	if fastExt {
		r[7] |= reservedFastExt
	}
	if extMsg {
		r[5] |= reservedExtMsg
	}
	if dht {
		r[7] |= reservedDHT
	}
	return r
}
