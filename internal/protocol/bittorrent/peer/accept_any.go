package peer

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/smartass08/aria2go/internal/mse"
)

// AcceptAny completes an inbound peer handshake for any registered infohash.
// It first checks for the legacy plaintext BitTorrent handshake, then falls
// back to MSE/PE using the candidate infohash set. Plaintext is refused when
// the matched torrent requires encryption.
func AcceptAny(ctx context.Context, c net.Conn, configs map[[20]byte]Config) (*Conn, [20]byte, error) {
	if len(configs) == 0 {
		c.Close()
		return nil, [20]byte{}, fmt.Errorf("peer: no inbound torrent registrations")
	}
	stopClose := closeConnOnContextDone(ctx, c)
	defer stopClose()

	if err := setConnDeadline(ctx, c, maxAcceptTimeout(configs)); err != nil {
		c.Close()
		return nil, [20]byte{}, err
	}
	defer c.SetDeadline(time.Time{})

	var prefix [1 + pstrLen]byte
	if _, err := io.ReadFull(c, prefix[:]); err != nil {
		c.Close()
		return nil, [20]byte{}, fmt.Errorf("peer: read protocol prefix: %w", err)
	}

	transport := &prefixConn{Conn: c, pending: prefix[:]}
	if prefix[0] == pstrLen && string(prefix[1:]) == pstr {
		return acceptLegacyInbound(ctx, c, configs, prefix)
	}

	infoHashes := make([][20]byte, 0, len(configs))
	for infoHash := range configs {
		infoHashes = append(infoHashes, infoHash)
	}

	encConn, matchedHash, _, err := mse.Receive(transport, infoHashes, mse.Allow)
	if err != nil {
		c.Close()
		return nil, [20]byte{}, err
	}

	cfg, ok := configs[matchedHash]
	if !ok {
		encConn.Close()
		return nil, [20]byte{}, fmt.Errorf("peer: no inbound registration for matched infohash")
	}

	conn, err := acceptHandshake(ctx, encConn, cfg)
	if err != nil {
		return nil, [20]byte{}, err
	}
	return conn, matchedHash, nil
}

func acceptLegacyInbound(ctx context.Context, c net.Conn, configs map[[20]byte]Config, prefix [1 + pstrLen]byte) (*Conn, [20]byte, error) {
	var raw [handshakeLen]byte
	copy(raw[:1+pstrLen], prefix[:])
	if _, err := io.ReadFull(c, raw[1+pstrLen:]); err != nil {
		c.Close()
		return nil, [20]byte{}, fmt.Errorf("peer: read handshake: %w", err)
	}

	peerHS, err := parseHandshake(raw[:])
	if err != nil {
		c.Close()
		return nil, [20]byte{}, err
	}

	cfg, ok := configs[peerHS.InfoHash]
	if !ok {
		c.Close()
		return nil, [20]byte{}, fmt.Errorf("%w: handshake info hash mismatch", ErrProtocolViolation)
	}
	if cfg.Encrypt == mse.Require {
		c.Close()
		return nil, [20]byte{}, fmt.Errorf("peer: plaintext handshake rejected by crypto policy")
	}

	conn, err := acceptHandshake(ctx, &prefixConn{Conn: c, pending: raw[:]}, cfg)
	if err != nil {
		return nil, [20]byte{}, err
	}
	return conn, peerHS.InfoHash, nil
}

func maxAcceptTimeout(configs map[[20]byte]Config) time.Duration {
	timeout := defaultTimeout
	for _, cfg := range configs {
		if t := cfg.timeout(); t > timeout {
			timeout = t
		}
	}
	return timeout
}
