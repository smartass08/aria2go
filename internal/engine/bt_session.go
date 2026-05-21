package engine

import (
	"crypto/rand"

	"github.com/smartass08/aria2go/internal/config"
)

// BtSession holds BitTorrent session-level state shared across all BT
// downloads in the engine. In aria2 this corresponds to BtRuntime which
// holds port, peerId, and runtime counters.
type BtSession struct {
	port    int
	peerID  [20]byte
	dhtPort int
}

// NewBtSession creates a new BT session with a random peer ID and the
// listen port from configuration. If ListenPort is empty or "6881-6999",
// a random port in that range is used. If DHTListenPort is set, it is
// used as the DHT port; otherwise the listen port is reused.
func NewBtSession(cfg *config.Options) *BtSession {
	port := parseListenPort(cfg.ListenPort)
	dhtPort := port
	if cfg.DHTListenPort != "" {
		if p := parseListenPort(cfg.DHTListenPort); p != 0 {
			dhtPort = p
		}
	}

	var peerID [20]byte
	if _, err := rand.Read(peerID[:]); err != nil {
		panic("engine: failed to generate BT peer ID: " + err.Error())
	}

	return &BtSession{
		port:    port,
		peerID:  peerID,
		dhtPort: dhtPort,
	}
}

// PeerID returns the 20-byte random peer ID for this BT session.
func (s *BtSession) PeerID() [20]byte {
	return s.peerID
}

// Port returns the BT listen port.
func (s *BtSession) Port() int {
	return s.port
}

// DHTPort returns the DHT port for this BT session.
func (s *BtSession) DHTPort() int {
	return s.dhtPort
}
