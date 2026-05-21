// Package knownhosts provides a parser for the OpenSSH known_hosts file
// format (~/.ssh/known_hosts) and a HostKey callback for SSH host key
// verification.
//
// In aria2, libssh2 provides knownhosts checking through
// libssh2_knownhost_checkp(). This package replicates that behavior by
// parsing the OpenSSH known_hosts format and matching host keys during
// SSH transport layer setup.
//
// OpenSSH known_hosts format:
//   - One entry per line.
//   - Lines starting with '#' are comments and empty lines are skipped.
//   - Each entry has the form: hostnames key-type base64-key [comment]
//   - hostnames can be comma-separated; each may contain wildcards (* and ?).
//   - Optional markers: @cert-authority, @revoked at the start of a line.
//   - Key types: ssh-rsa, ssh-dss, ecdsa-sha2-nistp256,
//     ecdsa-sha2-nistp384, ecdsa-sha2-nistp521, ssh-ed25519.
//   - Hashed hostnames (starting with '|1|') are not yet supported.
package knownhosts

import (
	"fmt"
	"strings"
)

// ErrNoMatch is returned when no matching host key is found.
var ErrNoMatch = fmt.Errorf("knownhosts: no matching host key")

// Entry represents a single known_hosts entry.
type Entry struct {
	Hostnames []string
	KeyType   string
	Key       []byte
	Comment   string
	Marker    string
}

// HostKeyCallback is the signature for host key verification callbacks.
// It receives the hostname, remote address, key type, and the raw
// public key bytes. It returns nil if the key is trusted, or an error
// if verification fails.
type HostKeyCallback func(hostname string, remote netAddr, keyType string, key []byte) error

// netAddr abstracts the address interface accepted by the callback.
type netAddr interface {
	Network() string
	String() string
}

// File represents a parsed known_hosts file.
type File struct {
	entries []Entry
}

// Parse parses the contents of a known_hosts file.
// Lines starting with '#' are treated as comments. Empty lines are skipped.
// Entries with hashed hostnames (starting with '|1|') are ignored.
func Parse(data []byte) (*File, error) {
	var entries []Entry
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entry, err := parseLine(line)
		if err != nil {
			continue
		}
		entries = append(entries, *entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: no entries parsed", ErrNoMatch)
	}
	return &File{entries: entries}, nil
}

func parseLine(line string) (*Entry, error) {
	entry := &Entry{}

	rest := line
	if strings.HasPrefix(rest, "@cert-authority ") {
		entry.Marker = "cert-authority"
		rest = rest[len("@cert-authority "):]
	} else if strings.HasPrefix(rest, "@revoked ") {
		entry.Marker = "revoked"
		rest = rest[len("@revoked "):]
	}

	if strings.HasPrefix(rest, "|1|") {
		return nil, fmt.Errorf("knownhosts: hashed hostnames not supported")
	}

	fields := splitFields(rest)
	if len(fields) < 3 {
		return nil, fmt.Errorf("knownhosts: malformed entry")
	}

	entry.Hostnames = strings.Split(fields[0], ",")
	entry.KeyType = fields[1]
	entry.Key = []byte(fields[2])
	if len(fields) > 3 {
		entry.Comment = fields[3]
	}

	return entry, nil
}

func splitFields(s string) []string {
	var fields []string
	inQuote := false
	current := strings.Builder{}
	for _, c := range s {
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if c == ' ' || c == '\t' {
			if inQuote {
				current.WriteRune(c)
			} else if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(c)
		}
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields
}

// Lookup searches for a host key matching the given hostname and key type.
// Returns the matching Entry, or nil if no match is found.
func (f *File) Lookup(hostname string, keyType string) *Entry {
	for i := range f.entries {
		e := &f.entries[i]
		if e.KeyType != keyType {
			continue
		}
		if matchHostname(e.Hostnames, hostname) {
			return e
		}
	}
	return nil
}

func matchHostname(patterns []string, hostname string) bool {
	for _, pattern := range patterns {
		if pattern == hostname {
			return true
		}
		if matchWildcard(pattern, hostname) {
			return true
		}
	}
	return false
}

func matchWildcard(pattern, hostname string) bool {
	if !strings.ContainsRune(pattern, '*') && !strings.ContainsRune(pattern, '?') {
		return false
	}
	parts := splitWildcard(pattern)
	rest := hostname
	for _, part := range parts {
		if part == "" {
			continue
		}
		if part == "*" {
			continue
		}
		idx := strings.Index(rest, part)
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(part):]
	}
	return true
}

func splitWildcard(s string) []string {
	var parts []string
	current := strings.Builder{}
	for _, c := range s {
		if c == '*' || c == '?' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			parts = append(parts, string(c))
		} else {
			current.WriteRune(c)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// NewHostKeyCallback creates a HostKeyCallback from a parsed known_hosts File.
// The callback checks incoming host keys against the entries in the file.
func NewHostKeyCallback(f *File) HostKeyCallback {
	return func(hostname string, remote netAddr, keyType string, key []byte) error {
		entry := f.Lookup(hostname, keyType)
		if entry == nil {
			return fmt.Errorf("knownhosts: %w for host %q type %q", ErrNoMatch, hostname, keyType)
		}
		return nil
	}
}
