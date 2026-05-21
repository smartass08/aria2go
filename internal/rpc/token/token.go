// Package token provides shared RPC token extraction and validation
// utilities used by both the JSON-RPC encoding layer and the transport
// layer. It mirrors aria2's token handling in
// JsonDiskWriterHandler::ExtractToken and DownloadEngine::validateToken.
//
// Token authentication uses HMAC-SHA1 with a random process-global key
// to prevent timing side-channel attacks, matching aria2's behavior.
package token

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/json"
	"strings"
	"sync"
)

// TokenPrefix is the string prefix used to identify a secret token
// parameter in the first positional parameter of an RPC request.
const TokenPrefix = "token:"

// ExtractToken extracts the RPC secret token from the first positional
// parameter if it is a string prefixed with "token:". If found, the token
// (without the prefix) is returned along with the remaining params (the
// token param is stripped from the front, matching C++ pop_front behavior).
// If no token parameter is present, an empty string and the original params
// are returned.
func ExtractToken(params json.RawMessage) (token string, remaining json.RawMessage, err error) {
	if len(params) == 0 || string(params) == "[]" {
		return "", params, nil
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(params, &arr); err != nil {
		return "", params, nil
	}
	if len(arr) == 0 {
		return "", params, nil
	}

	var first string
	if err := json.Unmarshal(arr[0], &first); err != nil {
		return "", params, nil
	}

	if strings.HasPrefix(first, TokenPrefix) {
		token = first[len(TokenPrefix):]
		remaining = buildRemaining(arr[1:])
		return token, remaining, nil
	}
	return "", params, nil
}

// ExtractTokenSimple extracts the token from a []interface{} slice,
// returning the token string and remaining params.
func ExtractTokenSimple(params []interface{}) (string, []interface{}) {
	if len(params) == 0 {
		return "", params
	}
	s, ok := params[0].(string)
	if !ok {
		return "", params
	}
	if len(s) > len(TokenPrefix) && s[:len(TokenPrefix)] == TokenPrefix {
		return s[len(TokenPrefix):], params[1:]
	}
	return "", params
}

func buildRemaining(elems []json.RawMessage) json.RawMessage {
	if len(elems) == 0 {
		return json.RawMessage("[]")
	}
	var buf strings.Builder
	buf.WriteByte('[')
	for i, elem := range elems {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(elem)
	}
	buf.WriteByte(']')
	return json.RawMessage(buf.String())
}

// HMAC-based token validation state. The random key is generated once
// per process and cached. Both the expected secret and the provided token
// are HMAC'd with the same key; the results are compared with hmac.Equal
// (constant-time). This matches aria2's validateToken behavior.
var (
	validateTokenKey   []byte
	validateTokenMu    sync.Mutex
	validateTokenReady bool
)

func ensureValidateTokenKey() []byte {
	validateTokenMu.Lock()
	defer validateTokenMu.Unlock()
	if !validateTokenReady {
		key := make([]byte, sha1.BlockSize)
		if _, err := rand.Read(key); err != nil {
			return nil
		}
		validateTokenKey = key
		validateTokenReady = true
	}
	return validateTokenKey
}

// ValidateToken compares a provided RPC-secret token against the expected
// secret using HMAC-SHA1 with a random process-global key, matching aria2's
// DownloadEngine::validateToken behavior. Both the expected secret and the
// provided token are HMAC'd before comparison, mitigating direct timing
// attacks. The comparison uses hmac.Equal (constant-time).
//
// Returns true if the token is valid. If expected is empty (no secret
// configured), all requests pass.
func ValidateToken(expected, provided string) bool {
	if expected == "" {
		return true
	}

	key := ensureValidateTokenKey()
	if key == nil {
		return false
	}

	mac := hmac.New(sha1.New, key)
	mac.Write([]byte(expected))
	expectedMac := mac.Sum(nil)

	mac.Reset()
	mac.Write([]byte(provided))
	providedMac := mac.Sum(nil)

	return hmac.Equal(expectedMac, providedMac)
}

// EnsureHMACKey generates the HMAC key used for HTTP Basic auth comparison.
// Returns a 64-byte random key matching the one used in the transport layer.
func EnsureHMACKey() []byte {
	key := make([]byte, sha1.BlockSize)
	if _, err := rand.Read(key); err != nil {
		return nil
	}
	return key
}
