package sessionfile

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/core"
)

func TestWriteSingleEntry(t *testing.T) {
	entry := Entry{
		URIs:   []string{"https://example.com/file1.zip", "https://mirror.example.com/file1.zip"},
		GID:    core.GID(0x0123456789abcdef),
		Status: core.StatusPaused,
		Options: map[string]string{
			"dir":   "/home/user/downloads",
			"out":   "file1.zip",
			"split": "5",
		},
	}

	var buf bytes.Buffer
	if err := Write(&buf, []Entry{entry}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()
	t.Logf("output:\n%s", out)

	// URI line: each URI is followed by a tab before the newline.
	if !strings.HasPrefix(out, "https://example.com/file1.zip\thttps://mirror.example.com/file1.zip\t\n") {
		t.Errorf("expected URI line with tab-separated URIs, got: %q", strings.SplitN(out, "\n", 2)[0])
	}

	// gid appears as first option (special position 1)
	if !strings.Contains(out, " gid=0123456789abcdef\n") {
		t.Errorf("expected gid=0123456789abcdef on option line")
	}

	// pause appears at special position 2 (when status is Paused)
	idxGID := strings.Index(out, " gid=")
	idxPause := strings.Index(out, " pause=")
	if idxGID < 0 || idxPause < 0 || idxPause < idxGID {
		t.Errorf("expected gid then pause in that order, gid at %d, pause at %d", idxGID, idxPause)
	}
}

func TestWriteWaitingStatus(t *testing.T) {
	entry := Entry{
		URIs:   []string{"https://example.com/file.iso"},
		GID:    core.GID(42),
		Status: core.StatusWaiting,
		Options: map[string]string{
			"dir": "/tmp",
		},
	}

	var buf bytes.Buffer
	if err := Write(&buf, []Entry{entry}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()

	// No pause line for Waiting status
	if strings.Contains(out, " pause=") {
		t.Error("expected no pause= line for Waiting status")
	}

	// gid should still appear
	if !strings.Contains(out, " gid=000000000000002a\n") {
		t.Errorf("expected gid line, got output:\n%s", out)
	}
}

func TestWriteActiveStatus(t *testing.T) {
	entry := Entry{
		URIs:   []string{"https://example.com/file.iso"},
		GID:    core.GID(42),
		Status: core.StatusActive,
	}

	var buf bytes.Buffer
	if err := Write(&buf, []Entry{entry}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()

	// No pause line for Active status
	if strings.Contains(out, " pause=") {
		t.Error("expected no pause= line for Active status")
	}
}

func TestWriteMultipleEntries(t *testing.T) {
	entries := []Entry{
		{
			URIs:    []string{"https://example.com/file1.zip"},
			GID:     core.GID(1),
			Status:  core.StatusWaiting,
			Options: map[string]string{"dir": "/downloads"},
		},
		{
			URIs:    []string{"https://example.com/file2.zip"},
			GID:     core.GID(2),
			Status:  core.StatusPaused,
			Options: map[string]string{"dir": "/downloads"},
		},
	}

	var buf bytes.Buffer
	if err := Write(&buf, entries); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()
	if strings.Count(out, " gid=") != 2 {
		t.Errorf("expected 2 gid lines (special-first-key per entry), got %d\n%s", strings.Count(out, " gid="), out)
	}
}

func TestWriteCanonicalKeyOrder(t *testing.T) {
	entry := Entry{
		URIs:   []string{"https://example.com/file.zip"},
		GID:    core.GID(1),
		Status: core.StatusWaiting,
		Options: map[string]string{
			"dir":                       "/downloads",
			"out":                       "file.zip",
			"split":                     "5",
			"timeout":                   "30",
			"max-connection-per-server": "4",
		},
	}

	var buf bytes.Buffer
	if err := Write(&buf, []Entry{entry}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")

	// Check key ordering: after the URI line and special gid, options
	// should appear in canonical order.
	// We have: dir (ID 9), out (ID 10), split (ID 11), timeout (ID 3),
	//          max-connection-per-server (ID 79)
	// Expected order: timeout, dir, out, split, max-connection-per-server
	// (timeout ID 3 < dir ID 9 < out ID 10 < split ID 11 < max-connection-per-server ID 79)
	foundOrder := []string{}
	for _, line := range lines {
		if strings.HasPrefix(line, " ") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimPrefix(parts[0], " ")
				if key != "gid" {
					foundOrder = append(foundOrder, key)
				}
			}
		}
	}

	// Timeout (ID 3) should appear before dir (ID 9)
	idxTimeout := indexOf(foundOrder, "timeout")
	idxDir := indexOf(foundOrder, "dir")
	idxOut := indexOf(foundOrder, "out")
	idxSplit := indexOf(foundOrder, "split")
	idxMaxConn := indexOf(foundOrder, "max-connection-per-server")

	if idxTimeout < 0 || idxDir < 0 || idxOut < 0 || idxSplit < 0 || idxMaxConn < 0 {
		t.Fatalf("missing expected keys in output: %v", foundOrder)
	}
	if idxTimeout >= idxDir {
		t.Errorf("timeout (ID 3) should appear before dir (ID 9): order=%v", foundOrder)
	}
	if idxDir >= idxOut {
		t.Errorf("dir (ID 9) should appear before out (ID 10): order=%v", foundOrder)
	}
	if idxOut >= idxSplit {
		t.Errorf("out (ID 10) should appear before split (ID 11): order=%v", foundOrder)
	}
	if idxSplit >= idxMaxConn {
		t.Errorf("split (ID 11) should appear before max-connection-per-server (ID 79): order=%v", foundOrder)
	}
}

func TestWriteUnknownKeys(t *testing.T) {
	entry := Entry{
		URIs:    []string{"https://example.com/file.zip"},
		GID:     core.GID(1),
		Status:  core.StatusWaiting,
		Options: map[string]string{"dir": "/downloads"},
		Unknown: map[string]string{
			"future-option": "some-value",
			"new-feature":   "enabled",
		},
	}

	var buf bytes.Buffer
	if err := Write(&buf, []Entry{entry}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, " future-option=some-value\n") {
		t.Error("expected unknown key future-option to be preserved")
	}
	if !strings.Contains(out, " new-feature=enabled\n") {
		t.Error("expected unknown key new-feature to be preserved")
	}
}

func TestWriteCumulativeOptions(t *testing.T) {
	entry := Entry{
		URIs:   []string{"magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		GID:    core.GID(1),
		Status: core.StatusWaiting,
		Options: map[string]string{
			"bt-tracker": "http://tracker1.example.com/announce\nhttp://tracker2.example.com/announce",
			"header":     "X-Custom: value1\nX-Custom: value2",
		},
	}

	var buf bytes.Buffer
	if err := Write(&buf, []Entry{entry}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()

	// Each tracker URI on its own line
	if strings.Count(out, " bt-tracker=") != 2 {
		t.Errorf("expected 2 bt-tracker lines (cumulative), got:\n%s", out)
	}

	// Each header value on its own line
	if strings.Count(out, " header=") != 2 {
		t.Errorf("expected 2 header lines (cumulative), got:\n%s", out)
	}
}

func TestWriteCumulativeOptionsSkipsEmptySplitValues(t *testing.T) {
	entry := Entry{
		URIs:   []string{"https://example.com/file.zip"},
		GID:    core.GID(1),
		Status: core.StatusWaiting,
		Options: map[string]string{
			"header": "X-One: 1\n",
		},
	}

	var buf bytes.Buffer
	if err := Write(&buf, []Entry{entry}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()
	if strings.Count(out, " header=") != 1 {
		t.Fatalf("expected one header line, got:\n%s", out)
	}
	if strings.Contains(out, " header=\n") {
		t.Fatalf("did not expect empty header value, got:\n%s", out)
	}
}

func TestWriteToGzip(t *testing.T) {
	entry := Entry{
		URIs:    []string{"https://example.com/file.zip"},
		GID:     core.GID(1),
		Status:  core.StatusWaiting,
		Options: map[string]string{"dir": "/tmp"},
	}

	var buf bytes.Buffer
	if err := WriteGzip(&buf, []Entry{entry}); err != nil {
		t.Fatalf("WriteGzip: %v", err)
	}

	out := buf.Bytes()
	if len(out) < 2 || out[0] != 0x1f || out[1] != 0x8b {
		t.Errorf("gzip magic bytes not found, got: %02x %02x", out[0], out[1])
	}
}

func TestAtomicSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.session")

	entry := Entry{
		URIs:    []string{"https://example.com/file.zip"},
		GID:     core.GID(1),
		Status:  core.StatusWaiting,
		Options: map[string]string{"dir": "/tmp"},
	}

	if err := AtomicSave(path, []Entry{entry}, false); err != nil {
		t.Fatalf("AtomicSave: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !strings.Contains(string(data), " gid=0000000000000001\n") {
		t.Errorf("saved file missing gid line:\n%s", string(data))
	}

	// Temp file should not exist
	if _, err := os.Stat(path + "__temp"); !os.IsNotExist(err) {
		t.Error("temp file should have been renamed")
	}
}

func TestAtomicSaveGzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.session.gz")

	entry := Entry{
		URIs:   []string{"https://example.com/file.zip"},
		GID:    core.GID(1),
		Status: core.StatusWaiting,
	}

	if err := AtomicSave(path, []Entry{entry}, true); err != nil {
		t.Fatalf("AtomicSave: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		t.Errorf("gzip magic bytes not found, got: %02x %02x", data[0], data[1])
	}
}

func TestWriteGIDDuplication(t *testing.T) {
	// aria2 writes gid at special position 1 AND at ID 99 in canonical iteration.
	entry := Entry{
		URIs:   []string{"https://example.com/file.zip"},
		GID:    core.GID(1),
		Status: core.StatusWaiting,
		Options: map[string]string{
			"gid": "0000000000000001", // explicit gid in options triggers duplication
		},
	}

	var buf bytes.Buffer
	if err := Write(&buf, []Entry{entry}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()
	count := strings.Count(out, " gid=0000000000000001\n")
	if count < 2 {
		t.Errorf("expected gid to appear at least twice when in options, got %d\n%s", count, out)
	}
}

func TestWritePauseDuplication(t *testing.T) {
	// aria2 writes pause at special position 2 AND at ID 90 in canonical iteration
	// when pause=true and definedLocal.
	entry := Entry{
		URIs:   []string{"https://example.com/file.zip"},
		GID:    core.GID(1),
		Status: core.StatusPaused,
		Options: map[string]string{
			"pause": "true",
		},
	}

	var buf bytes.Buffer
	if err := Write(&buf, []Entry{entry}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()
	count := strings.Count(out, " pause=true\n")
	if count < 2 {
		t.Errorf("expected pause to appear at least twice when in options, got %d\n%s", count, out)
	}
}

func TestWriteNoTrailingNewline(t *testing.T) {
	entry := Entry{
		URIs:   []string{"https://example.com/file.zip"},
		GID:    core.GID(1),
		Status: core.StatusWaiting,
	}

	var buf bytes.Buffer
	if err := Write(&buf, []Entry{entry}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()
	if strings.HasSuffix(out, "\n\n") {
		t.Error("output should not have trailing blank line")
	}
}

func TestWriteNoURIsEntry(t *testing.T) {
	// Entry with no URIs should be skipped (empty URI line is not valid)
	entry := Entry{
		URIs:   nil,
		GID:    core.GID(1),
		Status: core.StatusWaiting,
	}

	var buf bytes.Buffer
	if err := Write(&buf, []Entry{entry}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected empty output for entry with no URIs, got: %s", buf.String())
	}
}

func TestSerializedHashDeterministic(t *testing.T) {
	entries := []Entry{
		{
			URIs:    []string{"https://example.com/file.zip"},
			GID:     core.GID(1),
			Status:  core.StatusWaiting,
			Options: map[string]string{"dir": "/tmp"},
		},
	}

	first, err := SerializedHash(entries)
	if err != nil {
		t.Fatalf("SerializedHash first: %v", err)
	}
	second, err := SerializedHash(entries)
	if err != nil {
		t.Fatalf("SerializedHash second: %v", err)
	}
	if first != second {
		t.Fatalf("SerializedHash mismatch: %x != %x", first, second)
	}

	entries[0].Options["dir"] = "/var/tmp"
	changed, err := SerializedHash(entries)
	if err != nil {
		t.Fatalf("SerializedHash changed: %v", err)
	}
	if changed == first {
		t.Fatal("SerializedHash did not change after serialized content changed")
	}
}

func indexOf(s []string, target string) int {
	for i, v := range s {
		if v == target {
			return i
		}
	}
	return -1
}
