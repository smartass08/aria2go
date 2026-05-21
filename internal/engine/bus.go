package engine

import (
	"sync"

	"github.com/smartass08/aria2go/internal/core"
)

// EventBus provides in-process pub/sub for download events.
// Subscribers receive events on channels, matching aria2's Notifier
// pattern (Notifier.h) where DownloadEventListener receives onEvent calls.
type EventBus struct {
	mu     sync.RWMutex
	subs   map[int64]chan core.Event
	nextID int64
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subs: make(map[int64]chan core.Event),
	}
}

// Subscribe registers a channel to receive events. Returns a function that
// unsubscribes the channel. The channel is non-buffered; callers should use
// a buffered channel or a select-based drain to avoid blocking emits.
func (b *EventBus) Subscribe(ch chan core.Event) (unsubscribe func()) {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	b.subs[id] = ch
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
	}
}

// Emit sends an event to all subscribers. If a subscriber's channel is full,
// the event is dropped for that subscriber (non-blocking). This matches
// aria2's Notifier::notifyDownloadEvent which calls each listener's onEvent
// synchronously — here we use channels for decoupling.
func (b *EventBus) Emit(ev core.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Len returns the current number of subscribers.
func (b *EventBus) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
