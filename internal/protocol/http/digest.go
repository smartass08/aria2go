// Package http provides HTTP Digest authentication (RFC 7616/RFC 2617)
// for computing Authorization header values in response to
// WWW-Authenticate: Digest challenges.
package http

import (
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"strings"
)

// DigestAuth holds the parameters for computing an HTTP Digest
// authentication response per RFC 7616.
type DigestAuth struct {
	Username   string
	Password   string
	Realm      string
	Nonce      string
	URI        string
	Algorithm  string // "MD5", "MD5-sess", "SHA-256", "SHA-256-sess"
	QOP        string // "auth", "auth-int", or comma-separated "auth,auth-int"
	NonceCount int
	CNonce     string
	Opaque     string
	Method     string // "GET", "POST", etc.
	Domain     string
	Stale      bool
}

// ParseChallenge parses a WWW-Authenticate: Digest ... header value
// and returns a DigestAuth with the challenge parameters populated.
// Returns an error if the header is not a valid Digest challenge.
func ParseChallenge(header string) (*DigestAuth, error) {
	const prefix = "Digest "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return nil, fmt.Errorf("http: not a Digest challenge: %q", header)
	}
	rest := header[len(prefix):]

	d := &DigestAuth{}

	i := 0
	for i < len(rest) {
		for i < len(rest) && isLWS(rest[i]) {
			i++
		}
		if i >= len(rest) {
			break
		}

		// Parse key
		keyStart := i
		for i < len(rest) && rest[i] != '=' && rest[i] != ',' && !isLWS(rest[i]) {
			i++
		}
		key := strings.ToLower(strings.TrimSpace(rest[keyStart:i]))

		// Skip whitespace and optional comma
		for i < len(rest) && (isLWS(rest[i]) || rest[i] == ',') {
			i++
		}
		if i >= len(rest) || rest[i] != '=' {
			// No value for this key, skip to next comma
			for i < len(rest) && rest[i] != ',' {
				i++
			}
			if i < len(rest) {
				i++ // skip comma
			}
			continue
		}
		i++ // skip '='

		// Parse value (quoted string or token)
		var value string
		for i < len(rest) && isLWS(rest[i]) {
			i++
		}
		if i < len(rest) && rest[i] == '"' {
			i++ // skip opening quote
			valStart := i
			for i < len(rest) {
				if rest[i] == '"' && (i == valStart || rest[i-1] != '\\') {
					value = rest[valStart:i]
					i++ // skip closing quote
					break
				}
				i++
			}
		} else {
			valStart := i
			for i < len(rest) && rest[i] != ',' && !isLWS(rest[i]) {
				i++
			}
			value = rest[valStart:i]
		}

		// Assign to struct field
		switch key {
		case "realm":
			d.Realm = value
		case "nonce":
			d.Nonce = value
		case "algorithm":
			d.Algorithm = value
		case "qop":
			d.QOP = value
		case "opaque":
			d.Opaque = value
		case "domain":
			d.Domain = value
		case "stale":
			d.Stale = strings.EqualFold(value, "true")
		}

		// Skip to next comma or end
		for i < len(rest) && rest[i] != ',' {
			i++
		}
		if i < len(rest) && rest[i] == ',' {
			i++
		}
	}

	if d.Realm == "" || d.Nonce == "" {
		return nil, fmt.Errorf("http: Digest challenge missing required realm or nonce")
	}

	return d, nil
}

// ComputeResponse computes the full Authorization header value
// for this Digest authentication request.
func (d *DigestAuth) ComputeResponse() string {
	algorithm := d.Algorithm
	if algorithm == "" {
		algorithm = "MD5"
	}

	useSHA := strings.EqualFold(algorithm, "SHA-256") || strings.EqualFold(algorithm, "SHA-256-sess")

	var ha1 string
	if useSHA {
		ha1 = sha256Hex(d.Username + ":" + d.Realm + ":" + d.Password)
		if strings.EqualFold(algorithm, "SHA-256-sess") && d.Nonce != "" && d.CNonce != "" {
			ha1 = sha256Hex(ha1 + ":" + d.Nonce + ":" + d.CNonce)
		}
	} else {
		ha1 = md5Hex(d.Username + ":" + d.Realm + ":" + d.Password)
		if strings.EqualFold(algorithm, "MD5-sess") && d.Nonce != "" && d.CNonce != "" {
			ha1 = md5Hex(ha1 + ":" + d.Nonce + ":" + d.CNonce)
		}
	}

	qop := strings.ToLower(d.QOP)
	hasQOP := qop == "auth" || qop == "auth-int" || strings.Contains(qop, "auth")

	var ha2 string
	if useSHA {
		ha2 = sha256Hex(d.Method + ":" + d.URI)
	} else {
		ha2 = md5Hex(d.Method + ":" + d.URI)
	}

	var response string
	hexNC := fmt.Sprintf("%08x", d.NonceCount)

	if hasQOP {
		if useSHA {
			response = sha256Hex(ha1 + ":" + d.Nonce + ":" + hexNC + ":" + d.CNonce + ":auth:" + ha2)
		} else {
			response = md5Hex(ha1 + ":" + d.Nonce + ":" + hexNC + ":" + d.CNonce + ":auth:" + ha2)
		}
	} else {
		if useSHA {
			response = sha256Hex(ha1 + ":" + d.Nonce + ":" + ha2)
		} else {
			response = md5Hex(ha1 + ":" + d.Nonce + ":" + ha2)
		}
	}

	// Build response header
	var sb strings.Builder
	sb.WriteString("Digest ")
	sb.WriteString(`username="`)
	sb.WriteString(d.Username)
	sb.WriteString(`", realm="`)
	sb.WriteString(d.Realm)
	sb.WriteString(`", nonce="`)
	sb.WriteString(d.Nonce)
	sb.WriteString(`", uri="`)
	sb.WriteString(d.URI)
	sb.WriteString(`"`)

	if hasQOP {
		sb.WriteString(`, qop=auth`)
		sb.WriteString(", nc=")
		sb.WriteString(hexNC)
		sb.WriteString(`, cnonce="`)
		sb.WriteString(d.CNonce)
		sb.WriteString(`"`)
	}

	sb.WriteString(`, response="`)
	sb.WriteString(response)
	sb.WriteString(`"`)

	if !strings.EqualFold(algorithm, "MD5") {
		sb.WriteString(", algorithm=")
		sb.WriteString(algorithm)
	}

	if d.Opaque != "" {
		sb.WriteString(`, opaque="`)
		sb.WriteString(d.Opaque)
		sb.WriteString(`"`)
	}

	return sb.String()
}

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%02x", h[:])
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%02x", h[:])
}

func isLWS(c byte) bool {
	return c == ' ' || c == '\t'
}
