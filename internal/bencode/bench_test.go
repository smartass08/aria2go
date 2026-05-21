package bencode_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/bencode"
)

var benchResult bencode.Value

func BenchmarkDecodeString(b *testing.B) {
	data := []byte("4:spam")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := bencode.NewDecoder(bytes.NewReader(data))
		benchResult, _ = d.Decode()
	}
}

func BenchmarkDecodeInt(b *testing.B) {
	data := []byte("i42e")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := bencode.NewDecoder(bytes.NewReader(data))
		benchResult, _ = d.Decode()
	}
}

func BenchmarkDecodeList(b *testing.B) {
	data := []byte("li1ei2ei3ee")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := bencode.NewDecoder(bytes.NewReader(data))
		benchResult, _ = d.Decode()
	}
}

func BenchmarkDecodeDict(b *testing.B) {
	data := []byte("d3:cow3:moo4:spam4:eggse")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := bencode.NewDecoder(bytes.NewReader(data))
		benchResult, _ = d.Decode()
	}
}

func BenchmarkMarshalString(b *testing.B) {
	v := bencode.StringVal{S: "spam"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bencode.Marshal(v)
	}
}

func BenchmarkMarshalInt(b *testing.B) {
	v := bencode.IntVal{I: 42}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bencode.Marshal(v)
	}
}

func BenchmarkMarshalList(b *testing.B) {
	v := bencode.ListVal{L: []bencode.Value{
		bencode.IntVal{I: 1},
		bencode.IntVal{I: 2},
		bencode.IntVal{I: 3},
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bencode.Marshal(v)
	}
}

func BenchmarkMarshalDict(b *testing.B) {
	v := func() bencode.Value {
		d := bencode.NewDict()
		d.Set("cow", bencode.NewString("moo"))
		d.Set("spam", bencode.NewString("eggs"))
		return d
	}()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bencode.Marshal(v)
	}
}

func BenchmarkExtractRaw(b *testing.B) {
	data := []byte("d8:announce16:http://tracker/e4:infod6:lengthi1234e4:name4:test12:piece lengthi262144eee")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = bencode.ExtractRaw(data, "info", "name")
	}
}

func BenchmarkTorrentDecode(b *testing.B) {
	longStr := strings.Repeat("x", 200)
	input := "d8:announce16:http://tracker/e4:infod6:lengthi" + longStr + "e4:name4:test12:piece lengthi262144eee"
	data := []byte(input)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := bencode.NewDecoder(bytes.NewReader(data))
		benchResult, _ = d.Decode()
	}
}

func BenchmarkStringVal_String(b *testing.B) {
	v := bencode.StringVal{S: "spam"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.String()
	}
}

func BenchmarkIntVal_String(b *testing.B) {
	v := bencode.IntVal{I: 42}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.String()
	}
}

func BenchmarkDictVal_String(b *testing.B) {
	v := func() *bencode.DictVal {
		d := bencode.NewDict()
		d.Set("cow", bencode.NewString("moo"))
		d.Set("spam", bencode.NewString("eggs"))
		return d
	}()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.String()
	}
}
