package netx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const defaultTTL = 60 * time.Second

// ResolverConfig controls async DNS lookup behaviour.
type ResolverConfig struct {
	Servers    string
	EnableIPv6 bool
}

// Resolver provides async DNS resolution with caching and singleflight
// deduplication to avoid redundant concurrent lookups.
type Resolver struct {
	resolver   *net.Resolver
	cache      sync.Map // resolverCacheKey -> *cacheEntry
	sf         singleflightGroup
	ttl        time.Duration
	enableIPv6 bool
	serverIdx  atomic.Uint32
	servers    []string
}

type resolverCacheKey struct {
	host       string
	enableIPv6 bool
}

type cacheEntry struct {
	addrs   []string
	expires time.Time
}

// NewResolver creates a Resolver with a 60-second TTL cache.
func NewResolver() *Resolver {
	return NewResolverWithConfig(ResolverConfig{})
}

// NewResolverWithConfig creates a Resolver with caller-controlled server and
// IPv6 settings.
func NewResolverWithConfig(cfg ResolverConfig) *Resolver {
	r := &Resolver{
		sf:         singleflightGroup{m: make(map[string]*call)},
		ttl:        defaultTTL,
		enableIPv6: cfg.EnableIPv6,
	}

	if servers := parseResolverServers(cfg.Servers); len(servers) > 0 {
		r.servers = servers
		r.resolver = &net.Resolver{
			PreferGo: true,
			Dial:     r.dialResolverServer,
		}
	} else {
		r.resolver = net.DefaultResolver
	}

	return r
}

// LookupHost resolves host to IP addresses. Results are cached for TTL
// duration. Uses singleflight to avoid duplicate concurrent lookups for
// the same host. Respects ctx cancellation.
func (r *Resolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	key := resolverCacheKey{host: host, enableIPv6: r.enableIPv6}
	if addrs, ok := r.cacheHit(key); ok {
		return addrs, nil
	}

	sfKey := host
	if r.enableIPv6 {
		sfKey += "|v6"
	}
	addrs, err := r.sf.Do(sfKey, func() ([]string, error) {
		if addrs, ok := r.cacheHit(key); ok {
			return addrs, nil
		}

		addrs, err := r.lookupHost(ctx, host)
		if err != nil {
			return nil, err
		}
		r.cache.Store(key, &cacheEntry{
			addrs:   append([]string(nil), addrs...),
			expires: time.Now().Add(r.ttl),
		})
		return addrs, nil
	})
	if err != nil {
		return nil, err
	}

	cp := make([]string, len(addrs))
	copy(cp, addrs)
	return cp, nil
}

func (r *Resolver) cacheHit(key resolverCacheKey) ([]string, bool) {
	if v, ok := r.cache.Load(key); ok {
		ce := v.(*cacheEntry)
		if time.Now().Before(ce.expires) {
			addrs := make([]string, len(ce.addrs))
			copy(addrs, ce.addrs)
			return addrs, true
		}
	}
	return nil, false
}

func (r *Resolver) lookupHost(ctx context.Context, host string) ([]string, error) {
	if !r.enableIPv6 {
		return r.lookupFamily(ctx, "ip4", host)
	}

	ctx4, cancel4 := context.WithCancel(ctx)
	defer cancel4()
	ctx6, cancel6 := context.WithCancel(ctx)
	defer cancel6()

	type result struct {
		addrs []string
		err   error
	}
	ipv4Ch := make(chan result, 1)
	ipv6Ch := make(chan result, 1)

	go func() {
		addrs, err := r.lookupFamily(ctx4, "ip4", host)
		ipv4Ch <- result{addrs: addrs, err: err}
	}()
	go func() {
		addrs, err := r.lookupFamily(ctx6, "ip6", host)
		ipv6Ch <- result{addrs: addrs, err: err}
	}()

	var ipv6 result
	ipv6Ready := false
	select {
	case ipv6 = <-ipv6Ch:
		ipv6Ready = true
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var ipv4 result
	select {
	case ipv4 = <-ipv4Ch:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if len(ipv4.addrs) > 0 {
		cancel6()
		if !ipv6Ready {
			select {
			case ipv6 = <-ipv6Ch:
				ipv6Ready = true
			default:
			}
		}
		addrs := make([]string, 0, len(ipv6.addrs)+len(ipv4.addrs))
		if ipv6Ready && len(ipv6.addrs) > 0 {
			addrs = append(addrs, ipv6.addrs...)
		}
		addrs = append(addrs, ipv4.addrs...)
		return addrs, nil
	}

	cancel4()
	if !ipv6Ready {
		select {
		case ipv6 = <-ipv6Ch:
			ipv6Ready = true
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if len(ipv6.addrs) > 0 {
		return ipv6.addrs, nil
	}
	if ipv4.err != nil {
		if ipv6.err != nil {
			return nil, fmt.Errorf("netx: DNS lookup failed for %q: %w", host, errors.Join(ipv4.err, ipv6.err))
		}
		return nil, ipv4.err
	}
	if ipv6.err != nil {
		return nil, ipv6.err
	}
	return nil, fmt.Errorf("netx: DNS lookup returned no addresses for %q", host)
}

func (r *Resolver) lookupFamily(ctx context.Context, network, host string) ([]string, error) {
	ipAddrs, err := r.resolver.LookupNetIP(ctx, network, host)
	if err != nil {
		return nil, err
	}
	addrs := make([]string, 0, len(ipAddrs))
	for _, addr := range ipAddrs {
		addrs = append(addrs, addr.String())
	}
	return addrs, nil
}

func (r *Resolver) dialResolverServer(ctx context.Context, network, _ string) (net.Conn, error) {
	if len(r.servers) == 0 {
		return nil, fmt.Errorf("netx: resolver has no configured servers")
	}
	idx := r.serverIdx.Add(1) - 1
	server := r.servers[idx%uint32(len(r.servers))]
	var d net.Dialer
	return d.DialContext(ctx, network, server)
}

func parseResolverServers(csv string) []string {
	if csv == "" {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(csv, ",") {
		server := normalizeResolverServer(raw)
		if server != "" {
			out = append(out, server)
		}
	}
	return out
}

func normalizeResolverServer(server string) string {
	server = strings.TrimSpace(server)
	if server == "" {
		return ""
	}
	if _, err := netip.ParseAddr(server); err == nil {
		return net.JoinHostPort(server, "53")
	}
	if host, port, err := net.SplitHostPort(server); err == nil {
		if host == "" || port == "" {
			return ""
		}
		return net.JoinHostPort(host, port)
	}
	return net.JoinHostPort(server, "53")
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
