package sessionfile

import (
	"bytes"
	"compress/gzip"
	"os"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/core"
)

func TestReadSingleEntry(t *testing.T) {
	input := "https://example.com/file1.zip\thttps://mirror.example.com/file1.zip\n" +
		" gid=0123456789abcdef\n" +
		" dir=/home/user/downloads\n" +
		" out=file1.zip\n" +
		" split=5\n" +
		" max-connection-per-server=1\n" +
		" pause=true\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if len(e.URIs) != 2 {
		t.Errorf("expected 2 URIs, got %d: %v", len(e.URIs), e.URIs)
	}
	if e.URIs[0] != "https://example.com/file1.zip" {
		t.Errorf("URI[0] = %q", e.URIs[0])
	}
	if e.URIs[1] != "https://mirror.example.com/file1.zip" {
		t.Errorf("URI[1] = %q", e.URIs[1])
	}
	if e.GID.Hex() != "0123456789abcdef" {
		t.Errorf("GID = %s", e.GID.Hex())
	}
	if e.Status != core.StatusPaused {
		t.Errorf("expected Paused (pause=true), got %v", e.Status)
	}
	if e.Options["dir"] != "/home/user/downloads" {
		t.Errorf("dir = %q", e.Options["dir"])
	}
	if e.Options["out"] != "file1.zip" {
		t.Errorf("out = %q", e.Options["out"])
	}
	if e.Options["split"] != "5" {
		t.Errorf("split = %q", e.Options["split"])
	}
}

func TestReadWaitingStatus(t *testing.T) {
	// No pause key → Waiting
	input := "https://example.com/file.iso\n" +
		" gid=000000000000002a\n" +
		" dir=/tmp\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Status != core.StatusWaiting {
		t.Errorf("expected Waiting (no pause key), got %v", entries[0].Status)
	}
}

func TestReadMultipleEntries(t *testing.T) {
	input := "https://example.com/file1.zip\n" +
		" gid=0000000000000001\n" +
		" dir=/downloads\n" +
		"https://example.com/file2.zip\n" +
		" gid=0000000000000002\n" +
		" dir=/downloads\n" +
		" pause=true\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].GID.Hex() != "0000000000000001" {
		t.Errorf("entry 0 GID = %s", entries[0].GID.Hex())
	}
	if entries[0].Status != core.StatusWaiting {
		t.Errorf("entry 0: expected Waiting, got %v", entries[0].Status)
	}

	if entries[1].GID.Hex() != "0000000000000002" {
		t.Errorf("entry 1 GID = %s", entries[1].GID.Hex())
	}
	if entries[1].Status != core.StatusPaused {
		t.Errorf("entry 1: expected Paused, got %v", entries[1].Status)
	}
}

func TestReadCommentAndBlankLines(t *testing.T) {
	// Comment lines (starting with #) and blank lines are skipped.
	input := "# this is a comment\n" +
		"\n" +
		"https://example.com/file.zip\n" +
		" gid=0000000000000001\n" +
		" dir=/tmp\n" +
		"# another comment\n" +
		"\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestReadUnknownKeys(t *testing.T) {
	input := "https://example.com/file.zip\n" +
		" gid=0000000000000001\n" +
		" future-option=some-value\n" +
		" new-feature=enabled\n" +
		" dir=/tmp\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Unknown["future-option"] != "some-value" {
		t.Errorf("Unknown[future-option] = %q, want %q", e.Unknown["future-option"], "some-value")
	}
	if e.Unknown["new-feature"] != "enabled" {
		t.Errorf("Unknown[new-feature] = %q", e.Unknown["new-feature"])
	}
	// Known keys should be in Options, not Unknown
	if e.Unknown["dir"] != "" {
		t.Errorf("known key 'dir' should be in Options, not Unknown")
	}
	if e.Options["dir"] != "/tmp" {
		t.Errorf("Options[dir] = %q", e.Options["dir"])
	}
	if len(e.UnknownOrder) != 2 {
		t.Fatalf("UnknownOrder len = %d, want 2", len(e.UnknownOrder))
	}
	if e.UnknownOrder[0] != (OptionLine{Key: "future-option", Value: "some-value"}) {
		t.Errorf("UnknownOrder[0] = %#v", e.UnknownOrder[0])
	}
}

func TestReadGzipContent(t *testing.T) {
	entries := []Entry{
		{
			URIs:    []string{"https://example.com/file.zip"},
			GID:     core.GID(1),
			Status:  core.StatusWaiting,
			Options: map[string]string{"dir": "/tmp"},
		},
	}

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	if err := Write(gw, entries); err != nil {
		t.Fatalf("Write to gzip: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	// Read should auto-detect gzip
	result, err := Read(&gzBuf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].GID != 1 {
		t.Errorf("GID = %d", result[0].GID)
	}
}

func TestReadEmptyFile(t *testing.T) {
	entries, err := Read(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestReadOnlyComments(t *testing.T) {
	input := "# just a comment\n# another comment\n"
	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestReadCumulativeOptions(t *testing.T) {
	// Multiple bt-tracker lines → joined by newline
	input := "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		" gid=0000000000000001\n" +
		" bt-tracker=http://t1.example/announce\n" +
		" bt-tracker=http://t2.example/announce\n" +
		" dht-entry-point=router.bittorrent.com:6881\n" +
		" dht-entry-point=dht.transmissionbt.com:6881\n" +
		" header=X-Custom: val1\n" +
		" header=X-Custom: val2\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Options["bt-tracker"] != "http://t1.example/announce\nhttp://t2.example/announce" {
		t.Errorf("bt-tracker = %q", e.Options["bt-tracker"])
	}
	if e.Options["header"] != "X-Custom: val1\nX-Custom: val2" {
		t.Errorf("header = %q", e.Options["header"])
	}
	if e.Options["dht-entry-point"] != "router.bittorrent.com:6881\ndht.transmissionbt.com:6881" {
		t.Errorf("dht-entry-point = %q", e.Options["dht-entry-point"])
	}
}

func TestReadSingleURI(t *testing.T) {
	input := "https://example.com/file.zip\n" +
		" gid=0000000000000001\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if len(entries[0].URIs) != 1 {
		t.Errorf("expected 1 URI, got %d", len(entries[0].URIs))
	}
}

func TestReadAria2TrailingURISeparator(t *testing.T) {
	input := "https://example.com/file.zip\t\n" +
		" gid=0000000000000001\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if len(entries[0].URIs) != 1 || entries[0].URIs[0] != "https://example.com/file.zip" {
		t.Fatalf("URIs = %#v", entries[0].URIs)
	}
}

func TestRoundTrip(t *testing.T) {
	original := []Entry{
		{
			URIs:   []string{"https://example.com/file1.zip", "https://mirror.example.com/file1.zip"},
			GID:    core.GID(0xabcdef1234567890),
			Status: core.StatusPaused,
			Options: map[string]string{
				"dir":                       "/home/user/downloads",
				"out":                       "file1.zip",
				"split":                     "5",
				"max-connection-per-server": "1",
				"timeout":                   "60",
			},
			Unknown: map[string]string{
				"custom-key": "custom-value",
			},
		},
		{
			URIs:   []string{"https://example.com/file2.iso"},
			GID:    core.GID(42),
			Status: core.StatusWaiting,
			Options: map[string]string{
				"dir": "/tmp",
			},
		},
	}

	// Write
	var buf bytes.Buffer
	if err := Write(&buf, original); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Read back
	result, err := Read(&buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(result) != len(original) {
		t.Fatalf("expected %d entries, got %d", len(original), len(result))
	}

	for i := range original {
		orig := original[i]
		res := result[i]

		if len(res.URIs) != len(orig.URIs) {
			t.Errorf("entry %d: URI count: %d vs %d", i, len(res.URIs), len(orig.URIs))
		}
		for j := range orig.URIs {
			if res.URIs[j] != orig.URIs[j] {
				t.Errorf("entry %d URI[%d]: %q vs %q", i, j, res.URIs[j], orig.URIs[j])
			}
		}

		if res.GID != orig.GID {
			t.Errorf("entry %d: GID %s vs %s", i, res.GID, orig.GID)
		}
		if res.Status != orig.Status {
			t.Errorf("entry %d: Status %v vs %v", i, res.Status, orig.Status)
		}

		for k, v := range orig.Options {
			if res.Options[k] != v {
				t.Errorf("entry %d: Options[%s] = %q vs %q", i, k, res.Options[k], v)
			}
		}
		for k, v := range orig.Unknown {
			if res.Unknown[k] != v {
				t.Errorf("entry %d: Unknown[%s] = %q vs %q", i, k, res.Unknown[k], v)
			}
		}
	}
}

func TestRoundTripPreservesDuplicateUnknownLines(t *testing.T) {
	input := "https://example.com/file.zip\t\n" +
		" gid=0000000000000001\n" +
		" future-option=first\n" +
		" another-option=middle\n" +
		" future-option=second\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Unknown["future-option"] != "first\nsecond" {
		t.Fatalf("future-option = %q", entries[0].Unknown["future-option"])
	}

	var buf bytes.Buffer
	if err := Write(&buf, entries); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	if strings.Count(out, " future-option=") != 2 {
		t.Fatalf("expected duplicate future-option lines, got:\n%s", out)
	}
	if strings.Index(out, " future-option=first\n") > strings.Index(out, " another-option=middle\n") {
		t.Fatalf("unknown line order not preserved:\n%s", out)
	}
	if strings.Index(out, " another-option=middle\n") > strings.Index(out, " future-option=second\n") {
		t.Fatalf("unknown line order not preserved:\n%s", out)
	}
}

func TestRoundTripGzip(t *testing.T) {
	original := []Entry{
		{
			URIs:   []string{"https://example.com/file.zip"},
			GID:    core.GID(1),
			Status: core.StatusWaiting,
			Options: map[string]string{
				"dir": "/tmp",
				"out": "file.zip",
			},
		},
	}

	// Write gzip
	var buf bytes.Buffer
	if err := WriteGzip(&buf, original); err != nil {
		t.Fatalf("WriteGzip: %v", err)
	}

	// Read (auto-detect gzip)
	result, err := Read(&buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].GID != 1 {
		t.Errorf("GID = %d", result[0].GID)
	}
	if result[0].Options["dir"] != "/tmp" {
		t.Errorf("dir = %q", result[0].Options["dir"])
	}
}

func TestReadOptionLineWithSpaceIndent(t *testing.T) {
	// Parser accepts lines starting with space as option lines.
	input := "https://example.com/file.zip\n" +
		" gid=0000000000000001\n" +
		" dir=/tmp\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].GID != 1 {
		t.Errorf("GID = %d", entries[0].GID)
	}
	if entries[0].Options["dir"] != "/tmp" {
		t.Errorf("dir = %q", entries[0].Options["dir"])
	}
}

func TestReadGIDDuplication(t *testing.T) {
	// aria2 emits gid twice — reader must handle duplicate keys.
	input := "https://example.com/file.zip\n" +
		" gid=0123456789abcdef\n" +
		" dir=/tmp\n" +
		" gid=0123456789abcdef\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].GID.Hex() != "0123456789abcdef" {
		t.Errorf("GID = %s", entries[0].GID.Hex())
	}
}

func TestReadEntryWithoutGID(t *testing.T) {
	// Entry with URIs but no gid should still parse (gid defaults to 0).
	input := "https://example.com/file.zip\n" +
		" dir=/tmp\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].GID != 0 {
		t.Errorf("GID should default to 0, got %d", entries[0].GID)
	}
}

func TestReadSingleTabURISeparator(t *testing.T) {
	// Two URIs with exactly one tab between them.
	input := "https://example.com/file1\tfile2.zip\n" +
		" gid=0000000000000001\n"

	entries, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if len(entries[0].URIs) != 2 {
		t.Errorf("expected 2 URIs, got %d: %v", len(entries[0].URIs), entries[0].URIs)
	}
}

func TestReadBasicSessionTestdata(t *testing.T) {
	data, err := os.ReadFile("testdata/basic.session")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	entries, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if len(e.URIs) != 1 || e.URIs[0] != "https://example.com/file.zip" {
		t.Errorf("URIs = %v", e.URIs)
	}
	if e.GID != core.GID(1) {
		t.Errorf("GID = %d", e.GID)
	}
	if e.Options["dir"] != "/home/user/downloads" {
		t.Errorf("dir = %q", e.Options["dir"])
	}
	if e.Options["out"] != "file.zip" {
		t.Errorf("out = %q", e.Options["out"])
	}
	if e.Options["split"] != "5" {
		t.Errorf("split = %q", e.Options["split"])
	}
}

func TestReadMultiSessionTestdata(t *testing.T) {
	data, err := os.ReadFile("testdata/multi.session")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	entries, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

func TestReadGzipSessionTestdata(t *testing.T) {
	data, err := os.ReadFile("testdata/gzip.session.gz")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	entries, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Options["dir"] != "/tmp" {
		t.Errorf("dir = %q", entries[0].Options["dir"])
	}
}
