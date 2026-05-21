package wire

import (
	"bytes"
	"math/big"
	"testing"
)

func TestBuilder_Basics(t *testing.T) {
	b := NewBuilder()
	b.PutByte(0xFF)
	b.WriteBool(true)
	b.WriteBool(false)
	b.WriteUint32(0xDEADBEEF)
	b.WriteUint64(0xCAFEBABEDEADBEEF)
	b.WriteString("hello")

	payload := b.Payload()

	r := &Reader{Buf: payload}
	if v := r.GetByte(); v != 0xFF {
		t.Errorf("byte: got %x", v)
	}
	if v := r.ReadBool(); !v {
		t.Error("bool1")
	}
	if v := r.ReadBool(); v {
		t.Error("bool2")
	}
	if v := r.ReadUint32(); v != 0xDEADBEEF {
		t.Errorf("uint32: %x", v)
	}
	if v := r.ReadUint64(); v != 0xCAFEBABEDEADBEEF {
		t.Errorf("uint64: %x", v)
	}
	if v := r.ReadString(); v != "hello" {
		t.Errorf("string: %q", v)
	}
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
}

func TestBuilder_WriteBytes(t *testing.T) {
	b := NewBuilder()
	b.WriteBytes([]byte{1, 2, 3})

	r := &Reader{Buf: b.Payload()}
	d := r.ReadBytes()
	if !bytes.Equal(d, []byte{1, 2, 3}) {
		t.Errorf("ReadBytes: got %v", d)
	}
}

func TestBuilder_WriteMpint(t *testing.T) {
	b := NewBuilder()
	b.WriteMpint(big.NewInt(0))
	b.WriteMpint(big.NewInt(42))
	b.WriteMpint(big.NewInt(-1))

	r := &Reader{Buf: b.Payload()}
	if v := r.ReadMpint(); v.Cmp(big.NewInt(0)) != 0 {
		t.Errorf("mpint 0: got %s", v)
	}
	if v := r.ReadMpint(); v.Cmp(big.NewInt(42)) != 0 {
		t.Errorf("mpint 42: got %s", v)
	}
	if v := r.ReadMpint(); v.Cmp(big.NewInt(-1)) != 0 {
		t.Errorf("mpint -1: got %s", v)
	}
}

func TestBuilder_WriteMpintNil(t *testing.T) {
	b := NewBuilder()
	b.WriteMpint(nil)
	r := &Reader{Buf: b.Payload()}
	v := r.ReadMpint()
	if v.Cmp(big.NewInt(0)) != 0 {
		t.Errorf("WriteMpint(nil): got %s", v)
	}
}

func TestBuilder_WriteNameList(t *testing.T) {
	b := NewBuilder()
	b.WriteNameList([]string{"publickey", "password", "keyboard-interactive"})

	r := &Reader{Buf: b.Payload()}
	s := r.ReadString()
	if s != "publickey,password,keyboard-interactive" {
		t.Errorf("name-list: %q", s)
	}
}

func TestJoinNameList(t *testing.T) {
	if s := JoinNameList(nil); s != "" {
		t.Errorf("nil: %q", s)
	}
	if s := JoinNameList([]string{"a"}); s != "a" {
		t.Errorf("single: %q", s)
	}
	if s := JoinNameList([]string{"a", "b"}); s != "a,b" {
		t.Errorf("two: %q", s)
	}
}

func TestReader_GetByteEarlyError(t *testing.T) {
	r := &Reader{Err: bytes.ErrTooLarge}
	if b := r.GetByte(); b != 0 {
		t.Errorf("expected 0 on early error")
	}
}

func TestReader_GetByteShort(t *testing.T) {
	r := &Reader{Buf: []byte{}}
	b := r.GetByte()
	if b != 0 || r.Err == nil {
		t.Error("expected error on empty buffer")
	}
}

func TestReader_ReadUint32Short(t *testing.T) {
	r := &Reader{Buf: []byte{1, 2, 3}}
	v := r.ReadUint32()
	if v != 0 || r.Err == nil {
		t.Error("expected error on short uint32 read")
	}
}

func TestReader_ReadStringShort(t *testing.T) {
	data := make([]byte, 10)
	data[0] = 0
	data[1] = 0
	data[2] = 0
	data[3] = 100 // claim 100 bytes
	r := &Reader{Buf: data}
	s := r.ReadString()
	if s != "" || r.Err == nil {
		t.Error("expected error on short string")
	}
}

func TestReader_ReadBytesCopy(t *testing.T) {
	b := NewBuilder()
	b.WriteBytes([]byte{10, 20, 30})

	r := &Reader{Buf: b.Payload()}
	d := r.ReadBytes()
	d[0] = 99 // mutate copy

	// Original payload should be unchanged.
	if b.Buf[4] != 10 {
		t.Errorf("original modified: %d", b.Buf[4])
	}
}

func TestReader_Remaining(t *testing.T) {
	b := NewBuilder()
	b.WriteUint32(42)

	r := &Reader{Buf: b.Payload()}
	if rem := r.Remaining(); rem != 4 {
		t.Errorf("remaining: %d", rem)
	}
	_ = r.ReadUint32()
	if rem := r.Remaining(); rem != 0 {
		t.Errorf("remaining after read: %d", rem)
	}
}

func TestBuilder_Payload(t *testing.T) {
	b := NewBuilder()
	b.WriteUint32(1)
	p1 := b.Payload()
	p1[0] = 99
	if b.Buf[0] != 99 {
		t.Error("Payload returns the internal buffer (shared, zero-copy)")
	}
}
