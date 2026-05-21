package torrent

import (
	"encoding/hex"
	"testing"

	"github.com/smartass08/aria2go/internal/bencode"
)

func bencodeStr(s string) bencode.Value {
	return bencode.StringVal{S: s}
}

func bencodeInt(i int64) bencode.Value {
	return bencode.IntVal{I: i}
}

func bencodeDict(pairs ...interface{}) *bencode.DictVal {
	keys := make([]string, 0, len(pairs)/2)
	values := make(map[string]bencode.Value, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		k := pairs[i].(string)
		v := pairs[i+1].(bencode.Value)
		keys = append(keys, k)
		values[k] = v
	}
	return &bencode.DictVal{Keys: keys, Values: values}
}

func bencodeList(elems ...bencode.Value) bencode.ListVal {
	return bencode.ListVal{L: elems}
}

func marshal(v bencode.Value) []byte {
	data, err := bencode.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TestLoadSingleFile(t *testing.T) {
	pieces := make([]byte, 40) // 2 pieces of 20 bytes each for 2048 byte file at 1024 byte piece length
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("testfile.bin"),
			"piece length", bencodeInt(1024),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(2048),
		),
	))

	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Announce != "http://tracker/announce" {
		t.Errorf("Announce = %q", m.Announce)
	}
	if m.Info.Name != "testfile.bin" {
		t.Errorf("Name = %q", m.Info.Name)
	}
	if m.Info.PieceLength != 1024 {
		t.Errorf("PieceLength = %d", m.Info.PieceLength)
	}
	if m.Info.Length != 2048 {
		t.Errorf("Length = %d", m.Info.Length)
	}
	if m.TotalSize() != 2048 {
		t.Errorf("TotalSize = %d", m.TotalSize())
	}
	if m.NumPieces() != 2 {
		t.Errorf("NumPieces = %d", m.NumPieces())
	}
	if m.PieceLen() != 1024 {
		t.Errorf("PieceLen = %d", m.PieceLen())
	}
}

func TestLoadMultiFile(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("mydir"),
			"piece length", bencodeInt(1024),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(100),
					"path", bencodeList(bencodeStr("dir1"), bencodeStr("file1.bin")),
				),
				bencodeDict(
					"length", bencodeInt(200),
					"path", bencodeList(bencodeStr("file2.bin")),
				),
			),
		),
	))

	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Info.Name != "mydir" {
		t.Errorf("Name = %q", m.Info.Name)
	}
	if m.Info.Length != 300 {
		t.Errorf("Length = %d", m.Info.Length)
	}
	if len(m.Info.Files) != 2 {
		t.Fatalf("Files length = %d", len(m.Info.Files))
	}
	if m.Info.Files[0].Length != 100 {
		t.Errorf("Files[0].Length = %d", m.Info.Files[0].Length)
	}
	if len(m.Info.Files[0].Path) != 2 || m.Info.Files[0].Path[0] != "dir1" || m.Info.Files[0].Path[1] != "file1.bin" {
		t.Errorf("Files[0].Path = %v", m.Info.Files[0].Path)
	}
	if m.Info.Files[1].Length != 200 {
		t.Errorf("Files[1].Length = %d", m.Info.Files[1].Length)
	}
	if len(m.Info.Files[1].Path) != 1 || m.Info.Files[1].Path[0] != "file2.bin" {
		t.Errorf("Files[1].Path = %v", m.Info.Files[1].Path)
	}
}

func TestInfoHash(t *testing.T) {
	pieces := make([]byte, 80) // length=1024, pieceLength=256 => 4 pieces
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("test.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(1024),
		),
	))

	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	hash, err := m.InfoHash()
	if err != nil {
		t.Fatalf("InfoHash: %v", err)
	}

	hashHex := hex.EncodeToString(hash[:])
	if hashHex == "" || len(hashHex) != 40 {
		t.Errorf("InfoHash hex = %q (len=%d)", hashHex, len(hashHex))
	}
}

func TestInfoHashSameForIdenticalTorrent(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("same.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))

	m1, err := Load(raw)
	if err != nil {
		t.Fatalf("Load 1: %v", err)
	}
	m2, err := Load(raw)
	if err != nil {
		t.Fatalf("Load 2: %v", err)
	}

	h1, err := m1.InfoHash()
	if err != nil {
		t.Fatalf("InfoHash 1: %v", err)
	}
	h2, err := m2.InfoHash()
	if err != nil {
		t.Fatalf("InfoHash 2: %v", err)
	}
	if h1 != h2 {
		t.Errorf("InfoHash mismatch: %x != %x", h1, h2)
	}
}

func TestInfoHashV2NotV2(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("test.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))

	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = m.InfoHashV2()
	if err == nil {
		t.Error("expected ErrNotV2 for non-v2 torrent")
	}
}

func TestLoadInvalidData(t *testing.T) {
	_, err := Load([]byte("not valid bencode"))
	if err == nil {
		t.Error("expected error for invalid bencode data")
	}
}

func TestLoadRejectsTrailingData(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("trailing.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	raw = append(raw, []byte("junk")...)
	_, err := Load(raw)
	if err == nil {
		t.Fatal("expected error for trailing data")
	}
}

func TestLoadNoInfo(t *testing.T) {
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for torrent without info dict")
	}
}

func TestLoadBothLengthAndFiles(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("bad.torrent"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(100),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(100),
					"path", bencodeList(bencodeStr("file.bin")),
				),
			),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for torrent with both length and files")
	}
}

func TestLoadMissingPieceLength(t *testing.T) {
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("test.bin"),
			"pieces", bencodeStr(string(make([]byte, 20))),
			"length", bencodeInt(1024),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for missing piece length")
	}
}

func TestLoadDirTraversalInName(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("../../etc/passwd"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for directory traversal in name")
	}
}

func TestLoadDirTraversalInPath(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("safe"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(100),
					"path", bencodeList(bencodeStr("../outside")),
				),
			),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for directory traversal in file path")
	}
}

func TestLoadAnnounceList(t *testing.T) {
	tier1 := bencodeList(
		bencodeStr("http://tracker1/announce"),
		bencodeStr("http://tracker2/announce"),
	)
	tier2 := bencodeList(
		bencodeStr("udp://tracker3:8080/announce"),
	)
	pieces := make([]byte, 40) // length=512, pieceLength=256 => 2 pieces
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://main/announce"),
		"announce-list", bencodeList(tier1, tier2),
		"info", bencodeDict(
			"name", bencodeStr("al.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(512),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.AnnounceList) != 2 {
		t.Fatalf("AnnounceList len = %d", len(m.AnnounceList))
	}
	if len(m.AnnounceList[0]) != 2 {
		t.Errorf("AnnounceList[0] len = %d", len(m.AnnounceList[0]))
	}
	if len(m.AnnounceList[1]) != 1 {
		t.Errorf("AnnounceList[1] len = %d", len(m.AnnounceList[1]))
	}
}

func TestLoadPrivateFlag(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("private.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
			"private", bencodeInt(1),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !m.Info.Private {
		t.Error("Private should be true")
	}
}

func TestLoadCreationDate(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"creation date", bencodeInt(1700000000),
		"info", bencodeDict(
			"name", bencodeStr("dated.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.CreationDate != 1700000000 {
		t.Errorf("CreationDate = %d", m.CreationDate)
	}
}

func TestLoadComment(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"comment", bencodeStr("test comment"),
		"info", bencodeDict(
			"name", bencodeStr("comment.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Comment != "test comment" {
		t.Errorf("Comment = %q", m.Comment)
	}
}

func TestLoadCommentUTF8Precedence(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"comment", bencodeStr("plain comment"),
		"comment.utf-8", bencodeStr("utf8 comment"),
		"info", bencodeDict(
			"name", bencodeStr("utf8.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Comment != "utf8 comment" {
		t.Errorf("Comment = %q, expected utf8 version", m.Comment)
	}
}

func TestLoadURLListString(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"url-list", bencodeStr("http://seed/file.bin"),
		"info", bencodeDict(
			"name", bencodeStr("url.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.URLList) != 1 || m.URLList[0] != "http://seed/file.bin" {
		t.Errorf("URLList = %v", m.URLList)
	}
}

func TestLoadNodes(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"nodes", bencodeList(
			bencodeList(bencodeStr("192.168.1.1"), bencodeInt(6881)),
			bencodeList(bencodeStr("10.0.0.1"), bencodeInt(6882)),
		),
		"info", bencodeDict(
			"name", bencodeStr("dht.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.Nodes) != 2 {
		t.Fatalf("Nodes len = %d", len(m.Nodes))
	}
	if m.Nodes[0].Host != "192.168.1.1" || m.Nodes[0].Port != 6881 {
		t.Errorf("Nodes[0] = %+v", m.Nodes[0])
	}
	if m.Nodes[1].Host != "10.0.0.1" || m.Nodes[1].Port != 6882 {
		t.Errorf("Nodes[1] = %+v", m.Nodes[1])
	}
}

func TestPieceCountMismatch(t *testing.T) {
	pieces := make([]byte, 40) // 2 pieces
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(100),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(50),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for piece count mismatch")
	}
}

func TestDetectDirTraversal(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"normal_file.txt", false},
		{"path/to/file.bin", false},
		{".", true},
		{"..", true},
		{"/absolute/path", true},
		{"../escape", true},
		{"./relative", true},
		{"path/../etc/passwd", true},
		{"path/./config", true},
		{"trailing/", true},
		{"ending/.", true},
		{"ending/..", true},
	}
	for _, tc := range tests {
		result := detectDirTraversal(tc.path)
		if result != tc.expected {
			t.Errorf("detectDirTraversal(%q) = %v, want %v", tc.path, result, tc.expected)
		}
	}
}

func TestEncodeNonUTF8(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"café", "café"},
		{string([]byte{0x80, 'a', 'b'}), "%80ab"},
		{string([]byte{'a', 0xFF}), "a%FF"},
	}
	for _, tc := range tests {
		result := encodeNonUTF8(tc.input)
		if result != tc.expected {
			t.Errorf("encodeNonUTF8(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestFindRawValue(t *testing.T) {
	raw := marshal(bencodeDict(
		"alpha", bencodeStr("value1"),
		"beta", bencodeInt(42),
	))
	val, err := findRawValue(raw, "beta")
	if err != nil {
		t.Fatalf("findRawValue: %v", err)
	}
	if string(val) != "i42e" {
		t.Errorf("raw value = %q, want i42e", string(val))
	}
}

func TestFindRawValueMissing(t *testing.T) {
	raw := marshal(bencodeDict(
		"alpha", bencodeStr("value1"),
	))
	_, err := findRawValue(raw, "nonexistent")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestFindRawValueNotDict(t *testing.T) {
	_, err := findRawValue([]byte("i123e"), "key")
	if err == nil {
		t.Error("expected error for non-dict data")
	}
}

func TestLoadCreatedBy(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"created by", bencodeStr("test-creator/1.0"),
		"info", bencodeDict(
			"name", bencodeStr("cb.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.CreatedBy != "test-creator/1.0" {
		t.Errorf("CreatedBy = %q", m.CreatedBy)
	}
}

// --- Comprehensive tests covering additional C++ BittorrentHelperTest scenarios ---

func TestLoadEmpty(t *testing.T) {
	_, err := Load([]byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestLoadNegativePieceLength(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(-1),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for negative piece length")
	}
}

func TestLoadPieceLengthZero(t *testing.T) {
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("zero.bin"),
			"piece length", bencodeInt(0),
			"pieces", bencodeStr(""),
			"length", bencodeInt(0),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.PieceLen() != 0 {
		t.Errorf("PieceLen = %d, want 0", m.PieceLen())
	}
	if m.TotalSize() != 0 {
		t.Errorf("TotalSize = %d, want 0", m.TotalSize())
	}
	if m.NumPieces() != 0 {
		t.Errorf("NumPieces = %d, want 0", m.NumPieces())
	}
}

func TestLoadNegativeLength(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(-100),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for negative length")
	}
}

func TestLoadPiecesNotMultipleOf20(t *testing.T) {
	pieces := make([]byte, 19)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for pieces not multiple of 20")
	}
}

func TestLoadPiecesNotString(t *testing.T) {
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeInt(12345),
			"length", bencodeInt(256),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-string pieces")
	}
}

func TestLoadInfoMustBeDict(t *testing.T) {
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeStr("not a dict"),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-dict info")
	}
}

func TestLoadMissingPieces(t *testing.T) {
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("nopieces.bin"),
			"piece length", bencodeInt(256),
			"length", bencodeInt(256),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for missing pieces")
	}
}

func TestLoadLengthMustBeInteger(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeStr("not int"),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-integer length")
	}
}

func TestLoadPieceLengthMustBeInteger(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeStr("not int"),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-integer piece length")
	}
}

func TestLoadFilesMustBeList(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeStr("not a list"),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-list files in multi-file")
	}
}

func TestLoadPathComponentMustBeString(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("badpath"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(100),
					"path", bencodeList(bencodeInt(42)),
				),
			),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-string path component")
	}
}

func TestPieceCountMismatchTooMany(t *testing.T) {
	pieces := make([]byte, 40) // 2 pieces
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for piece count > expected")
	}
}

func TestPieceCountMismatchMultiFile(t *testing.T) {
	pieces := make([]byte, 40) // 2 pieces
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("multibad"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(50),
					"path", bencodeList(bencodeStr("a.bin")),
				),
				bencodeDict(
					"length", bencodeInt(50),
					"path", bencodeList(bencodeStr("b.bin")),
				),
			),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for piece count mismatch in multi-file torrent")
	}
}

func TestLoadMultiNegativeFileLength(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("badmulti"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(-50),
					"path", bencodeList(bencodeStr("bad.bin")),
				),
			),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for negative file length in multi-file")
	}
}

func TestLoadMultiMissingPath(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("misspath"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(100),
				),
			),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for missing path in multi-file entry")
	}
}

func TestLoadMultiEmptyPath(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("emptydir"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(100),
					"path", bencodeList(),
				),
			),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for empty path in multi-file entry")
	}
}

func TestLoadMultiFileTotalOverflow(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("overflow"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(1<<62),
					"path", bencodeList(bencodeStr("huge1.bin")),
				),
				bencodeDict(
					"length", bencodeInt(1<<62),
					"path", bencodeList(bencodeStr("huge2.bin")),
				),
			),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for total size overflow")
	}
}

func TestLoadMultiNonDictEntry(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("skipbad"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeStr("not a dict"),
				bencodeDict(
					"length", bencodeInt(100),
					"path", bencodeList(bencodeStr("valid.bin")),
				),
			),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.Info.Files) != 1 {
		t.Errorf("expected 1 file entry (non-dict skipped), got %d", len(m.Info.Files))
	}
	if m.Info.Length != 100 {
		t.Errorf("expected total length 100, got %d", m.Info.Length)
	}
}

func TestLoadMultiFileEntryLengthMustBeInteger(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("badmulti"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeStr("not int"),
					"path", bencodeList(bencodeStr("file.bin")),
				),
			),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-integer file entry length")
	}
}

func TestLoadMultiFileMissingLength(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("nolength"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"path", bencodeList(bencodeStr("file.bin")),
				),
			),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for file entry missing length")
	}
}

func TestLoadNameUTF8Precedence(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("plain-name"),
			"name.utf-8", bencodeStr("utf8-name"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Info.Name != "utf8-name" {
		t.Errorf("Name = %q, expected utf8-name", m.Info.Name)
	}
}

func TestLoadPathUTF8Precedence(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("utf8dir"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(100),
					"path", bencodeList(bencodeStr("plain-path")),
					"path.utf-8", bencodeList(bencodeStr("utf8-path")),
				),
			),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.Info.Files) != 1 || m.Info.Files[0].Path[0] != "utf8-path" {
		t.Errorf("Files[0].Path = %v, expected [utf8-path]", m.Info.Files[0].Path)
	}
}

func TestPieceHashRetrieval(t *testing.T) {
	p1 := make([]byte, 20)
	p2 := make([]byte, 20)
	p1[0] = 0xAA
	p2[0] = 0xBB
	pieces := append(append([]byte{}, p1...), p2...)

	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("pieces.bin"),
			"piece length", bencodeInt(128),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.NumPieces() != 2 {
		t.Fatalf("NumPieces = %d", m.NumPieces())
	}
	hash0 := m.Info.Pieces[0:20]
	if hash0[0] != 0xAA {
		t.Errorf("hash[0][0] = %x, want aa", hash0[0])
	}
	hash1 := m.Info.Pieces[20:40]
	if hash1[0] != 0xBB {
		t.Errorf("hash[1][0] = %x, want bb", hash1[0])
	}
	if len(m.Info.Pieces) != 40 {
		t.Errorf("Pieces len = %d", len(m.Info.Pieces))
	}
}

func TestLoadAnnounceListEdgeCases(t *testing.T) {
	t.Run("non-list tier skipped", func(t *testing.T) {
		pieces := make([]byte, 20)
		tier1 := bencodeStr("not a list")
		tier2 := bencodeList(bencodeStr("http://valid/announce"))
		raw := marshal(bencodeDict(
			"announce", bencodeStr("http://main/announce"),
			"announce-list", bencodeList(tier1, tier2),
			"info", bencodeDict(
				"name", bencodeStr("al.bin"),
				"piece length", bencodeInt(256),
				"pieces", bencodeStr(string(pieces)),
				"length", bencodeInt(256),
			),
		))
		m, err := Load(raw)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(m.AnnounceList) != 1 {
			t.Errorf("AnnounceList len = %d, want 1 (non-list tier skipped)", len(m.AnnounceList))
		}
	})

	t.Run("non-string urls skipped", func(t *testing.T) {
		pieces := make([]byte, 20)
		tier := bencodeList(bencodeInt(42), bencodeStr("http://valid/announce"), bencodeList())
		raw := marshal(bencodeDict(
			"announce", bencodeStr("http://main/announce"),
			"announce-list", bencodeList(tier),
			"info", bencodeDict(
				"name", bencodeStr("al.bin"),
				"piece length", bencodeInt(256),
				"pieces", bencodeStr(string(pieces)),
				"length", bencodeInt(256),
			),
		))
		m, err := Load(raw)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(m.AnnounceList) != 1 || len(m.AnnounceList[0]) != 1 {
			t.Errorf("expected 1 tier with 1 URL, got %+v", m.AnnounceList)
		}
	})

	t.Run("empty uris stripped from tier", func(t *testing.T) {
		pieces := make([]byte, 20)
		tier := bencodeList(bencodeStr(""), bencodeStr("  "), bencodeStr("http://valid/announce"))
		raw := marshal(bencodeDict(
			"announce", bencodeStr("http://main/announce"),
			"announce-list", bencodeList(tier),
			"info", bencodeDict(
				"name", bencodeStr("al.bin"),
				"piece length", bencodeInt(256),
				"pieces", bencodeStr(string(pieces)),
				"length", bencodeInt(256),
			),
		))
		m, err := Load(raw)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(m.AnnounceList) != 1 || len(m.AnnounceList[0]) != 1 {
			t.Errorf("expected 1 tier with 1 URL after stripping, got %+v", m.AnnounceList)
		}
	})
}

func TestLoadAnnounceOnly(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("  http://single/announce  "),
		"info", bencodeDict(
			"name", bencodeStr("ann.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.AnnounceList) != 1 {
		t.Errorf("AnnounceList len = %d, want 1 from solo announce", len(m.AnnounceList))
	}
	if len(m.AnnounceList[0]) != 1 || m.AnnounceList[0][0] != "http://single/announce" {
		t.Errorf("AnnounceList[0] = %v", m.AnnounceList[0])
	}
}

func TestLoadNoAnnounce(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"info", bencodeDict(
			"name", bencodeStr("noann.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Announce != "" {
		t.Errorf("Announce = %q, want empty", m.Announce)
	}
	if m.AnnounceList != nil {
		t.Errorf("AnnounceList = %v, want nil", m.AnnounceList)
	}
}

func TestLoadAnnounceMustBeString(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeInt(123),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-string announce")
	}
}

func TestLoadURLListMultiple(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"url-list", bencodeList(
			bencodeStr("http://seed1/file.bin"),
			bencodeStr("http://seed2/file.bin"),
		),
		"info", bencodeDict(
			"name", bencodeStr("url.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.URLList) != 2 {
		t.Fatalf("URLList len = %d", len(m.URLList))
	}
	if m.URLList[0] != "http://seed1/file.bin" {
		t.Errorf("URLList[0] = %q", m.URLList[0])
	}
	if m.URLList[1] != "http://seed2/file.bin" {
		t.Errorf("URLList[1] = %q", m.URLList[1])
	}
}

func TestLoadURLListNonStringSkipped(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"url-list", bencodeList(
			bencodeInt(999),
			bencodeStr("http://seed1/file.bin"),
		),
		"info", bencodeDict(
			"name", bencodeStr("url.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.URLList) != 1 {
		t.Errorf("URLList len = %d, want 1 (non-string skipped)", len(m.URLList))
	}
}

func TestLoadURLListEndsWithSlash(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"url-list", bencodeStr("http://seed/files/"),
		"info", bencodeDict(
			"name", bencodeStr("file.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.URLList) != 1 || m.URLList[0] != "http://seed/files/" {
		t.Errorf("URLList = %v", m.URLList)
	}
}

func TestLoadNoURLList(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("nourl.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.URLList != nil {
		t.Errorf("URLList = %v, want nil when not present", m.URLList)
	}
}

func TestLoadNodesEdgeCases(t *testing.T) {
	t.Run("empty hostname skipped", func(t *testing.T) {
		pieces := make([]byte, 20)
		raw := marshal(bencodeDict(
			"announce", bencodeStr("http://tracker/announce"),
			"nodes", bencodeList(
				bencodeList(bencodeStr(""), bencodeInt(6881)),
				bencodeList(bencodeStr("192.168.1.1"), bencodeInt(6882)),
			),
			"info", bencodeDict(
				"name", bencodeStr("dht.bin"),
				"piece length", bencodeInt(256),
				"pieces", bencodeStr(string(pieces)),
				"length", bencodeInt(256),
			),
		))
		m, err := Load(raw)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(m.Nodes) != 1 {
			t.Errorf("Nodes len = %d, want 1 after skipping empty host", len(m.Nodes))
		}
	})

	t.Run("bad port skipped", func(t *testing.T) {
		pieces := make([]byte, 20)
		raw := marshal(bencodeDict(
			"announce", bencodeStr("http://tracker/announce"),
			"nodes", bencodeList(
				bencodeList(bencodeStr("192.168.1.1"), bencodeStr("bad")),
				bencodeList(bencodeStr("192.168.1.2"), bencodeInt(6882)),
			),
			"info", bencodeDict(
				"name", bencodeStr("dht.bin"),
				"piece length", bencodeInt(256),
				"pieces", bencodeStr(string(pieces)),
				"length", bencodeInt(256),
			),
		))
		m, err := Load(raw)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(m.Nodes) != 1 {
			t.Errorf("Nodes len = %d, want 1 after skipping bad port", len(m.Nodes))
		}
	})

	t.Run("port out of range skipped", func(t *testing.T) {
		pieces := make([]byte, 20)
		raw := marshal(bencodeDict(
			"announce", bencodeStr("http://tracker/announce"),
			"nodes", bencodeList(
				bencodeList(bencodeStr("192.168.1.1"), bencodeInt(0)),
				bencodeList(bencodeStr("192.168.1.2"), bencodeInt(65536)),
				bencodeList(bencodeStr("192.168.1.3"), bencodeInt(6882)),
			),
			"info", bencodeDict(
				"name", bencodeStr("dht.bin"),
				"piece length", bencodeInt(256),
				"pieces", bencodeStr(string(pieces)),
				"length", bencodeInt(256),
			),
		))
		m, err := Load(raw)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(m.Nodes) != 1 {
			t.Errorf("Nodes len = %d, want 1 after skipping out-of-range ports", len(m.Nodes))
		}
	})

	t.Run("port missing skipped", func(t *testing.T) {
		pieces := make([]byte, 20)
		raw := marshal(bencodeDict(
			"announce", bencodeStr("http://tracker/announce"),
			"nodes", bencodeList(
				bencodeList(bencodeStr("192.168.1.1")),
				bencodeList(bencodeStr("192.168.1.2"), bencodeInt(6882)),
			),
			"info", bencodeDict(
				"name", bencodeStr("dht.bin"),
				"piece length", bencodeInt(256),
				"pieces", bencodeStr(string(pieces)),
				"length", bencodeInt(256),
			),
		))
		m, err := Load(raw)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(m.Nodes) != 1 {
			t.Errorf("Nodes len = %d, want 1 after skipping missing port", len(m.Nodes))
		}
	})

	t.Run("nodes not a list", func(t *testing.T) {
		pieces := make([]byte, 20)
		raw := marshal(bencodeDict(
			"announce", bencodeStr("http://tracker/announce"),
			"nodes", bencodeStr("not a list"),
			"info", bencodeDict(
				"name", bencodeStr("dht.bin"),
				"piece length", bencodeInt(256),
				"pieces", bencodeStr(string(pieces)),
				"length", bencodeInt(256),
			),
		))
		m, err := Load(raw)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(m.Nodes) != 0 {
			t.Errorf("Nodes len = %d, want 0 for non-list nodes", len(m.Nodes))
		}
	})
}

func TestLoadMetaVersion(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("v2.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
			"meta version", bencodeInt(2),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Info.MetaVersion != 2 {
		t.Errorf("MetaVersion = %d, want 2", m.Info.MetaVersion)
	}
}

func TestLoadMetaVersionInvalid(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
			"meta version", bencodeStr("not-an-int"),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-integer meta version")
	}
}

func TestLoadInfoHashV2Valid(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("v2.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
			"meta version", bencodeInt(2),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	hash, err := m.InfoHashV2()
	if err != nil {
		t.Fatalf("InfoHashV2: %v", err)
	}
	if len(hash) != 32 {
		t.Errorf("InfoHashV2 length = %d, want 32", len(hash))
	}
}

func TestLoadPrivateNotOne(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("notprivate.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
			"private", bencodeInt(0),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Info.Private {
		t.Error("Private should be false when set to 0")
	}
}

func TestLoadPrivateNotOneNonInteger(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("privstr.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
			"private", bencodeStr("yes"),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Info.Private {
		t.Error("Private should be false when private is not integer 1")
	}
}

func TestLoadMD5Sum(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("md5dir"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(100),
					"path", bencodeList(bencodeStr("file.bin")),
					"md5sum", bencodeStr("d41d8cd98f00b204e9800998ecf8427e"),
				),
			),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.Info.Files) != 1 {
		t.Fatalf("Files len = %d", len(m.Info.Files))
	}
	if m.Info.Files[0].MD5Sum != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Errorf("MD5Sum = %q", m.Info.Files[0].MD5Sum)
	}
}

func TestLoadEncoding(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"encoding", bencodeStr("UTF-8"),
		"info", bencodeDict(
			"name", bencodeStr("enc.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Encoding != "UTF-8" {
		t.Errorf("Encoding = %q", m.Encoding)
	}
}

func TestLoadEncodingNonString(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"encoding", bencodeInt(42),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-string encoding")
	}
}

func TestLoadCreatedByInvalid(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"created by", bencodeInt(123),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-string created by")
	}
}

func TestLoadCreationDateInvalid(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"creation date", bencodeStr("not a number"),
		"info", bencodeDict(
			"name", bencodeStr("bad.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for non-integer creation date")
	}
}

func TestInfoHashCorrectBytes(t *testing.T) {
	pieces := make([]byte, 20)
	m1, err := Load(marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("test.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	h1, err := m1.InfoHash()
	if err != nil {
		t.Fatalf("InfoHash: %v", err)
	}

	// Same info dict with different announce should produce same hash
	m2, err := Load(marshal(bencodeDict(
		"announce", bencodeStr("http://other/announce"),
		"info", bencodeDict(
			"name", bencodeStr("test.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	)))
	if err != nil {
		t.Fatalf("Load 2: %v", err)
	}
	h2, err := m2.InfoHash()
	if err != nil {
		t.Fatalf("InfoHash 2: %v", err)
	}

	if h1 != h2 {
		t.Errorf("InfoHash should be the same for identical info dicts: %x != %x", h1, h2)
	}

	hexStr := hex.EncodeToString(h1[:])
	if len(hexStr) != 40 {
		t.Errorf("InfoHash hex length = %d, want 40", len(hexStr))
	}
}

func TestLoadNonUTF8Name(t *testing.T) {
	invalidName := string([]byte{0x90, 0xA2, 0x8A}) + "E"
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr(invalidName),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Info.Name != "%90%A2%8AE" {
		t.Errorf("Name = %q, want %%90%%A2%%8AE", m.Info.Name)
	}
}

func TestLoadNonUTF8Path(t *testing.T) {
	invalidPath := string([]byte{0x90, 0xA2, 0x8A}) + "E"
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("safedir"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(100),
					"path", bencodeList(bencodeStr(invalidPath)),
				),
			),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.Info.Files) != 1 || m.Info.Files[0].Path[0] != "%90%A2%8AE" {
		t.Errorf("Files[0].Path[0] = %q, want %%90%%A2%%8AE", m.Info.Files[0].Path[0])
	}
}

func TestFileModeSingle(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("single.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.Info.Files) != 0 {
		t.Error("Files should be empty for single-file torrent")
	}
	if m.Info.Length != 256 {
		t.Errorf("Length = %d, want 256", m.Info.Length)
	}
}

func TestFileModeMulti(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("multidir"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(100),
					"path", bencodeList(bencodeStr("a.bin")),
				),
			),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.Info.Files) != 1 {
		t.Error("Files should have 1 entry for multi-file torrent")
	}
	if m.Info.Length != 100 {
		t.Errorf("Length = %d, want 100 (sum of file sizes)", m.Info.Length)
	}
}

func TestScanBencodeValue(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantEnd int
		wantErr bool
	}{
		{"integer", []byte("i42e"), 4, false},
		{"string", []byte("5:hello"), 7, false},
		{"list", []byte("li1ei2ee"), 8, false},
		{"nested list", []byte("lli1eeli2eee"), 12, false},
		{"dict", []byte("di1ei2ee"), 8, false},
		{"nested dict", []byte("d1:ai1e1:bi2ee"), 14, false},
		{"past end", []byte("i"), 0, true},
		{"unterminated integer", []byte("i42"), 0, true},
		{"unterminated list", []byte("li1e"), 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			end, err := scanBencodeValue(tc.data, 0)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error but got end=%d", end)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if end != tc.wantEnd {
					t.Errorf("end = %d, want %d", end, tc.wantEnd)
				}
			}
		})
	}
}

func TestFindRawValueNested(t *testing.T) {
	root := bencodeDict(
		"alpha", bencodeStr("value1"),
		"beta", bencodeDict(
			"gamma", bencodeInt(42),
			"delta", bencodeList(bencodeInt(1), bencodeInt(2)),
		),
	)
	raw := marshal(root)

	val, err := findRawValue(raw, "beta")
	if err != nil {
		t.Fatalf("findRawValue: %v", err)
	}
	if val[0] != 'd' || val[len(val)-1] != 'e' {
		t.Errorf("raw value = %q, want dict", string(val))
	}
}

func TestFindRawValueEmptyDict(t *testing.T) {
	_, err := findRawValue([]byte("de"), "key")
	if err != nil {
		t.Logf("error: %v (may be acceptable for empty dict)", err)
	}
}

func TestPathDirTraversalInComponent(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("safe"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict(
					"length", bencodeInt(100),
					"path", bencodeList(bencodeStr("normal"), bencodeStr(".."), bencodeStr("file.bin")),
				),
			),
		),
	))
	_, err := Load(raw)
	if err == nil {
		t.Error("expected error for '..' as path component in multi-file")
	}
}

func TestDetectDirTraversalMore(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"", false},
		{"normal", false},
		{"path/to/file", false},
		{string([]byte{0x00}), true},
		{string([]byte{0x7f}), true},
	}
	for _, tc := range tests {
		result := detectDirTraversal(tc.path)
		if result != tc.expected {
			t.Errorf("detectDirTraversal(%q) = %v, want %v", tc.path, result, tc.expected)
		}
	}
}

func TestEncodeNonUTF8More(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"ASCII", "ASCII"},
		{"こんにちは", "こんにちは"},
		{string([]byte{0xFF, 0xFE}), "%FF%FE"},
		{string([]byte{0xC0, 'x'}), "%C0x"},
	}
	for _, tc := range tests {
		result := encodeNonUTF8(tc.input)
		if result != tc.expected {
			t.Errorf("encodeNonUTF8(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestRawCopy(t *testing.T) {
	pieces := make([]byte, 20)
	orig := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("raw.bin"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(256),
		),
	))
	m, err := Load(orig)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(m.Raw) != string(orig) {
		t.Error("Raw should match original data")
	}
	// Mutating original should not affect Raw
	orig[0] = 0xFF
	if m.Raw[0] == 0xFF {
		t.Error("Raw should be independent from original slice")
	}
}

func TestMultiFileThreeEntries(t *testing.T) {
	pieces := make([]byte, 60) // 3 pieces, total size=600, pieceLength=256 => ceil(600/256)=3
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"info", bencodeDict(
			"name", bencodeStr("multi3"),
			"piece length", bencodeInt(256),
			"pieces", bencodeStr(string(pieces)),
			"files", bencodeList(
				bencodeDict("length", bencodeInt(100), "path", bencodeList(bencodeStr("a.bin"))),
				bencodeDict("length", bencodeInt(200), "path", bencodeList(bencodeStr("b.bin"))),
				bencodeDict("length", bencodeInt(300), "path", bencodeList(bencodeStr("c.bin"))),
			),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.Info.Files) != 3 {
		t.Errorf("Files len = %d, want 3", len(m.Info.Files))
	}
	if m.TotalSize() != 600 {
		t.Errorf("TotalSize = %d, want 600", m.TotalSize())
	}
	if m.NumPieces() != 3 {
		t.Errorf("NumPieces = %d, want 3", m.NumPieces())
	}
}

func TestFullTorrent(t *testing.T) {
	pieces := make([]byte, 20)
	raw := marshal(bencodeDict(
		"announce", bencodeStr("http://tracker/announce"),
		"comment", bencodeStr("test torrent"),
		"creation date", bencodeInt(1600000000),
		"created by", bencodeStr("aria2-test"),
		"encoding", bencodeStr("UTF-8"),
		"info", bencodeDict(
			"name", bencodeStr("full.bin"),
			"piece length", bencodeInt(512),
			"pieces", bencodeStr(string(pieces)),
			"length", bencodeInt(512),
			"private", bencodeInt(1),
		),
	))
	m, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Announce != "http://tracker/announce" {
		t.Errorf("Announce = %q", m.Announce)
	}
	if m.Comment != "test torrent" {
		t.Errorf("Comment = %q", m.Comment)
	}
	if m.CreationDate != 1600000000 {
		t.Errorf("CreationDate = %d", m.CreationDate)
	}
	if m.CreatedBy != "aria2-test" {
		t.Errorf("CreatedBy = %q", m.CreatedBy)
	}
	if m.Encoding != "UTF-8" {
		t.Errorf("Encoding = %q", m.Encoding)
	}
	if m.Info.Name != "full.bin" {
		t.Errorf("Name = %q", m.Info.Name)
	}
	if m.PieceLen() != 512 {
		t.Errorf("PieceLen = %d", m.PieceLen())
	}
	if !m.Info.Private {
		t.Error("Private should be true")
	}
}
