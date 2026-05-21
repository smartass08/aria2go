// Package tracker implements BitTorrent tracker protocols — HTTP tracker
// (BEP 3), UDP tracker (BEP 15), and HTTP scrape (BEP 48) — matching aria2
// 1.37.0 behavior.
//
// This package provides synchronous, single-call functions for announcing
// to a tracker and scraping swarm metadata. It handles UDP connection-id
// handshake, retry with backoff, compact peer decoding, bencode
// marshalling/unmarshalling, and IPv4/IPv6 peer lists.
package tracker

import (
	"errors"
	"net"
)

// Package-level sentinel errors.
var (
	ErrTimeout     = errors.New("tracker: request timed out")
	ErrInvalidResp = errors.New("tracker: invalid response")
	ErrNetwork     = errors.New("tracker: network error")
	ErrBadEvent    = errors.New("tracker: invalid event string")
	ErrBencode     = errors.New("tracker: bencode parse error")
	ErrShutdown    = errors.New("tracker: shutdown")
)

// ScrapeData holds swarm metadata returned by a scrape request (BEP 48).
type ScrapeData struct {
	Complete   int32
	Incomplete int32
	Downloaded int32
}

// AnnounceRequest holds the parameters for a tracker announce.
type AnnounceRequest struct {
	InfoHash      [20]byte
	PeerID        [20]byte
	Port          uint16
	Uploaded      int64
	Downloaded    int64
	Left          int64
	Event         string // "started", "stopped", "completed", or "" (regular)
	NumWant       int
	Key           string // HTTP: last 8 bytes of PeerID are used; UDP: caller should pass a random string (C++ uses randomizer_->getRandomNumber)
	TrackerID     string
	CryptoSupport string // "supportcrypto", "requirecrypto", or "" (none)
	ExternalIP    string
}

// ValidateEvent returns an error if the event string is not one of the
// valid BEP 3 values.
func (r *AnnounceRequest) ValidateEvent() error {
	switch r.Event {
	case "", "started", "stopped", "completed":
		return nil
	default:
		return ErrBadEvent
	}
}

// AnnounceResponse holds the parsed tracker announce reply.
type AnnounceResponse struct {
	Interval       int32
	MinInterval    int32
	TrackerID      string
	WarningMessage string
	Complete       int32
	Incomplete     int32
	Peers          []PeerInfo
	Peers6         []PeerInfo // IPv6 peers
}

// PeerInfo holds a single peer address.
type PeerInfo struct {
	IP   net.IP
	Port uint16
}
