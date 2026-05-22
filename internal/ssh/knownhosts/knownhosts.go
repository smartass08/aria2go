// Package knownhosts provides a lenient known_hosts parser and callback for
// SFTP host-key verification.
package knownhosts

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	xssh "golang.org/x/crypto/ssh"
)

var (
	ErrNoMatch     = errors.New("knownhosts: no matching host key")
	ErrKeyMismatch = errors.New("knownhosts: host key mismatch")
	ErrRevoked     = errors.New("knownhosts: revoked host key")
)

// HostKeyCallback verifies a host key blob against parsed known_hosts entries.
type HostKeyCallback func(hostname string, remote netAddr, keyType string, key []byte) error

type netAddr interface {
	Network() string
	String() string
}

// Entry is a single known_hosts line.
type Entry struct {
	Marker  string
	Hosts   []string
	KeyType string
	Key     []byte
}

// File holds parsed known_hosts entries.
type File struct {
	entries []Entry
}

// New loads a known_hosts file from path.
func New(path string) (HostKeyCallback, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	file, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return NewHostKeyCallback(file), nil
}

// Parse parses known_hosts contents, ignoring malformed lines to match the
// repository's existing lenient behavior.
func Parse(data []byte) (*File, error) {
	var entries []Entry
	for len(data) > 0 {
		marker, hosts, pubKey, _, rest, err := xssh.ParseKnownHosts(data)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			_, rest = consumeLine(data)
			data = rest
			continue
		}
		entries = append(entries, Entry{
			Marker:  marker,
			Hosts:   append([]string(nil), hosts...),
			KeyType: pubKey.Type(),
			Key:     append([]byte(nil), pubKey.Marshal()...),
		})
		data = rest
	}
	if len(entries) == 0 {
		return nil, ErrNoMatch
	}
	return &File{entries: entries}, nil
}

func consumeLine(data []byte) ([]byte, []byte) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return data[:i], data[i+1:]
	}
	return data, nil
}

// KeyType returns the SSH key format encoded in hostKeyBlob.
func KeyType(hostKeyBlob []byte) (string, error) {
	pubKey, err := xssh.ParsePublicKey(hostKeyBlob)
	if err != nil {
		return "", err
	}
	return pubKey.Type(), nil
}

// HostPort formats host and port using OpenSSH's bracket rules.
func HostPort(host string, port int) string {
	if port == 22 || port == 0 {
		return strings.Trim(host, "[]")
	}
	host = strings.Trim(host, "[]")
	return "[" + host + "]:" + strconv.Itoa(port)
}

// NewHostKeyCallback builds a host-key verifier over the parsed file.
func NewHostKeyCallback(f *File) HostKeyCallback {
	return func(hostname string, _ netAddr, keyType string, key []byte) error {
		hostname = normalizeHost(hostname)
		var sawHost bool
		for _, entry := range f.entries {
			if !matchPatterns(entry.Hosts, hostname) {
				continue
			}
			sawHost = true
			if entry.Marker == "revoked" {
				if entry.KeyType == keyType && bytes.Equal(entry.Key, key) {
					return fmt.Errorf("%w for %s", ErrRevoked, hostname)
				}
				continue
			}
			if entry.KeyType == keyType && bytes.Equal(entry.Key, key) {
				return nil
			}
		}
		if sawHost {
			return fmt.Errorf("%w for %s (%s)", ErrKeyMismatch, hostname, keyType)
		}
		return fmt.Errorf("%w for %s", ErrNoMatch, hostname)
	}
}

func matchPatterns(patterns []string, hostname string) bool {
	matched := false
	for _, pattern := range patterns {
		negate := strings.HasPrefix(pattern, "!")
		if negate {
			pattern = pattern[1:]
		}
		if !matchPattern(pattern, hostname) {
			continue
		}
		if negate {
			return false
		}
		matched = true
	}
	return matched
}

func matchPattern(pattern, hostname string) bool {
	if strings.HasPrefix(pattern, "|1|") {
		return matchHashedHost(pattern, hostname)
	}
	return wildcardMatch([]byte(pattern), []byte(hostname))
}

func matchHashedHost(pattern, hostname string) bool {
	parts := strings.Split(pattern, "|")
	if len(parts) != 4 || parts[1] != "1" {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(normalizeHost(hostname)))
	return hmac.Equal(mac.Sum(nil), want)
}

func normalizeHost(hostname string) string {
	if host, port, err := net.SplitHostPort(hostname); err == nil {
		host = strings.Trim(host, "[]")
		if port == "22" {
			return host
		}
		return "[" + host + "]:" + port
	}
	return strings.Trim(hostname, "[]")
}

func wildcardMatch(pattern, s []byte) bool {
	for {
		if len(pattern) == 0 {
			return len(s) == 0
		}
		if len(s) == 0 {
			for _, b := range pattern {
				if b != '*' {
					return false
				}
			}
			return true
		}
		switch pattern[0] {
		case '*':
			if len(pattern) == 1 {
				return true
			}
			for i := range s {
				if wildcardMatch(pattern[1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			pattern = pattern[1:]
			s = s[1:]
		default:
			if pattern[0] != s[0] {
				return false
			}
			pattern = pattern[1:]
			s = s[1:]
		}
	}
}
