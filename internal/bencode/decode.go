package bencode

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Maximum nesting depth for lists and dictionaries.
const maxDepth = 50

// Package errors.
var (
	ErrInvalidBencode   = errors.New("invalid bencode")
	ErrIntOverflow      = errors.New("integer overflow")
	ErrDictKeyOrder     = errors.New("dictionary keys must be sorted lexicographically")
	ErrDictKeyType      = errors.New("dictionary key must be a bencoded string")
	ErrStructureTooDeep = errors.New("bencode structure exceeds maximum nesting depth")
)

// reader is a buffered reader for streaming decode.
type reader struct {
	r   io.Reader
	buf [256]byte
	pos int
	end int
}

func newReader(r io.Reader) *reader {
	return &reader{r: r}
}

func (r *reader) readByte() (byte, error) {
	if r.pos >= r.end {
		b, err := r.refill()
		if err != nil {
			return 0, err
		}
		r.pos++
		return b, nil
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

func (r *reader) peekByte() (byte, error) {
	if r.pos >= r.end {
		_, err := r.refill()
		if err != nil {
			return 0, err
		}
	}
	return r.buf[r.pos], nil
}

func (r *reader) refill() (byte, error) {
	if r.r == nil {
		return 0, io.EOF
	}
	n, err := r.r.Read(r.buf[:])
	if n > 0 {
		r.end = n
		r.pos = 0
		return r.buf[0], nil
	}
	if err != nil {
		return 0, err
	}
	return 0, io.EOF
}

// readFull reads exactly n bytes. It first drains any buffered data, then
// reads remaining bytes directly from the underlying reader.
func (r *reader) readFull(n int64) ([]byte, error) {
	buf := make([]byte, n)
	if avail := r.end - r.pos; avail > 0 {
		toCopy := avail
		if int64(toCopy) > n {
			toCopy = int(n)
		}
		copy(buf, r.buf[r.pos:r.pos+toCopy])
		r.pos += toCopy
		n -= int64(toCopy)
		if n == 0 {
			return buf, nil
		}
	}
	if r.r == nil {
		return nil, io.ErrUnexpectedEOF
	}
	_, err := io.ReadFull(r.r, buf[len(buf)-int(n):])
	if err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return buf, nil
}

// Decoder reads bencoded values from an io.Reader.
type Decoder struct {
	r *reader
}

// NewDecoder creates a new bencode Decoder reading from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: newReader(r)}
}

// Decode reads and returns the next bencoded value from the stream.
func (d *Decoder) Decode() (Value, error) {
	return d.decodeWithDepth(0)
}

func (d *Decoder) decodeWithDepth(depth int) (Value, error) {
	b, err := d.r.peekByte()
	if err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	switch {
	case b >= '0' && b <= '9':
		return d.decodeString()
	case b == 'i':
		return d.decodeInt()
	case b == 'l':
		return d.decodeList(depth)
	case b == 'd':
		return d.decodeDict(depth)
	default:
		return nil, fmt.Errorf("%w: unexpected byte 0x%02x", ErrInvalidBencode, b)
	}
}

func (d *Decoder) decodeString() (Value, error) {
	length, err := d.readDecimal()
	if err != nil {
		return nil, fmt.Errorf("%w: invalid string length: %v", ErrInvalidBencode, err)
	}

	b, err := d.r.readByte()
	if err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	if b != ':' {
		return nil, fmt.Errorf("%w: expected ':' after string length", ErrInvalidBencode)
	}

	if length < 0 {
		return nil, fmt.Errorf("%w: negative string length", ErrInvalidBencode)
	}

	buf, err := d.r.readFull(length)
	if err != nil {
		return nil, err
	}

	return StringVal{S: string(buf)}, nil
}

func (d *Decoder) decodeInt() (Value, error) {
	// Consume 'i'
	_, err := d.r.readByte()
	if err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	// Collect digits into a stack-allocated scratch buffer.
	// Most ints fit in 24 bytes.
	var numBuf [24]byte
	nb := 0

	b, err := d.r.readByte()
	if err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	if b == '-' || b == '+' {
		numBuf[nb] = b
		nb++
		b, err = d.r.readByte()
		if err != nil {
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}

	if b < '0' || b > '9' {
		return nil, fmt.Errorf("%w: expected digit in integer, got 0x%02x", ErrInvalidBencode, b)
	}

	numBuf[nb] = b
	nb++

	// Consume remaining digits
	for {
		nb2, err2 := d.r.peekByte()
		if err2 != nil {
			if err2 == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err2
		}
		if nb2 < '0' || nb2 > '9' {
			break
		}
		d.r.readByte()
		if nb < len(numBuf) {
			numBuf[nb] = nb2
		}
		nb++
	}

	nextB, err := d.r.peekByte()
	if err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	// Float notation skip (aria2 compatibility)
	if nextB == '.' || nextB == 'E' || nextB == '+' || nextB == '-' {
		for {
			skipB, err2 := d.r.readByte()
			if err2 != nil {
				if err2 == io.EOF {
					return nil, io.ErrUnexpectedEOF
				}
				return nil, err2
			}
			if skipB == 'e' {
				return IntVal{I: 0}, nil
			}
			if isDigitOrFloatChar(skipB) {
				continue
			}
			return nil, fmt.Errorf("%w: invalid floating-point number in integer field", ErrInvalidBencode)
		}
	}

	if nextB != 'e' {
		return nil, fmt.Errorf("%w: expected 'e' to terminate integer, got 0x%02x", ErrInvalidBencode, nextB)
	}
	d.r.readByte() // consume 'e'

	numStr := string(numBuf[:min(nb, len(numBuf))])
	if nb > len(numBuf) {
		numStr = "overflow"
	}
	if err := validateIntString(numStr); err != nil {
		return nil, err
	}

	i, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return nil, ErrIntOverflow
		}
		return nil, fmt.Errorf("%w: invalid integer: %v", ErrInvalidBencode, err)
	}

	return IntVal{I: i}, nil
}

func isDigitOrFloatChar(c byte) bool {
	return (c >= '0' && c <= '9') || c == '.' || c == 'E' || c == 'e' || c == '+' || c == '-'
}

// validateIntString checks for leading zeros. aria2 accepts negative zero (i-0e).
func validateIntString(s string) error {
	if s == "" || s == "overflow" {
		return fmt.Errorf("%w: empty or overflowed integer", ErrInvalidBencode)
	}

	if s[0] == '-' {
		if len(s) > 2 && s[1] == '0' {
			return fmt.Errorf("%w: leading zeros not allowed in integer", ErrInvalidBencode)
		}
		return nil
	}

	if s[0] == '+' {
		if len(s) > 2 && s[1] == '0' {
			return fmt.Errorf("%w: leading zeros not allowed in integer", ErrInvalidBencode)
		}
		return nil
	}

	if len(s) > 1 && s[0] == '0' {
		return fmt.Errorf("%w: leading zeros not allowed in integer", ErrInvalidBencode)
	}

	return nil
}

func (d *Decoder) decodeList(depth int) (Value, error) {
	if depth >= maxDepth {
		return nil, ErrStructureTooDeep
	}

	// Consume 'l'
	_, err := d.r.readByte()
	if err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	var items []Value
	for {
		b, err := d.r.peekByte()
		if err != nil {
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if b == 'e' {
			_, _ = d.r.readByte()
			break
		}
		v, err := d.decodeWithDepth(depth + 1)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}

	return ListVal{L: items}, nil
}

func (d *Decoder) decodeDict(depth int) (Value, error) {
	if depth >= maxDepth {
		return nil, ErrStructureTooDeep
	}

	// Consume 'd'
	_, err := d.r.readByte()
	if err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	dict := AcquireDict()
	var lastKey string
	ok := false
	defer func() {
		if !ok {
			ReleaseDict(dict)
		}
	}()

	for {
		b, err := d.r.peekByte()
		if err != nil {
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if b == 'e' {
			_, _ = d.r.readByte()
			break
		}

		// Keys must be strings
		if b < '0' || b > '9' {
			return nil, fmt.Errorf("%w: dictionary key must be a string", ErrDictKeyType)
		}

		keyVal, err := d.decodeString()
		if err != nil {
			return nil, err
		}
		key := keyVal.(StringVal).S

		val, err := d.decodeWithDepth(depth + 1)
		if err != nil {
			return nil, err
		}

		dict.Set(key, val)

		if lastKey != "" && key < lastKey {
			return nil, fmt.Errorf("%w: key %q before %q", ErrDictKeyOrder, key, lastKey)
		}
		lastKey = key
	}

	ok = true
	return dict, nil
}

// readDecimal reads a sequence of decimal digits from the stream and returns
// the parsed int64 value. It validates that leading zeros are not present
// (except for "0" itself) and checks for int64 overflow during digit
// accumulation.
func (d *Decoder) readDecimal() (int64, error) {
	b, err := d.r.readByte()
	if err != nil {
		if err == io.EOF {
			return 0, io.ErrUnexpectedEOF
		}
		return 0, err
	}

	if b < '0' || b > '9' {
		return 0, fmt.Errorf("%w: expected digit, got 0x%02x", ErrInvalidBencode, b)
	}

	result := int64(b - '0')

	for {
		nb, err := d.r.peekByte()
		if err != nil {
			if err == io.EOF {
				break
			}
			return 0, err
		}
		if nb < '0' || nb > '9' {
			break
		}
		d.r.readByte()

		digit := int64(nb - '0')
		if (int64(^uint64(0)>>1)-digit)/10 < result {
			return 0, fmt.Errorf("%w: string length exceeds int64 range", ErrInvalidBencode)
		}
		result = result*10 + digit
	}

	return result, nil
}

// Unmarshal decodes bencoded data into v. v must be a pointer to a type that
// can hold the decoded value:
//
//	*Value        — direct assignment of decoded value
//	*int64        — expects the top-level value to be an integer
//	*string       — expects the top-level value to be a string
//	*[]Value      — expects a list
//	*map[string]Value — expects a dictionary
func Unmarshal(data []byte, v any) error {
	d := NewDecoder(bytes.NewReader(data))
	val, err := d.Decode()
	if err != nil {
		return err
	}

	switch target := v.(type) {
	case *Value:
		*target = val
	case *int64:
		iv, ok := val.(IntVal)
		if !ok {
			return fmt.Errorf("%w: expected integer, got %s", ErrInvalidBencode, val.Kind())
		}
		*target = iv.I
	case *string:
		sv, ok := val.(StringVal)
		if !ok {
			return fmt.Errorf("%w: expected string, got %s", ErrInvalidBencode, val.Kind())
		}
		*target = sv.S
	case *[]Value:
		lv, ok := val.(ListVal)
		if !ok {
			return fmt.Errorf("%w: expected list, got %s", ErrInvalidBencode, val.Kind())
		}
		*target = lv.L
	case *map[string]Value:
		dv, ok := val.(*DictVal)
		if !ok {
			return fmt.Errorf("%w: expected dict, got %s", ErrInvalidBencode, val.Kind())
		}
		*target = dv.Values
	default:
		return fmt.Errorf("%w: unsupported target type %T", ErrInvalidBencode, target)
	}

	return nil
}
