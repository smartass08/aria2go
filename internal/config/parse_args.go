package config

import (
	"fmt"
	"strings"
)

var shortFlags = map[byte]string{
	'd': "dir",
	'i': "input-file",
	'l': "log",
	'j': "max-concurrent-downloads",
	'V': "check-integrity",
	'c': "continue",
	'D': "daemon",
	's': "split",
	'x': "max-connection-per-server",
	'a': "file-allocation",
	'k': "min-split-size",
	'u': "max-upload-limit",
	'o': "out",
	'q': "quiet",
	'Z': "force-sequential",
	'U': "user-agent",
	'p': "ftp-pasv",
	'n': "no-netrc",
	't': "timeout",
	'm': "max-tries",
	'R': "remote-time",
	'P': "parameterized-uri",
	'T': "torrent-file",
	'S': "show-files",
	'O': "index-out",
	'M': "metalink-file",
}

// ParseArgs parses command-line arguments into Options.
// Returns the populated Options and any remaining non-flag arguments.
// Supports aria2's CLI flag syntax exactly.
func ParseArgs(argv []string) (*Options, []string, error) {
	opts := &Options{}
	var positional []string

	for i := 1; i < len(argv); i++ {
		arg := argv[i]

		if arg == "--" {
			positional = append(positional, argv[i+1:]...)
			break
		}

		if strings.HasPrefix(arg, "--") {
			name, value, hasValue := parseLongFlag(arg)

			if name == "help" || name == "version" {
				continue
			}

			setter, ok := fieldSetters[name]
			if !ok {
				return nil, nil, fmt.Errorf("config: unknown flag: --%s", name)
			}

			if !hasValue {
				if isBoolSetter(setter, name) {
					if err := setParsedOption(opts, name, "true"); err != nil {
						return nil, nil, err
					}
					continue
				}
				if i+1 < len(argv) && (!strings.HasPrefix(argv[i+1], "-") || argv[i+1] == "-") {
					i++
					value = argv[i]
				} else {
					return nil, nil, fmt.Errorf("config: missing value for --%s", name)
				}
			}

			if err := setParsedOption(opts, name, value); err != nil {
				return nil, nil, fmt.Errorf("config: --%s=%s: %w", name, value, err)
			}
		} else if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			shortByte := arg[1]

			if shortByte == 'h' || shortByte == 'v' {
				continue
			}

			longName, ok := shortFlags[shortByte]
			if !ok {
				return nil, nil, fmt.Errorf("config: unknown flag: %s", arg)
			}

			setter := fieldSetters[longName]

			if len(arg) > 2 {
				value := arg[2:]
				if err := setParsedOption(opts, longName, value); err != nil {
					return nil, nil, fmt.Errorf("config: -%c: %w", shortByte, err)
				}
			} else {
				if isBoolSetter(setter, longName) {
					if err := setParsedOption(opts, longName, "true"); err != nil {
						return nil, nil, err
					}
					continue
				}
				if i+1 < len(argv) {
					i++
					if err := setParsedOption(opts, longName, argv[i]); err != nil {
						return nil, nil, fmt.Errorf("config: -%c: %w", shortByte, err)
					}
				} else {
					return nil, nil, fmt.Errorf("config: missing value for -%c", shortByte)
				}
			}
		} else {
			positional = append(positional, arg)
		}
	}

	return opts, positional, nil
}

func setParsedOption(opts *Options, name, value string) error {
	if err := fieldSetters[name](opts, value); err != nil {
		return err
	}
	opts.markExplicit(name)
	return nil
}

func parseLongFlag(arg string) (name string, value string, hasValue bool) {
	rest := arg[2:]
	if idx := strings.IndexByte(rest, '='); idx >= 0 {
		return rest[:idx], rest[idx+1:], true
	}
	return rest, "", false
}

// isBoolSetter detects whether a setter is a boolean setter by trying
// to set "true" and checking for a parse error. The parseBool function
// in field.go only returns nil for "true"/"false".
func isBoolSetter(setter fieldSetter, name string) bool {
	// Boolean setters accept "true" successfully; non-bool would fail.
	// We detect by checking if setting "false" succeeds (bool) vs fails (int/string).
	// Actually, we can just test: bool fields only accept "true"/"false".
	// The simplest check: try setting "false" and see if it errors.
	// But this has a side effect. Instead, we pre-build a bool-set-map.
	return boolFields[name]
}

// boolFields marks which keys are boolean.
var boolFields = func() map[string]bool {
	m := make(map[string]bool, 64)
	for _, k := range []string{
		"check-integrity", "continue", "daemon", "quiet", "show-console-readout",
		"truncate-console-readout", "human-readable", "force-sequential", "stderr",
		"enable-http-keep-alive", "enable-http-pipelining", "http-accept-gzip",
		"http-auth-challenge", "http-no-cache", "no-want-digest-header", "use-head",
		"check-certificate", "ftp-pasv", "ftp-reuse-connection", "no-netrc",
		"remote-time", "reuse-uri", "dry-run", "parameterized-uri",
		"realtime-chunk-checksum", "bt-metadata-only", "bt-save-metadata",
		"bt-load-saved-metadata", "bt-enable-lpd", "bt-hash-check-seed",
		"bt-seed-unverified", "bt-remove-unselected-file", "bt-detach-seed-only",
		"bt-enable-hook-after-hash-check", "bt-force-encryption", "bt-require-crypto",
		"show-files", "enable-dht", "enable-dht6", "enable-peer-exchange",
		"metalink-enable-unique-protocol", "enable-rpc", "rpc-listen-all",
		"rpc-allow-origin-all", "rpc-secure", "rpc-save-upload-metadata",
		"pause", "pause-metadata", "no-conf", "allow-overwrite",
		"allow-piece-length-change", "always-resume", "auto-file-renaming",
		"conditional-get", "select-least-used-host",
		"content-disposition-default-utf8", "enable-mmap", "force-save",
		"save-not-found", "remove-control-file", "hash-check-only",
		"disable-ipv6", "async-dns", "enable-async-dns6",
		"deferred-input", "keep-unfinished-download-result", "enable-color",
	} {
		m[k] = true
	}
	return m
}()
