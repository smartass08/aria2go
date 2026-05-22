package config

import "testing"

func TestParseDHTEntryPointValue(t *testing.T) {
	tests := []struct {
		name      string
		option    string
		value     string
		wantHost  string
		wantPort  string
		wantError bool
	}{
		{
			name:     "hostname",
			option:   "dht-entry-point",
			value:    "router.bittorrent.com:6881",
			wantHost: "router.bittorrent.com",
			wantPort: "6881",
		},
		{
			name:     "ipv6_bracketed",
			option:   "dht-entry-point6",
			value:    "[2001:db8::1]:6881",
			wantHost: "2001:db8::1",
			wantPort: "6881",
		},
		{
			name:      "missing_port",
			option:    "dht-entry-point",
			value:     "router.bittorrent.com",
			wantError: true,
		},
		{
			name:      "port_zero",
			option:    "dht-entry-point",
			value:     "router.bittorrent.com:0",
			wantError: true,
		},
		{
			name:      "ipv6_without_brackets",
			option:    "dht-entry-point6",
			value:     "2001:db8::1:6881",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := parseDHTEntryPointValue(tt.option, tt.value)
			if tt.wantError {
				if err == nil {
					t.Fatalf("parseDHTEntryPointValue(%q, %q) returned nil error", tt.option, tt.value)
				}
				cfgErr, ok := err.(*Error)
				if !ok {
					t.Fatalf("error type = %T, want *Error", err)
				}
				if cfgErr.Code != ErrInvalidOption {
					t.Fatalf("error code = %v, want %v", cfgErr.Code, ErrInvalidOption)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDHTEntryPointValue(%q, %q): %v", tt.option, tt.value, err)
			}
			if host != tt.wantHost {
				t.Fatalf("host = %q, want %q", host, tt.wantHost)
			}
			if port != tt.wantPort {
				t.Fatalf("port = %q, want %q", port, tt.wantPort)
			}
		})
	}
}
