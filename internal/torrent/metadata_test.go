package torrent

import (
	"bytes"
	"crypto/sha1"
	"testing"

	"github.com/smartass08/aria2go/internal/bencode"
)

func TestFromMetadataBuildsTorrentWithAnnounceList(t *testing.T) {
	info := bencode.NewDict()
	info.Set("length", bencode.NewInt(12))
	info.Set("name", bencode.NewString("fixture.bin"))
	info.Set("piece length", bencode.NewInt(16*1024))
	sum := sha1.Sum([]byte("hello world!"))
	info.Set("pieces", bencode.NewString(string(sum[:])))

	infoRaw, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal(info) error = %v", err)
	}

	torrentRaw, err := FromMetadata(infoRaw, [][]string{{"http://tracker.example/announce"}, {"udp://tracker.example:80"}})
	if err != nil {
		t.Fatalf("FromMetadata() error = %v", err)
	}

	meta, err := Load(torrentRaw)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := len(meta.AnnounceList); got != 2 {
		t.Fatalf("announce-list tiers = %d, want 2", got)
	}
	if meta.Announce != "" {
		t.Fatalf("announce = %q, want empty source-truth style output", meta.Announce)
	}

	gotHash, err := meta.InfoHash()
	if err != nil {
		t.Fatalf("InfoHash() error = %v", err)
	}
	wantHash := sha1.Sum(infoRaw)
	if gotHash != wantHash {
		t.Fatalf("InfoHash = %x, want %x", gotHash, wantHash)
	}
	if !bytes.Equal(meta.infoRaw, infoRaw) {
		t.Fatal("embedded info dictionary bytes changed")
	}
}

func TestFromMetadataRejectsInvalidInput(t *testing.T) {
	if _, err := FromMetadata([]byte("not-bencode"), nil); err == nil {
		t.Fatal("expected invalid metadata error")
	}
}
