// Package cookies implements http.CookieJar for Netscape cookies.txt format.
// It does not enforce a Public Suffix List, matching aria2's permissive
// cookie domain matching behavior.
package cookies

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sentinel value for session cookies (expiry=0 means never expires).
// Uses 1<<62 (~146 billion years from epoch) to avoid time.Time overflow.
var sessionExpiry = time.Unix(1<<62, 0)

var cookiePool = sync.Pool{
	New: func() any { return new(http.Cookie) },
}

var cookieEntryPool = sync.Pool{
	New: func() any { return &cookieEntry{} },
}

type cookieEntry struct {
	c            *http.Cookie
	creationTime time.Time
}

// Jar implements http.CookieJar for Netscape cookies.txt format.
// No Public Suffix List enforcement (matches aria2 behavior).
// Thread-safe via RWMutex.
type Jar struct {
	mu      sync.RWMutex
	entries []*cookieEntry
}

// New returns a new empty Jar.
func New() *Jar {
	return &Jar{}
}

// LoadNetscape reads Netscape-format cookies.txt from r.
// Format: domain\tflag\tpath\tsecure\texpires\tname\tvalue
// Lines starting with # are comments. Empty lines are skipped.
// Accepts 6+ tab-separated fields; value defaults to "" when only 6 fields.
// Lines with empty domain, empty name, or invalid path are silently skipped.
func (j *Jar) LoadNetscape(r io.Reader) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	sc := bufio.NewScanner(r)
	lineNum := 0
	j.entries = nil

	for sc.Scan() {
		lineNum++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 6 {
			continue
		}

		domain := strings.TrimLeft(fields[0], ".")
		flag := strings.ToUpper(fields[1]) == "TRUE"
		path := fields[2]
		secure := strings.ToUpper(fields[3]) == "TRUE"
		expiresStr := fields[4]
		name := fields[5]
		value := ""
		if len(fields) >= 7 {
			value = fields[6]
		}

		// Check for empty domain or empty name or invalid path
		if domain == "" || name == "" || !goodPath(path) {
			continue
		}

		expiryFloat, err := strconv.ParseFloat(expiresStr, 64)
		if err != nil {
			continue
		}
		expiry := int64(expiryFloat)

		c := cookiePool.Get().(*http.Cookie)
		c.Name = name
		c.Value = value
		c.Path = path
		c.Domain = domain
		c.Secure = secure
		c.HttpOnly = false // Netscape format has no HttpOnly flag

		if expiry == 0 {
			// Session cookie: never expires
			c.Expires = sessionExpiry
		} else {
			c.Expires = time.Unix(expiry, 0)
		}

		// flag=TRUE means the cookie can be sent to any host in the domain.
		// flag=FALSE means the cookie is host-only.
		// Numeric hosts (IP addresses) are always host-only regardless of flag.
		if flag && !isNumericHost(domain) {
			if !strings.HasPrefix(domain, ".") {
				c.Domain = "." + domain
			}
		}
		// flag=FALSE or numeric host: host-only; keep domain as-is for exact match.

		e := cookieEntryPool.Get().(*cookieEntry)
		e.c = c
		e.creationTime = now
		j.entries = append(j.entries, e)
	}

	return sc.Err()
}

// goodPath requires the path to be non-empty and start with '/'.
func goodPath(p string) bool {
	return len(p) > 0 && p[0] == '/'
}

// isNumericHost checks if host is an IP address (IPv4 or IPv6).
func isNumericHost(host string) bool {
	return net.ParseIP(host) != nil
}

// SaveNetscape writes cookies in Netscape format to w.
// Output is sorted by (domain, path, name) for deterministic output.
func (j *Jar) SaveNetscape(w io.Writer) error {
	j.mu.RLock()
	defer j.mu.RUnlock()

	var buf strings.Builder
	buf.WriteString("# Netscape HTTP Cookie File\n")
	buf.WriteString("# This is a generated file!  Do not edit.\n")
	buf.WriteString("\n")

	// Sort by (domain, path, name)
	sorted := make([]*cookieEntry, len(j.entries))
	copy(sorted, j.entries)
	sort.Slice(sorted, func(i, k int) bool {
		a, b := sorted[i].c, sorted[k].c
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Name < b.Name
	})

	for _, e := range sorted {
		c := e.c
		domain := c.Domain
		flag := "TRUE"
		if !strings.HasPrefix(domain, ".") {
			flag = "FALSE"
		}

		secure := "FALSE"
		if c.Secure {
			secure = "TRUE"
		}

		expires := int64(0)
		if c.Expires.Equal(sessionExpiry) {
			expires = 0
		} else if !c.Expires.IsZero() {
			expires = c.Expires.Unix()
		}

		buf.WriteString(domain)
		buf.WriteByte('\t')
		buf.WriteString(flag)
		buf.WriteByte('\t')
		buf.WriteString(c.Path)
		buf.WriteByte('\t')
		buf.WriteString(secure)
		buf.WriteByte('\t')
		buf.WriteString(strconv.FormatInt(expires, 10))
		buf.WriteByte('\t')
		buf.WriteString(c.Name)
		buf.WriteByte('\t')
		buf.WriteString(c.Value)
		buf.WriteByte('\n')
	}

	_, err := io.WriteString(w, buf.String())
	return err
}

// Cookies implements http.CookieJar. It returns cookies that match the given
// URL by domain suffix and path prefix, excluding expired and secure-only
// cookies on non-HTTPS requests. Results are sorted per RFC 6265 §5.4:
// longer paths first, earlier creation times first.
func (j *Jar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.RLock()
	defer j.mu.RUnlock()

	now := time.Now()
	host := u.Hostname()
	matched := make([]*cookieEntry, 0, len(j.entries))

	for _, e := range j.entries {
		c := e.c
		if !c.Expires.IsZero() && c.Expires.Before(now) {
			continue
		}

		if c.Secure && u.Scheme != "https" {
			continue
		}

		if !domainMatch(c.Domain, host) {
			continue
		}

		if !pathMatch(c.Path, u.Path) {
			continue
		}

		matched = append(matched, e)
	}

	// Sort per RFC 6265 §5.4: longer paths first, earlier creation times first
	sort.Slice(matched, func(i, k int) bool {
		a, b := matched[i], matched[k]
		ad, bd := pathDepth(a.c.Path), pathDepth(b.c.Path)
		if ad != bd {
			return ad > bd
		}
		return a.creationTime.Before(b.creationTime)
	})

	result := make([]*http.Cookie, len(matched))
	for i, e := range matched {
		c := cookiePool.Get().(*http.Cookie)
		c.Name = e.c.Name
		c.Value = e.c.Value
		result[i] = c
	}
	return result
}

// pathDepth returns the depth of a path as counted by aria2.
// Follows the C++ CookiePathDivider logic: each path component adds 1.
// '/foo/bar' returns 2, '/foo/bar/' returns 2, '/' returns 0.
func pathDepth(p string) int {
	if len(p) == 0 {
		return 0
	}
	depth := 0
	for i := 1; i < len(p); i++ {
		if p[i] == '/' && p[i-1] != '/' {
			depth++
		}
	}
	if p[len(p)-1] != '/' {
		depth++
	}
	return depth
}

// SetCookies implements http.CookieJar. It stores cookies from Set-Cookie
// headers, respecting domain, path, secure, expires, and max-age.
func (j *Jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	host := u.Hostname()

	for _, c := range cookies {
		// Strip leading dots from Domain attribute (C++ parse() lstrip).
		cookieDomain := strings.TrimLeft(c.Domain, ".")
		if !validCookieDomain(cookieDomain, host) {
			continue
		}

		domain := cookieDomain
		if domain == "" {
			domain = host
		} else if isNumericHost(host) {
			domain = host
		} else if !strings.HasPrefix(domain, ".") {
			domain = "." + domain
		}

		// Path defaults to the path of the request URL
		path := c.Path
		if path == "" {
			path = "/"
			if u.Path != "" && u.Path != "/" {
				p := u.Path
				if idx := strings.LastIndex(p, "/"); idx > 0 {
					path = p[:idx]
				}
			}
		}

		// MaxAge handling
		if c.MaxAge < 0 {
			continue // delete immediately
		}
		if c.MaxAge > 0 {
			c.Expires = now.Add(time.Duration(c.MaxAge) * time.Second)
		}

		if c.MaxAge == 0 && c.Expires.IsZero() && c.Raw == "" && c.RawExpires == "" {
			j.entries = deleteCookie(j.entries, domain, path, c.Name)
			continue
		}

		// Explicit past expiry means delete
		if !c.Expires.IsZero() && c.Expires.Before(now) {
			j.entries = deleteCookie(j.entries, domain, path, c.Name)
			continue
		}

		// Session cookie: no Expires set and not a delete
		if c.Expires.IsZero() {
			c.Expires = sessionExpiry
		}

		cookie := cookiePool.Get().(*http.Cookie)
		cookie.Name = c.Name
		cookie.Value = c.Value
		cookie.Path = path
		cookie.Domain = domain
		cookie.Expires = c.Expires
		cookie.Secure = c.Secure
		cookie.HttpOnly = c.HttpOnly

		// Replace existing cookie with same domain+path+name
		j.entries = deleteCookie(j.entries, domain, path, c.Name)
		e := cookieEntryPool.Get().(*cookieEntry)
		e.c = cookie
		e.creationTime = now
		j.entries = append(j.entries, e)
	}
}

// domainMatch checks if cookieDomain matches host.
// Per RFC 6265 §5.1.3 without PSL enforcement.
// IP addresses never match subdomains (per aria2's !isNumericHost check).
func domainMatch(cookieDomain, host string) bool {
	if cookieDomain == host {
		return true
	}

	if isNumericHost(host) {
		return false
	}

	if strings.HasPrefix(cookieDomain, ".") {
		// .example.com matches example.com and *.example.com
		suffix := cookieDomain
		if strings.HasSuffix(host, suffix) {
			return true
		}
		// Also match the domain without the leading dot
		if host == cookieDomain[1:] {
			return true
		}
	} else {
		// Exact match only (host-only)
		return cookieDomain == host
	}

	return false
}

// pathMatch checks if cookiePath is a prefix of requestPath.
// Per RFC 6265 §5.1.4.
func pathMatch(cookiePath, requestPath string) bool {
	if cookiePath == requestPath {
		return true
	}
	if strings.HasPrefix(requestPath, cookiePath) {
		if strings.HasSuffix(cookiePath, "/") {
			return true
		}
		if len(requestPath) > len(cookiePath) && requestPath[len(cookiePath)] == '/' {
			return true
		}
	}
	return false
}

// validCookieDomain checks if the cookie's Domain attribute is valid for the
// given host per RFC 6265 §5.1.3. The domain parameter has already had leading
// dots stripped (matching C++ cookie_helper::parse behavior). Validation checks
// exact match or suffix match with a dot separator (and host must not be numeric).
func validCookieDomain(domain, host string) bool {
	if domain == "" {
		return true
	}
	if domain == host {
		return true
	}
	// Suffix match: domain (already stripped of leading dots) matches the
	// end of host, and the boundary is a '.' separator. Equivalent to C++
	// domainMatch(host, strippedDomain) which uses util::endsWith.
	if strings.HasSuffix(host, domain) {
		prefixLen := len(host) - len(domain)
		if prefixLen > 0 && host[prefixLen-1] == '.' && !isNumericHost(host) {
			return true
		}
	}
	return false
}

// deleteCookie removes a cookie with the given domain, path, and name
// from the slice. Returns the filtered slice.
func deleteCookie(entries []*cookieEntry, domain, path, name string) []*cookieEntry {
	n := 0
	for _, e := range entries {
		c := e.c
		if c.Domain == domain && c.Path == path && c.Name == name {
			continue
		}
		entries[n] = e
		n++
	}
	return entries[:n]
}
