// Package jsonrpc provides JSON-RPC 2.0 request/response encoding and
// decoding for the aria2go RPC layer.
//
// It follows aria2's JSON-RPC semantics:
//   - Params are positional arrays, not named objects.
//   - The jsonrpc key is never validated (aria2 ignores it).
//   - Notifications are not supported; missing id returns -32600.
//   - Batch requests (top-level JSON array) produce batch responses.
//   - Non-object batch elements are silently skipped.
//   - Auth tokens arrive as the first positional param prefixed with "token:".
package jsonrpc

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// JSON-RPC 2.0 standard error codes.
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603

	// ErrCodeApplication is the code used for aria2 application-level errors.
	ErrCodeApplication = 1
)

// TokenPrefix is the string prefix used to identify a secret token parameter.
const TokenPrefix = "token:"

// Standard JSON-RPC error messages.
const (
	MsgParseError     = "Parse error."
	MsgInvalidRequest = "Invalid Request."
	MsgMethodNotFound = "Method not found."
	MsgInvalidParams  = "Invalid params."
	MsgInternalError  = "Internal error."
)

var (
	ErrMissingID     = errors.New("jsonrpc: request is missing id")
	ErrMissingMethod = errors.New("jsonrpc: request is missing method")
	ErrEmptyBatch    = errors.New("jsonrpc: empty batch array")
)

// Pre-computed JSON error responses for hot paths.
var (
	jsonParseError     []byte
	jsonInvalidRequest []byte
	jsonMethodNotFound []byte
	jsonInvalidParams  []byte
	jsonInternalError  []byte
	precomputedOnce    sync.Once
)

func ensurePrecomputed() {
	precomputedOnce.Do(func() {
		jsonParseError = mustMarshalError(nil, ErrCodeParse, MsgParseError)
		jsonInvalidRequest = mustMarshalError(nil, ErrCodeInvalidRequest, MsgInvalidRequest)
		jsonMethodNotFound = mustMarshalError(nil, ErrCodeMethodNotFound, MsgMethodNotFound)
		jsonInvalidParams = mustMarshalError(nil, ErrCodeInvalidParams, MsgInvalidParams)
		jsonInternalError = mustMarshalError(nil, ErrCodeInternal, MsgInternalError)
	})
}

func mustMarshalError(id interface{}, code int, msg string) []byte {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: msg},
	}
	b, _ := json.Marshal(resp)
	return applyAria2Escaping(b)
}

// bufPool is used by Encode / EncodeBatch to avoid allocating fresh
// encoding buffers on every response.
var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// escBufPool is used by applyAria2Escaping for the output buffer.
var escBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 4096)
		return &b
	},
}

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

// Request is a JSON-RPC 2.0 request object.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Response is a JSON-RPC 2.0 response object.
// Field order matches aria2 C++ output: id first, then jsonrpc, then result/error.
type Response struct {
	ID      interface{} `json:"id"`
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error object.
// It implements the builtin error interface so it can be used as a
// return value from functions like Decode.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error returns a string representation for the error interface.
func (e *Error) Error() string {
	return "jsonrpc: " + e.Message
}

// IsNotification returns true if the request has no id field, which makes it
// a JSON-RPC 2.0 notification. aria2 does not support notifications;
// requests without an id should be rejected with -32600 Invalid Request.
func (r *Request) IsNotification() bool {
	return len(r.ID) == 0
}

// ---------------------------------------------------------------------------
// Decode
// ---------------------------------------------------------------------------

// Decode parses a JSON body containing one or more JSON-RPC 2.0 requests.
//
//   - A JSON object is decoded as a single *Request; batch is nil.
//   - A JSON array is decoded as a batch []Request; single is nil.
//   - Non-JSON or malformed payloads return an error.
func Decode(data []byte) (single *Request, batch []Request, err error) {
	data = trimSpace(data)
	if len(data) == 0 {
		err = &Error{Code: ErrCodeParse, Message: MsgParseError}
		return
	}

	switch data[0] {
	case '{':
		single, err = decodeSingle(data)
		return
	case '[':
		batch, err = decodeBatch(data)
		return
	default:
		err = &Error{Code: ErrCodeParse, Message: MsgParseError}
		return
	}
}

func trimSpace(data []byte) []byte {
	start := 0
	for start < len(data) && (data[start] == ' ' || data[start] == '\t' ||
		data[start] == '\n' || data[start] == '\r') {
		start++
	}
	end := len(data) - 1
	for end >= start && (data[end] == ' ' || data[end] == '\t' ||
		data[end] == '\n' || data[end] == '\r') {
		end--
	}
	return data[start : end+1]
}

func decodeSingle(data []byte) (*Request, error) {
	var reqMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &reqMap); err != nil {
		return nil, &Error{Code: ErrCodeParse, Message: MsgParseError}
	}

	// jsonrpc field is never validated by aria2 — it is optional.
	_ = reqMap["jsonrpc"]

	// id is optional here; the transport layer rejects notifications.
	id, _ := reqMap["id"]

	// method is required.
	methodRaw, ok := reqMap["method"]
	if !ok {
		return nil, &Error{Code: ErrCodeInvalidRequest, Message: MsgInvalidRequest}
	}
	var method string
	if err := json.Unmarshal(methodRaw, &method); err != nil {
		return nil, &Error{Code: ErrCodeInvalidRequest, Message: MsgInvalidRequest}
	}

	// params is optional; defaults to empty array.
	params, _ := reqMap["params"]
	if params != nil {
		if len(params) > 0 && params[0] != '[' {
			return nil, &Error{Code: ErrCodeInvalidParams, Message: MsgInvalidParams}
		}
	} else {
		params = json.RawMessage("[]")
	}

	return &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}, nil
}

func decodeBatch(data []byte) ([]Request, error) {
	var rawSlice []json.RawMessage
	if err := json.Unmarshal(data, &rawSlice); err != nil {
		return nil, &Error{Code: ErrCodeParse, Message: MsgParseError}
	}

	batch := make([]Request, 0, len(rawSlice))
	for _, raw := range rawSlice {
		// Silently skip non-object batch elements (matches C++ behavior).
		trimmed := trimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			continue
		}
		req, err := decodeSingle(raw)
		if err != nil {
			return nil, err
		}
		batch = append(batch, *req)
	}
	// After filtering, if nothing remains, return empty slice (encodes to []).
	return batch, nil
}

// ---------------------------------------------------------------------------
// Encode
// ---------------------------------------------------------------------------

// Encode marshals a single JSON-RPC 2.0 Response to JSON bytes with
// aria2-compatible escaping (/ becomes \/, control chars use uppercase hex).
func Encode(resp Response) ([]byte, error) {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	err := json.NewEncoder(buf).Encode(resp)
	// json.Encoder appends a trailing newline; strip it for wire compatibility.
	if err == nil && buf.Len() > 0 && buf.Bytes()[buf.Len()-1] == '\n' {
		buf.Truncate(buf.Len() - 1)
	}
	if err != nil {
		bufPool.Put(buf)
		return nil, err
	}
	escaped := applyAria2Escaping(buf.Bytes())
	bufPool.Put(buf)
	return escaped, nil
}

// EncodeBatch marshals multiple JSON-RPC 2.0 responses as a JSON array.
func EncodeBatch(resps []Response) ([]byte, error) {
	if resps == nil {
		return []byte("[]"), nil
	}
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	err := json.NewEncoder(buf).Encode(resps)
	if err == nil && buf.Len() > 0 && buf.Bytes()[buf.Len()-1] == '\n' {
		buf.Truncate(buf.Len() - 1)
	}
	if err != nil {
		bufPool.Put(buf)
		return nil, err
	}
	escaped := applyAria2Escaping(buf.Bytes())
	bufPool.Put(buf)
	return escaped, nil
}

// applyAria2Escaping post-processes JSON to match aria2's C++ encoder:
//   - Escapes "/" as "\/" inside strings.
//   - Uppercases hex digits in \uXXXX escape sequences.
//
// The result is a newly allocated slice; the input b is not modified.
func applyAria2Escaping(b []byte) []byte {
	if len(b) == 0 {
		return b
	}

	// Use a pooled buffer. Grow heuristic: most strings need no escaping,
	// so start close to the input size.
	bufp := escBufPool.Get().(*[]byte)
	buf := *bufp
	buf = buf[:0]

	inString := false
	escaped := false

	for i := 0; i < len(b); i++ {
		c := b[i]

		if !inString {
			buf = append(buf, c)
			if c == '"' {
				inString = true
			}
			continue
		}
		// Inside a JSON string.
		if escaped {
			escaped = false
			if c == 'u' && i+4 < len(b) {
				// Uppercase the 4 hex digits after \u.
				buf = append(buf, 'u')
				for j := 1; j <= 4; j++ {
					d := b[i+j]
					if d >= 'a' && d <= 'f' {
						d -= 32 // to uppercase
					}
					buf = append(buf, d)
				}
				i += 4
				continue
			}
			buf = append(buf, c)
			continue
		}
		if c == '\\' {
			escaped = true
			buf = append(buf, c)
			continue
		}
		if c == '"' {
			inString = false
			buf = append(buf, c)
			continue
		}
		// Escape "/" as "\/" inside strings (aria2 compatibility).
		if c == '/' {
			buf = append(buf, '\\', '/')
			continue
		}
		buf = append(buf, c)
	}

	// Copy result to a new slice before returning buffer to pool.
	result := make([]byte, len(buf))
	copy(result, buf)
	*bufp = buf[:0]
	escBufPool.Put(bufp)
	return result
}

// ---------------------------------------------------------------------------
// Response constructors
// ---------------------------------------------------------------------------

// NewResponse creates a success Response for the given request.
func NewResponse(req *Request, result interface{}) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      rawToInterface(req.ID),
		Result:  result,
	}
}

// NewErrorResponse creates an error Response for the given request.
func NewErrorResponse(req *Request, code int, message string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      rawToInterface(req.ID),
		Error:   &Error{Code: code, Message: message},
	}
}

// NewParseError creates a parse error Response with a null id.
func NewParseError() Response {
	return Response{
		JSONRPC: "2.0",
		ID:      nil,
		Error:   &Error{Code: ErrCodeParse, Message: MsgParseError},
	}
}

// PrecomputedParseError returns the pre-marshaled JSON bytes
// for a standard JSON-RPC parse error response with null id.
// This avoids encoding work on every malformed request.
func PrecomputedParseError() []byte {
	ensurePrecomputed()
	return jsonParseError
}

// NewInvalidRequestError creates an invalid request error Response.
func NewInvalidRequestError(id interface{}) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: ErrCodeInvalidRequest, Message: MsgInvalidRequest},
	}
}

// rawToInterface converts a json.RawMessage to an interface{} suitable
// for JSON serialization. This preserves the original JSON type of the id
// (string, number, or null).
func rawToInterface(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

// ---------------------------------------------------------------------------
// Token extraction and validation
// ---------------------------------------------------------------------------

// ExtractToken extracts the RPC secret token from the first positional
// parameter if it is a string prefixed with "token:". If found, the token
// (without the prefix) is returned along with the remaining params (the
// token param is stripped from the front, matching C++ pop_front behavior).
// If no token parameter is present, an empty string and the original params
// are returned. Only string params are inspected; non-string values or
// arrays without a string first element are ignored.
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
		// Build remaining params without the token element.
		remainingBytes, _ := buildJSONArray(arr[1:])
		return token, remainingBytes, nil
	}
	return "", params, nil
}

// buildJSONArray creates a JSON array from a slice of RawMessage elements.
// Returns "[]" if empty.
func buildJSONArray(elems []json.RawMessage) (json.RawMessage, error) {
	if len(elems) == 0 {
		return json.RawMessage("[]"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, elem := range elems {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(elem)
	}
	buf.WriteByte(']')
	return json.RawMessage(buf.Bytes()), nil
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
			// Fallback: use time-based key if crypto/rand fails
			now := time.Now().UnixNano()
			binary.LittleEndian.PutUint64(key[:8], uint64(now))
			binary.LittleEndian.PutUint64(key[8:16], uint64(now>>8))
			for i := 16; i < len(key); i++ {
				key[i] = byte(now >> (i % 64))
			}
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
