package peer

import (
	"sync/atomic"
	"time"
)

type Stat struct {
	Downloaded   int64
	Uploaded     int64
	LastRead     time.Time
	LastWrite    time.Time
	Choked       bool
	Interested   bool
	PeerChoking  bool
	PeerInterest bool
	NumPieces    int
}

type statStore struct {
	downloaded   atomic.Int64
	uploaded     atomic.Int64
	lastRead     atomic.Int64 // unix nano
	lastWrite    atomic.Int64 // unix nano
	choked       atomic.Bool
	interested   atomic.Bool
	peerChoking  atomic.Bool
	peerInterest atomic.Bool
	numPieces    atomic.Int32
}

func (s *statStore) addDownloaded(n int) {
	s.downloaded.Add(int64(n))
	s.lastRead.Store(time.Now().UnixNano())
}

func (s *statStore) addUploaded(n int) {
	s.uploaded.Add(int64(n))
	s.lastWrite.Store(time.Now().UnixNano())
}

func (s *statStore) snapshot() Stat {
	return Stat{
		Downloaded:   s.downloaded.Load(),
		Uploaded:     s.uploaded.Load(),
		LastRead:     time.Unix(0, s.lastRead.Load()),
		LastWrite:    time.Unix(0, s.lastWrite.Load()),
		Choked:       s.choked.Load(),
		Interested:   s.interested.Load(),
		PeerChoking:  s.peerChoking.Load(),
		PeerInterest: s.peerInterest.Load(),
		NumPieces:    int(s.numPieces.Load()),
	}
}
