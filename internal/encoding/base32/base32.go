package base32

import (
	"errors"
	"fmt"
)

var b32table = [32]byte{
	'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H',
	'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P',
	'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X',
	'Y', 'Z', '2', '3', '4', '5', '6', '7',
}

var decodeTable [256]byte

func init() {
	for i := range decodeTable {
		decodeTable[i] = 0xFF
	}
	for i, c := range b32table {
		decodeTable[c] = byte(i)
		decodeTable[c|0x20] = byte(i)
	}
}

var ErrInvalidChar = errors.New("base32: invalid character in input")

// Encode encodes src to RFC 4648 base32 with = padding.
func Encode(src []byte) string {
	if len(src) == 0 {
		return ""
	}
	outLen := (len(src) + 4) / 5 * 8
	out := make([]byte, outLen)
	pos := 0
	count := 0
	var buf uint64
	for _, b := range src {
		buf = (buf << 8) | uint64(b)
		count++
		if count == 5 {
			out[pos+7] = b32table[buf&0x1f]
			buf >>= 5
			out[pos+6] = b32table[buf&0x1f]
			buf >>= 5
			out[pos+5] = b32table[buf&0x1f]
			buf >>= 5
			out[pos+4] = b32table[buf&0x1f]
			buf >>= 5
			out[pos+3] = b32table[buf&0x1f]
			buf >>= 5
			out[pos+2] = b32table[buf&0x1f]
			buf >>= 5
			out[pos+1] = b32table[buf&0x1f]
			buf >>= 5
			out[pos] = b32table[buf&0x1f]
			pos += 8
			count = 0
			buf = 0
		}
	}
	if count > 0 {
		var r int
		switch count {
		case 1:
			buf <<= 2
			r = 2
		case 2:
			buf <<= 4
			r = 4
		case 3:
			buf <<= 1
			r = 5
		case 4:
			buf <<= 3
			r = 7
		}
		for j := 0; j < r; j++ {
			out[pos+r-1-j] = b32table[buf&0x1f]
			buf >>= 5
		}
		for i := pos + r; i < outLen; i++ {
			out[i] = '='
		}
	}
	return string(out)
}

// EncodeToString is an alias for Encode.
func EncodeToString(src []byte) string {
	return Encode(src)
}

// Decode decodes an RFC 4648 base32 string with optional = padding.
// It is case-insensitive and returns ErrInvalidChar on invalid input.
func Decode(s string) ([]byte, error) {
	if len(s) == 0 {
		return nil, nil
	}
	if len(s)%8 != 0 {
		return nil, fmt.Errorf("base32: input length %d is not a multiple of 8: %w", len(s), ErrInvalidChar)
	}
	done := false
	var out []byte
	for i := 0; i < len(s) && !done; i += 8 {
		var buf uint64
		bits := 0
		for j := 0; j < 8; j++ {
			ch := s[i+j]
			if ch == '=' {
				done = true
				break
			}
			v := decodeTable[ch]
			if v == 0xFF {
				return nil, fmt.Errorf("base32: invalid character %q at position %d: %w", ch, i+j, ErrInvalidChar)
			}
			buf = (buf << 5) | uint64(v)
			bits += 5
		}
		buf >>= (bits % 8)
		bits = bits / 8 * 8
		if bits >= 8 {
			out = append(out, byte(buf>>(bits-8)))
			if bits >= 16 {
				out = append(out, byte(buf>>(bits-16)))
				if bits >= 24 {
					out = append(out, byte(buf>>(bits-24)))
					if bits >= 32 {
						out = append(out, byte(buf>>(bits-32)))
						if bits >= 40 {
							out = append(out, byte(buf>>(bits-40)))
						}
					}
				}
			}
		}
	}
	return out, nil
}

// DecodeString is an alias for Decode.
func DecodeString(s string) ([]byte, error) {
	return Decode(s)
}
