package metalink_test

import (
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/protocol/metalink"
)

var benchV4XML = `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <size>123456789</size>
    <hash type="sha-256">ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
    <url priority="1">http://mirror1.example.com/example.iso</url>
    <url priority="2" location="us">http://mirror2.example.com/example.iso</url>
    <version>1.0</version>
    <language>en</language>
    <os>linux</os>
  </file>
</metalink>`

var benchV3XML = `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file name="example.iso">
      <size>123456789</size>
      <version>1.0</version>
      <language>en</language>
      <language>ja</language>
      <resources>
        <url type="http" location="us" preference="100">http://mirror1.example.com/example.iso</url>
        <url type="ftp" location="eu" preference="50">ftp://mirror2.example.com/example.iso</url>
      </resources>
      <verification>
        <hash type="sha-256">ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
        <pieces length="262144" type="sha-256">
          <hash piece="0">ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
          <hash piece="1">ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a</hash>
        </pieces>
      </verification>
    </file>
  </files>
</metalink>`

func BenchmarkParseV4(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := metalink.ParseV4(strings.NewReader(benchV4XML))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseAutoDetectV4(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := metalink.Parse(strings.NewReader(benchV4XML))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseV3(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := metalink.Parse(strings.NewReader(benchV3XML))
		if err != nil {
			b.Fatal(err)
		}
	}
}
