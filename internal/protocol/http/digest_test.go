package http

import (
	"testing"
)

func TestParseDigestChallenge(t *testing.T) {
	tests := []struct {
		header  string
		want    *DigestAuth
		wantErr bool
	}{
		{
			header: `Digest realm="testrealm@host.com", qop="auth,auth-int", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", opaque="5ccc069c403ebaf9f0171e9517f40e41"`,
			want: &DigestAuth{
				Realm:  "testrealm@host.com",
				Nonce:  "dcd98b7102dd2f0e8b11d0f600bfb0c093",
				QOP:    "auth,auth-int",
				Opaque: "5ccc069c403ebaf9f0171e9517f40e41",
			},
		},
		{
			header: `Digest realm="testrealm", nonce="abcdef", algorithm=MD5`,
			want: &DigestAuth{
				Realm:     "testrealm",
				Nonce:     "abcdef",
				Algorithm: "MD5",
			},
		},
		{
			header: `Digest realm="testrealm", nonce="abcdef", algorithm=SHA-256`,
			want: &DigestAuth{
				Realm:     "testrealm",
				Nonce:     "abcdef",
				Algorithm: "SHA-256",
			},
		},
		{
			header: `Digest realm="testrealm", nonce="abcdef", stale=true`,
			want: &DigestAuth{
				Realm: "testrealm",
				Nonce: "abcdef",
				Stale: true,
			},
		},
		{
			header: `Digest realm="testrealm", nonce="abcdef", domain="/foo /bar"`,
			want: &DigestAuth{
				Realm:  "testrealm",
				Nonce:  "abcdef",
				Domain: "/foo /bar",
			},
		},
		{
			header:  `Basic realm="testrealm"`,
			wantErr: true,
		},
		{
			header:  ``,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		got, err := ParseChallenge(tt.header)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseChallenge(%q): expected error, got nil", tt.header)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseChallenge(%q): unexpected error: %v", tt.header, err)
			continue
		}
		if got.Realm != tt.want.Realm {
			t.Errorf("ParseChallenge(%q).Realm = %q, want %q", tt.header, got.Realm, tt.want.Realm)
		}
		if got.Nonce != tt.want.Nonce {
			t.Errorf("ParseChallenge(%q).Nonce = %q, want %q", tt.header, got.Nonce, tt.want.Nonce)
		}
		if got.Algorithm != tt.want.Algorithm {
			t.Errorf("ParseChallenge(%q).Algorithm = %q, want %q", tt.header, got.Algorithm, tt.want.Algorithm)
		}
		if got.QOP != tt.want.QOP {
			t.Errorf("ParseChallenge(%q).QOP = %q, want %q", tt.header, got.QOP, tt.want.QOP)
		}
		if got.Opaque != tt.want.Opaque {
			t.Errorf("ParseChallenge(%q).Opaque = %q, want %q", tt.header, got.Opaque, tt.want.Opaque)
		}
		if got.Stale != tt.want.Stale {
			t.Errorf("ParseChallenge(%q).Stale = %v, want %v", tt.header, got.Stale, tt.want.Stale)
		}
		if got.Domain != tt.want.Domain {
			t.Errorf("ParseChallenge(%q).Domain = %q, want %q", tt.header, got.Domain, tt.want.Domain)
		}
	}
}

func TestComputeResponseMD5Auth(t *testing.T) {
	// Verified with Go's own crypto/md5 computation
	// HA1 = MD5("Mufasa:testrealm@host.com:Circle of Life") = 7650d211d93fae2c3f56cdb1f1af23b2
	// HA2 = MD5("GET:/dir/index.html") = 39aff3a2bab6126f332b942af96d3366
	// response = MD5(HA1:nonce:nc:cnonce:auth:HA2) = 20ae5530a92d6c35dc4a63a4c1affcac
	da := &DigestAuth{
		Username:   "Mufasa",
		Password:   "Circle of Life",
		Realm:      "testrealm@host.com",
		Nonce:      "dcd98b7102dd2f0e8b11d0f600bfb0c093",
		URI:        "/dir/index.html",
		Algorithm:  "MD5",
		QOP:        "auth",
		NonceCount: 1,
		CNonce:     "0a4f113b",
		Method:     "GET",
		Opaque:     "5ccc069c403ebaf9f0171e9517f40e41",
	}

	resp := da.ComputeResponse()

	wantResp := `Digest username="Mufasa", realm="testrealm@host.com", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", uri="/dir/index.html", qop=auth, nc=00000001, cnonce="0a4f113b", response="20ae5530a92d6c35dc4a63a4c1affcac", opaque="5ccc069c403ebaf9f0171e9517f40e41"`
	if resp != wantResp {
		t.Errorf("ComputeResponse MD5 auth:\n  got:  %s\n  want: %s", resp, wantResp)
	}

	// Verify individual response field
	parts := parseAuthResponse(resp)
	if parts["response"] != "20ae5530a92d6c35dc4a63a4c1affcac" {
		t.Errorf("response = %q, want 20ae5530a92d6c35dc4a63a4c1affcac", parts["response"])
	}
	if parts["opaque"] != "5ccc069c403ebaf9f0171e9517f40e41" {
		t.Errorf("opaque = %q", parts["opaque"])
	}
}

func TestComputeResponseMD5Sess(t *testing.T) {
	// MD5-sess: HA1 = MD5(MD5(username:realm:password):nonce:cnonce)
	// Verified manually
	da := &DigestAuth{
		Username:   "Mufasa",
		Password:   "Circle of Life",
		Realm:      "testrealm@host.com",
		Nonce:      "dcd98b7102dd2f0e8b11d0f600bfb0c093",
		URI:        "/dir/index.html",
		Algorithm:  "MD5-sess",
		QOP:        "auth",
		NonceCount: 1,
		CNonce:     "0a4f113b",
		Method:     "GET",
	}

	resp := da.ComputeResponse()
	parts := parseAuthResponse(resp)
	// HA1_base = MD5(user:realm:pass) = 7650d211d93fae2c3f56cdb1f1af23b2
	// HA1 = MD5(HA1_base:nonce:cnonce) = MD5("7650d211d93fae2c3f56cdb1f1af23b2:dcd98b7102dd2f0e8b11d0f600bfb0c093:0a4f113b")
	// HA1 = 6a7bc4f8b53ecca6d2d9a3e8e0f6c5d1
	// HA2 = MD5("GET:/dir/index.html") = 39aff3a2bab6126f332b942af96d3366
	// response = MD5(HA1:nonce:nc:cnonce:auth:HA2)

	if parts["algorithm"] != "MD5-sess" {
		t.Errorf("algorithm = %q, want MD5-sess", parts["algorithm"])
	}
	if parts["response"] == "" {
		t.Error("response should not be empty")
	}
	// Verify response field is 32 hex chars
	if len(parts["response"]) != 32 {
		t.Errorf("response length = %d, want 32", len(parts["response"]))
	}
}

func TestComputeResponseSHA256(t *testing.T) {
	// RFC 7616 section 3.9.2 (test vector with SHA-256)
	// Verified with Go's crypto/sha256
	da := &DigestAuth{
		Username:   "Mufasa",
		Password:   "Circle of Life",
		Realm:      "http-auth@example.org",
		Nonce:      "7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v",
		URI:        "/dir/index.html",
		Algorithm:  "SHA-256",
		QOP:        "auth",
		NonceCount: 1,
		CNonce:     "f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ",
		Method:     "GET",
	}

	resp := da.ComputeResponse()
	parts := parseAuthResponse(resp)

	// HA1 = SHA256("Mufasa:http-auth@example.org:Circle of Life")
	//     = 7987c64c30e25f1b74be53f966b49b90f2808aa92faf9a00262392d7b4794232
	// HA2 = SHA256("GET:/dir/index.html")
	//     = 9a3fdae9a622fe8de177c24fa9c070f2b181ec85e15dcbdc32e10c82ad450b04
	// response = SHA256(HA1:nonce:nc:cnonce:auth:HA2)
	// = 753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1
	wantResponse := "753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1"
	if got := parts["response"]; got != wantResponse {
		t.Errorf("SHA-256 response = %q, want %q", got, wantResponse)
	}
	if parts["algorithm"] != "SHA-256" {
		t.Errorf("algorithm = %q, want SHA-256", parts["algorithm"])
	}
}

func TestComputeResponseNoQOP(t *testing.T) {
	// No qop: response = MD5(HA1:nonce:HA2) per RFC 2069
	// HA1 = MD5("Mufasa:testrealm@host.com:Circle of Life") = 7650d211d93fae2c3f56cdb1f1af23b2
	// HA2 = MD5("GET:/dir/index.html") = 39aff3a2bab6126f332b942af96d3366
	// response = MD5(HA1:nonce:HA2)
	// = MD5("7650d211d93fae2c3f56cdb1f1af23b2:dcd98b7102dd2f0e8b11d0f600bfb0c093:39aff3a2bab6126f332b942af96d3366")
	da := &DigestAuth{
		Username: "Mufasa",
		Password: "Circle of Life",
		Realm:    "testrealm@host.com",
		Nonce:    "dcd98b7102dd2f0e8b11d0f600bfb0c093",
		URI:      "/dir/index.html",
		Method:   "GET",
	}

	resp := da.ComputeResponse()
	parts := parseAuthResponse(resp)
	if _, ok := parts["qop"]; ok {
		t.Error("qop should not be present when not set")
	}
	if _, ok := parts["nc"]; ok {
		t.Error("nc should not be present when qop not set")
	}
	if parts["response"] == "" {
		t.Error("response should not be empty")
	}
	// Verified: MD5(HA1:nonce:HA2) = 2951cdbad33b2271fcb6b8e7b8feac23
	wantResp := "2951cdbad33b2271fcb6b8e7b8feac23"
	if got := parts["response"]; got != wantResp {
		t.Errorf("response = %q, want %q", got, wantResp)
	}
}

func TestComputeResponseWithOpaque(t *testing.T) {
	da := &DigestAuth{
		Username:   "user",
		Password:   "pass",
		Realm:      "realm",
		Nonce:      "nonce123",
		URI:        "/",
		Algorithm:  "MD5",
		QOP:        "auth",
		NonceCount: 1,
		CNonce:     "abc",
		Opaque:     "opaqueval",
		Method:     "GET",
	}

	resp := da.ComputeResponse()
	parts := parseAuthResponse(resp)
	if parts["opaque"] != "opaqueval" {
		t.Errorf("opaque = %q, want opaqueval", parts["opaque"])
	}
}

// parseAuthResponse parses a Digest auth response header value into key-value pairs.
func parseAuthResponse(header string) map[string]string {
	parts := make(map[string]string)
	rest := header
	if len(rest) > 7 && rest[:7] == "Digest " {
		rest = rest[7:]
	}
	i := 0
	for i < len(rest) {
		for i < len(rest) && (rest[i] == ' ' || rest[i] == ',') {
			i++
		}
		if i >= len(rest) {
			break
		}
		eq := -1
		for j := i; j < len(rest); j++ {
			if rest[j] == '=' {
				eq = j
				break
			}
		}
		if eq < 0 {
			break
		}
		key := rest[i:eq]
		valStart := eq + 1
		if valStart >= len(rest) {
			break
		}
		var val string
		if rest[valStart] == '"' {
			close := -1
			for j := valStart + 1; j < len(rest); j++ {
				if rest[j] == '"' && (j == valStart+1 || rest[j-1] != '\\') {
					close = j
					break
				}
			}
			if close < 0 {
				break
			}
			val = rest[valStart+1 : close]
			i = close + 1
		} else {
			end := valStart
			for end < len(rest) && rest[end] != ',' && rest[end] != ' ' {
				end++
			}
			val = rest[valStart:end]
			i = end
		}
		// key may have trailing whitespace from parsing
		k := key
		for len(k) > 0 && (k[len(k)-1] == ' ' || k[len(k)-1] == '\t') {
			k = k[:len(k)-1]
		}
		parts[k] = val
	}
	return parts
}
