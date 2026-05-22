package config

type RPCOptionError struct {
	Name string
	Err  error
}

func (e *RPCOptionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Name
	}
	return e.Name + ": " + e.Err.Error()
}

var rpcOptionParsers = map[string]ConfigValueParser{
	"auto-save-interval":          NewNumberOptionHandler("auto-save-interval", 60, 0, 600),
	"bt-max-open-files":           NewNumberOptionHandler("bt-max-open-files", 100, 1, -1),
	"bt-max-peers":                NewNumberOptionHandler("bt-max-peers", 55, 0, -1),
	"bt-min-crypto-level":         NewParameterOptionHandler("bt-min-crypto-level", "plain", []string{"plain", "arc4"}),
	"bt-request-peer-speed-limit": NewUnitNumberOptionHandler("bt-request-peer-speed-limit", "50K", "0", ""),
	"connect-timeout":             NewNumberOptionHandler("connect-timeout", 60, 1, 600),
	"download-result":             NewParameterOptionHandler("download-result", "default", []string{"default", "full", "hide"}),
	"follow-metalink":             NewParameterOptionHandler("follow-metalink", "true", []string{"true", "mem", "false"}),
	"follow-torrent":              NewParameterOptionHandler("follow-torrent", "true", []string{"true", "mem", "false"}),
	"ftp-type":                    NewParameterOptionHandler("ftp-type", "binary", []string{"binary", "ascii"}),
	"lowest-speed-limit":          NewUnitNumberOptionHandler("lowest-speed-limit", "0", "0", ""),
	"max-concurrent-downloads":    NewNumberOptionHandler("max-concurrent-downloads", 5, 1, -1),
	"max-connection-per-server":   NewNumberOptionHandler("max-connection-per-server", 1, 1, 16),
	"max-download-limit":          NewUnitNumberOptionHandler("max-download-limit", "0", "0", ""),
	"max-download-result":         NewNumberOptionHandler("max-download-result", 1000, 0, -1),
	"max-file-not-found":          NewNumberOptionHandler("max-file-not-found", 0, 0, -1),
	"max-mmap-limit":              NewUnitNumberOptionHandler("max-mmap-limit", "0", "0", ""),
	"max-overall-download-limit":  NewUnitNumberOptionHandler("max-overall-download-limit", "0", "0", ""),
	"max-overall-upload-limit":    NewUnitNumberOptionHandler("max-overall-upload-limit", "0", "0", ""),
	"max-resume-failure-tries":    NewNumberOptionHandler("max-resume-failure-tries", 0, 0, -1),
	"max-tries":                   NewNumberOptionHandler("max-tries", 5, 0, -1),
	"max-upload-limit":            NewUnitNumberOptionHandler("max-upload-limit", "0", "0", ""),
	"metalink-preferred-protocol": NewParameterOptionHandler("metalink-preferred-protocol", "none", []string{"http", "https", "ftp", "none"}),
	"min-split-size":              NewUnitNumberOptionHandler("min-split-size", "20M", "1M", "1024M"),
	"no-file-allocation-limit":    NewUnitNumberOptionHandler("no-file-allocation-limit", "5M", "0", ""),
	"piece-length":                NewUnitNumberOptionHandler("piece-length", "1M", "1M", "1024M"),
	"proxy-method":                NewParameterOptionHandler("proxy-method", "get", []string{"get", "tunnel"}),
	"retry-wait":                  NewNumberOptionHandler("retry-wait", 0, 0, 600),
	"rpc-listen-port":             NewNumberOptionHandler("rpc-listen-port", 6800, 1024, 65535),
	"rpc-max-request-size":        NewUnitNumberOptionHandler("rpc-max-request-size", "2M", "0", ""),
	"save-session-interval":       NewNumberOptionHandler("save-session-interval", 0, 0, -1),
	"seed-ratio":                  NewFloatNumberOptionHandler("seed-ratio", 1.0, 0.0, -1.0),
	"seed-time":                   NewFloatNumberOptionHandler("seed-time", 0.0, 0.0, -1.0),
	"server-stat-timeout":         NewNumberOptionHandler("server-stat-timeout", 86400, 0, -1),
	"socket-recv-buffer-size":     NewUnitNumberOptionHandler("socket-recv-buffer-size", "0", "0", ""),
	"split":                       NewNumberOptionHandler("split", 5, 1, -1),
	"stream-piece-selector":       NewParameterOptionHandler("stream-piece-selector", "default", []string{"default", "inorder", "random", "geom"}),
	"timeout":                     NewNumberOptionHandler("timeout", 60, 1, 600),
	"uri-selector":                NewParameterOptionHandler("uri-selector", "feedback", []string{"inorder", "feedback", "adaptive"}),
}

func ParseRPCOptions(raw map[string]interface{}, allow func(string) bool) (*Options, error) {
	if raw == nil {
		return nil, nil
	}

	opts := &Options{}
	for _, name := range fieldsSorted {
		value, ok := raw[name]
		if !ok {
			continue
		}
		if allow != nil && !allow(name) {
			continue
		}
		setter, ok := fieldSetters[name]
		if !ok {
			continue
		}

		values, usable := rpcOptionValues(name, value)
		if !usable || len(values) == 0 {
			continue
		}

		tmp := &Options{}
		for _, s := range values {
			if err := validateRPCOptionValue(name, s); err != nil {
				return nil, &RPCOptionError{Name: name, Err: err}
			}
			if err := setter(tmp, s); err != nil {
				return nil, &RPCOptionError{Name: name, Err: err}
			}
		}
		tmp.MarkExplicit(name)
		mergeInto(opts, tmp)
	}
	return opts, nil
}

func rpcOptionValues(name string, raw interface{}) ([]string, bool) {
	switch v := raw.(type) {
	case string:
		return []string{v}, true
	case []interface{}:
		if !accumulativeFieldsMap[name] {
			return nil, false
		}
		out := make([]string, 0, len(v))
		for _, elem := range v {
			s, ok := elem.(string)
			if ok {
				out = append(out, s)
			}
		}
		return out, true
	case []string:
		if !accumulativeFieldsMap[name] {
			return nil, false
		}
		return append([]string(nil), v...), true
	default:
		return nil, false
	}
}

func validateRPCOptionValue(name, value string) error {
	if parser, ok := rpcOptionParsers[name]; ok {
		_, err := parser.Parse(value)
		return err
	}

	switch name {
	case "console-log-level", "log-level":
		if value == "" || validateEnum(value, validLogLevels) {
			return nil
		}
		return &Error{Code: ErrInvalidOption, Msg: "invalid log level"}
	case "file-allocation":
		if value == "" || validateEnum(value, validFileAllocations) {
			return nil
		}
		return &Error{Code: ErrInvalidOption, Msg: "invalid file allocation"}
	case "optimize-concurrent-downloads":
		if value == "" || value == "true" || value == "false" || isOptimizeABFormat(value) {
			return nil
		}
		return &Error{Code: ErrInvalidOption, Msg: "invalid optimize-concurrent-downloads"}
	default:
		return nil
	}
}
