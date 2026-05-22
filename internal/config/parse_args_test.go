package config

import (
	"testing"
)

func TestParseArgsLongFlagEquals(t *testing.T) {
	argv := []string{"aria2c", "--dir=/tmp/dl", "--split=8", "--max-connection-per-server=4"}
	opts, pos, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.Dir != "/tmp/dl" {
		t.Errorf("Dir = %q, want /tmp/dl", opts.Dir)
	}
	if opts.Split != 8 {
		t.Errorf("Split = %d, want 8", opts.Split)
	}
	if opts.MaxConnectionPerServer != 4 {
		t.Errorf("MaxConnectionPerServer = %d, want 4", opts.MaxConnectionPerServer)
	}
	if len(pos) != 0 {
		t.Errorf("positional = %v, want empty", pos)
	}
}

func TestParseArgsLongFlagSpace(t *testing.T) {
	argv := []string{"aria2c", "--dir", "/tmp/dl", "--split", "8"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.Dir != "/tmp/dl" {
		t.Errorf("Dir = %q, want /tmp/dl", opts.Dir)
	}
	if opts.Split != 8 {
		t.Errorf("Split = %d, want 8", opts.Split)
	}
}

func TestParseArgsLongFlagSpaceAcceptsDashValue(t *testing.T) {
	argv := []string{"aria2c", "--input-file", "-"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.InputFile != "-" {
		t.Fatalf("InputFile = %q, want -", opts.InputFile)
	}
}

func TestParseArgsBooleanFlagBare(t *testing.T) {
	argv := []string{"aria2c", "--daemon", "--quiet"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if !opts.Daemon {
		t.Error("Daemon = false, want true")
	}
	if !opts.Quiet {
		t.Error("Quiet = false, want true")
	}
}

func TestParseArgsBooleanFlagFalse(t *testing.T) {
	argv := []string{"aria2c", "--daemon=false", "--enable-rpc=false"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.Daemon {
		t.Error("Daemon = true, want false")
	}
	if opts.EnableRPC {
		t.Error("EnableRPC = true, want false")
	}
}

func TestParseArgsBooleanFlagTrue(t *testing.T) {
	argv := []string{"aria2c", "--daemon=true", "--enable-rpc=true"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if !opts.Daemon {
		t.Error("Daemon = false, want true")
	}
	if !opts.EnableRPC {
		t.Error("EnableRPC = false, want true")
	}
}

func TestParseArgsBooleanFlagMixed(t *testing.T) {
	argv := []string{"aria2c", "--daemon", "--quiet=false", "--check-integrity=true"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if !opts.Daemon {
		t.Error("Daemon = false, want true")
	}
	if opts.Quiet {
		t.Error("Quiet = true, want false")
	}
	if !opts.CheckIntegrity {
		t.Error("CheckIntegrity = false, want true")
	}
}

func TestParseArgsShortFormSpace(t *testing.T) {
	argv := []string{"aria2c", "-d", "/tmp/dl", "-s", "8", "-x", "4"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.Dir != "/tmp/dl" {
		t.Errorf("Dir = %q, want /tmp/dl", opts.Dir)
	}
	if opts.Split != 8 {
		t.Errorf("Split = %d, want 8", opts.Split)
	}
	if opts.MaxConnectionPerServer != 4 {
		t.Errorf("MaxConnectionPerServer = %d, want 4", opts.MaxConnectionPerServer)
	}
}

func TestParseArgsShortFormConcat(t *testing.T) {
	argv := []string{"aria2c", "-d/tmp/dl", "-s8", "-j10"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.Dir != "/tmp/dl" {
		t.Errorf("Dir = %q, want /tmp/dl", opts.Dir)
	}
	if opts.Split != 8 {
		t.Errorf("Split = %d, want 8", opts.Split)
	}
	if opts.MaxConcurrentDownloads != 10 {
		t.Errorf("MaxConcurrentDownloads = %d, want 10", opts.MaxConcurrentDownloads)
	}
}

func TestParseArgsShortBooleanBare(t *testing.T) {
	argv := []string{"aria2c", "-D", "-q"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if !opts.Daemon {
		t.Error("Daemon = false, want true")
	}
	if !opts.Quiet {
		t.Error("Quiet = false, want true")
	}
}

func TestParseArgsShortBooleanFalse(t *testing.T) {
	argv := []string{"aria2c", "-Dfalse", "-qfalse"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.Daemon {
		t.Error("Daemon = true, want false")
	}
	if opts.Quiet {
		t.Error("Quiet = true, want false")
	}
}

func TestParseArgsShortBooleanTrue(t *testing.T) {
	argv := []string{"aria2c", "-Dtrue"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if !opts.Daemon {
		t.Error("Daemon = false, want true")
	}
}

func TestParseArgsAccumulativeHeader(t *testing.T) {
	argv := []string{"aria2c", "--header=X-Foo: bar", "--header=X-Baz: qux"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(opts.Header) != 2 {
		t.Fatalf("Header length = %d, want 2", len(opts.Header))
	}
	if opts.Header[0] != "X-Foo: bar" {
		t.Errorf("Header[0] = %q, want X-Foo: bar", opts.Header[0])
	}
	if opts.Header[1] != "X-Baz: qux" {
		t.Errorf("Header[1] = %q, want X-Baz: qux", opts.Header[1])
	}
}

func TestParseArgsAccumulativeBTTracker(t *testing.T) {
	argv := []string{"aria2c", "--bt-tracker=udp://tracker1:6881", "--bt-tracker=udp://tracker2:6881"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(opts.BTTracker) != 2 {
		t.Fatalf("BTTracker length = %d, want 2", len(opts.BTTracker))
	}
}

func TestParseArgsAccumulativeDHTPoint(t *testing.T) {
	argv := []string{"aria2c", "--dht-entry-point=router.bittorrent.com:6881", "--dht-entry-point=dht.transmissionbt.com:6881"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(opts.DHTEntryPoint) != 2 {
		t.Fatalf("DHTEntryPoint length = %d, want 2", len(opts.DHTEntryPoint))
	}
	if opts.DHTEntryPointHost != "dht.transmissionbt.com" {
		t.Fatalf("DHTEntryPointHost = %q, want dht.transmissionbt.com", opts.DHTEntryPointHost)
	}
	if opts.DHTEntryPointPort != "6881" {
		t.Fatalf("DHTEntryPointPort = %q, want 6881", opts.DHTEntryPointPort)
	}
}

func TestParseArgsAccumulativeIndexOut(t *testing.T) {
	argv := []string{"aria2c", "--index-out=1=/path/a", "--index-out=2=/path/b"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(opts.IndexOut) != 2 {
		t.Fatalf("IndexOut length = %d, want 2", len(opts.IndexOut))
	}
}

func TestParseArgsAccumulativeBTExclude(t *testing.T) {
	argv := []string{"aria2c", "--bt-exclude-tracker=udp://bad:6881", "--bt-exclude-tracker=udp://bad2:6881"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(opts.BTExcludeTracker) != 2 {
		t.Fatalf("BTExcludeTracker length = %d, want 2", len(opts.BTExcludeTracker))
	}
}

func TestParseArgsAccumulativeDHT6Point(t *testing.T) {
	argv := []string{"aria2c", "--dht-entry-point6=[::1]:6881", "--dht-entry-point6=[::2]:6881"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(opts.DHTEntryPoint6) != 2 {
		t.Fatalf("DHTEntryPoint6 length = %d, want 2", len(opts.DHTEntryPoint6))
	}
	if opts.DHTEntryPointHost6 != "::2" {
		t.Fatalf("DHTEntryPointHost6 = %q, want ::2", opts.DHTEntryPointHost6)
	}
	if opts.DHTEntryPointPort6 != "6881" {
		t.Fatalf("DHTEntryPointPort6 = %q, want 6881", opts.DHTEntryPointPort6)
	}
}

func TestParseArgsPositionalArgs(t *testing.T) {
	argv := []string{"aria2c", "--dir=/tmp", "http://example.com/file.zip", "http://other.com/file2.zip"}
	opts, pos, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.Dir != "/tmp" {
		t.Errorf("Dir = %q, want /tmp", opts.Dir)
	}
	if len(pos) != 2 {
		t.Fatalf("positional length = %d, want 2", len(pos))
	}
	if pos[0] != "http://example.com/file.zip" {
		t.Errorf("pos[0] = %q", pos[0])
	}
	if pos[1] != "http://other.com/file2.zip" {
		t.Errorf("pos[1] = %q", pos[1])
	}
}

func TestParseArgsDashDashTerminator(t *testing.T) {
	argv := []string{"aria2c", "--dir=/tmp", "--", "--not-a-flag", "file.txt"}
	opts, pos, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.Dir != "/tmp" {
		t.Errorf("Dir = %q, want /tmp", opts.Dir)
	}
	if len(pos) != 2 {
		t.Fatalf("positional length = %d, want 2", len(pos))
	}
	if pos[0] != "--not-a-flag" {
		t.Errorf("pos[0] = %q, want --not-a-flag", pos[0])
	}
	if pos[1] != "file.txt" {
		t.Errorf("pos[1] = %q, want file.txt", pos[1])
	}
}

func TestParseArgsNoFlags(t *testing.T) {
	argv := []string{"aria2c", "http://example.com/file.zip"}
	_, pos, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(pos) != 1 {
		t.Fatalf("positional length = %d, want 1", len(pos))
	}
	if pos[0] != "http://example.com/file.zip" {
		t.Errorf("pos[0] = %q", pos[0])
	}
}

func TestParseArgsSkipHelpVersion(t *testing.T) {
	tests := []struct {
		argv []string
	}{
		{[]string{"aria2c", "--help"}},
		{[]string{"aria2c", "--version"}},
		{[]string{"aria2c", "-h"}},
		{[]string{"aria2c", "-v"}},
		{[]string{"aria2c", "--help", "--version"}},
	}

	for _, tt := range tests {
		opts, _, err := ParseArgs(tt.argv)
		if err != nil {
			t.Errorf("ParseArgs(%v): %v", tt.argv, err)
			continue
		}
		if opts.Daemon {
			t.Errorf("ParseArgs(%v): Daemon should not be set", tt.argv)
		}
	}
}

func TestParseArgsUnknownFlag(t *testing.T) {
	argv := []string{"aria2c", "--no-such-flag=value"}
	_, _, err := ParseArgs(argv)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestParseArgsUnknownShortFlag(t *testing.T) {
	argv := []string{"aria2c", "-y"}
	_, _, err := ParseArgs(argv)
	if err == nil {
		t.Fatal("expected error for unknown short flag")
	}
}

func TestParseArgsMissingValue(t *testing.T) {
	argv := []string{"aria2c", "--dir"}
	_, _, err := ParseArgs(argv)
	if err == nil {
		t.Fatal("expected error for missing value")
	}
}

func TestParseArgsMissingValueShort(t *testing.T) {
	argv := []string{"aria2c", "-d"}
	_, _, err := ParseArgs(argv)
	if err == nil {
		t.Fatal("expected error for missing value")
	}
}

func TestParseArgsInvalidInteger(t *testing.T) {
	argv := []string{"aria2c", "--split=abc"}
	_, _, err := ParseArgs(argv)
	if err == nil {
		t.Fatal("expected error for invalid integer")
	}
}

func TestParseArgsInvalidBoolean(t *testing.T) {
	argv := []string{"aria2c", "--daemon=yes"}
	_, _, err := ParseArgs(argv)
	if err == nil {
		t.Fatal("expected error for invalid boolean")
	}
}

func TestParseArgsShortFormAllMappings(t *testing.T) {
	// Verify all short flags map correctly
	shortChecks := map[string]struct {
		short    string
		longName string
		value    string
		checkFn  func(t *testing.T, o *Options)
	}{
		"dir": {
			short: "-d/tmp/x", checkFn: func(t *testing.T, o *Options) {
				if o.Dir != "/tmp/x" {
					t.Errorf("Dir = %q, want /tmp/x", o.Dir)
				}
			},
		},
		"input-file": {
			short: "-i/input.txt", checkFn: func(t *testing.T, o *Options) {
				if o.InputFile != "/input.txt" {
					t.Errorf("InputFile = %q", o.InputFile)
				}
			},
		},
		"log": {
			short: "-l/log.txt", checkFn: func(t *testing.T, o *Options) {
				if o.Log != "/log.txt" {
					t.Errorf("Log = %q", o.Log)
				}
			},
		},
		"max-concurrent-downloads": {
			short: "-j3", checkFn: func(t *testing.T, o *Options) {
				if o.MaxConcurrentDownloads != 3 {
					t.Errorf("MaxConcurrentDownloads = %d", o.MaxConcurrentDownloads)
				}
			},
		},
		"check-integrity": {
			short: "-Vtrue", checkFn: func(t *testing.T, o *Options) {
				if !o.CheckIntegrity {
					t.Error("CheckIntegrity = false")
				}
			},
		},
		"continue": {
			short: "-ctrue", checkFn: func(t *testing.T, o *Options) {
				if !o.Continue {
					t.Error("Continue = false")
				}
			},
		},
		"split": {
			short: "-s7", checkFn: func(t *testing.T, o *Options) {
				if o.Split != 7 {
					t.Errorf("Split = %d", o.Split)
				}
			},
		},
		"min-split-size": {
			short: "-k10M", checkFn: func(t *testing.T, o *Options) {
				if o.MinSplitSize != "10M" {
					t.Errorf("MinSplitSize = %q", o.MinSplitSize)
				}
			},
		},
		"out": {
			short: "-o/file.zip", checkFn: func(t *testing.T, o *Options) {
				if o.Out != "/file.zip" {
					t.Errorf("Out = %q", o.Out)
				}
			},
		},
		"max-upload-limit": {
			short: "-u50K", checkFn: func(t *testing.T, o *Options) {
				if o.MaxUploadLimit != "50K" {
					t.Errorf("MaxUploadLimit = %q", o.MaxUploadLimit)
				}
			},
		},
		"force-sequential": {
			short: "-Ztrue", checkFn: func(t *testing.T, o *Options) {
				if !o.ForceSequential {
					t.Error("ForceSequential = false")
				}
			},
		},
		"user-agent": {
			short: "-UMyAgent/1.0", checkFn: func(t *testing.T, o *Options) {
				if o.UserAgent != "MyAgent/1.0" {
					t.Errorf("UserAgent = %q", o.UserAgent)
				}
			},
		},
		"ftp-pasv": {
			short: "-ptrue", checkFn: func(t *testing.T, o *Options) {
				if !o.FTPPasv {
					t.Error("FTPPasv = false")
				}
			},
		},
		"no-netrc": {
			short: "-ntrue", checkFn: func(t *testing.T, o *Options) {
				if !o.NoNetrc {
					t.Error("NoNetrc = false")
				}
			},
		},
		"timeout": {
			short: "-t30", checkFn: func(t *testing.T, o *Options) {
				if o.Timeout != "30" {
					t.Errorf("Timeout = %q", o.Timeout)
				}
			},
		},
		"max-tries": {
			short: "-m3", checkFn: func(t *testing.T, o *Options) {
				if o.MaxTries != 3 {
					t.Errorf("MaxTries = %d", o.MaxTries)
				}
			},
		},
		"remote-time": {
			short: "-Rtrue", checkFn: func(t *testing.T, o *Options) {
				if !o.RemoteTime {
					t.Error("RemoteTime = false")
				}
			},
		},
		"parameterized-uri": {
			short: "-Ptrue", checkFn: func(t *testing.T, o *Options) {
				if !o.ParameterizedURI {
					t.Error("ParameterizedURI = false")
				}
			},
		},
		"torrent-file": {
			short: "-T/file.torrent", checkFn: func(t *testing.T, o *Options) {
				if o.TorrentFile != "/file.torrent" {
					t.Errorf("TorrentFile = %q", o.TorrentFile)
				}
			},
		},
		"show-files": {
			short: "-Strue", checkFn: func(t *testing.T, o *Options) {
				if !o.ShowFiles {
					t.Error("ShowFiles = false")
				}
			},
		},
		"metalink-file": {
			short: "-M/file.metalink", checkFn: func(t *testing.T, o *Options) {
				if o.MetalinkFile != "/file.metalink" {
					t.Errorf("MetalinkFile = %q", o.MetalinkFile)
				}
			},
		},
	}

	for name, sc := range shortChecks {
		t.Run(name, func(t *testing.T) {
			argv := []string{"aria2c", sc.short}
			opts, _, err := ParseArgs(argv)
			if err != nil {
				t.Fatalf("ParseArgs: %v", err)
			}
			sc.checkFn(t, opts)
		})
	}
}

func TestParseArgsIndexOutShort(t *testing.T) {
	argv := []string{"aria2c", "-O1=/path/a", "-O2=/path/b"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(opts.IndexOut) != 2 {
		t.Fatalf("IndexOut length = %d, want 2", len(opts.IndexOut))
	}
	if opts.IndexOut[0] != "1=/path/a" {
		t.Errorf("IndexOut[0] = %q", opts.IndexOut[0])
	}
	if opts.IndexOut[1] != "2=/path/b" {
		t.Errorf("IndexOut[1] = %q", opts.IndexOut[1])
	}
}

func TestParseArgsManyFlags(t *testing.T) {
	argv := []string{
		"aria2c",
		"--dir=/downloads",
		"--split=16",
		"--max-connection-per-server=8",
		"--daemon",
		"--enable-rpc",
		"--rpc-listen-port=6900",
		"--log-level=info",
		"--disk-cache=32M",
		"--file-allocation=none",
		"http://example.com/file.zip",
	}
	opts, pos, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.Dir != "/downloads" {
		t.Errorf("Dir = %q", opts.Dir)
	}
	if opts.Split != 16 {
		t.Errorf("Split = %d", opts.Split)
	}
	if opts.MaxConnectionPerServer != 8 {
		t.Errorf("MaxConnectionPerServer = %d", opts.MaxConnectionPerServer)
	}
	if !opts.Daemon {
		t.Error("Daemon = false")
	}
	if !opts.EnableRPC {
		t.Error("EnableRPC = false")
	}
	if opts.RPCListenPort != 6900 {
		t.Errorf("RPCListenPort = %d", opts.RPCListenPort)
	}
	if opts.LogLevel != "info" {
		t.Errorf("LogLevel = %q", opts.LogLevel)
	}
	if opts.DiskCache != "32M" {
		t.Errorf("DiskCache = %q", opts.DiskCache)
	}
	if opts.FileAllocation != "none" {
		t.Errorf("FileAllocation = %q", opts.FileAllocation)
	}
	if len(pos) != 1 {
		t.Fatalf("positional length = %d, want 1", len(pos))
	}
	if pos[0] != "http://example.com/file.zip" {
		t.Errorf("pos[0] = %q", pos[0])
	}
}

func TestParseArgsBoolDoesNotConsumeNextArg(t *testing.T) {
	// --daemon alone means true; the next arg should be positional, not consumed as value
	argv := []string{"aria2c", "--daemon", "http://example.com/file.zip"}
	opts, pos, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if !opts.Daemon {
		t.Error("Daemon = false, want true")
	}
	if len(pos) != 1 {
		t.Fatalf("positional length = %d, want 1", len(pos))
	}
	if pos[0] != "http://example.com/file.zip" {
		t.Errorf("pos[0] = %q", pos[0])
	}
}

func TestParseArgsBoolSpaceSyntaxTrueFalse(t *testing.T) {
	// When --flag is followed by "true" or "false" with a space
	// The space-syntax only applies to non-boolean flags; booleans
	// without = are always treated as bare=true.
	argv := []string{"aria2c", "--daemon", "false"}
	opts, pos, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if !opts.Daemon {
		t.Error("Daemon = false, want true (bare boolean defaults to true)")
	}
	if len(pos) != 1 {
		t.Fatalf("positional length = %d, want 1", len(pos))
	}
	if pos[0] != "false" {
		t.Errorf("pos[0] = %q, want false (positional)", pos[0])
	}
}

func TestParseArgsOverwrite(t *testing.T) {
	// Last appearance of non-accumulative flag wins
	argv := []string{"aria2c", "--dir=/first", "--dir=/second"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.Dir != "/second" {
		t.Errorf("Dir = %q, want /second", opts.Dir)
	}
}

func TestParseArgsStringValuesStoredAsIs(t *testing.T) {
	argv := []string{"aria2c", "--disk-cache=16M", "--timeout=60", "--summary-interval=30"}
	opts, _, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if opts.DiskCache != "16M" {
		t.Errorf("DiskCache = %q, want 16M", opts.DiskCache)
	}
	if opts.Timeout != "60" {
		t.Errorf("Timeout = %q, want 60", opts.Timeout)
	}
	if opts.SummaryInterval != "30" {
		t.Errorf("SummaryInterval = %q, want 30", opts.SummaryInterval)
	}
}

func TestParseArgsEmptyArgs(t *testing.T) {
	argv := []string{"aria2c"}
	opts, pos, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(pos) != 0 {
		t.Errorf("positional = %v, want empty", pos)
	}
	if opts.Daemon {
		t.Error("Daemon should not be set with no args")
	}
}

func TestParseArgsProgramNameOnly(t *testing.T) {
	argv := []string{"aria2c"}
	opts, pos, err := ParseArgs(argv)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if pos != nil && len(pos) != 0 {
		t.Errorf("pos = %v, want empty", pos)
	}
	_ = opts
}
