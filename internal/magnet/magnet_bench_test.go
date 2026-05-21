package magnet

import (
	"testing"
)

func BenchmarkParseSimpleV1(b *testing.B) {
	raw := "magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Parse(raw)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseFullURI(b *testing.B) {
	raw := "magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" +
		"&dn=My%20File" +
		"&xl=1048576" +
		"&tr=http%3A%2F%2Ftracker.example.com%3A6969%2Fannounce" +
		"&tr=udp%3A%2F%2Ftracker2.example.com%3A6881%2Fannounce" +
		"&xs=http%3A%2F%2Fexample.com%2Ffile.torrent" +
		"&as=http%3A%2F%2Fexample.com%2Fsource" +
		"&x.pe=192.168.1.1%3A6881" +
		"&kt=keyword1" +
		"&kt=keyword2" +
		"&mt=topic1"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Parse(raw)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkString(b *testing.B) {
	v1Hex := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	m, err := Parse("magnet:?xt=urn:btih:" + v1Hex + "&dn=Test%20File&xl=1048576&tr=http://tracker.example.com/announce")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.String()
	}
}

func BenchmarkParseRealWorld(b *testing.B) {
	raw := "magnet:?xt=urn:btih:7C2C2DDF4F22F3A366A2A23ECB0B37B7793C9E21" +
		"&dn=ubuntu-22.04.3-desktop-amd64.iso" +
		"&tr=http://tracker.example.com:6969/announce" +
		"&tr=udp://tracker.opentrackr.org:1337/announce" +
		"&xl=5041676288"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Parse(raw)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseCaseInsensitive(b *testing.B) {
	raw := "magnet:?XT=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" +
		"&DN=hello" +
		"&XL=500" +
		"&TR=http://t.example.com/announce"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Parse(raw)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPercentDecode(b *testing.B) {
	s := "My%20File%2Etxt%20with%20spaces"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = percentDecodeBytes(make([]byte, 0, len(s)), []byte(s))
	}
}

func BenchmarkMagnetPercentEncode(b *testing.B) {
	type tc struct {
		name string
		in   string
	}
	cases := []tc{
		{"ascii", "http://tracker.example.com:6969/announce"},
		{"with_spaces", "My File.txt"},
		{"with_percent", "100% file"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = magnetPercentEncode(c.in)
			}
		})
	}
}
