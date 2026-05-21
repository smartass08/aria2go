package engine

import (
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/netrc"
)

// AuthConfig holds authentication credentials for a download request.
// Matches aria2's AuthConfig class.
type AuthConfig struct {
	user     string
	password string
}

// NewAuthConfig creates an AuthConfig. Returns nil if user is empty.
// Matches aria2's AuthConfig::create.
func NewAuthConfig(user, password string) *AuthConfig {
	if user == "" {
		return nil
	}
	return &AuthConfig{user: user, password: password}
}

// User returns the username.
func (a *AuthConfig) User() string { return a.user }

// Password returns the password.
func (a *AuthConfig) Password() string { return a.password }

// GetAuthText returns "user:password" for HTTP Basic auth.
func (a *AuthConfig) GetAuthText() string {
	return a.user + ":" + a.password
}

// basicCred stores cached HTTP auth credentials for a host/port/path triple.
// Matches aria2's BasicCred class.
type basicCred struct {
	user      string
	password  string
	host      string
	port      string
	path      string
	activated bool
}

// newBasicCred creates a basicCred. Ensures path ends with "/".
// Matches aria2's BasicCred constructor.
func newBasicCred(user, password, host, port, path string, activated bool) *basicCred {
	if path == "" || path[len(path)-1] != '/' {
		path += "/"
	}
	return &basicCred{
		user:      user,
		password:  password,
		host:      host,
		port:      port,
		path:      path,
		activated: activated,
	}
}

func (b *basicCred) less(other *basicCred) bool {
	if b.host != other.host {
		return b.host < other.host
	}
	if b.port != other.port {
		return b.port < other.port
	}
	return b.path > other.path
}

func (b *basicCred) equal(other *basicCred) bool {
	return b.host == other.host && b.port == other.port && b.path == other.path
}

// AuthConfigFactory creates AuthConfig objects from URIs and configuration.
// Matches aria2's AuthConfigFactory.
type AuthConfigFactory struct {
	mu           sync.Mutex
	netrcEntries []netrc.Entry
	netrcDefault *netrc.DefaultEntry
	basicCreds   []*basicCred
	interned     map[string]string
}

// NewAuthConfigFactory creates a new AuthConfigFactory.
func NewAuthConfigFactory() *AuthConfigFactory {
	return &AuthConfigFactory{
		interned: make(map[string]string),
	}
}

func (f *AuthConfigFactory) intern(s string) string {
	if existing, ok := f.interned[s]; ok {
		return existing
	}
	f.interned[s] = s
	return s
}

// SetNetrc sets netrc entries and default for auth resolution.
func (f *AuthConfigFactory) SetNetrc(entries []netrc.Entry, def *netrc.DefaultEntry) {
	f.netrcEntries = entries
	f.netrcDefault = def
}

// CreateAuthConfig creates an AuthConfig for the given URI using the provided
// options. Matches aria2's AuthConfigFactory::createAuthConfig.
func (f *AuthConfigFactory) CreateAuthConfig(uri string, opts *config.Options) *AuthConfig {
	f.mu.Lock()
	defer f.mu.Unlock()

	parsed, err := url.Parse(uri)
	if err != nil {
		return nil
	}

	proto := parsed.Scheme
	host := f.intern(parsed.Hostname())
	port := parsed.Port()
	if port == "" {
		if proto == "https" {
			port = "443"
		} else if proto == "http" {
			port = "80"
		} else if proto == "ftp" {
			port = "21"
		}
	}
	dir := parsed.Path
	reqUser := parsed.User.Username()
	reqPass, hasPass := parsed.User.Password()

	switch proto {
	case "http", "https":
		if opts.HTTPAuthChallenge {
			if reqUser != "" {
				bc := newBasicCred(reqUser, reqPass, host, port, dir, true)
				f.updateBasicCred(bc)
				return NewAuthConfig(reqUser, reqPass)
			}
			i := f.findBasicCred(host, port, dir)
			if i < 0 {
				return nil
			}
			return NewAuthConfig(f.basicCreds[i].user, f.basicCreds[i].password)
		}
		if reqUser != "" {
			return NewAuthConfig(reqUser, reqPass)
		}
		return f.resolveHTTPAuth(host, opts)

	case "ftp", "sftp":
		if reqUser != "" {
			if hasPass {
				return NewAuthConfig(reqUser, reqPass)
			}
			if !opts.NoNetrc {
				ac := f.resolveNetrcAuth(host, true)
				if ac != nil && ac.user == reqUser {
					return ac
				}
			}
			return NewAuthConfig(reqUser, opts.FTPPasswd)
		}
		return f.resolveFTPAuth(host, opts)
	}
	return nil
}

// resolveHTTPAuth resolves auth for HTTP protocol using netrc or
// http-user/http-passwd options. Matches aria2's createHttpAuthResolver:
// user-defined creds take priority over netrc.
func (f *AuthConfigFactory) resolveHTTPAuth(host string, opts *config.Options) *AuthConfig {
	if opts.HTTPUser != "" {
		return NewAuthConfig(opts.HTTPUser, opts.HTTPPasswd)
	}
	if opts.NoNetrc {
		return nil
	}
	return f.resolveNetrcAuth(host, true)
}

// resolveFTPAuth resolves auth for FTP protocol with anonymous default.
// User-defined creds (ftp-user/ftp-passwd) take priority over netrc.
// Matches aria2's createFtpAuthResolver pattern: setUserDefinedCred
// checked first in resolver.
func (f *AuthConfigFactory) resolveFTPAuth(host string, opts *config.Options) *AuthConfig {
	if opts.FTPUser != "" {
		return NewAuthConfig(opts.FTPUser, opts.FTPPasswd)
	}
	if opts.NoNetrc {
		return NewAuthConfig("anonymous", "ARIA2USER@")
	}
	ac := f.resolveNetrcAuth(host, false)
	if ac != nil {
		return ac
	}
	return NewAuthConfig("anonymous", "ARIA2USER@")
}

// resolveNetrcAuth looks up auth from netrc entries for the given host.
// If ignoreDefault is true, the "default" entry is skipped.
// Matches aria2's NetrcAuthResolver::resolveAuthConfig +
// findNetrcAuthenticator patterns.
func (f *AuthConfigFactory) resolveNetrcAuth(host string, ignoreDefault bool) *AuthConfig {
	for _, e := range f.netrcEntries {
		if matchHost(e.Machine, host) {
			if e.Login != "" {
				return NewAuthConfig(e.Login, e.Password)
			}
		}
	}
	if !ignoreDefault && f.netrcDefault != nil && f.netrcDefault.Login != "" {
		return NewAuthConfig(f.netrcDefault.Login, f.netrcDefault.Password)
	}
	return nil
}

// matchHost checks if a netrc machine entry matches a hostname.
// Matches aria2's Authenticator::match which uses noProxyDomainMatch.
func matchHost(machine, host string) bool {
	if machine == "" {
		return true
	}
	return strings.EqualFold(machine, host) ||
		strings.HasSuffix(host, "."+machine)
}

// updateBasicCred inserts or replaces a BasicCred. Matches aria2's
// updateBasicCred which uses lower_bound and checks equality.
func (f *AuthConfigFactory) updateBasicCred(bc *basicCred) {
	idx := sort.Search(len(f.basicCreds), func(i int) bool {
		return !f.basicCreds[i].less(bc)
	})
	if idx < len(f.basicCreds) && f.basicCreds[idx].equal(bc) {
		f.basicCreds[idx] = bc
		return
	}
	f.basicCreds = append(f.basicCreds, nil)
	copy(f.basicCreds[idx+1:], f.basicCreds[idx:])
	f.basicCreds[idx] = bc
}

// findBasicCred finds a BasicCred matching host, port, and path prefix.
// Returns the index or -1 if not found. Matches aria2's findBasicCred.
func (f *AuthConfigFactory) findBasicCred(host, port, path string) int {
	searchPath := path
	if searchPath == "" || searchPath[len(searchPath)-1] != '/' {
		searchPath += "/"
	}
	bc := newBasicCred("", "", host, port, searchPath, false)
	idx := sort.Search(len(f.basicCreds), func(i int) bool {
		return !f.basicCreds[i].less(bc)
	})
	for i := idx; i < len(f.basicCreds); i++ {
		c := f.basicCreds[i]
		if c.host != host || c.port != port {
			break
		}
		if strings.HasPrefix(searchPath, c.path) {
			return i
		}
	}
	return -1
}
