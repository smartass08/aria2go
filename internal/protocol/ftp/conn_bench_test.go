package ftp

import (
	"testing"
)

func BenchmarkParsePASV(b *testing.B) {
	msg := "227 Entering Passive Mode (127,0,0,1,4,210)."
	for b.Loop() {
		_, _, _ = parsePASV(msg)
	}
}

func BenchmarkParseEPSV(b *testing.B) {
	msg := "229 Entering Extended Passive Mode (|||1234|)."
	for b.Loop() {
		_, _ = parseEPSV(msg)
	}
}

func BenchmarkBuildHost(b *testing.B) {
	octets := []int{127, 0, 0, 1}
	for b.Loop() {
		_ = buildHost(octets)
	}
}
