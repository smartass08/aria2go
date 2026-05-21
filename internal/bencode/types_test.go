package bencode_test

import (
	"testing"

	"github.com/smartass08/aria2go/internal/bencode"
)

func TestStringVal_Kind(t *testing.T) {
	v := bencode.StringVal{S: "hello"}
	if v.Kind() != bencode.KindString {
		t.Errorf("StringVal.Kind() = %v, want KindString", v.Kind())
	}
}

func TestStringVal_String(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"spam", "4:spam"},
		{"", "0:"},
		{"hello world!", "12:hello world!"},
	}
	for _, tt := range tests {
		v := bencode.StringVal{S: tt.input}
		got := v.String()
		if got != tt.want {
			t.Errorf("StringVal{%q}.String() = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIntVal_Kind(t *testing.T) {
	v := bencode.IntVal{I: 42}
	if v.Kind() != bencode.KindInt {
		t.Errorf("IntVal.Kind() = %v, want KindInt", v.Kind())
	}
}

func TestIntVal_String(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{3, "i3e"},
		{-42, "i-42e"},
		{0, "i0e"},
		{99999999, "i99999999e"},
	}
	for _, tt := range tests {
		v := bencode.IntVal{I: tt.input}
		got := v.String()
		if got != tt.want {
			t.Errorf("IntVal{%d}.String() = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestListVal_Kind(t *testing.T) {
	v := bencode.ListVal{L: []bencode.Value{}}
	if v.Kind() != bencode.KindList {
		t.Errorf("ListVal.Kind() = %v, want KindList", v.Kind())
	}
}

func TestListVal_String(t *testing.T) {
	tests := []struct {
		name string
		list bencode.ListVal
		want string
	}{
		{
			name: "empty list",
			list: bencode.ListVal{L: []bencode.Value{}},
			want: "le",
		},
		{
			name: "two strings",
			list: bencode.ListVal{L: []bencode.Value{
				bencode.StringVal{S: "spam"},
				bencode.StringVal{S: "eggs"},
			}},
			want: "l4:spam4:eggse",
		},
		{
			name: "three integers",
			list: bencode.ListVal{L: []bencode.Value{
				bencode.IntVal{I: 1},
				bencode.IntVal{I: 2},
				bencode.IntVal{I: 3},
			}},
			want: "li1ei2ei3ee",
		},
		{
			name: "nested lists",
			list: bencode.ListVal{L: []bencode.Value{
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
			got := tt.list.String()
			if got != tt.want {
				t.Errorf("ListVal.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDictVal_Kind(t *testing.T) {
	d := bencode.NewDict()
	if d.Kind() != bencode.KindDict {
		t.Errorf("DictVal.Kind() = %v, want KindDict", d.Kind())
	}
}

func TestDictVal_String(t *testing.T) {
	tests := []struct {
		name string
		dict *bencode.DictVal
		want string
	}{
		{
			name: "empty dict",
			dict: func() *bencode.DictVal {
				return bencode.NewDict()
			}(),
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
			got := tt.dict.String()
			if got != tt.want {
				t.Errorf("DictVal.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDictVal_String_SortedKeys(t *testing.T) {
	d := bencode.NewDict()
	// Insert in non-lexicographic order
	d.Set("spam", bencode.NewString("eggs"))
	d.Set("cow", bencode.NewString("moo"))
	d.Set("abc", bencode.NewInt(1))

	got := d.String()
	want := "d3:abci1e3:cow3:moo4:spam4:eggse"
	if got != want {
		t.Errorf("DictVal.String() = %q, want %q (keys should be sorted)", got, want)
	}

	// Keys should still be in insertion order
	if len(d.Keys) != 3 || d.Keys[0] != "spam" || d.Keys[1] != "cow" || d.Keys[2] != "abc" {
		t.Errorf("Keys = %v, want [spam cow abc] (insertion order preserved)", d.Keys)
	}
}

func TestDictVal_Set_And_Get(t *testing.T) {
	d := bencode.NewDict()

	// Set a new key
	d.Set("foo", bencode.NewInt(42))
	v, ok := d.Get("foo")
	if !ok {
		t.Error("Get(foo) = _, false; want _, true")
	}
	if v.String() != "i42e" {
		t.Errorf("Get(foo).String() = %q, want i42e", v.String())
	}

	// Update existing key
	d.Set("foo", bencode.NewString("bar"))
	v, ok = d.Get("foo")
	if !ok {
		t.Error("Get(foo) after update = _, false; want _, true")
	}
	if sv, ok := v.(bencode.StringVal); !ok || sv.S != "bar" {
		t.Errorf("Get(foo) = %v, want StringVal{bar}", v)
	}

	// Keys should not duplicate
	if len(d.Keys) != 1 {
		t.Errorf("Keys length = %d, want 1 (no duplicates)", len(d.Keys))
	}

	// Get missing key
	_, ok = d.Get("missing")
	if ok {
		t.Error("Get(missing) = _, true; want _, false")
	}
}

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind bencode.Kind
		want string
	}{
		{bencode.KindString, "string"},
		{bencode.KindInt, "integer"},
		{bencode.KindList, "list"},
		{bencode.KindDict, "dict"},
	}
	for _, tt := range tests {
		got := tt.kind.String()
		if got != tt.want {
			t.Errorf("Kind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestHelperConstructors(t *testing.T) {
	// NewString
	sv := bencode.NewString("test")
	if sv.Kind() != bencode.KindString {
		t.Error("NewString.Kind() != KindString")
	}

	// NewInt
	iv := bencode.NewInt(99)
	if iv.Kind() != bencode.KindInt {
		t.Error("NewInt.Kind() != KindInt")
	}

	// NewList
	lv := bencode.NewList(bencode.NewInt(1), bencode.NewInt(2))
	if lv.Kind() != bencode.KindList {
		t.Error("NewList.Kind() != KindList")
	}

	// NewDict
	dv := bencode.NewDict()
	if dv.Kind() != bencode.KindDict {
		t.Error("NewDict.Kind() != KindDict")
	}
	if dv.Values == nil {
		t.Error("NewDict.Values is nil")
	}
	if dv.Keys == nil {
		t.Error("NewDict.Keys is nil")
	}
}

func TestStringVal_NonUTF8(t *testing.T) {
	v := bencode.StringVal{S: "\xFF\xFE\xFD"}
	got := v.String()
	want := "3:\xff\xfe\xfd"
	if got != want {
		t.Errorf("StringVal.String() = %q, want %q", got, want)
	}
}
