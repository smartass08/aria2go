// Package wire provides SSH wire-format encoding and decoding helpers
// used by the SSH authentication, connection, and transport layers.
package wire

import (
	"encoding/binary"
	"io"
	"math/big"
	"sync"
)

var builderPool = sync.Pool{
	New: func() any { return &Builder{} },
}

// GetBuilder returns a pooled Builder with a preallocated buffer.
func GetBuilder() *Builder {
	b := builderPool.Get().(*Builder)
	b.Buf = b.Buf[:0]
	return b
}

// PutBuilder returns the Builder to the pool.
func PutBuilder(b *Builder) {
	builderPool.Put(b)
}

// Builder incrementally builds an SSH wire-format payload.
type Builder struct {
	Buf []byte
}

// NewBuilder returns a Builder with preallocated capacity.
func NewBuilder() *Builder {
	return &Builder{Buf: make([]byte, 0, 256)}
}

// Reset clears the Builder's buffer for reuse.
func (b *Builder) Reset() {
	b.Buf = b.Buf[:0]
}

// Payload returns the built payload bytes.
func (b *Builder) Payload() []byte { return b.Buf }

// PutByte appends a byte.
func (b *Builder) PutByte(v byte) { b.Buf = append(b.Buf, v) }

// WriteBool appends a boolean (0 or 1).
func (b *Builder) WriteBool(v bool) {
	if v {
		b.Buf = append(b.Buf, 1)
	} else {
		b.Buf = append(b.Buf, 0)
	}
}

// WriteUint32 appends a uint32 in big-endian.
func (b *Builder) WriteUint32(v uint32) {
	b.Buf = append(b.Buf, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// WriteUint64 appends a uint64 in big-endian.
func (b *Builder) WriteUint64(v uint64) {
	b.Buf = append(b.Buf,
		byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// WriteString appends an SSH string (uint32 length prefix + data).
func (b *Builder) WriteString(s string) {
	binary.BigEndian.PutUint32(b.extend(4), uint32(len(s)))
	b.Buf = append(b.Buf, s...)
}

// WriteBytes appends an SSH byte string (uint32 length prefix + data).
func (b *Builder) WriteBytes(d []byte) {
	binary.BigEndian.PutUint32(b.extend(4), uint32(len(d)))
	b.Buf = append(b.Buf, d...)
}

// WriteMpint appends an SSH mpint (uint32 length prefix + big-endian
// two's complement, no unnecessary leading zeros).
func (b *Builder) WriteMpint(n *big.Int) {
	if n == nil || n.Sign() == 0 {
		b.WriteUint32(0)
		return
	}

	var raw []byte
	if n.Sign() < 0 {
		// For negative numbers, produce two's complement representation.
		// Compute -n, subtract 1 to get (|n|-1), invert bits to get ~(|n|-1).
		one := big.NewInt(1)
		tmp := new(big.Int).Neg(n) // |n|
		tmp.Sub(tmp, one)          // |n| - 1
		raw = tmp.Bytes()          // big-endian bytes of |n|-1
		// Invert each byte to produce complement.
		for i := range raw {
			raw[i] ^= 0xFF
		}
		// Ensure the MSB is 1 to indicate negative.
		if len(raw) == 0 || raw[0]&0x80 == 0 {
			raw = append([]byte{0xFF}, raw...)
		}
	} else {
		raw = n.Bytes()
		// If MSB is set, prepend a zero byte for positive number.
		if len(raw) > 0 && raw[0]&0x80 != 0 {
			raw = append([]byte{0x00}, raw...)
		}
	}

	binary.BigEndian.PutUint32(b.extend(4), uint32(len(raw)))
	b.Buf = append(b.Buf, raw...)
}

// WriteNameList appends an SSH name-list (comma-separated string).
func (b *Builder) WriteNameList(names []string) {
	b.WriteString(JoinNameList(names))
}

// JoinNameList creates a comma-separated string from a slice of names.
func JoinNameList(names []string) string {
	l := 0
	for i, n := range names {
		if i > 0 {
			l++
		}
		l += len(n)
	}
	b := make([]byte, l)
	off := 0
	for i, n := range names {
		if i > 0 {
			b[off] = ','
			off++
		}
		copy(b[off:], n)
		off += len(n)
	}
	return string(b)
}

func (b *Builder) extend(n int) []byte {
	off := len(b.Buf)
	b.Buf = append(b.Buf, make([]byte, n)...)
	return b.Buf[off : off+n]
}

// Reader incrementally reads SSH wire-format payloads.
type Reader struct {
	Buf []byte
	Pos int
	Err error
}

// Remaining returns the number of unconsumed bytes.
func (r *Reader) Remaining() int { return len(r.Buf) - r.Pos }

// GetByte reads a single byte.
func (r *Reader) GetByte() byte {
	if r.Err != nil {
		return 0
	}
	if r.Pos >= len(r.Buf) {
		r.Err = io.ErrUnexpectedEOF
		return 0
	}
	b := r.Buf[r.Pos]
	r.Pos++
	return b
}

// ReadBool reads a boolean (0 = false, non-zero = true).
func (r *Reader) ReadBool() bool {
	return r.GetByte() != 0
}

// ReadUint32 reads a uint32 in big-endian.
func (r *Reader) ReadUint32() uint32 {
	if r.Err != nil {
		return 0
	}
	if r.Pos+4 > len(r.Buf) {
		r.Err = io.ErrUnexpectedEOF
		return 0
	}
	v := binary.BigEndian.Uint32(r.Buf[r.Pos:])
	r.Pos += 4
	return v
}

// ReadUint64 reads a uint64 in big-endian.
func (r *Reader) ReadUint64() uint64 {
	if r.Err != nil {
		return 0
	}
	if r.Pos+8 > len(r.Buf) {
		r.Err = io.ErrUnexpectedEOF
		return 0
	}
	v := binary.BigEndian.Uint64(r.Buf[r.Pos:])
	r.Pos += 8
	return v
}

// ReadString reads an SSH string (uint32 length + UTF-8 data).
func (r *Reader) ReadString() string {
	if r.Err != nil {
		return ""
	}
	l := r.ReadUint32()
	if r.Err != nil {
		return ""
	}
	if r.Pos+int(l) > len(r.Buf) {
		r.Err = io.ErrUnexpectedEOF
		return ""
	}
	s := string(r.Buf[r.Pos : r.Pos+int(l)])
	r.Pos += int(l)
	return s
}

// ReadBytes reads an SSH byte string (uint32 length + data), returning
// a copy of the data.
func (r *Reader) ReadBytes() []byte {
	if r.Err != nil {
		return nil
	}
	l := r.ReadUint32()
	if r.Err != nil {
		return nil
	}
	if r.Pos+int(l) > len(r.Buf) {
		r.Err = io.ErrUnexpectedEOF
		return nil
	}
	d := make([]byte, l)
	copy(d, r.Buf[r.Pos:r.Pos+int(l)])
	r.Pos += int(l)
	return d
}

// ReadMpint reads an SSH mpint and returns it as a *big.Int.
func (r *Reader) ReadMpint() *big.Int {
	b := r.ReadBytes()
	if r.Err != nil {
		return nil
	}
	if len(b) == 0 {
		return new(big.Int)
	}
	// Check MSB for negative (two's complement).
	if b[0]&0x80 != 0 {
		// Negative: invert bits, add 1, negate.
		v := make([]byte, len(b))
		copy(v, b)
		for i := range v {
			v[i] ^= 0xFF
		}
		n := new(big.Int).SetBytes(v)
		n.Add(n, big.NewInt(1))
		return n.Neg(n)
	}
	return new(big.Int).SetBytes(b)
}
