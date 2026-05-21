package bencode_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/bencode"
)

func TestDecoder_DecodeString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"basic", "4:spam", "spam"},
		{"empty", "0:", ""},
		{"with spaces", "12:hello world!", "hello world!"},
		{"binary", "3:\xff\xfe\xfd", "\xff\xfe\xfd"},
		{"null byte", "4:a\x00b\x00", "a\x00b\x00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := bencode.NewDecoder(strings.NewReader(tt.input))
			v, err := d.Decode()
			if err != nil {
				t.Fatalf("Decode() error: %v", err)
			}
			sv, ok := v.(bencode.StringVal)
			if !ok {
				t.Fatalf("expected StringVal, got %T", v)
			}
			if sv.S != tt.want {
				t.Errorf("String() = %q, want %q", sv.S, tt.want)
			}
		})
	}
}

func TestDecoder_DecodeInt(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int64
	}{
		{"positive small", "i3e", 3},
		{"negative", "i-42e", -42},
		{"zero", "i0e", 0},
		{"large", "i99999999e", 99999999},
		{"max int64", "i9223372036854775807e", 9223372036854775807},
		{"min int64", "i-9223372036854775808e", -9223372036854775808},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := bencode.NewDecoder(strings.NewReader(tt.input))
			v, err := d.Decode()
			if err != nil {
				t.Fatalf("Decode() error: %v", err)
			}
			iv, ok := v.(bencode.IntVal)
			if !ok {
				t.Fatalf("expected IntVal, got %T", v)
			}
			if iv.I != tt.want {
				t.Errorf("I = %d, want %d", iv.I, tt.want)
			}
		})
	}
}

func TestDecoder_DecodeInt_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		// aria2 accepts i-0e (returns 0), so this is valid
		// {"negative zero", "i-0e"},

		{"leading zero", "i03e"},
		{"no digits", "ie"},
		{"missing terminator", "i42"},
		{"missing both", "i"},
		{"no terminator after minus", "i-"},
		{"too large", "i9223372036854775808e"},
		{"too small", "i-9223372036854775809e"},
		{"not a number", "iabce"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := bencode.NewDecoder(strings.NewReader(tt.input))
			_, err := d.Decode()
			if err == nil {
				t.Errorf("expected error for input %q", tt.input)
			}
		})
	}
}

func TestDecoder_DecodeList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // round-trip String()
	}{
		{"empty", "le", "le"},
		{"two strings", "l4:spam4:eggse", "l4:spam4:eggse"},
		{"three ints", "li1ei2ei3ee", "li1ei2ei3ee"},
		{"nested lists", "ll1:a1:bel2:xyee", "ll1:a1:bel2:xyee"},
		{"mixed types", "l4:spami42ee", "l4:spami42ee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := bencode.NewDecoder(strings.NewReader(tt.input))
			v, err := d.Decode()
			if err != nil {
				t.Fatalf("Decode() error: %v", err)
			}
			lv, ok := v.(bencode.ListVal)
			if !ok {
				t.Fatalf("expected ListVal, got %T", v)
			}
			if tt.name == "empty" && len(lv.L) != 0 {
				t.Errorf("expected empty list")
			}
			got := lv.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecoder_DecodeDict(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // round-trip String()
	}{
		{"empty", "de", "de"},
		{"string values", "d3:cow3:moo4:spam4:eggse", "d3:cow3:moo4:spam4:eggse"},
		{"mixed types", "d3:keyi42ee", "d3:keyi42ee"},
		{"nested dict", "d5:innerd1:xi1eee", "d5:innerd1:xi1eee"},
		{"list value", "d4:spaml1:a1:bee", "d4:spaml1:a1:bee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := bencode.NewDecoder(strings.NewReader(tt.input))
			v, err := d.Decode()
			if err != nil {
				t.Fatalf("Decode() error: %v", err)
			}
			dv, ok := v.(*bencode.DictVal)
			if !ok {
				t.Fatalf("expected *DictVal, got %T", v)
			}
			got := dv.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecoder_DecodeDict_KeyOrder(t *testing.T) {
	// Lexicographically sorted keys are valid
	validDict := "d3:abci1e3:cowi42e4:spam4:eggse"
	d := bencode.NewDecoder(strings.NewReader(validDict))
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode() error on sorted dict: %v", err)
	}
	dv := v.(*bencode.DictVal)
	if len(dv.Keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(dv.Keys))
	}

	// Unsorted keys should return ErrDictKeyOrder
	unsortedDict := "d4:spam4:eggs3:cow3:mooe"
	d2 := bencode.NewDecoder(strings.NewReader(unsortedDict))
	_, err = d2.Decode()
	if !errors.Is(err, bencode.ErrDictKeyOrder) {
		t.Errorf("expected ErrDictKeyOrder, got %v", err)
	}
}

func TestDecoder_DecodeDict_NonStringKey(t *testing.T) {
	// Dict with integer key should fail
	invalid := "di123ei42ee"
	d := bencode.NewDecoder(strings.NewReader(invalid))
	_, err := d.Decode()
	if !errors.Is(err, bencode.ErrDictKeyType) {
		t.Errorf("expected ErrDictKeyType, got %v", err)
	}
}

func TestDecoder_Decode_TruncatedInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"truncated string length", "5:sp"},
		{"truncated string after colon", "10:short"},
		{"truncated int", "i42"},
		{"truncated negative int", "i-42"},
		{"truncated list", "l4:spam"},
		{"truncated nested list", "ll1:ae"},
		{"truncated dict", "d3:cow3:moo"},
		{"truncated dict after key", "d3:cow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := bencode.NewDecoder(strings.NewReader(tt.input))
			_, err := d.Decode()
			if err == nil {
				t.Errorf("expected error for truncated input %q", tt.input)
			}
		})
	}
}

func TestDecoder_Decode_UnexpectedByte(t *testing.T) {
	d := bencode.NewDecoder(strings.NewReader("x"))
	_, err := d.Decode()
	if err == nil {
		t.Error("expected error for unexpected byte")
	}
}

func TestDecoder_Decode_EmptyInput(t *testing.T) {
	d := bencode.NewDecoder(strings.NewReader(""))
	_, err := d.Decode()
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"string", "4:spam"},
		{"int positive", "i42e"},
		{"int negative", "i-7e"},
		{"int zero", "i0e"},
		{"empty list", "le"},
		{"empty dict", "de"},
		{"list", "l4:spam4:eggse"},
		{"int list", "li1ei2ei3ee"},
		{"dict", "d3:cow3:moo4:spam4:eggse"},
		{"nested list", "ll1:a1:bel2:xyee"},
		{"nested dict", "d5:innerd1:xi1eee"},
		{"binary string", "3:\x00\x01\x02"},
		{"large int", "i9223372036854775807e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := bencode.NewDecoder(strings.NewReader(tt.input))
			v, err := d.Decode()
			if err != nil {
				t.Fatalf("Decode() error: %v", err)
			}
			got := v.String()
			if got != tt.input {
				t.Errorf("round-trip: String() = %q, want %q", got, tt.input)
			}
		})
	}
}

func TestUnmarshal_Value(t *testing.T) {
	data := []byte("4:spam")
	var v bencode.Value
	err := bencode.Unmarshal(data, &v)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	sv, ok := v.(bencode.StringVal)
	if !ok {
		t.Fatalf("expected StringVal, got %T", v)
	}
	if sv.S != "spam" {
		t.Errorf("expected spam, got %q", sv.S)
	}
}

func TestUnmarshal_Int64(t *testing.T) {
	data := []byte("i42e")
	var i int64
	err := bencode.Unmarshal(data, &i)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	if i != 42 {
		t.Errorf("expected 42, got %d", i)
	}
}

func TestUnmarshal_String(t *testing.T) {
	data := []byte("4:spam")
	var s string
	err := bencode.Unmarshal(data, &s)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	if s != "spam" {
		t.Errorf("expected spam, got %q", s)
	}
}

func TestUnmarshal_List(t *testing.T) {
	data := []byte("li1ei2ei3ee")
	var l []bencode.Value
	err := bencode.Unmarshal(data, &l)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	if len(l) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(l))
	}
	if l[0].String() != "i1e" {
		t.Errorf("l[0] = %q, want i1e", l[0].String())
	}
}

func TestUnmarshal_Map(t *testing.T) {
	data := []byte("d3:cow3:moo4:spam4:eggse")
	var m map[string]bencode.Value
	err := bencode.Unmarshal(data, &m)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	cow, ok := m["cow"]
	if !ok || cow.String() != "3:moo" {
		t.Errorf("m[cow] = %v", cow)
	}
}

func TestUnmarshal_TypeMismatch(t *testing.T) {
	data := []byte("4:spam")
	var i int64
	err := bencode.Unmarshal(data, &i)
	if err == nil {
		t.Error("expected error for type mismatch (string into int64)")
	}

	data2 := []byte("i42e")
	var s string
	err = bencode.Unmarshal(data2, &s)
	if err == nil {
		t.Error("expected error for type mismatch (int into string)")
	}
}

func TestStreamingDecode(t *testing.T) {
	// Multiple bencoded values concatenated
	input := "i1e4:spami2e"
	d := bencode.NewDecoder(strings.NewReader(input))

	// First value: i1e
	v1, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode() 1 error: %v", err)
	}
	if v1.String() != "i1e" {
		t.Errorf("v1 = %q, want i1e", v1.String())
	}

	// Second value: 4:spam
	v2, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode() 2 error: %v", err)
	}
	if v2.String() != "4:spam" {
		t.Errorf("v2 = %q, want 4:spam", v2.String())
	}

	// Third value: i2e
	v3, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode() 3 error: %v", err)
	}
	if v3.String() != "i2e" {
		t.Errorf("v3 = %q, want i2e", v3.String())
	}
}

func TestDecoder_ReadsFromIOReader(t *testing.T) {
	// Use bytes.Buffer to confirm streaming from io.Reader works
	buf := bytes.NewBufferString("l4:spami42ee")
	d := bencode.NewDecoder(buf)
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	want := "l4:spami42ee"
	got := v.String()
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestDecoder_AfterDecodeString_BufferResets(t *testing.T) {
	// Decode a string longer than the internal buffer (128 bytes) to ensure
	// the reader position is correctly reset after io.ReadFull.
	longStr := strings.Repeat("x", 200)
	longEncoded := "200:" + longStr
	d := bencode.NewDecoder(strings.NewReader(longEncoded))

	v1, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode() of long string error: %v", err)
	}
	sv := v1.(bencode.StringVal)
	if len(sv.S) != 200 {
		t.Errorf("expected 200-byte string, got %d", len(sv.S))
	}

	// The following value should still be parseable
	moreInput := longEncoded + "i42e"
	d2 := bencode.NewDecoder(strings.NewReader(moreInput))
	v2, err := d2.Decode()
	if err != nil {
		t.Fatalf("Decode() of long string error: %v", err)
	}
	_ = v2

	v3, err := d2.Decode()
	if err != nil {
		t.Fatalf("Decode() after long string error: %v", err)
	}
	if v3.String() != "i42e" {
		t.Errorf("v3 = %q, want i42e", v3.String())
	}
}

func TestDecoder_IntEdgeCases(t *testing.T) {
	// Single digit with no extras
	d := bencode.NewDecoder(strings.NewReader("i5e"))
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if v.String() != "i5e" {
		t.Errorf("String() = %q, want i5e", v.String())
	}
}

func TestDecoder_StringError_LongLength(t *testing.T) {
	// Valid length but not enough data
	d := bencode.NewDecoder(strings.NewReader("5:hi"))
	_, err := d.Decode()
	if err == nil {
		t.Error("expected error for truncated string")
	}
}

// errReader returns an error on every read.
type errReader struct{}

func (e errReader) Read(p []byte) (int, error) {
	return 0, errors.New("read error")
}

func TestDecoder_IOError(t *testing.T) {
	d := bencode.NewDecoder(errReader{})
	_, err := d.Decode()
	if err == nil {
		t.Error("expected error from faulty reader")
	}
}

// partialReader returns data in small chunks to exercise buffering.
type partialReader struct {
	data    []byte
	pos     int
	chunkSz int
}

func (r *partialReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	end := r.pos + r.chunkSz
	if end > len(r.data) {
		end = len(r.data)
	}
	n := copy(p, r.data[r.pos:end])
	r.pos += n
	if n == 0 && r.pos >= len(r.data) {
		return 0, io.EOF
	}
	return n, nil
}

func TestDecoder_PartialReads(t *testing.T) {
	pr := &partialReader{data: []byte("4:spam"), chunkSz: 1}
	d := bencode.NewDecoder(pr)
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if v.String() != "4:spam" {
		t.Errorf("String() = %q, want 4:spam", v.String())
	}
}

func TestDecoder_PartialReads_List(t *testing.T) {
	pr := &partialReader{data: []byte("li1ei2ee"), chunkSz: 1}
	d := bencode.NewDecoder(pr)
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if v.String() != "li1ei2ee" {
		t.Errorf("String() = %q, want li1ei2ee", v.String())
	}
}

func TestUnmarshal_InvalidData(t *testing.T) {
	var v bencode.Value
	err := bencode.Unmarshal([]byte("not valid"), &v)
	if err == nil {
		t.Error("expected error for invalid data")
	}
}

func TestUnmarshal_NilTarget(t *testing.T) {
	// This should panic or error - let's check nil handling
	err := bencode.Unmarshal([]byte("i0e"), nil)
	if err == nil {
		t.Error("expected error for nil target")
	}
}

func TestNewDecoder_NilReader(t *testing.T) {
	// NewDecoder with nil reader - reading from it should error
	d := bencode.NewDecoder(nil)
	_, _ = d.Decode() // should not panic
}

func TestDecoder_Decode_StringZeroLength(t *testing.T) {
	d := bencode.NewDecoder(strings.NewReader("0:"))
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	sv := v.(bencode.StringVal)
	if sv.S != "" {
		t.Errorf("expected empty string, got %q", sv.S)
	}
}

func TestDecoder_Decode_StringLeadingZeroRoundTrip(t *testing.T) {
	// "04:spam" decodes as 4-byte string "spam". Re-encoding via String()
	// must produce canonical "4:spam" (no leading zeros in length).
	d := bencode.NewDecoder(strings.NewReader("04:spam"))
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode(04:spam) error: %v", err)
	}
	sv := v.(bencode.StringVal)
	if sv.S != "spam" {
		t.Errorf("expected %q, got %q", "spam", sv.S)
	}
	got := sv.String()
	if got != "4:spam" {
		t.Errorf("re-encoded leading-zero string: got %q, want canonical %q", got, "4:spam")
	}

	// Normal string should round-trip cleanly
	d2 := bencode.NewDecoder(strings.NewReader("3:foo"))
	v2, err := d2.Decode()
	if err != nil {
		t.Fatalf("Decode(3:foo) error: %v", err)
	}
	if v2.String() != "3:foo" {
		t.Errorf("normal string round-trip: got %q, want %q", v2.String(), "3:foo")
	}

	// Multi-digit leading zero: "0042:..." should parse as length 42
	long := strings.Repeat("x", 42)
	input := "0042:" + long
	d3 := bencode.NewDecoder(strings.NewReader(input))
	v3, err := d3.Decode()
	if err != nil {
		t.Fatalf("Decode(0042:%s) error: %v", long, err)
	}
	sv3 := v3.(bencode.StringVal)
	if len(sv3.S) != 42 {
		t.Errorf("expected 42-byte string from 0042:..., got %d bytes", len(sv3.S))
	}
	if sv3.String() != "42:"+long {
		t.Errorf("re-encode of 0042: got %q, want %q", sv3.String(), "42:"+long)
	}
}

func TestDecoder_Decode_StringLeadingZeroLength(t *testing.T) {
	// aria2 C++ accepts leading zeros in string lengths: "04:spam" → "spam"
	d := bencode.NewDecoder(strings.NewReader("04:spam"))
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode(04:spam) error: %v (aria2 accepts leading zeros in string lengths)", err)
	}
	sv := v.(bencode.StringVal)
	if sv.S != "spam" {
		t.Errorf("expected %q, got %q", "spam", sv.S)
	}

	// "00:" → empty string (double zero)
	d2 := bencode.NewDecoder(strings.NewReader("00:"))
	v2, err := d2.Decode()
	if err != nil {
		t.Fatalf("Decode(00:) error: %v", err)
	}
	sv2 := v2.(bencode.StringVal)
	if sv2.S != "" {
		t.Errorf("expected empty string, got %q", sv2.S)
	}
}

func TestKindDetection(t *testing.T) {
	tests := []struct {
		input string
		kind  bencode.Kind
	}{
		{"4:spam", bencode.KindString},
		{"i42e", bencode.KindInt},
		{"le", bencode.KindList},
		{"de", bencode.KindDict},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d := bencode.NewDecoder(strings.NewReader(tt.input))
			v, err := d.Decode()
			if err != nil {
				t.Fatalf("Decode() error: %v", err)
			}
			if v.Kind() != tt.kind {
				t.Errorf("Kind() = %v, want %v", v.Kind(), tt.kind)
			}
		})
	}
}

func TestTorrentLikeDict(t *testing.T) {
	// Simulates a minimal .torrent info dict structure (keys sorted)
	input := "d8:announce16:http://tracker/e4:infod6:lengthi1234e4:name4:test12:piece lengthi262144eee"
	d := bencode.NewDecoder(strings.NewReader(input))
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	dv, ok := v.(*bencode.DictVal)
	if !ok {
		t.Fatalf("expected *DictVal, got %T", v)
	}
	if len(dv.Keys) != 2 {
		t.Errorf("expected 2 top-level keys, got %d", len(dv.Keys))
	}

	info, ok := dv.Get("info")
	if !ok {
		t.Fatal("missing info key")
	}
	infoDict, ok := info.(*bencode.DictVal)
	if !ok {
		t.Fatalf("info is not *DictVal: %T", info)
	}
	name, ok := infoDict.Get("name")
	if !ok {
		t.Fatal("missing name in info")
	}
	if name.String() != "4:test" {
		t.Errorf("name = %q, want 4:test", name.String())
	}
}

func TestDecoder_NegativeZero(t *testing.T) {
	// aria2 accepts i-0e (returns 0). Verify we match.
	d := bencode.NewDecoder(strings.NewReader("i-0e"))
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode(i-0e) error: %v", err)
	}
	iv := v.(bencode.IntVal)
	if iv.I != 0 {
		t.Errorf("i-0e parsed as %d, want 0", iv.I)
	}
}

func TestDecoder_PositiveSign(t *testing.T) {
	d := bencode.NewDecoder(strings.NewReader("i+42e"))
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode(i+42e) error: %v", err)
	}
	iv := v.(bencode.IntVal)
	if iv.I != 42 {
		t.Errorf("i+42e parsed as %d, want 42", iv.I)
	}
}

func TestDecoder_PositiveSign_Zero(t *testing.T) {
	d := bencode.NewDecoder(strings.NewReader("i+0e"))
	v, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode(i+0e) error: %v", err)
	}
	iv := v.(bencode.IntVal)
	if iv.I != 0 {
		t.Errorf("i+0e parsed as %d, want 0", iv.I)
	}
}

func TestDecoder_PositiveSign_NoDigits(t *testing.T) {
	d := bencode.NewDecoder(strings.NewReader("i+e"))
	_, err := d.Decode()
	if err == nil {
		t.Error("expected error for i+e (sign without digits)")
	}
}

func TestDecoder_FloatNotation(t *testing.T) {
	// aria2 tolerates floating-point notation in integer fields by
	// skipping bytes and returning 0.
	tests := []string{
		"i-1.134E+3e",
		"i1.0e",
		"i0e-3e",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			d := bencode.NewDecoder(strings.NewReader(input))
			v, err := d.Decode()
			if err != nil {
				t.Fatalf("Decode(%q) error: %v", input, err)
			}
			iv := v.(bencode.IntVal)
			if iv.I != 0 {
				t.Errorf("float %q parsed as %d, want 0", input, iv.I)
			}
		})
	}
}

func TestDecoder_NestingDepthLimit(t *testing.T) {
	// Build a list nested 51 levels deep: llll...leeee...
	depth := 51
	var builder strings.Builder
	for i := 0; i < depth; i++ {
		builder.WriteByte('l')
	}
	for i := 0; i < depth; i++ {
		builder.WriteByte('e')
	}
	d := bencode.NewDecoder(strings.NewReader(builder.String()))
	_, err := d.Decode()
	if !errors.Is(err, bencode.ErrStructureTooDeep) {
		t.Errorf("expected ErrStructureTooDeep, got %v", err)
	}
}

func TestDecoder_NestingDepthDict(t *testing.T) {
	// Build a dict nested 51 levels deep
	depth := 51
	var builder strings.Builder
	for i := 0; i < depth; i++ {
		builder.WriteString("d1:k")
	}
	for i := 0; i < depth; i++ {
		builder.WriteByte('e')
	}
	d := bencode.NewDecoder(strings.NewReader(builder.String()))
	_, err := d.Decode()
	if !errors.Is(err, bencode.ErrStructureTooDeep) {
		t.Errorf("expected ErrStructureTooDeep for dict, got %v", err)
	}
}

func TestDecoder_NestingDepthAtLimit(t *testing.T) {
	// 50 levels deep should succeed
	depth := 49 // maxDepth=50, starting from depth=0
	var builder strings.Builder
	for i := 0; i < depth; i++ {
		builder.WriteByte('l')
	}
	for i := 0; i < depth; i++ {
		builder.WriteByte('e')
	}
	d := bencode.NewDecoder(strings.NewReader(builder.String()))
	_, err := d.Decode()
	if err != nil {
		t.Errorf("49-level list should succeed, got: %v", err)
	}
}
