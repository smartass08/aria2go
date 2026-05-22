package config

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

var sessionOptionFieldKinds = func() map[string]reflect.Kind {
	typ := reflect.TypeOf(Options{})
	kinds := make(map[string]reflect.Kind, len(optionFieldIndices))
	for name, index := range optionFieldIndices {
		kinds[name] = typ.Field(index).Type.Kind()
	}
	return kinds
}()

// CloneExplicitOptions returns a copy of src containing only explicitly set
// option fields. Slice fields are deep-copied.
func CloneExplicitOptions(src *Options) *Options {
	if src == nil || len(src.explicit) == 0 {
		return nil
	}

	dst := &Options{}
	srcValue := reflect.ValueOf(src).Elem()
	dstValue := reflect.ValueOf(dst).Elem()

	for name := range src.explicit {
		fieldIndex, ok := optionFieldIndices[name]
		if !ok {
			continue
		}
		dstValue.Field(fieldIndex).Set(cloneOptionFieldValue(srcValue.Field(fieldIndex)))
		dst.markExplicit(name)
	}

	return dst
}

func cloneOptionFieldValue(v reflect.Value) reflect.Value {
	if v.Kind() != reflect.Slice {
		return v
	}
	if v.IsNil() {
		return reflect.Zero(v.Type())
	}
	cp := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
	reflect.Copy(cp, v)
	return cp
}

// SessionOptionMap returns the aria2 session-serializable option surface for
// explicitly set request-local options.
func SessionOptionMap(opts *Options) map[string]string {
	if opts == nil || len(opts.explicit) == 0 {
		return nil
	}

	value := reflect.ValueOf(opts).Elem()
	out := make(map[string]string, len(opts.explicit))
	for _, name := range fieldsSorted {
		if !opts.explicit[name] || !sessionInitialOptions[name] {
			continue
		}
		if name == "gid" || name == "pause" {
			continue
		}
		fieldIndex, ok := optionFieldIndices[name]
		if !ok {
			continue
		}
		formatted := formatSessionOptionValue(name, value.Field(fieldIndex), sessionOptionFieldKinds[name])
		if sessionOptionFieldKinds[name] == reflect.Slice && formatted == "" {
			continue
		}
		out[name] = formatted
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func formatSessionOptionValue(name string, field reflect.Value, kind reflect.Kind) string {
	switch kind {
	case reflect.String:
		value := field.String()
		if normalized, ok := normalizeSessionUnitOption(name, value); ok {
			return normalized
		}
		if normalized, ok := normalizeSessionProxyOption(name, value); ok {
			return normalized
		}
		return value
	case reflect.Int:
		return strconv.FormatInt(field.Int(), 10)
	case reflect.Bool:
		if field.Bool() {
			return "true"
		}
		return "false"
	case reflect.Slice:
		if field.Len() == 0 {
			return ""
		}
		values := make([]string, 0, field.Len())
		for i := 0; i < field.Len(); i++ {
			values = append(values, field.Index(i).String())
		}
		return strings.Join(values, "\n")
	default:
		return ""
	}
}

func normalizeSessionUnitOption(name, value string) (string, bool) {
	if !sessionUnitOptions[name] || value == "" {
		return "", false
	}
	n, err := parseSessionUnit(value)
	if err != nil {
		return value, true
	}
	return strconv.FormatInt(n, 10), true
}

func parseSessionUnit(value string) (int64, error) {
	mult := int64(1)
	switch last := value[len(value)-1]; last {
	case 'K', 'k':
		mult = 1024
		value = value[:len(value)-1]
	case 'M', 'm':
		mult = 1024 * 1024
		value = value[:len(value)-1]
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, strconv.ErrSyntax
	}
	if n > (1<<63-1)/mult {
		return 0, strconv.ErrRange
	}
	return n * mult, nil
}

func normalizeSessionProxyOption(name, value string) (string, bool) {
	if !sessionProxyOptions[name] || value == "" {
		return "", false
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return value, true
	}
	if !strings.HasSuffix(value, "/") {
		value += "/"
	}
	return value, true
}

var sessionUnitOptions = map[string]bool{
	"bt-request-peer-speed-limit": true,
	"disk-cache":                  true,
	"lowest-speed-limit":          true,
	"max-download-limit":          true,
	"max-mmap-limit":              true,
	"max-upload-limit":            true,
	"min-split-size":              true,
	"no-file-allocation-limit":    true,
	"piece-length":                true,
}

var sessionProxyOptions = map[string]bool{
	"all-proxy":   true,
	"ftp-proxy":   true,
	"http-proxy":  true,
	"https-proxy": true,
}

// sessionInitialOptions is derived from aria2's OptionHandlerFactory.cc
// setInitialOption(true) surface and kept in option name form.
var sessionInitialOptions = map[string]bool{
	"all-proxy":                        true,
	"all-proxy-passwd":                 true,
	"all-proxy-user":                   true,
	"allow-overwrite":                  true,
	"allow-piece-length-change":        true,
	"always-resume":                    true,
	"async-dns":                        true,
	"auto-file-renaming":               true,
	"bt-enable-hook-after-hash-check":  true,
	"bt-enable-lpd":                    true,
	"bt-exclude-tracker":               true,
	"bt-external-ip":                   true,
	"bt-force-encryption":              true,
	"bt-hash-check-seed":               true,
	"bt-load-saved-metadata":           true,
	"bt-max-peers":                     true,
	"bt-metadata-only":                 true,
	"bt-min-crypto-level":              true,
	"bt-prioritize-piece":              true,
	"bt-remove-unselected-file":        true,
	"bt-request-peer-speed-limit":      true,
	"bt-require-crypto":                true,
	"bt-save-metadata":                 true,
	"bt-seed-unverified":               true,
	"bt-stop-timeout":                  true,
	"bt-tracker":                       true,
	"bt-tracker-connect-timeout":       true,
	"bt-tracker-interval":              true,
	"bt-tracker-timeout":               true,
	"check-integrity":                  true,
	"checksum":                         true,
	"conditional-get":                  true,
	"connect-timeout":                  true,
	"content-disposition-default-utf8": true,
	"continue":                         true,
	"dir":                              true,
	"dry-run":                          true,
	"enable-async-dns6":                true,
	"enable-http-keep-alive":           true,
	"enable-http-pipelining":           true,
	"enable-mmap":                      true,
	"enable-peer-exchange":             true,
	"file-allocation":                  true,
	"follow-metalink":                  true,
	"follow-torrent":                   true,
	"force-save":                       true,
	"ftp-passwd":                       true,
	"ftp-pasv":                         true,
	"ftp-proxy":                        true,
	"ftp-proxy-passwd":                 true,
	"ftp-proxy-user":                   true,
	"ftp-reuse-connection":             true,
	"ftp-type":                         true,
	"ftp-user":                         true,
	"gid":                              true,
	"hash-check-only":                  true,
	"header":                           true,
	"http-accept-gzip":                 true,
	"http-auth-challenge":              true,
	"http-no-cache":                    true,
	"http-passwd":                      true,
	"http-proxy":                       true,
	"http-proxy-passwd":                true,
	"http-proxy-user":                  true,
	"http-user":                        true,
	"https-proxy":                      true,
	"https-proxy-passwd":               true,
	"https-proxy-user":                 true,
	"index-out":                        true,
	"max-connection-per-server":        true,
	"max-download-limit":               true,
	"max-file-not-found":               true,
	"max-mmap-limit":                   true,
	"max-resume-failure-tries":         true,
	"max-tries":                        true,
	"max-upload-limit":                 true,
	"metalink-base-uri":                true,
	"metalink-enable-unique-protocol":  true,
	"metalink-language":                true,
	"metalink-location":                true,
	"metalink-os":                      true,
	"metalink-preferred-protocol":      true,
	"metalink-version":                 true,
	"min-split-size":                   true,
	"no-file-allocation-limit":         true,
	"no-netrc":                         true,
	"no-proxy":                         true,
	"no-want-digest-header":            true,
	"out":                              true,
	"parameterized-uri":                true,
	"pause":                            true,
	"pause-metadata":                   true,
	"piece-length":                     true,
	"proxy-method":                     true,
	"realtime-chunk-checksum":          true,
	"referer":                          true,
	"remote-time":                      true,
	"remove-control-file":              true,
	"retry-wait":                       true,
	"reuse-uri":                        true,
	"rpc-save-upload-metadata":         true,
	"save-not-found":                   true,
	"seed-ratio":                       true,
	"seed-time":                        true,
	"select-file":                      true,
	"split":                            true,
	"ssh-host-key-md":                  true,
	"stream-piece-selector":            true,
	"timeout":                          true,
	"uri-selector":                     true,
	"use-head":                         true,
	"user-agent":                       true,
}
