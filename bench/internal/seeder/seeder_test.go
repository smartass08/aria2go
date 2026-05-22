package seeder

import (
	"testing"
)

func TestSeederStart(t *testing.T) {
	cfg := Config{
		NumTorrents: 2,
		FileSizeMB:  1,
		PieceLen:    64 * 1024,
	}

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if got := s.TrackerURL(); got == "" {
		t.Fatalf("TrackerURL empty")
	}

	if err := s.Start(cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := len(s.InfoHashes()); got != cfg.NumTorrents {
		t.Fatalf("InfoHashes len=%d want=%d", got, cfg.NumTorrents)
	}
	if got := len(s.Torrents()); got != cfg.NumTorrents {
		t.Fatalf("Torrents len=%d want=%d", got, cfg.NumTorrents)
	}
	if got := len(s.MagnetURIs()); got != cfg.NumTorrents {
		t.Fatalf("MagnetURIs len=%d want=%d", got, cfg.NumTorrents)
	}

	for i, uri := range s.MagnetURIs() {
		if uri == "" {
			t.Errorf("MagnetURI[%d] empty", i)
		}
		if len(uri) < 60 {
			t.Errorf("MagnetURI[%d] too short: %s", i, uri)
		}
	}

	for _, ih := range s.InfoHashes() {
		mi := s.MetainfoBytes(ih)
		if mi == nil {
			t.Errorf("MetainfoBytes for %x is nil", ih)
		}
		if len(mi) < 50 {
			t.Errorf("MetainfoBytes for %x too short: %d bytes", ih, len(mi))
		}
	}
}

func TestSeederCloseClean(t *testing.T) {
	cfg := Config{
		NumTorrents: 1,
		FileSizeMB:  1,
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestPRNGDeterministic(t *testing.T) {
	seed := uint64(0x1234567890abcdef)

	b1 := generateByte(seed, 0)
	b2 := generateByte(seed, 0)
	if b1 != b2 {
		t.Fatalf("PRNG not deterministic: %d != %d", b1, b2)
	}

	b3 := generateByte(seed, 100)
	b4 := generateByte(seed, 100)
	if b3 != b4 {
		t.Fatalf("PRNG not deterministic at offset 100: %d != %d", b3, b4)
	}
}

func TestGeneratedStorageReadOnly(t *testing.T) {
	p := generatedPiece{
		torrent: generatedTorrent{seed: 1, length: 100},
	}

	_, err := p.WriteAt([]byte{1, 2, 3}, 0)
	if err == nil {
		t.Fatalf("WriteAt should fail on read-only storage")
	}
}

func TestGeneratedPieceCompletion(t *testing.T) {
	p := generatedPiece{
		torrent: generatedTorrent{seed: 1, length: 100},
	}

	comp := p.Completion()
	if !comp.Ok {
		t.Fatalf("Completion not Ok")
	}
	if !comp.Complete {
		t.Fatalf("Completion not Complete")
	}
}
