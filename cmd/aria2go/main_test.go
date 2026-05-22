package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/engine"
)

func TestPublicExitCodeMapsInternalInProgressSentinel(t *testing.T) {
	if got := publicExitCode(core.ExitInProgress); got != int(core.ExitUnfinishedDownloads) {
		t.Fatalf("publicExitCode(ExitInProgress) = %d, want %d", got, core.ExitUnfinishedDownloads)
	}
	if got := publicExitCode(core.ExitSuccess); got != int(core.ExitSuccess) {
		t.Fatalf("publicExitCode(ExitSuccess) = %d, want %d", got, core.ExitSuccess)
	}
}

func TestShowVersionOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	showVersion(buf)
	output := buf.String()
	if !strings.Contains(output, "aria2 version 1.37.0") {
		t.Error("version should contain correct version string")
	}
	if !strings.Contains(output, "Copyright") {
		t.Error("version should contain copyright")
	}
	if !strings.Contains(output, "Clean-room Go reimplementation") {
		t.Error("version should contain clean-room implementation text")
	}
	if !strings.Contains(output, "Apache-2.0") {
		t.Error("version should contain Apache-2.0 license text")
	}
	if !strings.Contains(output, "Enabled Features") {
		t.Error("version should contain enabled features")
	}
	for _, forbidden := range []string{"GPL", "GNU", "General Public License", "Free Software Foundation"} {
		if strings.Contains(output, forbidden) {
			t.Errorf("version output should not contain %q", forbidden)
		}
	}
}

func TestVersionFlagOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	showVersion(buf)
	output := buf.String()
	if !strings.Contains(output, packageName) {
		t.Error("version output should contain package name")
	}
	if !strings.Contains(output, version) {
		t.Error("version output should contain version number")
	}
	if !strings.Contains(output, "Compiler:") {
		t.Error("version output should contain compiler info")
	}
	if !strings.Contains(output, "System:") {
		t.Error("version output should contain system info")
	}
	if !strings.Contains(output, "Hash Algorithms:") {
		t.Error("version output should list hash algorithms")
	}
	if !strings.Contains(output, "Libraries:") {
		t.Error("version output should list libraries")
	}
}

func TestHelpFlag(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "")
	output := buf.String()
	if !strings.Contains(output, "Usage: aria2go") {
		t.Error("--help should show usage with program name")
	}
	if !strings.Contains(output, "Basic Options") {
		t.Error("--help should show basic options category")
	}
	if !strings.Contains(output, "RPC Options") {
		t.Error("--help should show RPC options category")
	}
	if !strings.Contains(output, "--enable-rpc") {
		t.Error("--help should mention --enable-rpc")
	}
}

func TestHelpFlagAll(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "#all")
	output := buf.String()
	if !strings.Contains(output, "Printing all options.") {
		t.Error("--help=#all should print all options message")
	}
	if !strings.Contains(output, "--dir=DIR") {
		t.Error("--help=#all should contain --dir")
	}
	if !strings.Contains(output, "--bt-tracker=URI") {
		t.Error("--help=#all should contain BT options")
	}
	if !strings.Contains(output, "--rpc-secret=TOKEN") {
		t.Error("--help=#all should contain RPC options")
	}
	if !strings.Contains(output, "--on-bt-download-complete=COMMAND") {
		t.Error("--help=#all should contain hook options")
	}
}

func TestHelpTagged(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"#http", "http-user"},
		{"#bittorrent", "bt-tracker"},
		{"#ftp", "ftp-user"},
		{"#rpc", "enable-rpc"},
		{"#metalink", "metalink-file"},
		{"#checksum", "checksum"},
		{"#cookie", "load-cookies"},
		{"#hook", "on-download-start"},
		{"#file", "dir"},
		{"#advanced", "conf-path"},
		{"#https", "https-proxy"},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			buf := new(bytes.Buffer)
			showHelp(buf, tt.tag)
			output := buf.String()
			if !strings.Contains(output, tt.want) {
				t.Errorf("help tag %s should contain option %s", tt.tag, tt.want)
			}
			if !strings.Contains(output, "Printing options tagged with") {
				t.Errorf("help tag %s should print tagging message", tt.tag)
			}
		})
	}
}

func TestHelpTaggedBittorrent(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "#bittorrent")
	output := buf.String()
	wantOptions := []string{
		"--bt-metadata-only",
		"--bt-tracker=",
		"--bt-max-peers=",
		"--enable-dht",
		"--torrent-file=",
		"--seed-ratio=",
	}
	for _, want := range wantOptions {
		if !strings.Contains(output, want) {
			t.Errorf("#bittorrent help should contain %q", want)
		}
	}
}

func TestShowBasicHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "")
	output := buf.String()
	if !strings.Contains(output, "Usage:") {
		t.Error("empty tag help should show basic usage")
	}
	if !strings.Contains(output, "--enable-rpc") {
		t.Error("help should contain --enable-rpc")
	}
	if !strings.Contains(output, "--input-file") {
		t.Error("help should contain --input-file")
	}
}

func TestHelpTaggedRPC(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "#rpc")
	output := buf.String()
	if !strings.Contains(strings.ToLower(output), "rpc") {
		t.Error("RPC help should contain rpc-related options")
	}
}

func TestHelpAllTags(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "#help")
	output := buf.String()
	if !strings.Contains(output, "Available tags") {
		t.Error("help tags should list available tags")
	}
}

func TestHelpUnknownTag(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "#nonexistent")
	output := buf.String()
	if !strings.Contains(output, "Unknown tag") {
		t.Error("should report unknown tag")
	}
}

func TestHelpKeywordMatch(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "log")
	output := buf.String()
	if !strings.Contains(output, "--log=") || !strings.Contains(output, "--log-level=") {
		t.Error("keyword search 'log' should show log-related options")
	}
}

func TestHelpAllKeyword(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "#all")
	output := buf.String()
	if !strings.Contains(output, "--timeout=") {
		t.Error("#all should show --timeout=")
	}
}

func TestHelpHTTPTag(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "#http")
	output := buf.String()
	if !strings.Contains(output, "http-user") {
		t.Error("#http tag should show HTTP-related options")
	}
}

func TestBasicHelpShowsShortFlags(t *testing.T) {
	buf := new(bytes.Buffer)
	showBasicHelp(buf)
	output := buf.String()
	if !strings.Contains(output, "-d, --dir=DIR") {
		t.Error("basic help should contain -d, --dir=DIR")
	}
	if !strings.Contains(output, "-T, --torrent-file=") {
		t.Error("basic help should contain torrent short flag")
	}
}

func TestReadInputFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/input.txt"
	if err := os.WriteFile(path, []byte("# comment\nhttp://example.com/file1\n\n\nhttp://example.com/file2\n"), 0644); err != nil {
		t.Fatalf("write input file: %v", err)
	}
	uris, err := readInputFile(path)
	if err != nil {
		t.Fatalf("readInputFile: %v", err)
	}
	if len(uris) != 2 {
		t.Fatalf("expected 2 URIs, got %d", len(uris))
	}
	if uris[0] != "http://example.com/file1" {
		t.Errorf("uri[0] = %q", uris[0])
	}
	if uris[1] != "http://example.com/file2" {
		t.Errorf("uri[1] = %q", uris[1])
	}
}

func TestReadInputFileNotFound(t *testing.T) {
	_, err := readInputFile("/nonexistent/path/file.txt")
	if err == nil {
		t.Error("expected error for nonexistent input file")
	}
}

func TestReadInputFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/empty.txt"
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	uris, err := readInputFile(path)
	if err != nil {
		t.Fatalf("readInputFile: %v", err)
	}
	if len(uris) != 0 {
		t.Errorf("expected 0 URIs, got %d", len(uris))
	}
}

func TestCleanupPostStartupTemplate(t *testing.T) {
	opts := &config.Options{
		Out:             "file.bin",
		ForceSequential: true,
		InputFile:       "input.txt",
		IndexOut:        []string{"1=piece.bin"},
		SelectFile:      "1",
		Pause:           true,
		Checksum:        "sha-1=deadbeef",
		GID:             "00000000000000ab",
	}
	for _, name := range []string{"out", "force-sequential", "input-file", "index-out", "select-file", "pause", "checksum", "gid"} {
		opts.MarkExplicit(name)
	}

	cleanupPostStartupTemplate(opts)

	if opts.Out != "" || opts.InputFile != "" || opts.SelectFile != "" || opts.Checksum != "" || opts.GID != "" {
		t.Fatalf("cleanup left string fields behind: %+v", opts)
	}
	if opts.ForceSequential || opts.Pause {
		t.Fatalf("cleanup left boolean fields behind: %+v", opts)
	}
	if len(opts.IndexOut) != 0 {
		t.Fatalf("cleanup left index-out entries behind: %+v", opts.IndexOut)
	}
	if got := opts.ExplicitNames(); len(got) != 0 {
		t.Fatalf("cleanup left explicit markers behind: %v", got)
	}
}

func TestGuessTorrentFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.torrent"
	if err := os.WriteFile(path, []byte("d8:announce..."), 0644); err != nil {
		t.Fatalf("write mock torrent: %v", err)
	}
	if !guessTorrentFile(path) {
		t.Error("should detect torrent file starting with 'd'")
	}
}

func TestGuessTorrentFileNotFound(t *testing.T) {
	if guessTorrentFile("/nonexistent/file.torrent") {
		t.Error("should return false for nonexistent file")
	}
}

func TestGuessMetalinkFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.metalink"
	if err := os.WriteFile(path, []byte("<?xml version='1.0'?>"), 0644); err != nil {
		t.Fatalf("write mock metalink: %v", err)
	}
	if !guessMetalinkFile(path) {
		t.Error("should detect metalink file starting with '<?xml'")
	}
}

func TestGuessMetalinkFileNotFound(t *testing.T) {
	if guessMetalinkFile("/nonexistent/file.meta4") {
		t.Error("should return false for nonexistent file")
	}
}

func TestDualHandlerWithAllDisabled(t *testing.T) {
	h := &dualHandler{
		file:    nopHandler{},
		console: nopHandler{},
	}
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("dualHandler with both disabled should return false for Enabled")
	}
}

type nopHandler struct{}

func (h nopHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (h nopHandler) Handle(context.Context, slog.Record) error { return nil }
func (h nopHandler) WithAttrs([]slog.Attr) slog.Handler        { return h }
func (h nopHandler) WithGroup(string) slog.Handler             { return h }

func TestDualHandlerWithAttrs(t *testing.T) {
	h := &dualHandler{
		file:    nopHandler{},
		console: nopHandler{},
	}
	rh := h.WithAttrs(nil)
	if rh == nil {
		t.Error("WithAttrs should not return nil")
	}
	gh := h.WithGroup("test")
	if gh == nil {
		t.Error("WithGroup should not return nil")
	}
}

func TestHelpCoversAllTags(t *testing.T) {
	expectedTags := []string{"#basic", "#advanced", "#http", "#https", "#ftp",
		"#metalink", "#bittorrent", "#cookie", "#hook", "#file",
		"#rpc", "#checksum", "#experimental", "#deprecated", "#help"}
	for _, tag := range expectedTags {
		if _, ok := helpTags[tag]; !ok {
			t.Errorf("missing help tag %q in helpTags map", tag)
		}
	}
}

func TestShowFilesTorrent(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	showTorrentFile("test.torrent")
	w.Close()
	os.Stderr = old
	io.ReadAll(r)
}

func TestShowFilesMetalink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.meta4")
	data := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="a.bin"><size>500</size><url>http://example.invalid/a.bin</url></file>
  <file name="sub/b.bin"><size>700</size><url>http://example.invalid/sub/b.bin</url></file>
</metalink>`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write metalink fixture: %v", err)
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	showMetalinkFile(path, nil)
	w.Close()
	os.Stdout = old
	output, _ := io.ReadAll(r)
	text := string(output)
	for _, want := range []string{"Files:", "  1|a.bin", "500B (500)", "  2|sub/b.bin", "700B (700)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("showMetalinkFile output missing %q:\n%s", want, text)
		}
	}
}

func TestFindDefaultConfigPath(t *testing.T) {
	path := findDefaultConfigPath()
	_ = path
}

func TestShowPositionalFiles(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	showPositionalFiles([]string{"/nonexistent"}, nil, nil)
	w.Close()
	os.Stderr = old
	io.ReadAll(r)
}

func TestLoadConfigFileValid(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "aria2.conf")
	if err := os.WriteFile(p, []byte("dir=/downloads\nsplit=8\nmax-concurrent-downloads=10\n"), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	opts, err := loadConfigFile(p)
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}
	if opts.Dir != "/downloads" {
		t.Errorf("Dir = %q, want /downloads", opts.Dir)
	}
	if opts.Split != 8 {
		t.Errorf("Split = %d, want 8", opts.Split)
	}
	if opts.MaxConcurrentDownloads != 10 {
		t.Errorf("MaxConcurrentDownloads = %d, want 10", opts.MaxConcurrentDownloads)
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := loadConfigFile("/no/such/config.conf")
	if err == nil {
		t.Error("expected error for nonexistent config file")
	}
}

func TestLoadConfigFileEmpty(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.conf")
	if err := os.WriteFile(p, []byte(""), 0644); err != nil {
		t.Fatalf("write empty config: %v", err)
	}
	opts, err := loadConfigFile(p)
	if err != nil {
		t.Fatalf("loadConfigFile empty: %v", err)
	}
	if opts == nil {
		t.Fatal("expected non-nil options for empty config")
	}
}

func TestOverrideEnv(t *testing.T) {
	t.Setenv("http_proxy", "http://proxy:8080")
	t.Setenv("https_proxy", "https://secure:8443")
	t.Setenv("ftp_proxy", "ftp://ftp-proxy:2121")
	t.Setenv("all_proxy", "socks5://all:1080")
	t.Setenv("no_proxy", "localhost,127.0.0.1")

	opts := &config.Options{}
	overrideEnv(opts)

	if opts.HTTPProxy != "http://proxy:8080" {
		t.Errorf("HTTPProxy = %q, want http://proxy:8080", opts.HTTPProxy)
	}
	if opts.HTTPSProxy != "https://secure:8443" {
		t.Errorf("HTTPSProxy = %q, want https://secure:8443", opts.HTTPSProxy)
	}
	if opts.FTPProxy != "ftp://ftp-proxy:2121" {
		t.Errorf("FTPProxy = %q, want ftp://ftp-proxy:2121", opts.FTPProxy)
	}
	if opts.AllProxy != "socks5://all:1080" {
		t.Errorf("AllProxy = %q, want socks5://all:1080", opts.AllProxy)
	}
	if opts.NoProxy != "localhost,127.0.0.1" {
		t.Errorf("NoProxy = %q, want localhost,127.0.0.1", opts.NoProxy)
	}
}

func TestOverrideEnvNoProxyVars(t *testing.T) {
	opts := &config.Options{
		Dir: "/default",
	}
	overrideEnv(opts)
	if opts.Dir != "/default" {
		t.Error("overrideEnv should not modify non-proxy fields")
	}
	if opts.HTTPProxy != "" {
		t.Error("HTTPProxy should remain empty without env var")
	}
}

func TestOverrideEnvEmptyValues(t *testing.T) {
	t.Setenv("http_proxy", "")
	opts := &config.Options{}
	overrideEnv(opts)
	if opts.HTTPProxy != "" {
		t.Error("empty env var should not override")
	}
}

func TestNoArgsBehavior(t *testing.T) {
	argv := []string{"aria2c"}
	opts, pos, err := config.ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs with no args: %v", err)
	}
	if len(pos) != 0 {
		t.Errorf("expected 0 positionals, got %d", len(pos))
	}
	_ = opts
}

func TestParseArgsIntegration(t *testing.T) {
	argv := []string{"aria2c", "--dir=/downloads", "--daemon", "--enable-rpc=true", "--max-concurrent-downloads=10", "http://example.com/file"}
	opts, pos, err := config.ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.Dir != "/downloads" {
		t.Errorf("Dir = %q, want /downloads", opts.Dir)
	}
	if !opts.Daemon {
		t.Error("Daemon should be true")
	}
	if !opts.EnableRPC {
		t.Error("EnableRPC should be true")
	}
	if opts.MaxConcurrentDownloads != 10 {
		t.Errorf("MaxConcurrentDownloads = %d, want 10", opts.MaxConcurrentDownloads)
	}
	if len(pos) != 1 || pos[0] != "http://example.com/file" {
		t.Errorf("positionals = %v, want [http://example.com/file]", pos)
	}
}

func TestDaemonChildArgsStripsDaemonFlags(t *testing.T) {
	argv := []string{
		"aria2go",
		"--daemon",
		"--dir=/downloads",
		"-D",
		"--daemon=true",
		"-D=true",
		"http://example.com/file",
	}

	got := daemonChildArgs(argv)
	want := []string{"aria2go", "--dir=/downloads", "http://example.com/file"}
	if len(got) != len(want) {
		t.Fatalf("daemonChildArgs(%v) = %v, want %v", argv, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("daemonChildArgs(%v) = %v, want %v", argv, got, want)
		}
	}
}

func TestConfigMergeOrder(t *testing.T) {
	defaults := config.Default()
	cli := &config.Options{Dir: "/cli-dir", Split: 16}
	merged := config.Merge(defaults, cli)
	if merged.Dir != "/cli-dir" {
		t.Errorf("Dir = %q, want /cli-dir (CLI should override default)", merged.Dir)
	}
	if merged.Split != 16 {
		t.Errorf("Split = %d, want 16 (CLI should override default)", merged.Split)
	}
	if merged.MaxConcurrentDownloads != 5 {
		t.Errorf("MaxConcurrentDownloads = %d, want 5 (default carried through)", merged.MaxConcurrentDownloads)
	}
}

func TestConfigMergeFullPipeline(t *testing.T) {
	defaults := config.Default()
	conf := &config.Options{Dir: "/conf-dir", LogLevel: "info"}
	cli := &config.Options{Dir: "/cli-dir", MaxConcurrentDownloads: 20}

	opts := config.Merge(defaults)
	opts = config.Merge(opts, conf)
	opts = config.Merge(opts, cli)

	if opts.Dir != "/cli-dir" {
		t.Errorf("Dir = %q, want /cli-dir", opts.Dir)
	}
	if opts.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", opts.LogLevel)
	}
	if opts.MaxConcurrentDownloads != 20 {
		t.Errorf("MaxConcurrentDownloads = %d, want 20", opts.MaxConcurrentDownloads)
	}
}

func TestDualHandlerHandle(t *testing.T) {
	var fileBuf, consoleBuf bytes.Buffer
	fileH := slog.NewTextHandler(&fileBuf, &slog.HandlerOptions{Level: slog.LevelDebug})
	consoleH := slog.NewTextHandler(&consoleBuf, &slog.HandlerOptions{Level: slog.LevelInfo})

	dh := &dualHandler{file: fileH, console: consoleH}

	record := slog.NewRecord(time.Now(), slog.LevelDebug, "debug message", 0)
	if err := dh.Handle(context.Background(), record); err != nil {
		t.Errorf("Handle debug: %v", err)
	}
	if fileBuf.Len() == 0 {
		t.Error("file handler should have received debug record")
	}
	if consoleBuf.Len() != 0 {
		t.Error("console handler should NOT have received debug record (level info)")
	}

	fileBuf.Reset()
	consoleBuf.Reset()

	record2 := slog.NewRecord(time.Now(), slog.LevelError, "error message", 0)
	if err := dh.Handle(context.Background(), record2); err != nil {
		t.Errorf("Handle error: %v", err)
	}
	if fileBuf.Len() == 0 {
		t.Error("file handler should have received error record")
	}
	if consoleBuf.Len() == 0 {
		t.Error("console handler should have received error record")
	}
}

func TestDualHandlerHandleConsoleOnly(t *testing.T) {
	var buf bytes.Buffer
	discardH := nopHandler{}
	consoleH := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})

	dh := &dualHandler{file: discardH, console: consoleH}

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "info message", 0)
	if err := dh.Handle(context.Background(), record); err != nil {
		t.Errorf("Handle: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("console handler should have received record")
	}
}

func TestShowFileContents(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	showFileContents("/nonexistent.torrent", "", nil, nil)
	w.Close()
	os.Stderr = old
	output, _ := io.ReadAll(r)
	if !strings.Contains(string(output), "not yet implemented") {
		t.Error("showFileContents for torrent should output not-yet-implemented message")
	}

	old = os.Stderr
	r2, w2, _ := os.Pipe()
	os.Stderr = w2
	showFileContents("", "/nonexistent.meta4", nil, nil)
	w2.Close()
	os.Stderr = old
	output2, _ := io.ReadAll(r2)
	if !strings.Contains(string(output2), "cannot read metalink file") {
		t.Error("showFileContents for metalink should report read error")
	}

	old = os.Stderr
	r3, w3, _ := os.Pipe()
	os.Stderr = w3
	showFileContents("/nonexistent.torrent", "/nonexistent.meta4", nil, nil)
	w3.Close()
	os.Stderr = old
	output3, _ := io.ReadAll(r3)
	if !strings.Contains(string(output3), "not yet implemented") || !strings.Contains(string(output3), "cannot read metalink file") {
		t.Error("showFileContents for both should output both error messages")
	}
}

func TestFindDefaultConfigPathHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	expected := filepath.Join(home, ".aria2", "aria2.conf")
	path := findDefaultConfigPath()
	if path != "" && path != expected {
		t.Errorf("findDefaultConfigPath = %q, expected %q or empty", path, expected)
	}
}

func TestEnvOverridesMap(t *testing.T) {
	expectedKeys := []string{"http_proxy", "https_proxy", "ftp_proxy", "all_proxy", "no_proxy"}
	for _, k := range expectedKeys {
		if _, ok := envOverrides[k]; !ok {
			t.Errorf("envOverrides missing key %q", k)
		}
	}
	if len(envOverrides) != len(expectedKeys) {
		t.Errorf("envOverrides has %d entries, want %d", len(envOverrides), len(expectedKeys))
	}
}

func TestShowVersionDetailed(t *testing.T) {
	buf := new(bytes.Buffer)
	showVersion(buf)
	output := buf.String()
	if !strings.Contains(output, "License: Apache-2.0") {
		t.Error("version should contain Apache-2.0 license info")
	}
	if !strings.Contains(output, "Clean-room Go reimplementation") {
		t.Error("version should contain clean-room implementation info")
	}
	if !strings.Contains(output, "Report bugs to") {
		t.Error("version should contain bug report URL")
	}
	if !strings.Contains(output, "github.com/smartass08/aria2go") {
		t.Error("version should contain project URL")
	}
	for _, forbidden := range []string{"GPL", "GNU", "General Public License", "Free Software Foundation"} {
		if strings.Contains(output, forbidden) {
			t.Errorf("version output should not contain %q", forbidden)
		}
	}
}

func TestGuessTorrentFileNotTorrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not.torrent")
	if err := os.WriteFile(path, []byte("not a bencoded dict"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if guessTorrentFile(path) {
		t.Error("should return false for non-torrent file that doesn't start with 'd'")
	}
}

func TestGuessMetalinkFileNotMetalink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not.meta4")
	if err := os.WriteFile(path, []byte("not xml"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if guessMetalinkFile(path) {
		t.Error("should return false for non-metalink file")
	}
}

func TestGuessTorrentFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.torrent")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if guessTorrentFile(path) {
		t.Error("should return false for empty file")
	}
}

func TestGuessMetalinkFileTooShort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "short.meta4")
	if err := os.WriteFile(path, []byte("<"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if guessMetalinkFile(path) {
		t.Error("should return false for file shorter than 5 bytes")
	}
}

func TestReadInputFileOnlyComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "comments.txt")
	if err := os.WriteFile(path, []byte("# comment 1\n # another\n\n# more\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	uris, err := readInputFile(path)
	if err != nil {
		t.Fatalf("readInputFile: %v", err)
	}
	if len(uris) != 0 {
		t.Errorf("expected 0 URIs from comment-only file, got %d", len(uris))
	}
}

func TestReadInputFileWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whitespace.txt")
	if err := os.WriteFile(path, []byte("  http://example.com/file  \n\t\thttp://other.com/file\t\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	uris, err := readInputFile(path)
	if err != nil {
		t.Fatalf("readInputFile: %v", err)
	}
	if len(uris) != 2 {
		t.Fatalf("expected 2 URIs, got %d", len(uris))
	}
	if uris[0] != "http://example.com/file" {
		t.Errorf("uri[0] = %q, want http://example.com/file", uris[0])
	}
	if uris[1] != "http://other.com/file" {
		t.Errorf("uri[1] = %q, want http://other.com/file", uris[1])
	}
}

func TestParseArgsHelpAndVersionSkipped(t *testing.T) {
	argv := []string{"aria2c", "--help", "--dir=/tmp", "--version", "--split=8", "-h", "-v"}
	opts, _, err := config.ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs with help/version mixed: %v", err)
	}
	if opts.Dir != "/tmp" {
		t.Errorf("Dir = %q, want /tmp", opts.Dir)
	}
	if opts.Split != 8 {
		t.Errorf("Split = %d, want 8", opts.Split)
	}
}

func TestHelpDeprecatedTag(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "#deprecated")
	output := buf.String()
	if !strings.Contains(output, "Printing options tagged with") {
		t.Error("should print tagged message for #deprecated")
	}
}

func TestHelpExperimentalTag(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "#experimental")
	output := buf.String()
	if !strings.Contains(output, "Printing options tagged with") {
		t.Error("should print tagged message for #experimental")
	}
}

func TestHelpKeywordCaseInsensitive(t *testing.T) {
	buf := new(bytes.Buffer)
	showHelp(buf, "LOG")
	output := buf.String()
	if !strings.Contains(output, "--log=") || !strings.Contains(output, "--log-level=") {
		t.Error("keyword search 'LOG' should be case-insensitive")
	}
}

func TestShowVersionConsistency(t *testing.T) {
	buf := new(bytes.Buffer)
	showVersion(buf)
	output := buf.String()
	if strings.Count(output, "aria2 version") != 1 {
		t.Error("version should appear exactly once")
	}
}

type captureAdder struct {
	specs []engine.AddSpec
}

func (a *captureAdder) Add(spec engine.AddSpec) (core.GID, error) {
	a.specs = append(a.specs, spec)
	return core.GID(len(a.specs)), nil
}

func TestStandalonePositionalsDisabledForExclusiveSources(t *testing.T) {
	tests := []struct {
		name string
		opts *config.Options
	}{
		{name: "torrent", opts: &config.Options{TorrentFile: "file.torrent"}},
		{name: "metalink", opts: &config.Options{MetalinkFile: "file.meta4"}},
		{name: "input", opts: &config.Options{InputFile: "uris.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if shouldAddStandalonePositionals(tt.opts) {
				t.Fatalf("shouldAddStandalonePositionals(%+v) = true, want false", tt.opts)
			}
		})
	}
	if !shouldAddStandalonePositionals(&config.Options{}) {
		t.Fatal("standalone positionals should be enabled when no exclusive source is set")
	}
}

func TestAddTorrentFileUsesPositionalsAsWebSeeds(t *testing.T) {
	dir := t.TempDir()
	torrentPath := filepath.Join(dir, "file.torrent")
	if err := os.WriteFile(torrentPath, []byte("torrent-data"), 0644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}
	adder := &captureAdder{}
	if err := addTorrentFile(adder, &config.Options{}, &config.Options{}, torrentPath, "http://seed1", "http://seed2"); err != nil {
		t.Fatalf("addTorrentFile: %v", err)
	}
	if len(adder.specs) != 1 {
		t.Fatalf("spec count = %d, want 1", len(adder.specs))
	}
	if string(adder.specs[0].Torrent) != "torrent-data" {
		t.Fatalf("torrent data = %q", string(adder.specs[0].Torrent))
	}
	want := []string{"http://seed1", "http://seed2"}
	got := adder.specs[0].URIs
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("web seed URIs = %v, want %v", got, want)
	}
	if adder.specs[0].MetadataURI != torrentPath {
		t.Fatalf("MetadataURI = %q, want %q", adder.specs[0].MetadataURI, torrentPath)
	}
}

func TestAddDownloadSourceExpandsParameterizedForceSequential(t *testing.T) {
	adder := &captureAdder{}
	added, err := addDownloadSource(adder, &config.Options{
		ParameterizedURI: true,
		ForceSequential:  true,
	}, &config.Options{}, "http://example.com/asset-[1-2].bin")
	if err != nil {
		t.Fatalf("addDownloadSource() error = %v", err)
	}
	if added != 2 {
		t.Fatalf("added = %d, want 2", added)
	}
	if len(adder.specs) != 2 {
		t.Fatalf("spec count = %d, want 2", len(adder.specs))
	}
	want := []string{
		"http://example.com/asset-1.bin",
		"http://example.com/asset-2.bin",
	}
	for i := range want {
		if len(adder.specs[i].URIs) != 1 || adder.specs[i].URIs[0] != want[i] {
			t.Fatalf("spec %d URIs = %v, want [%s]", i, adder.specs[i].URIs, want[i])
		}
	}
}
