package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_ParseMinimalConfig(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "aria2.conf")
	configContent := ""
	if err := os.WriteFile(confPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ref, err := RunRef(t, []string{"--conf-path=" + confPath, "--version"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"--conf-path=" + confPath, "--version"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	AssertEqualExit(t, ref, impl)
}

func TestConfig_ParseWithOptions(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "aria2.conf")
	configContent := `# Test config
max-concurrent-downloads=3
split=5
continue=true
dir=/tmp
`
	if err := os.WriteFile(confPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ref, err := RunRef(t, []string{"--conf-path=" + confPath, "--version"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"--conf-path=" + confPath, "--version"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	AssertEqualExit(t, ref, impl)
}

func TestConfig_GlobalOptionViaRPC(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "aria2.conf")
	configContent := `max-concurrent-downloads=5
split=10
max-connection-per-server=4
`
	if err := os.WriteFile(confPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort, "--conf-path="+confPath)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort, "--conf-path="+confPath)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getGlobalOption", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getGlobalOption", []any{})

	expected := map[string]string{
		"max-concurrent-downloads":  "5",
		"split":                     "10",
		"max-connection-per-server": "4",
	}
	requireStringMapValues(t, "ref global options", rr.Result, expected)
	requireStringMapValues(t, "impl global options", ir.Result, expected)
}

func TestConfig_RPCSecretAuth(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "aria2.conf")
	configContent := "rpc-secret=testsecret123\n"
	if err := os.WriteFile(confPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort, "--conf-path="+confPath)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort, "--conf-path="+confPath)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	// system.listMethods and system.listNotifications should work without secret.
	rrNoAuth := rpcCallOK(t, rPort, "system.listMethods", []any{})
	irNoAuth := rpcCallOK(t, iPort, "system.listMethods", []any{})

	var refMethods, implMethods []string
	if err := json.Unmarshal(rrNoAuth.Result, &refMethods); err != nil {
		t.Fatalf("unmarshal ref methods: %v", err)
	}
	if err := json.Unmarshal(irNoAuth.Result, &implMethods); err != nil {
		t.Fatalf("unmarshal impl methods: %v", err)
	}
	compareStringSet(t, "system.listMethods with rpc-secret", refMethods, implMethods)

	if len(refMethods) == 0 {
		t.Error("ref system.listMethods returned empty without auth (secret configured)")
	}
	if len(implMethods) == 0 {
		t.Error("impl system.listMethods returned empty without auth (secret configured)")
	}

	// getVersion should require the secret.
	rrAuth := rpcCall(t, rPort, "aria2.getVersion", []any{"token:testsecret123"})
	irAuth := rpcCall(t, iPort, "aria2.getVersion", []any{"token:testsecret123"})

	if rrAuth.Error != nil {
		t.Errorf("ref getVersion with correct secret failed: %s", rrAuth.Error.Message)
	}
	if irAuth.Error != nil {
		t.Errorf("impl getVersion with correct secret failed: %s", irAuth.Error.Message)
	}

	// Wrong secret should fail.
	rrWrong := rpcCall(t, rPort, "aria2.getVersion", []any{"token:wrongsecret"})
	irWrong := rpcCall(t, iPort, "aria2.getVersion", []any{"token:wrongsecret"})

	if rrWrong.Error == nil {
		t.Error("ref should reject wrong secret")
	}
	if irWrong.Error == nil {
		t.Error("impl should reject wrong secret")
	}

	// No secret should also fail.
	rrNone := rpcCall(t, rPort, "aria2.getVersion", []any{})
	irNone := rpcCall(t, iPort, "aria2.getVersion", []any{})

	if rrNone.Error == nil {
		t.Error("ref should reject missing secret")
	}
	if irNone.Error == nil {
		t.Error("impl should reject missing secret")
	}
}

func TestConfig_BooleanOptions(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "aria2.conf")
	configContent := `continue=true
allow-overwrite=false
quiet=true
check-certificate=false
`
	if err := os.WriteFile(confPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort, "--conf-path="+confPath)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort, "--conf-path="+confPath)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getGlobalOption", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getGlobalOption", []any{})

	expected := map[string]string{
		"continue":          "true",
		"allow-overwrite":   "false",
		"quiet":             "true",
		"check-certificate": "false",
	}
	requireStringMapValues(t, "ref boolean options", rr.Result, expected)
	requireStringMapValues(t, "impl boolean options", ir.Result, expected)
}

func TestConfig_SizeOptions(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "aria2.conf")
	configContent := `min-split-size=20M
disk-cache=64M
max-overall-download-limit=1M
`
	if err := os.WriteFile(confPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort, "--conf-path="+confPath)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort, "--conf-path="+confPath)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getGlobalOption", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getGlobalOption", []any{})

	compareStringMapValues(t, "size options", rr.Result, ir.Result, []string{
		"min-split-size",
		"disk-cache",
		"max-overall-download-limit",
	})
}

func TestConfig_CommentsAndWhitespace(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "aria2.conf")
	configContent := `# This is a comment
  # indented comment
max-concurrent-downloads = 8
  split  =  16

continue = false

`
	if err := os.WriteFile(confPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort, "--conf-path="+confPath)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort, "--conf-path="+confPath)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getGlobalOption", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getGlobalOption", []any{})

	var refOpts, implOpts map[string]string
	if err := json.Unmarshal(rr.Result, &refOpts); err != nil {
		t.Fatalf("unmarshal ref options: %v", err)
	}
	if err := json.Unmarshal(ir.Result, &implOpts); err != nil {
		t.Fatalf("unmarshal impl options: %v", err)
	}

	t.Logf("ref options with comments: %d keys", len(refOpts))
	t.Logf("impl options with comments: %d keys", len(implOpts))

	commentSensitiveKeys := []string{"max-concurrent-downloads", "split", "continue"}
	compareStringMapValues(t, "comments and whitespace options", rr.Result, ir.Result, commentSensitiveKeys)
	for _, key := range commentSensitiveKeys {
		if refV, ok := refOpts[key]; ok {
			t.Logf("ref %s = %s", key, refV)
		}
		if implV, ok := implOpts[key]; ok {
			t.Logf("impl %s = %s", key, implV)
		}
	}
}

func TestConfig_CLIFlagsOverrideConfig(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "aria2.conf")
	configContent := "max-concurrent-downloads=1\n"
	if err := os.WriteFile(confPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// CLI flag should override config file.
	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort,
		"--conf-path="+confPath,
		"--max-concurrent-downloads=99",
	)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort,
		"--conf-path="+confPath,
		"--max-concurrent-downloads=99",
	)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getGlobalOption", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getGlobalOption", []any{})

	expected := map[string]string{"max-concurrent-downloads": "99"}
	requireStringMapValues(t, "ref CLI override", rr.Result, expected)
	requireStringMapValues(t, "impl CLI override", ir.Result, expected)
}

func TestConfig_NoConfSuppressesDefaults(t *testing.T) {
	SkipIfNoRef(t)

	// When --no-conf is used, only CLI flags and defaults should apply.
	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort, "--no-conf")
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort, "--no-conf")
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getGlobalOption", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getGlobalOption", []any{})
	compareStringMapValues(t, "--no-conf defaults", rr.Result, ir.Result, []string{
		"max-concurrent-downloads",
		"split",
		"continue",
	})

	// Both should still work — they just use default options.
	t.Log("--no-conf servers started successfully")
}

func TestConfig_GlobalOptionMatrix(t *testing.T) {
	SkipIfNoRef(t)

	baseDir := t.TempDir()
	cases := []struct {
		name string
		args []string
		keys []string
	}{
		{
			name: "basic_download_options",
			args: []string{
				"--dir=" + filepath.Join(baseDir, "basic"),
				"--max-concurrent-downloads=7",
				"--split=3",
				"--max-connection-per-server=2",
				"--continue=false",
				"--allow-overwrite=false",
				"--auto-file-renaming=false",
				"--file-allocation=none",
				"--conditional-get=true",
				"--remote-time=true",
			},
			keys: []string{
				"dir",
				"max-concurrent-downloads",
				"split",
				"max-connection-per-server",
				"continue",
				"allow-overwrite",
				"auto-file-renaming",
				"file-allocation",
				"conditional-get",
				"remote-time",
			},
		},
		{
			name: "size_and_speed_units",
			args: []string{
				"--min-split-size=2M",
				"--max-overall-download-limit=1M",
				"--max-download-limit=512K",
				"--max-overall-upload-limit=256K",
				"--max-upload-limit=128K",
				"--lowest-speed-limit=4K",
				"--bt-request-peer-speed-limit=16K",
				"--no-file-allocation-limit=8M",
				"--max-mmap-limit=4M",
			},
			keys: []string{
				"min-split-size",
				"max-overall-download-limit",
				"max-download-limit",
				"max-overall-upload-limit",
				"max-upload-limit",
				"lowest-speed-limit",
				"bt-request-peer-speed-limit",
				"no-file-allocation-limit",
				"max-mmap-limit",
			},
		},
		{
			name: "http_ftp_proxy_options",
			args: []string{
				"--user-agent=aria2go-conformance-agent",
				"--referer=http://127.0.0.1/ref",
				"--enable-http-keep-alive=false",
				"--enable-http-pipelining=true",
				"--http-accept-gzip=true",
				"--http-no-cache=true",
				"--ftp-pasv=false",
				"--ftp-type=ascii",
				"--proxy-method=tunnel",
				"--all-proxy=http://proxy.example:8080",
				"--all-proxy-user=proxy-user",
				"--all-proxy-passwd=proxy-pass",
				"--no-proxy=localhost,127.0.0.1",
			},
			keys: []string{
				"user-agent",
				"referer",
				"enable-http-keep-alive",
				"enable-http-pipelining",
				"http-accept-gzip",
				"http-no-cache",
				"ftp-pasv",
				"ftp-type",
				"proxy-method",
				"all-proxy",
				"all-proxy-user",
				"all-proxy-passwd",
				"no-proxy",
			},
		},
		{
			name: "bittorrent_and_metalink_options",
			args: []string{
				"--bt-enable-lpd=true",
				"--bt-max-peers=47",
				"--bt-require-crypto=true",
				"--bt-force-encryption=true",
				"--bt-min-crypto-level=arc4",
				"--seed-ratio=2.5",
				"--follow-torrent=mem",
				"--follow-metalink=mem",
				"--metalink-preferred-protocol=https",
			},
			keys: []string{
				"bt-enable-lpd",
				"bt-max-peers",
				"bt-require-crypto",
				"bt-force-encryption",
				"bt-min-crypto-level",
				"seed-ratio",
				"follow-torrent",
				"follow-metalink",
				"metalink-preferred-protocol",
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rPort := findFreePort(t)
			refSrv := startRPCRef(t, rPort, tt.args...)
			defer refSrv.Stop(t)
			refSrv.WaitReady(t)

			iPort := findFreePort(t)
			implSrv := startRPCImpl(t, iPort, tt.args...)
			defer implSrv.Stop(t)
			implSrv.WaitReadyOrSkip(t)

			rr := rpcCallOK(t, rPort, "aria2.getGlobalOption", []any{})
			ir := rpcCallOK(t, iPort, "aria2.getGlobalOption", []any{})
			compareStringMapValues(t, tt.name, rr.Result, ir.Result, tt.keys)
		})
	}
}

func TestConfig_HelpDerivedDownloadLoadMatrix(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "uris.txt")
	saveSessionPath := filepath.Join(dir, "aria2.session")
	confPath := filepath.Join(dir, "aria2.conf")
	if err := os.WriteFile(inputPath, nil, 0o644); err != nil {
		t.Fatalf("write empty input file: %v", err)
	}
	requireRefHelpOptions(t,
		"dir",
		"out",
		"allow-overwrite",
		"auto-file-renaming",
		"continue",
		"input-file",
		"save-session",
		"download-result",
		"quiet",
		"stderr",
		"summary-interval",
		"show-console-readout",
		"header",
		"user-agent",
		"referer",
		"conditional-get",
		"remote-time",
		"content-disposition-default-utf8",
		"parameterized-uri",
	)

	configContent := strings.Join([]string{
		"dir=" + filepath.Join(dir, "downloads"),
		"out=matrix.bin",
		"allow-overwrite=true",
		"auto-file-renaming=false",
		"continue=true",
		"input-file=" + inputPath,
		"save-session=" + saveSessionPath,
		"download-result=full",
		"quiet=false",
		"stderr=true",
		"summary-interval=0",
		"show-console-readout=false",
		"header=X-Matrix: config",
		"user-agent=aria2go-config-matrix",
		"referer=http://127.0.0.1/config-ref",
		"conditional-get=true",
		"remote-time=true",
		"content-disposition-default-utf8=true",
		"parameterized-uri=true",
		"",
	}, "\n")
	if err := os.WriteFile(confPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort, "--conf-path="+confPath)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort, "--conf-path="+confPath)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	rr := rpcCallOK(t, rPort, "aria2.getGlobalOption", []any{})
	ir := rpcCallOK(t, iPort, "aria2.getGlobalOption", []any{})
	// aria2 accepts out/input-file in config files, but omits these
	// source-selection keys from getGlobalOption.
	compareStringMapValues(t, "help-derived download config load matrix", rr.Result, ir.Result, []string{
		"dir",
		"allow-overwrite",
		"auto-file-renaming",
		"continue",
		"save-session",
		"download-result",
		"quiet",
		"stderr",
		"summary-interval",
		"show-console-readout",
		"header",
		"user-agent",
		"referer",
		"conditional-get",
		"remote-time",
		"content-disposition-default-utf8",
		"parameterized-uri",
	})
}
