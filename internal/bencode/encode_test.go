package bencode_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/bencode"
)

func TestMarshal_StringVal(t *testing.T) {
	tests := []struct {
		name  string
		input bencode.StringVal
		want  string
	}{
		{"basic", bencode.StringVal{S: "spam"}, "4:spam"},
		{"empty", bencode.StringVal{S: ""}, "0:"},
		{"with spaces", bencode.StringVal{S: "hello world!"}, "12:hello world!"},
		{"binary", bencode.StringVal{S: "\xff\xfe\xfd"}, "3:\xff\xfe\xfd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bencode.Marshal(tt.input)
			if err != nil {
				t.Fatalf("Marshal() error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestMarshal_IntVal(t *testing.T) {
	tests := []struct {
		name  string
		input bencode.IntVal
		want  string
	}{
		{"positive", bencode.IntVal{I: 3}, "i3e"},
		{"negative", bencode.IntVal{I: -42}, "i-42e"},
		{"zero", bencode.IntVal{I: 0}, "i0e"},
		{"large", bencode.IntVal{I: 99999999}, "i99999999e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bencode.Marshal(tt.input)
			if err != nil {
				t.Fatalf("Marshal() error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestMarshal_ListVal(t *testing.T) {
	tests := []struct {
		name  string
		input bencode.ListVal
		want  string
	}{
		{
			name:  "empty",
			input: bencode.ListVal{L: []bencode.Value{}},
			want:  "le",
		},
		{
			name: "two strings",
			input: bencode.ListVal{L: []bencode.Value{
				bencode.StringVal{S: "spam"},
				bencode.StringVal{S: "eggs"},
			}},
			want: "l4:spam4:eggse",
		},
		{
			name: "three ints",
			input: bencode.ListVal{L: []bencode.Value{
				bencode.IntVal{I: 1},
				bencode.IntVal{I: 2},
				bencode.IntVal{I: 3},
			}},
			want: "li1ei2ei3ee",
		},
		{
			name: "nested lists",
			input: bencode.ListVal{L: []bencode.Value{
				bencode.ListVal{L: []bencode.Value{
					bencode.StringVal{S: "a"},
					bencode.StringVal{S: "b"},
				}},
				bencode.ListVal{L: []bencode.Value{
					bencode.StringVal{S: "xy"},
				}},
			}},
			want: "ll1:a1:bel2:xyee",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bencode.Marshal(tt.input)
			if err != nil {
				t.Fatalf("Marshal() error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestMarshal_DictVal(t *testing.T) {
	tests := []struct {
		name string
		dict *bencode.DictVal
		want string
	}{
		{
			name: "empty dict",
			dict: bencode.NewDict(),
			want: "de",
		},
		{
			name: "string values",
			dict: func() *bencode.DictVal {
				d := bencode.NewDict()
				d.Set("cow", bencode.NewString("moo"))
				d.Set("spam", bencode.NewString("eggs"))
				return d
			}(),
			want: "d3:cow3:moo4:spam4:eggse",
		},
		{
			name: "mixed types",
			dict: func() *bencode.DictVal {
				d := bencode.NewDict()
				d.Set("key", bencode.NewInt(42))
				return d
			}(),
			want: "d3:keyi42ee",
		},
		{
			name: "nested dict",
			dict: func() *bencode.DictVal {
				inner := bencode.NewDict()
				inner.Set("x", bencode.NewInt(1))
				d := bencode.NewDict()
				d.Set("inner", inner)
				return d
			}(),
			want: "d5:innerd1:xi1eee",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bencode.Marshal(tt.dict)
			if err != nil {
				t.Fatalf("Marshal() error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestMarshal_DictKeySorting(t *testing.T) {
	d := bencode.NewDict()
	d.Set("spam", bencode.NewString("eggs"))
	d.Set("cow", bencode.NewString("moo"))
	d.Set("abc", bencode.NewInt(1))

	got, err := bencode.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	want := "d3:abci1e3:cow3:moo4:spam4:eggse"
	if string(got) != want {
		t.Errorf("Marshal() = %q, want %q (keys must be sorted)", string(got), want)
	}

	if string(d.Keys[0]) != "spam" {
		t.Errorf("Keys insertion order preserved: %v", d.Keys)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	tests := []string{
		"4:spam",
		"i42e",
		"i-7e",
		"i0e",
		"le",
		"de",
		"l4:spam4:eggse",
		"li1ei2ei3ee",
		"d3:cow3:moo4:spam4:eggse",
		"ll1:a1:bel2:xyee",
		"d5:innerd1:xi1eee",
		"3:\x00\x01\x02",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			d := bencode.NewDecoder(strings.NewReader(input))
			v, err := d.Decode()
			if err != nil {
				t.Fatalf("Decode() error: %v", err)
			}
			got, err := bencode.Marshal(v)
			if err != nil {
				t.Fatalf("Marshal() error: %v", err)
			}
			if string(got) != input {
				t.Errorf("Marshal(Decode(%q)) = %q", input, string(got))
			}
		})
	}
}

func TestMarshal_NilValue(t *testing.T) {
	_, err := bencode.Marshal(nil)
	if err == nil {
		t.Error("Marshal(nil) should return an error")
	}
}

func TestExtractRaw_TopLevelDict(t *testing.T) {
	data := []byte("d8:announce16:http://tracker/e4:infod6:lengthi1234e4:name4:test12:piece lengthi262144eee")

	start, end, err := bencode.ExtractRaw(data, "info")
	if err != nil {
		t.Fatalf("ExtractRaw(info) error: %v", err)
	}

	infoBytes := data[start:end]
	expected := "d6:lengthi1234e4:name4:test12:piece lengthi262144ee"
	if string(infoBytes) != expected {
		t.Errorf("ExtractRaw(info) = %q, want %q", string(infoBytes), expected)
	}
}

func TestExtractRaw_NestedDictKey(t *testing.T) {
	data := []byte("d8:announce16:http://tracker/e4:infod6:lengthi1234e4:name4:test12:piece lengthi262144eee")

	start, end, err := bencode.ExtractRaw(data, "info", "name")
	if err != nil {
		t.Fatalf("ExtractRaw(info, name) error: %v", err)
	}

	nameBytes := data[start:end]
	if string(nameBytes) != "4:test" {
		t.Errorf("ExtractRaw(info, name) = %q, want %q", string(nameBytes), "4:test")
	}
}

func TestExtractRaw_NestedInt(t *testing.T) {
	data := []byte("d8:announce16:http://tracker/e4:infod6:lengthi1234e4:name4:test12:piece lengthi262144eee")

	start, end, err := bencode.ExtractRaw(data, "info", "length")
	if err != nil {
		t.Fatalf("ExtractRaw(info, length) error: %v", err)
	}

	lenBytes := data[start:end]
	expected := "i1234e"
	if string(lenBytes) != expected {
		t.Errorf("ExtractRaw(info, length) = %q, want %q", string(lenBytes), expected)
	}
}

func TestExtractRaw_WithListPath(t *testing.T) {
	data := []byte("d4:listli1ei2ei3ee8:infod6:lengthi999eee")

	start, end, err := bencode.ExtractRaw(data, "list", "1")
	if err != nil {
		t.Fatalf("ExtractRaw(list, 1) error: %v", err)
	}

	itemBytes := data[start:end]
	if string(itemBytes) != "i2e" {
		t.Errorf("ExtractRaw(list, 1) = %q, want %q", string(itemBytes), "i2e")
	}

	start, end, err = bencode.ExtractRaw(data, "list", "2")
	if err != nil {
		t.Fatalf("ExtractRaw(list, 2) error: %v", err)
	}
	if string(data[start:end]) != "i3e" {
		t.Errorf("ExtractRaw(list, 2) = %q, want %q", string(data[start:end]), "i3e")
	}
}

func TestExtractRaw_KeyNotFound(t *testing.T) {
	data := []byte("d3:keyi42ee")

	_, _, err := bencode.ExtractRaw(data, "missing")
	if err == nil {
		t.Error("ExtractRaw(missing) should return an error")
	}
}

func TestExtractRaw_ListIndexOutOfRange(t *testing.T) {
	data := []byte("li1ei2ee")

	_, _, err := bencode.ExtractRaw(data, "5")
	if err == nil {
		t.Error("ExtractRaw(5) should return an error for out-of-range list index")
	}
}

func TestExtractRaw_EndOffsetExclusive(t *testing.T) {
	data := []byte("4:spam")
	start, end, err := bencode.ExtractRaw(data)
	if err != nil {
		t.Fatalf("ExtractRaw() error: %v", err)
	}
	if start != 0 {
		t.Errorf("start = %d, want 0", start)
	}
	if end != 6 {
		t.Errorf("end = %d, want 6 (exclusive)", end)
	}
	if data[start:end] == nil {
		t.Error("data[start:end] should not be nil")
	}
}

func TestExtractRaw_IntegerValue(t *testing.T) {
	data := []byte("i42e")
	start, end, err := bencode.ExtractRaw(data)
	if err != nil {
		t.Fatalf("ExtractRaw() error: %v", err)
	}
	if string(data[start:end]) != "i42e" {
		t.Errorf("ExtractRaw() = %q, want %q", string(data[start:end]), "i42e")
	}
}

func TestExtractRaw_ListValue(t *testing.T) {
	data := []byte("l4:spami42ee")
	start, end, err := bencode.ExtractRaw(data)
	if err != nil {
		t.Fatalf("ExtractRaw() error: %v", err)
	}
	if string(data[start:end]) != "l4:spami42ee" {
		t.Errorf("ExtractRaw() = %q, want %q", string(data[start:end]), "l4:spami42ee")
	}
}

func TestExtractRaw_EmptyPath(t *testing.T) {
	data := []byte("d3:keyi42ee")
	start, end, err := bencode.ExtractRaw(data)
	if err != nil {
		t.Fatalf("ExtractRaw() error: %v", err)
	}
	if string(data[start:end]) != "d3:keyi42ee" {
		t.Errorf("ExtractRaw() = %q, want %q", string(data[start:end]), "d3:keyi42ee")
	}
}

func TestExtractRaw_InvalidData(t *testing.T) {
	_, _, err := bencode.ExtractRaw([]byte(""), "key")
	if err == nil {
		t.Error("ExtractRaw on empty data should error")
	}
}

func TestExtractRaw_InfoHashScenario(t *testing.T) {
	torrentData := []byte("d8:announce16:http://tracker/e4:infod6:lengthi1234e4:name4:test12:piece lengthi262144eee")

	start, end, err := bencode.ExtractRaw(torrentData, "info")
	if err != nil {
		t.Fatalf("ExtractRaw(info) error: %v", err)
	}

	infoBytes := torrentData[start:end]

	// Verify we can decode the extracted bytes
	d := bencode.NewDecoder(bytes.NewReader(infoBytes))
	val, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode info bytes error: %v", err)
	}
	dv, ok := val.(*bencode.DictVal)
	if !ok {
		t.Fatalf("info is not *DictVal")
	}

	name, _ := dv.Get("name")
	if name.String() != "4:test" {
		t.Errorf("name = %q", name.String())
	}
	length, _ := dv.Get("length")
	if length.String() != "i1234e" {
		t.Errorf("length = %q", length.String())
	}
}

func TestMarshal_AllTypesInList(t *testing.T) {
	d := bencode.NewDict()
	d.Set("k", bencode.NewString("v"))

	l := bencode.ListVal{L: []bencode.Value{
		bencode.StringVal{S: "s"},
		bencode.IntVal{I: 42},
		bencode.ListVal{L: []bencode.Value{}},
		d,
	}}

	got, err := bencode.Marshal(l)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	// Decode back and verify
	d2 := bencode.NewDecoder(bytes.NewReader(got))
	v, err := d2.Decode()
	if err != nil {
		t.Fatalf("Re-Decode() error: %v", err)
	}

	lv, ok := v.(bencode.ListVal)
	if !ok {
		t.Fatalf("expected ListVal, got %T", v)
	}

	if len(lv.L) != 4 {
		t.Errorf("list length = %d, want 4", len(lv.L))
	}
	if lv.L[0].String() != "1:s" {
		t.Errorf("lv.L[0] = %q, want 1:s", lv.L[0].String())
	}
	if lv.L[1].String() != "i42e" {
		t.Errorf("lv.L[1] = %q, want i42e", lv.L[1].String())
	}
	if lv.L[2].String() != "le" {
		t.Errorf("lv.L[2] = %q, want le", lv.L[2].String())
	}
	if lv.L[3].String() != "d1:k1:ve" {
		t.Errorf("lv.L[3] = %q, want d1:k1:ve", lv.L[3].String())
	}
}

func TestExtractRaw_NestedListInDict(t *testing.T) {
	data := []byte("d4:datad5:filesl9:file1.txt10:file2.txtee")

	start, end, err := bencode.ExtractRaw(data, "data", "files")
	if err != nil {
		t.Fatalf("ExtractRaw(data, files) error: %v", err)
	}

	subData := data[start:end]
	if string(subData) != "l9:file1.txt10:file2.txtee" {
		t.Errorf("ExtractRaw(data, files) = %q, want %q", string(subData), "l9:file1.txt10:file2.txtee")
	}
}

func TestExtractRaw_DictInList(t *testing.T) {
	data := []byte("ld1:xi1eed1:yi2eee")

	start, end, err := bencode.ExtractRaw(data, "1")
	if err != nil {
		t.Fatalf("ExtractRaw(1) error: %v", err)
	}

	if string(data[start:end]) != "d1:yi2ee" {
		t.Errorf("ExtractRaw(1) = %q, want d1:yi2ee", string(data[start:end]))
	}
}

func TestDecodeSampleBencode(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.bencode")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	d := bencode.NewDecoder(bytes.NewReader(data))
	val, err := d.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	dv, ok := val.(*bencode.DictVal)
	if !ok {
		t.Fatalf("expected *DictVal, got %T", val)
	}

	if ann, ok := dv.Get("announce"); !ok || ann.String() != "35:http://tracker.example.com/announce" {
		t.Errorf("announce = %v", ann)
	}
	if infoVal, ok := dv.Get("info"); ok {
		if infoDict, ok := infoVal.(*bencode.DictVal); ok {
			if name, ok := infoDict.Get("name"); !ok || name.String() != "11:complex.bin" {
				t.Errorf("name = %v", name)
			}
		}
	}
	// Round-trip marshal
	got, marshalErr := bencode.Marshal(val)
	if marshalErr != nil {
		t.Fatalf("Marshal: %v", marshalErr)
	}
	if !bytes.Equal(got, data) {
		t.Error("round-trip marshal does not match original data")
	}
}
