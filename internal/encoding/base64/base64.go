package base64

import (
	"fmt"
)

var encTable = [64]byte{
	'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
	'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
	'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
	'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '+', '/',
}

var decTable [256]int

func init() {
	for i := range decTable {
		decTable[i] = -1
	}
	for i, c := range encTable {
		decTable[c] = i
	}
}

// Encode encodes src to standard base64 with = padding.
func Encode(src []byte) string {
	if len(src) == 0 {
		return ""
	}
	outLen := ((len(src) + 2) / 3) * 4
	out := make([]byte, outLen)
	pos := 0
	i := 0
	j := (len(src) / 3) * 3
	for i < j {
		n := uint32(src[i])<<16 | uint32(src[i+1])<<8 | uint32(src[i+2])
		out[pos] = encTable[n>>18]
		out[pos+1] = encTable[(n>>12)&0x3f]
		out[pos+2] = encTable[(n>>6)&0x3f]
		out[pos+3] = encTable[n&0x3f]
		pos += 4
		i += 3
	}
	r := len(src) % 3
	if r == 2 {
		n := uint32(src[i])<<16 | uint32(src[i+1])<<8
		out[pos] = encTable[n>>18]
		out[pos+1] = encTable[(n>>12)&0x3f]
		out[pos+2] = encTable[(n>>6)&0x3f]
		out[pos+3] = '='
	} else if r == 1 {
		n := uint32(src[i]) << 16
		out[pos] = encTable[n>>18]
		out[pos+1] = encTable[(n>>12)&0x3f]
		out[pos+2] = '='
		out[pos+3] = '='
	}
	return string(out)
}

// ErrInvalidChar is returned when decoding encounters invalid base64 input.
var ErrInvalidChar = fmt.Errorf("base64: invalid character in input")

// Decode decodes a standard base64 string with optional whitespace/newline skipping.
// It matches aria2's lenient decoding: non-base64 characters are silently skipped.
func Decode(s string) ([]byte, error) {
	if len(s) == 0 {
		return nil, nil
	}
	out := make([]byte, 0, len(s)/4*3)
	k := [4]byte{}
	eq := 0
	pos := 0
	for pos < len(s) {
		eq = 0
		for i := 0; i < 4; i++ {
			nextPos := getNext(s, pos)
			if nextPos < 0 {
				if i != 0 {
					return nil, fmt.Errorf("base64: incomplete quad at position %d: %w", pos, ErrInvalidChar)
				}
				if len(out) == 0 {
					return nil, ErrInvalidChar
				}
				return out, nil
			}
			if s[nextPos] == '=' && eq == 0 {
				eq = i + 1
			}
			k[i] = s[nextPos]
			pos = nextPos + 1
		}
		if eq != 0 {
			break
		}
		v0 := uint32(decTable[k[0]])
		v1 := uint32(decTable[k[1]])
		v2 := uint32(decTable[k[2]])
		v3 := uint32(decTable[k[3]])
		n := (v0 << 18) | (v1 << 12) | (v2 << 6) | v3
		out = append(out, byte(n>>16), byte(n>>8), byte(n))
	}
	if eq != 0 {
		if eq <= 2 {
			return nil, fmt.Errorf("base64: invalid padding at position %d: %w", pos, ErrInvalidChar)
		}
		for i := eq; i <= 4; i++ {
			if k[i-1] != '=' {
				return nil, fmt.Errorf("base64: non-padding character after '=' at position %d: %w", pos, ErrInvalidChar)
			}
		}
		v0 := uint32(decTable[k[0]])
		v1 := uint32(decTable[k[1]])
		n := (v0 << 18) | (v1 << 12)
		if eq == 4 {
			v2 := uint32(decTable[k[2]])
			n |= v2 << 6
			out = append(out, byte(n>>16), byte(n>>8))
		} else {
			out = append(out, byte(n>>16))
		}
	}
	return out, nil
}

func getNext(s string, start int) int {
	for i := start; i < len(s); i++ {
		c := s[i]
		if decTable[c] >= 0 || c == '=' {
			return i
		}
	}
	return -1
}
