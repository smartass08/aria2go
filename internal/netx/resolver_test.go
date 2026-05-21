package netx

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testHost is a well-known host unlikely to be in /etc/hosts, ensuring
// the test resolver's Dial function is exercised.
const testHost = "google.com"

func TestNewResolver(t *testing.T) {
	r := NewResolver()
	if r == nil {
		t.Fatal("NewResolver returned nil")
	}
	if r.resolver == nil {
		t.Fatal("resolver is nil")
	}
	if r.ttl != defaultTTL {
		t.Fatalf("expected ttl %v, got %v", defaultTTL, r.ttl)
	}
}

func TestLookupHost_Basic(t *testing.T) {
	r := NewResolver()
	ctx := context.Background()

	addrs, err := r.LookupHost(ctx, "localhost")
	if err != nil {
		t.Fatalf("LookupHost failed: %v", err)
	}
	if len(addrs) == 0 {
		t.Fatal("expected at least one address for localhost")
	}
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip == nil {
			t.Fatalf("expected valid IP address, got %q", a)
		}
	}
}

func TestLookupHost_CacheHit(t *testing.T) {
	r := NewResolver()
	ctx := context.Background()

	// First lookup populates the cache.
	addrs1, err := r.LookupHost(ctx, testHost)
	if err != nil {
		t.Fatalf("first lookup failed: %v", err)
	}

	// Verify cache is populated.
	_, ok := r.cache.Load(testHost)
	if !ok {
		t.Fatal("cache should have an entry after first lookup")
	}

	// Replace resolver with one that records calls.
	var lookupCalled int32
	r.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			atomic.AddInt32(&lookupCalled, 1)
			return net.Dial(network, address)
		},
	}

	// Second lookup should hit the cache.
	addrs2, err := r.LookupHost(ctx, testHost)
	if err != nil {
		t.Fatalf("second lookup failed: %v", err)
	}

	if len(addrs1) != len(addrs2) {
		t.Fatalf("address count changed: %d vs %d", len(addrs1), len(addrs2))
	}
	for i := range addrs1 {
		if addrs1[i] != addrs2[i] {
			t.Fatalf("address %d changed: %q vs %q", i, addrs1[i], addrs2[i])
		}
	}

	if atomic.LoadInt32(&lookupCalled) > 0 {
		t.Fatal("expected cache hit, but resolver was called again")
	}
}

func TestLookupHost_CacheExpiry(t *testing.T) {
	r := NewResolver()
	r.ttl = 10 * time.Millisecond
	ctx := context.Background()

	_, err := r.LookupHost(ctx, testHost)
	if err != nil {
		t.Fatalf("first lookup failed: %v", err)
	}

	// Wait for the cache entry to expire.
	time.Sleep(20 * time.Millisecond)

	v, ok := r.cache.Load(testHost)
	if !ok {
		t.Fatal("expired entry should still be in cache (lazy eviction)")
	}
	ce := v.(*cacheEntry)
	if time.Now().Before(ce.expires) {
		t.Fatal("cache entry should be expired by now")
	}

	var lookupCalled int32
	r.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			atomic.AddInt32(&lookupCalled, 1)
			return net.Dial(network, address)
		},
	}

	// Second lookup should miss cache and call resolver.
	_, err = r.LookupHost(ctx, testHost)
	if err != nil {
		t.Fatalf("second lookup failed: %v", err)
	}

	if atomic.LoadInt32(&lookupCalled) == 0 {
		t.Fatal("expected resolver call for expired cache entry, but none occurred")
	}
}

func TestLookupHost_SingleflightDedup(t *testing.T) {
	r := NewResolver()

	var calls int32

	r.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			atomic.AddInt32(&calls, 1)
			// Simulate slow DNS to give other goroutines time to
			// join the singleflight.
			time.Sleep(100 * time.Millisecond)
			return net.Dial(network, address)
		},
	}

	ctx := context.Background()
	const n = 5
	var wg sync.WaitGroup
	errCh := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			addrs, err := r.LookupHost(ctx, testHost)
			if err != nil {
				errCh <- err
				return
			}
			if len(addrs) == 0 {
				errCh <- errors.New("no addresses returned")
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}

	if c := atomic.LoadInt32(&calls); c == 0 || c >= int32(n) {
		t.Fatalf("expected singleflight dedup (0 < calls < %d), got %d", n, c)
	}
}

func TestLookupHost_ContextCancellation(t *testing.T) {
	r := NewResolver()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.LookupHost(ctx, "localhost")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestLookupHost_ContextDeadline(t *testing.T) {
	r := NewResolver()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	_, err := r.LookupHost(ctx, "localhost")
	if err == nil {
		t.Fatal("expected error for deadline-exceeded context")
	}
}

func TestFlush(t *testing.T) {
	r := NewResolver()
	ctx := context.Background()

	_, err := r.LookupHost(ctx, testHost)
	if err != nil {
		t.Fatalf("first lookup failed: %v", err)
	}

	count := 0
	r.cache.Range(func(_, _ any) bool { count++; return true })
	if count == 0 {
		t.Fatal("cache should be non-empty after lookup")
	}

	r.Flush()

	count = 0
	r.cache.Range(func(_, _ any) bool { count++; return true })
	if count != 0 {
		t.Fatal("cache should be empty after Flush")
	}
}

func TestLookupHost_Concurrent(t *testing.T) {
	r := NewResolver()
	ctx := context.Background()

	const n = 10
	var wg sync.WaitGroup
	errCh := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			addrs, err := r.LookupHost(ctx, "localhost")
			if err != nil {
				errCh <- err
				return
			}
			if len(addrs) == 0 {
				errCh <- errors.New("no addresses returned")
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}
}

func TestLookupHost_ReturnsCopy(t *testing.T) {
	r := NewResolver()
	ctx := context.Background()

	addrs1, err := r.LookupHost(ctx, testHost)
	if err != nil {
		t.Fatalf("first lookup failed: %v", err)
	}
	if len(addrs1) == 0 {
		t.Fatal("no addresses")
	}

	// Capture the cached value before mutating.
	v, ok := r.cache.Load(testHost)
	if !ok {
		t.Fatal("cache entry missing")
	}
	ce := v.(*cacheEntry)
	cachedVal := ce.addrs[0]

	// Mutate the returned slice.
	addrs1[0] = "255.255.255.255"

	// The cache should still have the original value.
	v, ok = r.cache.Load(testHost)
	if !ok {
		t.Fatal("cache entry missing after mutation")
	}
	ce = v.(*cacheEntry)
	if ce.addrs[0] != cachedVal {
		t.Fatal("cache was mutated through returned slice")
	}
}

func TestLookupHost_CachePersistence(t *testing.T) {
	r := NewResolver()
	r.ttl = 10 * time.Millisecond
	ctx := context.Background()

	_, err := r.LookupHost(ctx, "localhost")
	if err != nil {
		t.Fatalf("first lookup failed: %v", err)
	}

	// Verify the cache entry exists.
	v, ok := r.cache.Load("localhost")
	if !ok {
		t.Fatal("cache entry should exist after lookup")
	}
	ce := v.(*cacheEntry)
	if time.Now().After(ce.expires) {
		t.Fatal("cache entry should not be expired yet")
	}

	// Wait for expiry.
	time.Sleep(20 * time.Millisecond)

	// Expired entry should still be in the map (lazy eviction).
	_, ok = r.cache.Load("localhost")
	if !ok {
		t.Fatal("expired entry should remain in map for lazy eviction")
	}

	// Next lookup should re-resolve.
	addrs2, err := r.LookupHost(ctx, "localhost")
	if err != nil {
		t.Fatalf("second lookup failed: %v", err)
	}

	if len(addrs2) == 0 {
		t.Fatal("second lookup returned no addresses")
	}
}
