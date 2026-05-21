package magnet

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/encoding/base32"
)

var (
	keyXT  = [2]byte{'x', 't'}
	keyDN  = [2]byte{'d', 'n'}
	keyXL  = [2]byte{'x', 'l'}
	keyTR  = [2]byte{'t', 'r'}
	keyXS  = [2]byte{'x', 's'}
	keyAS  = [2]byte{'a', 's'}
	keyMT  = [2]byte{'m', 't'}
	keyKT  = [2]byte{'k', 't'}
	keyXPE = [4]byte{'x', '.', 'p', 'e'}
)

var prefixBTIH = [9]byte{'u', 'r', 'n', ':', 'b', 't', 'i', 'h', ':'}
var prefixBTMH = [9]byte{'u', 'r', 'n', ':', 'b', 't', 'm', 'h', ':'}

const hexTable = "0123456789ABCDEF"

func bytePrefixFold(b []byte, prefix []byte) bool {
	if len(b) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		cb, cp := b[i], prefix[i]
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if cp >= 'A' && cp <= 'Z' {
			cp += 32
		}
		if cb != cp {
			return false
		}
	}
	return true
}

func byteEqFold(b []byte, key []byte) bool {
	if len(b) != len(key) {
		return false
	}
	for i := 0; i < len(key); i++ {
		cb, ck := b[i], key[i]
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ck >= 'A' && ck <= 'Z' {
			ck += 32
		}
		if cb != ck {
			return false
		}
	}
	return true
}

func percentDecodeBytes(dst []byte, b []byte) []byte {
	for i := 0; i < len(b); i++ {
		if b[i] == '%' && i+2 < len(b) && isHexDigit(b[i+1]) && isHexDigit(b[i+2]) {
			dst = append(dst, unhex(b[i+1])<<4|unhex(b[i+2]))
			i += 2
		} else {
			dst = append(dst, b[i])
		}
	}
	return dst
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return 0
}

func magnetPercentEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~',
			c == ':', c == '/', c == '?', c == '@',
			c == '!', c == '$', c == '\'', c == '(',
			c == ')', c == '*', c == '+', c == ',',
			c == ';', c == '[', c == ']':
			b.WriteByte(c)
		case c == ' ':
			b.WriteString("%20")
		default:
			b.WriteByte('%')
			b.WriteByte(hexTable[c>>4])
			b.WriteByte(hexTable[c&0x0F])
		}
	}
	return b.String()
}

type Magnet struct {
	InfoHashV1        *core.InfoHashV1
	InfoHashV2        *core.InfoHashV2
	DisplayName       string
	Length            int64
	Trackers          []string
	Peers             []string
	AcceptableSources []string
	ExactSources      []string
	ManifestTopics    []string
	KeywordTopics     []string
}

type Error struct {
	Code core.ErrorCode
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("magnet: %s", e.Err) }
func (e *Error) Unwrap() error { return e.Err }

func (e *Error) Is(target error) bool {
	var t *Error
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

func Parse(raw string) (*Magnet, error) {
	const prefix = "magnet:?"
	if !strings.HasPrefix(raw, prefix) {
		return nil, &Error{Code: core.ExitMagnetParseError, Err: fmt.Errorf("URI must start with %q", prefix)}
	}

	query := raw[len(prefix):]
	if query == "" {
		return nil, &Error{Code: core.ExitMagnetParseError, Err: errors.New("empty query string")}
	}

	m := &Magnet{}
	var hasXT bool
	var hasValidXT bool
	var decodeBuf []byte
	qs := []byte(query)

	for len(qs) > 0 {
		end := 0
		for end < len(qs) && qs[end] != '&' {
			end++
		}
		pair := qs[:end]
		if end < len(qs) {
			qs = qs[end+1:]
		} else {
			qs = nil
		}

		if len(pair) == 0 {
			continue
		}

		eq := 0
		for eq < len(pair) && pair[eq] != '=' {
			eq++
		}

		var keyBytes []byte
		var rawValBytes []byte
		if eq >= len(pair) {
			keyBytes = pair
			rawValBytes = nil
		} else {
			keyBytes = pair[:eq]
			rawValBytes = pair[eq+1:]
		}

		var val string
		if len(rawValBytes) == 0 {
			val = ""
		} else {
			hasPct := false
			for _, c := range rawValBytes {
				if c == '%' {
					hasPct = true
					break
				}
			}
			if !hasPct {
				val = string(rawValBytes)
			} else {
				decodeBuf = decodeBuf[:0]
				decodeBuf = percentDecodeBytes(decodeBuf, rawValBytes)
				val = string(decodeBuf)
			}
		}

		switch {
		case byteEqFold(keyBytes, keyXT[:]):
			hasXT = true
			recognized, err := m.parseXTBytes(val)
			if err != nil {
				return nil, err
			}
			if recognized {
				hasValidXT = true
			}
		case byteEqFold(keyBytes, keyDN[:]):
			m.DisplayName = val
		case byteEqFold(keyBytes, keyXL[:]):
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return nil, &Error{Code: core.ExitMagnetParseError, Err: fmt.Errorf("invalid xl value %q: %w", val, err)}
			}
			m.Length = n
		case byteEqFold(keyBytes, keyTR[:]):
			m.Trackers = append(m.Trackers, val)
		case byteEqFold(keyBytes, keyXS[:]):
			m.ExactSources = append(m.ExactSources, val)
		case byteEqFold(keyBytes, keyAS[:]):
			m.AcceptableSources = append(m.AcceptableSources, val)
		case byteEqFold(keyBytes, keyXPE[:]):
			m.Peers = append(m.Peers, val)
		case byteEqFold(keyBytes, keyMT[:]):
			m.ManifestTopics = append(m.ManifestTopics, val)
		case byteEqFold(keyBytes, keyKT[:]):
			m.KeywordTopics = append(m.KeywordTopics, val)
		}
	}

	if !hasXT {
		return nil, &Error{Code: core.ExitMagnetParseError, Err: errors.New("missing required xt parameter")}
	}
	if !hasValidXT {
		return nil, &Error{Code: core.ExitMagnetParseError, Err: errors.New("no valid BitTorrent xt parameter")}
	}

	return m, nil
}

func (m *Magnet) parseXTBytes(val string) (bool, error) {
	switch {
	case bytePrefixFold([]byte(val), prefixBTIH[:]):
		hash := val[len("urn:btih:"):]
		if len(hash) == 0 {
			return true, &Error{Code: core.ExitMagnetParseError, Err: fmt.Errorf("empty btih hash in %q", val)}
		}
		return true, m.parseInfoHashV1(hash)
	case bytePrefixFold([]byte(val), prefixBTMH[:]):
		hash := val[len("urn:btmh:"):]
		if len(hash) == 0 {
			return true, &Error{Code: core.ExitMagnetParseError, Err: fmt.Errorf("empty btmh hash in %q", val)}
		}
		return true, m.parseInfoHashV2(hash)
	default:
		return false, nil
	}
}

func (m *Magnet) parseInfoHashV1(s string) error {
	if m.InfoHashV1 != nil {
		return nil
	}

	if len(s) == 40 {
		h, err := core.ParseInfoHashV1(s)
		if err != nil {
			return &Error{Code: core.ExitMagnetParseError, Err: fmt.Errorf("invalid v1 infohash %q: %w", s, err)}
		}
		m.InfoHashV1 = &h
		return nil
	}

	if len(s) == 32 {
		dec, err := base32.Decode(s)
		if err != nil {
			return &Error{Code: core.ExitMagnetParseError, Err: fmt.Errorf("invalid base32 v1 infohash %q: %w", s, err)}
		}
		var h core.InfoHashV1
		copy(h[:], dec)
		m.InfoHashV1 = &h
		return nil
	}

	return &Error{Code: core.ExitMagnetParseError, Err: fmt.Errorf("v1 infohash must be 40 hex or 32 base32 chars, got %d", len(s))}
}

func (m *Magnet) parseInfoHashV2(s string) error {
	if m.InfoHashV2 != nil {
		return nil
	}

	if len(s) > 4 && isMultihashHexPrefix(s[:4]) && len(s) == 68 {
		s = s[4:]
	}

	h, err := core.ParseInfoHashV2(s)
	if err != nil {
		return &Error{Code: core.ExitMagnetParseError, Err: fmt.Errorf("invalid v2 infohash %q: %w", s, err)}
	}
	m.InfoHashV2 = &h
	return nil
}

func isMultihashHexPrefix(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func hexEncodeUpperBytes(dst []byte, src []byte) []byte {
	for _, b := range src {
		dst = append(dst, hexTable[b>>4], hexTable[b&0x0F])
	}
	return dst
}

func (m *Magnet) String() string {
	var b strings.Builder
	b.Grow(128)
	b.WriteString("magnet:?")

	first := true
	var hexBuf [64]byte

	writeParam := func(key, val string) {
		if !first {
			b.WriteByte('&')
		}
		first = false
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(magnetPercentEncode(val))
	}

	if m.InfoHashV1 != nil {
		h := hexEncodeUpperBytes(hexBuf[:0], m.InfoHashV1[:])
		b.WriteString("xt=urn:btih:")
		b.Write(h)
		first = false
	}
	if m.InfoHashV2 != nil {
		if !first {
			b.WriteByte('&')
		}
		h := hexEncodeUpperBytes(hexBuf[:0], m.InfoHashV2[:])
		b.WriteString("xt=urn:btmh:")
		b.Write(h)
		first = false
	}

	if m.DisplayName != "" {
		writeParam("dn", m.DisplayName)
	}
	if m.Length > 0 {
		writeParam("xl", strconv.FormatInt(m.Length, 10))
	}
	for _, t := range m.Trackers {
		writeParam("tr", t)
	}
	for _, s := range m.ExactSources {
		writeParam("xs", s)
	}
	for _, s := range m.AcceptableSources {
		writeParam("as", s)
	}
	for _, p := range m.Peers {
		writeParam("x.pe", p)
	}
	for _, t := range m.ManifestTopics {
		writeParam("mt", t)
	}
	for _, t := range m.KeywordTopics {
		writeParam("kt", t)
	}

	return b.String()
}
