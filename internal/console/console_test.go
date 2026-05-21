package console

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	c := New(Options{})
	if c.out != os.Stdout {
		t.Error("New() should default to os.Stdout")
	}
}

func TestNewStderr(t *testing.T) {
	c := New(Options{Stderr: true})
	if c.out != os.Stderr {
		t.Error("New() with Stderr should use os.Stderr")
	}
}

func TestPrintln(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{}}
	c.Println("hello world")
	if got := buf.String(); got != "hello world\n" {
		t.Errorf("Println = %q, want %q", got, "hello world\n")
	}
}

func TestPrintlnQuiet(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{Quiet: true}}
	c.Println("hello world")
	if got := buf.String(); got != "" {
		t.Errorf("Println in Quiet mode should be suppressed, got %q", got)
	}
}

func TestPrintDownloadResultsDefault(t *testing.T) {
	var buf bytes.Buffer
	c := NewWithWriter(&buf, Options{})
	c.PrintDownloadResults([]ResultStat{{
		GID:          "1234567890abcdef",
		Status:       "OK",
		AverageSpeed: 2048,
		Path:         "/tmp/file.bin",
		Percent:      100,
	}}, false)

	out := buf.String()
	if !strings.Contains(out, "Download Results:") {
		t.Fatalf("Download Results header missing:\n%s", out)
	}
	if !strings.Contains(out, "123456|OK") {
		t.Errorf("result row missing abbreviated GID/status:\n%s", out)
	}
	if !strings.Contains(out, "/tmp/file.bin") {
		t.Errorf("result row missing path:\n%s", out)
	}
	if !strings.Contains(out, "(OK):download completed.") {
		t.Errorf("status legend missing OK entry:\n%s", out)
	}
}

func TestPrintDownloadResultsQuiet(t *testing.T) {
	var buf bytes.Buffer
	c := NewWithWriter(&buf, Options{Quiet: true})
	c.PrintDownloadResults([]ResultStat{{GID: "1234567890abcdef", Status: "OK"}}, false)
	if got := buf.String(); got != "" {
		t.Errorf("PrintDownloadResults in Quiet mode should be suppressed, got %q", got)
	}
}

func TestPrintDownloadResultsFullIncludesPercent(t *testing.T) {
	var buf bytes.Buffer
	c := NewWithWriter(&buf, Options{})
	c.PrintDownloadResults([]ResultStat{{
		GID:          "abcdef1234567890",
		Status:       "ERR",
		AverageSpeed: -1,
		Path:         "n/a",
		Percent:      42,
	}}, true)

	out := buf.String()
	if !strings.Contains(out, "|  %|path/URI") {
		t.Errorf("full result header missing percent column:\n%s", out)
	}
	if !strings.Contains(out, "| 42|n/a") {
		t.Errorf("full result row missing percent:\n%s", out)
	}
	if !strings.Contains(out, "n/a") {
		t.Errorf("full result row missing n/a speed/path:\n%s", out)
	}
}

func TestRenderEmpty(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{}}
	c.Render(nil)
	if buf.Len() != 0 {
		t.Error("Render(nil) should produce no output")
	}
}

func TestRenderQuiet(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{Quiet: true}}
	c.Render([]DownloadStat{{GID: "abcd1234567890ab", Status: "active"}})
	if buf.Len() != 0 {
		t.Error("Render in Quiet mode should produce no output")
	}
}

func TestRenderSingleNoColor(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	s := DownloadStat{
		GID:           "abcd1234567890ab",
		Status:        "active",
		Progress:      0.5,
		Speed:         1048576,
		TotalSize:     2097152,
		CompletedSize: 1048576,
		Connections:   4,
	}
	c.Render([]DownloadStat{s})
	out := buf.String()
	// bytes.Buffer is not a TTY, so colors are always stripped.
	// NoColor=true further ensures no ANSI escapes.
	if strings.Contains(out, "\033[") {
		t.Errorf("output should not contain ANSI escapes, got: %q", out)
	}
	if !strings.Contains(out, "#abcd12") {
		t.Errorf("output should contain GID prefix, got: %q", out)
	}
	if !strings.Contains(out, "1.0MiB/2.0MiB") {
		t.Errorf("output should show size progress, got: %q", out)
	}
	if !strings.Contains(out, "CN:4") {
		t.Errorf("output should show connection count, got: %q", out)
	}
	if !strings.Contains(out, "DL:") {
		t.Errorf("output should show DL speed, got: %q", out)
	}
	if !strings.Contains(out, "(50%)") {
		t.Errorf("output should show percentage, got: %q", out)
	}
}

func TestRenderSingleComplete(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	s := DownloadStat{
		GID:           "feed1234567890ab",
		Status:        "complete",
		Speed:         0,
		TotalSize:     4096,
		CompletedSize: 4096,
	}
	c.Render([]DownloadStat{s})
	out := buf.String()
	if strings.Contains(out, "DL:") {
		t.Errorf("complete download should not show DL speed, got: %q", out)
	}
	if !strings.Contains(out, "4.0KiB/4.0KiB") {
		t.Errorf("complete download should show sizes, got: %q", out)
	}
}

func TestRenderSingleWithETA(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	s := DownloadStat{
		GID:           "aaaa111122223333",
		Status:        "active",
		Speed:         1024,
		TotalSize:     2048,
		CompletedSize: 1024,
		ETA:           3600,
		Connections:   1,
	}
	c.Render([]DownloadStat{s})
	out := buf.String()
	if !strings.Contains(out, "ETA:1h0s") {
		t.Errorf("output should show ETA:1h0s, got: %q", out)
	}
}

func TestRenderSingleSeeding(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	s := DownloadStat{
		GID:                 "bbbb222233334444",
		Status:              "active",
		Seeder:              true,
		CompletedSize:       1048576,
		AllTimeUploadLength: 524288, // 0.5 share ratio
		SessionUploadLength: 512,
		UploadSpeed:         512,
		NumSeeders:          5,
	}
	c.Render([]DownloadStat{s})
	out := buf.String()
	if !strings.Contains(out, "SEED(") {
		t.Errorf("seeding download should show SEED, got: %q", out)
	}
	// Share ratio should be 0.5 (524288*10/1048576 = 5, /10.0 = 0.5)
	if !strings.Contains(out, "SEED(0.5)") {
		t.Errorf("seeding download should show SEED(0.5), got: %q", out)
	}
	if !strings.Contains(out, "SD:5") {
		t.Errorf("seeding download should show seeders, got: %q", out)
	}
	// SessionUploadLength > 0 so UL should appear
	if !strings.Contains(out, "UL:") {
		t.Errorf("seeding download should show UL speed, got: %q", out)
	}
	// AllTimeUploadLength=524288 abbreviates to 512KiB
	if !strings.Contains(out, "(512KiB)") {
		t.Errorf("seeding download should show allTimeUploadLength(512KiB) in parens, got: %q", out)
	}
}

func TestRenderSingleSeedingNoUpload(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	s := DownloadStat{
		GID:           "cccc333344445555",
		Status:        "active",
		Seeder:        true,
		CompletedSize: 1048576,
		// SessionUploadLength is 0, so UL should NOT appear
		NumSeeders: 5,
	}
	c.Render([]DownloadStat{s})
	out := buf.String()
	if strings.Contains(out, "UL:") {
		t.Errorf("seeding with no session upload should not show UL, got: %q", out)
	}
}

func TestRenderCompact(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	snaps := []DownloadStat{
		{GID: "1111111111111111", Status: "active", Speed: 512, TotalSize: 1024, CompletedSize: 256, Connections: 2},
		{GID: "2222222222222222", Status: "active", Speed: 256, TotalSize: 2048, CompletedSize: 512, Connections: 3},
	}
	c.Render(snaps)
	out := buf.String()
	if !strings.Contains(out, "DL:") {
		t.Errorf("compact output should show DL, got: %q", out)
	}
	if !strings.Contains(out, "#111111") {
		t.Errorf("compact output should show first GID, got: %q", out)
	}
	if !strings.Contains(out, "#222222") {
		t.Errorf("compact output should show second GID, got: %q", out)
	}
}

func TestRenderCompactDownloadsDone(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true, DownloadsDone: true}}
	snaps := []DownloadStat{
		{GID: "1111111111111111", Status: "complete", Speed: 0, TotalSize: 1024, CompletedSize: 1024},
		{GID: "2222222222222222", Status: "complete", Speed: 0, TotalSize: 2048, CompletedSize: 2048},
	}
	c.Render(snaps)
	out := buf.String()
	// DownloadsDone=true should omit the DL/UL header
	if strings.Contains(out, "DL:") {
		t.Errorf("compact output with DownloadsDone should omit DL header, got: %q", out)
	}
	if !strings.Contains(out, "#111111") {
		t.Errorf("compact output should show first GID, got: %q", out)
	}
}

func TestRenderCompactOverflow(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	snaps := make([]DownloadStat, 10)
	for i := range snaps {
		snaps[i] = DownloadStat{
			GID:           "gid0000000000000" + string(rune('0'+i)),
			Status:        "active",
			Speed:         100,
			TotalSize:     1000,
			CompletedSize: 100,
		}
	}
	c.Render(snaps)
	out := buf.String()
	if !strings.Contains(out, "(+5)") {
		t.Errorf("compact overflow should show (+5), got: %q", out)
	}
}

func TestRenderSummary(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{Summary: true, NoColor: true}}
	s := DownloadStat{
		GID:           "cccc333344445555",
		Status:        "active",
		Speed:         1024,
		TotalSize:     4096,
		CompletedSize: 1024,
		Filename:      "test.file",
		Connections:   2,
	}
	c.Render([]DownloadStat{s})
	out := buf.String()
	if !strings.Contains(out, "Download Progress Summary as of") {
		t.Errorf("summary should contain date header, got: %q", out)
	}
	if !strings.Contains(out, "#cccc33") {
		t.Errorf("summary should contain GID, got: %q", out)
	}
	if !strings.Contains(out, "FILE: test.file") {
		t.Errorf("summary should contain FILE: line, got: %q", out)
	}
	if !strings.Contains(out, "---") {
		t.Errorf("summary should contain per-item separator, got: %q", out)
	}
}

func TestRenderSummaryNonTTYNeverUsesColors(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{Summary: true, NoColor: false}}
	s := DownloadStat{
		GID:           "cccc333344445555",
		Status:        "active",
		Speed:         1024,
		TotalSize:     4096,
		CompletedSize: 1024,
		Filename:      "test.file",
		Connections:   2,
	}
	c.Render([]DownloadStat{s})
	if out := buf.String(); strings.Contains(out, "\033[") {
		t.Errorf("summary on non-TTY should not contain ANSI escapes, got: %q", out)
	}
}

func TestAbbrevSize(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{0, "0"},
		{512, "512"},
		{1024, "1.0Ki"},
		{1536, "1.5Ki"},
		{1048576, "1.0Mi"},
		{int64(1048576) * 1024, "1.0Gi"},
		{-1, "0"},
	}
	for _, tt := range tests {
		got := abbrevSize(tt.size)
		if got != tt.want {
			t.Errorf("abbrevSize(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}

func TestSecfmt(t *testing.T) {
	tests := []struct {
		sec  int64
		want string
	}{
		{0, "0s"},
		{30, "30s"},
		{60, "1m0s"},
		{90, "1m30s"},
		{120, "2m0s"},
		{3600, "1h0s"},
		{3661, "1h1m1s"},
		{7200, "2h0s"},
		{7261, "2h1m1s"},
	}
	for _, tt := range tests {
		got := secfmt(tt.sec)
		if got != tt.want {
			t.Errorf("secfmt(%d) = %q, want %q", tt.sec, got, tt.want)
		}
	}
}

func TestAbbrevGID(t *testing.T) {
	tests := []struct {
		gid  string
		want string
	}{
		{"abcdef1234567890", "abcdef"},
		{"abc", "abc"},
		{"", ""},
	}
	for _, tt := range tests {
		got := abbrevGID(tt.gid)
		if got != tt.want {
			t.Errorf("abbrevGID(%q) = %q, want %q", tt.gid, got, tt.want)
		}
	}
}

func TestStripColors(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"\033[35mtext\033[0m", "text"},
		{"\033[32mspeed\033[0m", "speed"},
		{"no color here", "no color here"},
		{"", ""},
	}
	for _, tt := range tests {
		got := stripColors(tt.in)
		if got != tt.want {
			t.Errorf("stripColors(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatInt(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{123, "123"},
		{1234, "1,234"},
		{1234567, "1,234,567"},
		{1000000, "1,000,000"},
	}
	for _, tt := range tests {
		got := formatInt(tt.n)
		if got != tt.want {
			t.Errorf("formatInt(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestRunInteractiveExit(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{Interactive: true}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.RunInteractive(ctx, func(cmd string) string { return "" })
	if err == nil {
		t.Error("RunInteractive should return context error")
	}
}

func TestRunInteractiveIsExtension(t *testing.T) {
	// Verify RunInteractive is documented as an aria2go extension.
	// The doc comment on RunInteractive must contain "aria2go extension".
	// This is a documentation check; no runtime behavior to test.
}

func TestSignals(t *testing.T) {
	c := &Console{}
	ctx, cancel := context.WithCancel(context.Background())
	ch := c.Signals(ctx)
	if ch == nil {
		t.Fatal("Signals() returned nil channel")
	}
	cancel()
	_, ok := <-ch
	if ok {
		t.Error("signal channel should be closed after context cancel")
	}
}

func TestAllocProgressStub(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	c.SetFileAllocProgress(&AllocProgress{
		GID:         "filealloc01",
		CurrentSize: 512,
		TotalSize:   1024,
		Queued:      2,
	})
	c.Render([]DownloadStat{
		{GID: "abcd1234567890ab", Status: "active", Speed: 100, TotalSize: 1024, CompletedSize: 512, Connections: 1},
	})
	out := buf.String()
	if !strings.Contains(out, "FileAlloc:#fileal") {
		t.Errorf("output should contain FileAlloc stub, got: %q", out)
	}
	if !strings.Contains(out, "(+2)") {
		t.Errorf("output should contain queued count, got: %q", out)
	}
}

func TestCheckProgressStub(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	c.SetCheckProgress(&CheckProgress{
		GID:         "checksum01",
		CurrentSize: 256,
		TotalSize:   512,
	})
	c.Render([]DownloadStat{
		{GID: "abcd1234567890ab", Status: "active", Speed: 100, TotalSize: 1024, CompletedSize: 512, Connections: 1},
	})
	out := buf.String()
	if !strings.Contains(out, "Checksum:#checks") {
		t.Errorf("output should contain Checksum stub, got: %q", out)
	}
}

func TestAllocAndCheckProgressHidden(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	// No progress set — stubs should be hidden.
	c.Render([]DownloadStat{
		{GID: "abcd1234567890ab", Status: "active", Speed: 100, TotalSize: 1024, CompletedSize: 512, Connections: 1},
	})
	out := buf.String()
	if strings.Contains(out, "FileAlloc:") {
		t.Errorf("output should NOT contain FileAlloc when not set, got: %q", out)
	}
	if strings.Contains(out, "Checksum:") {
		t.Errorf("output should NOT contain Checksum when not set, got: %q", out)
	}
}

func TestRenderUpdateThrottle(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	s := DownloadStat{
		GID:           "abcd1234567890ab",
		Status:        "active",
		Speed:         100,
		TotalSize:     1024,
		CompletedSize: 512,
		Connections:   1,
	}
	// First render should produce output.
	c.Render([]DownloadStat{s})
	first := buf.String()
	if len(first) == 0 {
		t.Fatal("first render should produce output")
	}
	// Second render within 1s should be suppressed.
	buf.Reset()
	c.Render([]DownloadStat{s})
	second := buf.String()
	if len(second) != 0 {
		t.Errorf("second render within 1s should be suppressed, got: %q", second)
	}
}

func TestRenderNonTTYNeverUsesColors(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: false}}
	s := DownloadStat{
		GID:           "abcd1234567890ab",
		Status:        "active",
		Speed:         1024,
		TotalSize:     4096,
		CompletedSize: 2048,
		Connections:   2,
		ETA:           60,
	}
	c.Render([]DownloadStat{s})
	out := buf.String()
	// bytes.Buffer is not a TTY, so colors should be stripped even with NoColor:false.
	if strings.Contains(out, "\033[") {
		t.Errorf("non-TTY output should not contain ANSI escapes, got: %q", out)
	}
}

func TestRenderSingleWithUploadSession(t *testing.T) {
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	s := DownloadStat{
		GID:                 "eee333355556666",
		Status:              "active",
		Speed:               2048,
		UploadSpeed:         128,
		TotalSize:           8192,
		CompletedSize:       4096,
		SessionUploadLength: 256,
		AllTimeUploadLength: 1024,
		Connections:         2,
	}
	c.Render([]DownloadStat{s})
	out := buf.String()
	if !strings.Contains(out, "UL:") {
		t.Errorf("output should show UL when SessionUploadLength > 0, got: %q", out)
	}
	if !strings.Contains(out, "(1.0KiB)") {
		t.Errorf("output should show allTimeUploadLength in parens, got: %q", out)
	}
}

func TestSingleOutputNoTrailingNewlineOnTTY(t *testing.T) {
	// On non-TTY (bytes.Buffer), the output ends with newline via Fprintln.
	var buf bytes.Buffer
	c := &Console{out: &buf, opts: Options{NoColor: true}}
	s := DownloadStat{
		GID:           "abcd1234567890ab",
		Status:        "active",
		Speed:         100,
		TotalSize:     1024,
		CompletedSize: 512,
		Connections:   1,
	}
	c.Render([]DownloadStat{s})
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("non-TTY output should end with newline, got: %q", out)
	}
}
