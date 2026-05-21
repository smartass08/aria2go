package bencode

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
)

func appendDecimal(dst []byte, i int64) []byte {
	return strconv.AppendInt(dst, i, 10)
}

func appendInt(dst []byte, i int64) []byte {
	dst = append(dst, 'i')
	dst = strconv.AppendInt(dst, i, 10)
	dst = append(dst, 'e')
	return dst
}

func appendString(dst []byte, s string) []byte {
	dst = strconv.AppendInt(dst, int64(len(s)), 10)
	dst = append(dst, ':')
	dst = append(dst, s...)
	return dst
}

func appendValue(dst []byte, v Value) ([]byte, error) {
	switch val := v.(type) {
	case StringVal:
		return appendString(dst, val.S), nil
	case IntVal:
		return appendInt(dst, val.I), nil
	case ListVal:
		dst = append(dst, 'l')
		for _, elem := range val.L {
			var err error
			dst, err = appendValue(dst, elem)
			if err != nil {
				return dst, err
			}
		}
		dst = append(dst, 'e')
		return dst, nil
	case *DictVal:
		dst = append(dst, 'd')
		sorted := make([]string, len(val.Keys))
		copy(sorted, val.Keys)
		sort.Strings(sorted)
		for _, k := range sorted {
			dst = appendString(dst, k)
			var err error
			dst, err = appendValue(dst, val.Values[k])
			if err != nil {
				return dst, err
			}
		}
		dst = append(dst, 'e')
		return dst, nil
	default:
		return dst, fmt.Errorf("%w: unknown value type %T", ErrInvalidBencode, v)
	}
}

// Marshal encodes a bencode Value to its wire-format bytes.
func Marshal(v Value) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil value", ErrInvalidBencode)
	}
	buf := make([]byte, 0, 256)
	return appendValue(buf, v)
}

// ExtractRaw returns the byte offsets of a nested value within bencode data.
// path is a sequence of dict keys and list indexes (as strings).
// Returns (start, end) offsets within data. end is exclusive.
func ExtractRaw(data []byte, path ...string) (start int, end int, err error) {
	pos := 0
	for i, segment := range path {
		nextStart, nextEnd, err := findInValue(data, pos, segment, i == len(path)-1)
		if err != nil {
			return 0, 0, err
		}
		if i == len(path)-1 {
			return nextStart, nextEnd, nil
		}
		pos = nextStart
	}
	return pos, pos + skipValue(data, pos), nil
}

func findInValue(data []byte, pos int, segment string, last bool) (start, end int, err error) {
	if pos >= len(data) {
		return 0, 0, fmt.Errorf("%w: unexpected end of data at offset %d", ErrInvalidBencode, pos)
	}

	b := data[pos]

	if b >= '0' && b <= '9' {
		_ = pos + skipString(data, pos)
		return 0, 0, fmt.Errorf("%w: string found where dict/list expected for path segment %q", ErrInvalidBencode, segment)
	} else if b == 'i' {
		_ = pos + skipInt(data, pos)
		return 0, 0, fmt.Errorf("%w: integer found where dict/list expected for path segment %q", ErrInvalidBencode, segment)
	} else if b == 'l' {
		idx, convErr := strconv.Atoi(segment)
		if convErr != nil {
			return 0, 0, fmt.Errorf("%w: non-numeric path segment %q for list", ErrInvalidBencode, segment)
		}
		return walkListByIndex(data, pos, idx, last)
	} else if b == 'd' {
		return walkDictByKey(data, pos, segment, last)
	} else {
		return 0, 0, fmt.Errorf("%w: unexpected byte 0x%02x at offset %d", ErrInvalidBencode, b, pos)
	}
}

func walkListByIndex(data []byte, pos int, idx int, last bool) (start, end int, err error) {
	cur := pos + 1 // skip 'l'
	itemIdx := 0

	for cur < len(data) && data[cur] != 'e' {
		itemStart := cur
		itemEnd := cur + skipValue(data, cur)

		if itemIdx == idx {
			if last {
				return itemStart, itemEnd, nil
			}
			return itemStart, itemEnd, nil
		}

		cur = itemEnd
		itemIdx++
	}

	return 0, 0, fmt.Errorf("%w: list index %d out of range", ErrInvalidBencode, idx)
}

func walkDictByKey(data []byte, pos int, key string, last bool) (start, end int, err error) {
	cur := pos + 1 // skip 'd'

	for cur < len(data) && data[cur] != 'e' {
		keyEnd := cur + skipString(data, cur)
		k := extractStringKey(data, cur)
		valStart := keyEnd
		valEnd := keyEnd + skipValue(data, keyEnd)

		if k == key {
			if last {
				return valStart, valEnd, nil
			}
			return valStart, valEnd, nil
		}

		cur = valEnd
	}

	return 0, 0, fmt.Errorf("%w: key %q not found in dict", ErrInvalidBencode, key)
}

func skipValue(data []byte, pos int) int {
	if pos >= len(data) {
		return 1
	}
	b := data[pos]
	switch {
	case b >= '0' && b <= '9':
		return skipString(data, pos)
	case b == 'i':
		return skipInt(data, pos)
	case b == 'l':
		return skipContainer(data, pos)
	case b == 'd':
		return skipContainer(data, pos)
	default:
		return 1
	}
}

func skipString(data []byte, pos int) int {
	colonIdx := bytes.IndexByte(data[pos:], ':')
	if colonIdx < 0 {
		return len(data) - pos
	}
	length := atoiBytes(data[pos : pos+colonIdx])
	if length < 0 {
		return len(data) - pos
	}
	return colonIdx + 1 + int(length)
}

func skipInt(data []byte, pos int) int {
	endIdx := bytes.IndexByte(data[pos+1:], 'e')
	if endIdx < 0 {
		return len(data) - pos
	}
	return endIdx + 2 // skip 'i' + content + 'e'
}

func skipContainer(data []byte, pos int) int {
	cur := pos + 1 // skip 'l' or 'd'
	for cur < len(data) {
		if data[cur] == 'e' {
			return cur - pos + 1
		}
		cur += skipValue(data, cur)
	}
	return len(data) - pos
}

func extractStringKey(data []byte, pos int) string {
	colonIdx := bytes.IndexByte(data[pos:], ':')
	if colonIdx < 0 {
		return ""
	}
	length := atoiBytes(data[pos : pos+colonIdx])
	if length < 0 {
		return ""
	}
	return string(data[pos+colonIdx+1 : pos+colonIdx+1+length])
}

func extractString(data []byte, pos int) string {
	return extractStringKey(data, pos)
}

func atoiBytes(b []byte) int {
	if len(b) == 0 {
		return -1
	}
	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}
