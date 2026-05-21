// Package testutil provides hand-rolled mock implementations and test
// helpers for the aria2go project. All mocks follow the function-pointer
// pattern: each interface method has a corresponding Fn field that the
// mock calls. Tests set Fn fields to provide custom behavior; nil Fn
// fields produce sensible zero-value defaults.
package testutil

import (
	"context"
	"math/rand"
	"net"
	"strconv"
	"sync"

	"github.com/smartass08/aria2go/internal/dht"
	"github.com/smartass08/aria2go/internal/tracker"
)

// MockPieceSelector implements a piece selection strategy for testing.
// When SelectFn is nil, Select returns -1 (no piece selected).
// C++ reference: MockPieceSelector.h (returns false/0 always).
type MockPieceSelector struct {
	mu       sync.Mutex
	SelectFn func(missing []int, available map[int]int) int
	ResetFn  func()
}

// Select calls fn if set; otherwise returns -1.
func (m *MockPieceSelector) Select(missing []int, available map[int]int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.SelectFn != nil {
		return m.SelectFn(missing, available)
	}
	return -1
}

// Reset calls fn if set.
func (m *MockPieceSelector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ResetFn != nil {
		m.ResetFn()
	}
}

// MockPeer holds a peer's state for choking algorithm and peer selection
// tests. C++ reference: Peer.h test usage in MockPeerStorage.h.
type MockPeer struct {
	Snubbing       bool
	AmChoking      bool
	AmInterested   bool
	PeerChoking    bool
	PeerInterested bool
	Bitfield       []byte
	UploadLength   int64
	DownloadLength int64
	IP             string
	Port           uint16

	PeerID []byte

	Seeder bool
}

// PeerAddr returns the IP:port address string.
func (p *MockPeer) PeerAddr() string {
	return net.JoinHostPort(p.IP, strconv.Itoa(int(p.Port)))
}

// MockPeerStorage implements peer storage for testing choking algorithm
// and peer management. C++ reference: MockPeerStorage.h.
type MockPeerStorage struct {
	mu    sync.Mutex
	Peers []*MockPeer

	NumChokeExecuted int

	UnusedPeers  []*MockPeer
	ActivePeers  []*MockPeer
	DroppedPeers []*MockPeer

	AddPeerFn                   func(peer *MockPeer) bool
	IsPeerAvailableFn           func() bool
	ChokeRoundIntervalElapsedFn func() bool
	ExecuteChokeFn              func()
	IsBadPeerFn                 func(ip string) bool
}

// AddPeer adds a peer to the unused pool.
// C++ reference: MockPeerStorage::addPeer.
func (m *MockPeerStorage) AddPeer(peer *MockPeer) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.AddPeerFn != nil {
		return m.AddPeerFn(peer)
	}
	m.UnusedPeers = append(m.UnusedPeers, peer)
	return true
}

// IsPeerAvailable reports whether the storage has peers available.
func (m *MockPeerStorage) IsPeerAvailable() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.IsPeerAvailableFn != nil {
		return m.IsPeerAvailableFn()
	}
	return len(m.UnusedPeers) > 0 || len(m.ActivePeers) > 0
}

// ChokeRoundIntervalElapsed reports whether enough time has passed for
// the next choke round. Default: false.
func (m *MockPeerStorage) ChokeRoundIntervalElapsed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ChokeRoundIntervalElapsedFn != nil {
		return m.ChokeRoundIntervalElapsedFn()
	}
	return false
}

// ExecuteChoke runs the choke algorithm. Increments NumChokeExecuted.
// C++ reference: MockPeerStorage::executeChoke.
func (m *MockPeerStorage) ExecuteChoke() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.NumChokeExecuted++
	if m.ExecuteChokeFn != nil {
		m.ExecuteChokeFn()
	}
}

// IsBadPeer checks whether the peer at ip is marked as bad.
// C++ reference: MockPeerStorage::isBadPeer (always false).
func (m *MockPeerStorage) IsBadPeer(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.IsBadPeerFn != nil {
		return m.IsBadPeerFn(ip)
	}
	return false
}

// AddBadPeer marks a peer as bad. No-op by default (C++ default).
func (m *MockPeerStorage) AddBadPeer(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
}

// CountAllPeer returns the total number of peers in all pools.
func (m *MockPeerStorage) CountAllPeer() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.UnusedPeers) + len(m.ActivePeers) + len(m.DroppedPeers)
}

// MockBtAnnounce implements tracker announce for testing download
// lifecycle. C++ reference: MockBtAnnounce.h.
type MockBtAnnounce struct {
	mu sync.Mutex

	AnnounceFn  func(req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error)
	IsStoppedFn func() bool
	IsReadyFn   func() bool
	AllFailedFn func() bool
	NoMoreFn    func() bool
	ResetFn     func()

	AnnounceURL string
	PeerID      string
}

// Announce calls fn if set; returns a zero AnnounceResponse and nil error.
func (m *MockBtAnnounce) Announce(req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.AnnounceFn != nil {
		return m.AnnounceFn(req)
	}
	return &tracker.AnnounceResponse{}, nil
}

// IsStopped reports whether the announcer has been stopped.
func (m *MockBtAnnounce) IsStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.IsStoppedFn != nil {
		return m.IsStoppedFn()
	}
	return false
}

// IsAnnounceReady reports whether the announcer is ready to announce.
func (m *MockBtAnnounce) IsAnnounceReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.IsReadyFn != nil {
		return m.IsReadyFn()
	}
	return false
}

// IsAllAnnounceFailed reports whether all tracker announces have failed.
func (m *MockBtAnnounce) IsAllAnnounceFailed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.AllFailedFn != nil {
		return m.AllFailedFn()
	}
	return false
}

// NoMoreAnnounce reports whether no more announces are scheduled.
func (m *MockBtAnnounce) NoMoreAnnounce() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.NoMoreFn != nil {
		return m.NoMoreFn()
	}
	return false
}

// ResetAnnounce resets announce state.
func (m *MockBtAnnounce) ResetAnnounce() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ResetFn != nil {
		m.ResetFn()
	}
}

// MockDHTMessageDispatcher implements DHT message dispatch for testing.
// It records sent messages rather than sending them over the network.
// C++ reference: MockDHTMessageDispatcher.h.
type MockDHTMessageDispatcher struct {
	mu       sync.Mutex
	messages []*dht.Message

	SendMessageFn func(msg *dht.Message, addr net.Addr) error
	SendFn        func() error
}

// AddMessage adds a message to the internal queue (record-only).
// C++ reference: MockDHTMessageDispatcher::addMessageToQueue.
func (m *MockDHTMessageDispatcher) AddMessage(msg *dht.Message, _ uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
}

// SendMessages calls SendFn if set.
// C++ reference: MockDHTMessageDispatcher::sendMessages (no-op by default).
func (m *MockDHTMessageDispatcher) SendMessages() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.SendFn != nil {
		return m.SendFn()
	}
	return nil
}

// Send sends a single message using SendMessageFn if set.
func (m *MockDHTMessageDispatcher) Send(msg *dht.Message, addr net.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.SendMessageFn != nil {
		return m.SendMessageFn(msg, addr)
	}
	m.messages = append(m.messages, msg)
	return nil
}

// CountMessage returns the number of queued messages.
func (m *MockDHTMessageDispatcher) CountMessage() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

// Messages returns all queued messages (for test assertions).
func (m *MockDHTMessageDispatcher) Messages() []*dht.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*dht.Message, len(m.messages))
	copy(out, m.messages)
	return out
}

// Clear empties the message queue.
func (m *MockDHTMessageDispatcher) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = nil
}

// MockDHTTask implements the DHT Task interface for testing.
// When Fn fields are nil, methods return zero-value defaults.
// C++ reference: MockDHTTask.h.
type MockDHTTask struct {
	mu sync.Mutex

	ExecuteFn  func(ctx context.Context) error
	FinishedFn func() bool

	RemoteNode *MockDHTNode
	TargetID   [20]byte
	Finished   bool
}

// Execute calls fn if set; returns nil.
func (m *MockDHTTask) Execute(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ExecuteFn != nil {
		return m.ExecuteFn(ctx)
	}
	return nil
}

// IsFinished reports whether the task has completed.
func (m *MockDHTTask) IsFinished() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FinishedFn != nil {
		return m.FinishedFn()
	}
	return m.Finished
}

// MockDHTNode represents a DHT node for testing.
type MockDHTNode struct {
	ID   [20]byte
	IP   [4]byte
	Port uint16
}

// FixedRandom provides deterministic random values for tests.
// Implements enough of the random interface for aria2 use cases.
// C++ reference: FixedNumberRandomizer.h.
type FixedRandom struct {
	Value int32
}

// Int31 returns the fixed value.
func (r *FixedRandom) Int31() int32 { return r.Value }

// Int31n returns Value modulo n.
func (r *FixedRandom) Int31n(n int32) int32 {
	if n <= 0 {
		return 0
	}
	return r.Value % n
}

// Intn returns an int in [0, n).
func (r *FixedRandom) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.Value % int32(n))
}

// Float64 returns a float64 in [0.0, 1.0) derived from Value.
func (r *FixedRandom) Float64() float64 {
	const scale = 1.0 / (1 << 31)
	v := float64(r.Value&0x7fffffff) * scale
	if v >= 1.0 {
		v = 0.999999999999
	}
	return v
}

// Perm returns a deterministic permutation of [0, n).
func (r *FixedRandom) Perm(n int) []int {
	if n <= 0 {
		return nil
	}
	seed := int64(r.Value)
	src := rand.NewSource(seed)
	rng := rand.New(src)
	return rng.Perm(n)
}
