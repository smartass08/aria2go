package netx

import (
	"context"
	"net"
	"sync"
	"time"
)

const defaultTTL = 60 * time.Second

// Resolver provides async DNS resolution with caching and singleflight
// deduplication to avoid redundant concurrent lookups.
type Resolver struct {
	resolver *net.Resolver
	cache    sync.Map // string -> *cacheEntry
	sf       singleflightGroup
	ttl      time.Duration
}

type cacheEntry struct {
	addrs   []string
	expires time.Time
}

// NewResolver creates a Resolver with a 60-second TTL cache.
func NewResolver() *Resolver {
	return &Resolver{
		resolver: net.DefaultResolver,
		sf:       singleflightGroup{m: make(map[string]*call)},
		ttl:      defaultTTL,
	}
}

// LookupHost resolves host to IP addresses. Results are cached for TTL
// duration. Uses singleflight to avoid duplicate concurrent lookups for
// the same host. Respects ctx cancellation.
func (r *Resolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if v, ok := r.cache.Load(host); ok {
		ce := v.(*cacheEntry)
		if time.Now().Before(ce.expires) {
			addrs := make([]string, len(ce.addrs))
			copy(addrs, ce.addrs)
			return addrs, nil
		}
	}

	addrs, err := r.sf.Do(host, func() ([]string, error) {
		if v, ok := r.cache.Load(host); ok {
			ce := v.(*cacheEntry)
			if time.Now().Before(ce.expires) {
				addrs := make([]string, len(ce.addrs))
				copy(addrs, ce.addrs)
				return addrs, nil
			}
		}

		ipAddrs, err := r.resolver.LookupHost(ctx, host)
		if err != nil {
			return nil, err
		}
		addrs := make([]string, len(ipAddrs))
		copy(addrs, ipAddrs)

		r.cache.Store(host, &cacheEntry{
			addrs:   addrs,
			expires: time.Now().Add(r.ttl),
		})

		return addrs, nil
	})
	if err != nil {
		return nil, err
	}
	// Copy to avoid sharing the singleflight result slice with the cache.
	cp := make([]string, len(addrs))
	copy(cp, addrs)
	return cp, nil
}

// Flush clears the entire DNS cache.
func (r *Resolver) Flush() {
	r.cache.Range(func(key, _ any) bool {
		r.cache.Delete(key)
		return true
	})
}

// ---------------------------------------------------------------------------
// inline singleflight
// ---------------------------------------------------------------------------

type call struct {
	wg  sync.WaitGroup
	val []string
	err error
}

type singleflightGroup struct {
	mu sync.Mutex
	m  map[string]*call
}

func (g *singleflightGroup) Do(key string, fn func() ([]string, error)) ([]string, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &call{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	return c.val, c.err
}
