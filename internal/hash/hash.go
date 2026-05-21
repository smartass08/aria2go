// Package hash provides a unified Kind abstraction over stdlib crypto hash
// functions (md5, sha-1, sha-224, sha-256, sha-384, sha-512).  It matches
// the hash algorithm naming conventions used by aria2.
package hash

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"strings"
	"sync"
)

// Kind is a string identifier for a hash algorithm.  Valid values are the
// exported constants md5, sha-1, sha-224, sha-256, sha-384, sha-512.
type Kind string

const (
	MD5    Kind = "md5"
	SHA1   Kind = "sha-1"
	SHA224 Kind = "sha-224"
	SHA256 Kind = "sha-256"
	SHA384 Kind = "sha-384"
	SHA512 Kind = "sha-512"
)

var allKinds = [...]Kind{MD5, SHA1, SHA224, SHA256, SHA384, SHA512}

var sizeTab = [...]int{
	0: md5.Size,
	1: sha1.Size,
	2: sha256.Size224,
	3: sha256.Size,
	4: sha512.Size384,
	5: sha512.Size,
}

var kindIndex = map[string]int{
	"md5":    0,
	"sha1":   1,
	"sha224": 2,
	"sha256": 3,
	"sha384": 4,
	"sha512": 5,
}

var pools = [...]sync.Pool{
	{New: func() any { return md5.New() }},
	{New: func() any { return sha1.New() }},
	{New: func() any { return sha256.New224() }},
	{New: func() any { return sha256.New() }},
	{New: func() any { return sha512.New384() }},
	{New: func() any { return sha512.New() }},
}

func kindIdx(k Kind) int {
	switch k {
	case MD5:
		return 0
	case SHA1:
		return 1
	case SHA224:
		return 2
	case SHA256:
		return 3
	case SHA384:
		return 4
	case SHA512:
		return 5
	default:
		return -1
	}
}

// New returns a fresh hash.Hash for the given Kind. The returned hash is
// obtained from an internal sync.Pool, reset, and ready for use. Callers
// should call PoolPut when done to return the hash to the pool for reuse.
func New(k Kind) (hash.Hash, error) {
	idx := kindIdx(k)
	if idx < 0 {
		return nil, fmt.Errorf("hash: unknown kind %q", k)
	}
	h := pools[idx].Get().(hash.Hash)
	h.Reset()
	return h, nil
}

// PoolPut returns a hash.Hash obtained via New back to its internal pool
// for reuse. k must be the same Kind passed to New.
func PoolPut(k Kind, h hash.Hash) {
	idx := kindIdx(k)
	if idx >= 0 {
		pools[idx].Put(h)
	}
}

// Parse normalises a user-supplied hash name string to a Kind.  Matching is
// case-insensitive and dash-insensitive: "SHA-1", "sha1", "Sha1" all map to
// SHA1.  An error is returned when the name is not recognised.
func Parse(s string) (Kind, error) {
	normalised := strings.ToLower(s)
	normalised = strings.ReplaceAll(normalised, "-", "")
	idx, ok := kindIndex[normalised]
	if !ok {
		return "", fmt.Errorf("hash: unknown hash name %q", s)
	}
	return allKinds[idx], nil
}

// Size returns the digest size in bytes for the hash algorithm.
func (k Kind) Size() int {
	idx := kindIdx(k)
	if idx < 0 {
		return 0
	}
	return sizeTab[idx]
}
