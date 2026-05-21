package engine

import (
	"container/list"
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Throttle implements a token bucket rate limiter with 100ms refill ticks.
// Zero rate means unlimited — Wait returns immediately.
type Throttle struct {
	rate      atomic.Int64
	bucket    atomic.Int64
	carryover int64 // fractional refill accumulator (rate % 10)
	mu        sync.Mutex
	waiters   list.List
	ticker    *time.Ticker
	done      chan struct{}
}

// NewThrottle creates a Throttle with the given bytes/sec rate.
// Pass 0 for unlimited.
func NewThrottle(bytesPerSec int64) *Throttle {
	t := &Throttle{
		done: make(chan struct{}),
	}
	t.rate.Store(bytesPerSec)
	t.bucket.Store(0)

	if bytesPerSec > 0 {
		t.ticker = time.NewTicker(100 * time.Millisecond)
		go t.run()
	}
	return t
}

func (t *Throttle) run() {
	for {
		select {
		case <-t.ticker.C:
			t.refillBucket()
		case <-t.done:
			return
		}
	}
}

func (t *Throttle) refillBucket() {
	rate := t.rate.Load()
	if rate <= 0 {
		return
	}

	// Carryover prevents starvation at low rates where rate/10 would truncate to 0.
	// Example: rate=9 → 9*10=90ms each tick accumulates 0.9 tokens; carryover tracks
	// the fractional byte, releasing exactly 1 token every 10th tick (1 byte/s).
	incr := (rate + t.carryover) / 10
	t.carryover = (rate + t.carryover) % 10

	if incr == 0 {
		return
	}

	current := t.bucket.Load()
	for {
		newVal := current + incr
		if t.bucket.CompareAndSwap(current, newVal) {
			break
		}
		current = t.bucket.Load()
	}

	t.signalWaiters()
}

func (t *Throttle) signalWaiters() {
	t.mu.Lock()
	defer t.mu.Unlock()

	var next *list.Element
	for e := t.waiters.Front(); e != nil; e = next {
		next = e.Next()
		ch := e.Value.(chan struct{})
		select {
		case ch <- struct{}{}:
			t.waiters.Remove(e)
		default:
		}
	}
}

// SetRate changes the bytes/sec limit. Pass 0 for unlimited.
func (t *Throttle) SetRate(bytesPerSec int64) {
	old := t.rate.Swap(bytesPerSec)

	if old == 0 && bytesPerSec > 0 {
		t.mu.Lock()
		if t.ticker == nil {
			t.ticker = time.NewTicker(100 * time.Millisecond)
			go t.run()
		} else {
			// Ticker exists but may be stopped (eg after Stop() was called).
			// If done is closed, the refill goroutine has exited; restart it.
			select {
			case <-t.done:
				t.done = make(chan struct{})
				t.ticker = time.NewTicker(100 * time.Millisecond)
				go t.run()
			default:
			}
		}
		t.mu.Unlock()
	}
	if old > 0 && bytesPerSec == 0 {
		t.carryover = 0
		t.signalWaiters()
	}
}

// Wait blocks until n bytes of capacity are available or ctx is cancelled.
// If the rate is 0 (unlimited), Wait returns immediately.
func (t *Throttle) Wait(ctx context.Context, n int) error {
	rate := t.rate.Load()
	if rate <= 0 || n <= 0 {
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		current := t.bucket.Load()
		if current >= int64(n) {
			if t.bucket.CompareAndSwap(current, current-int64(n)) {
				return nil
			}
			continue
		}

		ch := make(chan struct{}, 1)
		t.mu.Lock()
		e := t.waiters.PushBack(ch)
		t.mu.Unlock()

		current = t.bucket.Load()
		if current >= int64(n) {
			if t.bucket.CompareAndSwap(current, current-int64(n)) {
				t.mu.Lock()
				t.waiters.Remove(e)
				t.mu.Unlock()
				return nil
			}
		}

		select {
		case <-ch:
		case <-ctx.Done():
			t.mu.Lock()
			t.waiters.Remove(e)
			t.mu.Unlock()
			return ctx.Err()
		case <-t.done:
			return nil
		}
	}
}

// Stop shuts down the refill goroutine.
func (t *Throttle) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ticker != nil {
		t.ticker.Stop()
	}
	select {
	case <-t.done:
	default:
		close(t.done)
	}
}

// parseSize parses a size string like "100K", "1M", "0", or plain bytes.
// Returns the int64 value in bytes.
func parseSize(s string) int64 {
	if s == "" {
		return 0
	}
	mult := int64(1)
	last := s[len(s)-1]
	switch last {
	case 'K', 'k':
		mult = 1024
		s = s[:len(s)-1]
	case 'M', 'm':
		mult = 1024 * 1024
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n * mult
}
