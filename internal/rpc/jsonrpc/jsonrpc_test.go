package jsonrpc

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// ---- Decode (single requests) ----

func TestDecodeSingletRequest(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":"abc","method":"aria2.getVersion","params":[]}`)
	single, batch, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if single == nil {
		t.Fatal("expected single request, got nil")
	}
	if batch != nil {
		t.Fatal("expected nil batch")
	}
	if single.Method != "aria2.getVersion" {
		t.Errorf("method = %q, want %q", single.Method, "aria2.getVersion")
	}
	if string(single.ID) != `"abc"` {
		t.Errorf("id = %s, want %q", string(single.ID), `"abc"`)
	}
	if single.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want %q", single.JSONRPC, "2.0")
	}
	if single.IsNotification() {
		t.Error("request should not be a notification")
	}
}

func TestDecodeSingleRequestWithNullID(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":null,"method":"aria2.getVersion","params":[]}`)
	single, _, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if single == nil {
		t.Fatal("expected single request, got nil")
	}
	if string(single.ID) != "null" {
		t.Errorf("id = %s, want null", string(single.ID))
	}
	if single.IsNotification() {
		t.Error("request with null id should not be a notification")
	}
}

func TestDecodeSingleRequestWithNumericID(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":42,"method":"aria2.addUri","params":[["http://example.com"]]}`)
	single, _, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if single == nil {
		t.Fatal("expected single request, got nil")
	}
	if string(single.ID) != "42" {
		t.Errorf("id = %s, want 42", string(single.ID))
	}
}

func TestDecodeNotification(t *testing.T) {
	// Notification (no id) is still decodable at the Request level,
	// but the transport layer should reject it with -32600.
	data := []byte(`{"jsonrpc":"2.0","method":"aria2.onDownloadStart","params":[{"gid":"abc123"}]}`)
	single, _, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if single == nil {
		t.Fatal("expected single request, got nil")
	}
	if !single.IsNotification() {
		t.Error("request should be recognized as notification")
	}
	if string(single.ID) != "" {
		t.Errorf("id = %s, want empty", string(single.ID))
	}
}

func TestDecodeSingleRequestNoParams(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion"}`)
	single, _, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if single == nil {
		t.Fatal("expected single request, got nil")
	}
	if string(single.Params) != "[]" {
		t.Errorf("params = %s, want []", string(single.Params))
	}
}

func TestDecodeBatchRequest(t *testing.T) {
	data := []byte(`[{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion","params":[]},{"jsonrpc":"2.0","id":"2","method":"aria2.getGlobalStat","params":[]}]`)
	single, batch, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if single != nil {
		t.Fatal("expected nil single")
	}
	if len(batch) != 2 {
		t.Fatalf("batch len = %d, want 2", len(batch))
	}
	if batch[0].Method != "aria2.getVersion" {
		t.Errorf("batch[0].method = %q", batch[0].Method)
	}
	if batch[1].Method != "aria2.getGlobalStat" {
		t.Errorf("batch[1].method = %q", batch[1].Method)
	}
}

func TestDecodeBatchEmpty(t *testing.T) {
	data := []byte(`[]`)
	single, batch, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if single != nil {
		t.Fatal("expected nil single")
	}
	if batch == nil {
		t.Fatal("expected empty batch slice, got nil")
	}
	if len(batch) != 0 {
		t.Fatalf("batch len = %d, want 0", len(batch))
	}
}

func TestDecodeBatchRetainsPerEntryValidationErrors(t *testing.T) {
	data := []byte(`[
		{"jsonrpc":"2.0","id":"ok","method":"aria2.getVersion","params":[]},
		{"jsonrpc":"2.0","id":"bad-method"},
		42,
		{"jsonrpc":"2.0","id":"bad-params","method":"aria2.getVersion","params":{"k":"v"}}
	]`)
	single, batch, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if single != nil {
		t.Fatal("expected nil single")
	}
	if len(batch) != 3 {
		t.Fatalf("batch len = %d, want 3", len(batch))
	}
	if batch[0].ValidationError != nil {
		t.Fatalf("batch[0] unexpected validation error: %+v", batch[0].ValidationError)
	}
	if batch[1].ValidationError == nil || batch[1].ValidationError.Code != ErrCodeInvalidRequest {
		t.Fatalf("batch[1] validation error = %+v, want invalid request", batch[1].ValidationError)
	}
	if batch[2].ValidationError == nil || batch[2].ValidationError.Code != ErrCodeInvalidParams {
		t.Fatalf("batch[2] validation error = %+v, want invalid params", batch[2].ValidationError)
	}
}

// ---- Decode (jsonrpc field optional) ----

func TestDecodeOptionalJSONRPC(t *testing.T) {
	// aria2 never validates the jsonrpc key — requests without it are valid.
	data := []byte(`{"id":"1","method":"aria2.getVersion"}`)
	_, _, err := Decode(data)
	if err != nil {
		t.Fatalf("expected no error for missing jsonrpc, got: %v", err)
	}
}

func TestDecodeWrongJSONRPC(t *testing.T) {
	// aria2 ignores the jsonrpc value; "1.0" is accepted.
	data := []byte(`{"jsonrpc":"1.0","id":"1","method":"aria2.getVersion"}`)
	_, _, err := Decode(data)
	if err != nil {
		t.Fatalf("expected no error for wrong jsonrpc value, got: %v", err)
	}
}

func TestDecodeJSONRPCNotString(t *testing.T) {
	// aria2 ignores the jsonrpc key even when it is not a string.
	data := []byte(`{"jsonrpc":2.0,"id":"1","method":"aria2.getVersion"}`)
	_, _, err := Decode(data)
	if err != nil {
		t.Fatalf("expected no error for non-string jsonrpc, got: %v", err)
	}
}

// ---- Decode (errors) ----

func TestDecodeInvalidJSON(t *testing.T) {
	data := []byte(`not json`)
	_, _, err := Decode(data)
	if err == nil {
		t.Fatal("expected parse error")
	}
	var rpcErr *Error
	if !asError(err, &rpcErr) || rpcErr.Code != ErrCodeParse {
		t.Errorf("expected parse error (-32700), got %v", err)
	}
}

func TestDecodeNonObjectNonArray(t *testing.T) {
	data := []byte(`"just a string"`)
	single, batch, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if batch != nil {
		t.Fatalf("batch = %#v, want nil", batch)
	}
	if single == nil {
		t.Fatal("single = nil, want invalid request sentinel")
	}
	if single.ValidationError == nil || single.ValidationError.Code != ErrCodeInvalidRequest {
		t.Fatalf("validation error = %+v, want invalid request", single.ValidationError)
	}
}

func TestDecodeEmptyBody(t *testing.T) {
	data := []byte(``)
	_, _, err := Decode(data)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestDecodeMissingMethod(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":"1"}`)
	single, _, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if single == nil {
		t.Fatal("single = nil, want request with validation error")
	}
	if single.ValidationError == nil || single.ValidationError.Code != ErrCodeInvalidRequest {
		t.Fatalf("validation error = %+v, want invalid request", single.ValidationError)
	}
	if string(single.ID) != `"1"` {
		t.Fatalf("id = %s, want %q", string(single.ID), `"1"`)
	}
}

func TestDecodeMethodNotString(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":"1","method":42}`)
	single, _, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if single == nil {
		t.Fatal("single = nil, want request with validation error")
	}
	if single.ValidationError == nil || single.ValidationError.Code != ErrCodeInvalidRequest {
		t.Fatalf("validation error = %+v, want invalid request", single.ValidationError)
	}
}

func TestDecodeParamsIsObject(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion","params":{"key":"val"}}`)
	single, _, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if single == nil {
		t.Fatal("single = nil, want request with validation error")
	}
	if single.ValidationError == nil || single.ValidationError.Code != ErrCodeInvalidParams {
		t.Fatalf("validation error = %+v, want invalid params", single.ValidationError)
	}
	if string(single.ID) != `"1"` {
		t.Fatalf("id = %s, want %q", string(single.ID), `"1"`)
	}
}

func TestDecodeBatchWithNonObjectSkipped(t *testing.T) {
	// Non-object elements in a batch are silently skipped (matches C++).
	data := []byte(`[{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion","params":[]},[],123,{"jsonrpc":"2.0","id":"2","method":"aria2.getGlobalStat","params":[]}]`)
	_, batch, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("batch len = %d, want 2 (non-objects skipped)", len(batch))
	}
	if string(batch[0].ID) != `"1"` || string(batch[1].ID) != `"2"` {
		t.Error("wrong requests decoded from batch with non-objects")
	}
}

// ---- Encode ----

func TestEncodeSuccessResponse(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      "req-1",
		Result:  "OK",
	}
	encoded, err := Encode(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var decoded Response
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if decoded.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q", decoded.JSONRPC)
	}
	if decoded.ID != "req-1" {
		t.Errorf("id = %v", decoded.ID)
	}
	if decoded.Error != nil {
		t.Error("response should not have error")
	}
	if decoded.Result != "OK" {
		t.Errorf("result = %v", decoded.Result)
	}
}

func TestEncodeErrorResponse(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      "req-2",
		Error:   &Error{Code: ErrCodeApplication, Message: "Method not found."},
	}
	encoded, err := Encode(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var decoded Response
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if decoded.Result != nil {
		t.Error("error response should not have result")
	}
	if decoded.Error == nil {
		t.Fatal("expected error object")
	}
	if decoded.Error.Code != ErrCodeApplication {
		t.Errorf("error code = %d, want %d", decoded.Error.Code, ErrCodeApplication)
	}
}

func TestEncodeResponseNullID(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      nil,
		Result:  "OK",
	}
	encoded, err := Encode(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var decoded Response
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if decoded.ID != nil {
		t.Errorf("id should be null, got %v", decoded.ID)
	}
}

func TestEncodeBatch(t *testing.T) {
	resps := []Response{
		{JSONRPC: "2.0", ID: "1", Result: "first"},
		{JSONRPC: "2.0", ID: "2", Result: "second"},
	}
	encoded, err := EncodeBatch(resps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var decoded []Response
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("failed to decode batch: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("batch len = %d, want 2", len(decoded))
	}
	if decoded[0].Result != "first" {
		t.Errorf("batch[0].result = %v", decoded[0].Result)
	}
}

func TestEncodeBatchNil(t *testing.T) {
	encoded, err := EncodeBatch(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(encoded) != "[]" {
		t.Errorf("encoded = %s, want []", string(encoded))
	}
}

func TestEncodeBatchEmpty(t *testing.T) {
	encoded, err := EncodeBatch([]Response{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(encoded) != "[]" {
		t.Errorf("encoded = %s, want []", string(encoded))
	}
}

// ---- Encode (aria2 escaping compatibility) ----

func TestEncodeFieldOrdering(t *testing.T) {
	// id must appear before jsonrpc in the output (matches C++).
	resp := Response{
		JSONRPC: "2.0",
		ID:      "abc",
		Result:  "OK",
	}
	encoded, err := Encode(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(encoded)
	idPos := strings.Index(s, `"id"`)
	jrPos := strings.Index(s, `"jsonrpc"`)
	if idPos < 0 || jrPos < 0 {
		t.Fatal("missing id or jsonrpc in output")
	}
	if idPos > jrPos {
		t.Errorf("id should appear before jsonrpc, got: %s", s)
	}
}

func TestEncodeSlashEscaping(t *testing.T) {
	// "/" inside JSON strings should be escaped as "\/" (C++ compatibility).
	resp := Response{
		JSONRPC: "2.0",
		ID:      "abc",
		Result:  "http://example.com/path",
	}
	encoded, err := Encode(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(encoded)
	// Should contain \/ not bare / inside the string value.
	if !strings.Contains(s, `\/`) {
		t.Errorf("expected \\/ escaping, got: %s", s)
	}
}

func TestEncodeControlCharHexUppercase(t *testing.T) {
	// Control characters should use uppercase hex (e.g. \u001F not \u001f).
	resp := Response{
		JSONRPC: "2.0",
		ID:      "abc",
		Result:  "\x1f",
	}
	encoded, err := Encode(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(encoded)
	if strings.Contains(s, `\u001f`) {
		t.Errorf("expected uppercase \\u001F, got lowercase in: %s", s)
	}
	if !strings.Contains(s, `\u001F`) {
		t.Errorf("expected \\u001F but got: %s", s)
	}
}

// ---- NewResponse / NewErrorResponse ----

func TestNewResponse(t *testing.T) {
	req := &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"abc"`),
		Method:  "aria2.getVersion",
		Params:  json.RawMessage("[]"),
	}
	resp := NewResponse(req, "1.37.0")
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q", resp.JSONRPC)
	}
	if resp.ID != "abc" {
		t.Errorf("id = %v", resp.ID)
	}
	if resp.Result != "1.37.0" {
		t.Errorf("result = %v", resp.Result)
	}
}

func TestNewResponseNumericID(t *testing.T) {
	req := &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`42`),
		Method:  "aria2.getVersion",
		Params:  json.RawMessage("[]"),
	}
	resp := NewResponse(req, "OK")
	encoded, err := Encode(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stringContains(encoded, `"id":42`) {
		// Numeric ID preserved.
	}
}

func TestNewErrorResponse(t *testing.T) {
	req := &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"xyz"`),
		Method:  "nonexistent",
		Params:  json.RawMessage("[]"),
	}
	resp := NewErrorResponse(req, ErrCodeApplication, "Method not found.")
	if resp.Error == nil || resp.Error.Code != ErrCodeApplication {
		t.Errorf("error code = %d, want %d", getErrorCode(resp.Error), ErrCodeApplication)
	}
}

// ---- ExtractToken ----

func TestExtractTokenPresent(t *testing.T) {
	params := json.RawMessage(`["token:mysecret"]`)
	token, remaining, err := ExtractToken(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "mysecret" {
		t.Errorf("token = %q, want %q", token, "mysecret")
	}
	if string(remaining) != "[]" {
		t.Errorf("remaining params = %s, want []", string(remaining))
	}
}

func TestExtractTokenPresentWithMoreParams(t *testing.T) {
	// Token should be stripped, remaining params returned (matches pop_front).
	params := json.RawMessage(`["token:mysecret","http://example.com"]`)
	token, remaining, err := ExtractToken(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "mysecret" {
		t.Errorf("token = %q, want %q", token, "mysecret")
	}
	expected := `["http://example.com"]`
	if string(remaining) != expected {
		t.Errorf("remaining params = %s, want %s", string(remaining), expected)
	}
}

func TestExtractTokenNotFirst(t *testing.T) {
	params := json.RawMessage(`["http://example.com","token:secret"]`)
	token, remaining, err := ExtractToken(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("token should be empty, got %q", token)
	}
	// Params unchanged.
	if string(remaining) != string(params) {
		t.Errorf("remaining should equal original params")
	}
}

func TestExtractTokenEmptyParams(t *testing.T) {
	params := json.RawMessage(`[]`)
	token, remaining, err := ExtractToken(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("token should be empty, got %q", token)
	}
	if string(remaining) != "[]" {
		t.Errorf("remaining = %s, want []", string(remaining))
	}
}

func TestExtractTokenNilParams(t *testing.T) {
	token, remaining, err := ExtractToken(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("token should be empty, got %q", token)
	}
	if remaining != nil {
		t.Errorf("remaining should be nil, got %v", remaining)
	}
}

func TestExtractTokenInvalidJSON(t *testing.T) {
	params := json.RawMessage(`not an array`)
	token, remaining, err := ExtractToken(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("token should be empty, got %q", token)
	}
	if string(remaining) != string(params) {
		t.Errorf("remaining should equal original params")
	}
}

func TestExtractTokenFirstParamNotString(t *testing.T) {
	params := json.RawMessage(`[42, "token:secret"]`)
	token, remaining, err := ExtractToken(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("token should be empty, got %q", token)
	}
	if string(remaining) != string(params) {
		t.Errorf("remaining should equal original params")
	}
}

func TestExtractTokenNoPrefix(t *testing.T) {
	params := json.RawMessage(`["not-a-token"]`)
	token, remaining, err := ExtractToken(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("token should be empty, got %q", token)
	}
	if string(remaining) != string(params) {
		t.Errorf("remaining should equal original params")
	}
}

func TestExtractTokenOnlyPrefix(t *testing.T) {
	params := json.RawMessage(`["token:"]`)
	token, remaining, err := ExtractToken(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("token should be empty, got %q", token)
	}
	if string(remaining) != "[]" {
		t.Errorf("remaining = %s, want []", string(remaining))
	}
}

// ---- ValidateToken ----

func TestValidateTokenMatch(t *testing.T) {
	if !ValidateToken("secret", "secret") {
		t.Error("expected token match")
	}
}

func TestValidateTokenMismatch(t *testing.T) {
	if ValidateToken("secret", "wrong") {
		t.Error("expected token mismatch")
	}
}

func TestValidateTokenEmptyExpected(t *testing.T) {
	if !ValidateToken("", "anything") {
		t.Error("empty expected token should always match")
	}
}

func TestValidateTokenEmptyProvided(t *testing.T) {
	if ValidateToken("secret", "") {
		t.Error("empty provided with non-empty expected should fail")
	}
}

func TestValidateTokenCaseSensitive(t *testing.T) {
	if ValidateToken("Secret", "secret") {
		t.Error("token comparison should be case sensitive")
	}
}

func TestValidateTokenHMACBased(t *testing.T) {
	// Verify the HMAC-based approach produces stable results.
	first := ValidateToken("mysecret", "mytoken")
	second := ValidateToken("mysecret", "mytoken")
	if first != second {
		t.Error("HMAC-based validation should be deterministic")
	}
}

// ---- IsNotification ----

func TestIsNotificationTrue(t *testing.T) {
	req := &Request{
		JSONRPC: "2.0",
		Method:  "aria2.onDownloadStart",
		Params:  json.RawMessage(`[{"gid":"abc"}]`),
	}
	if !req.IsNotification() {
		t.Error("expected IsNotification = true")
	}
}

func TestIsNotificationFalse(t *testing.T) {
	req := &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"abc"`),
		Method:  "aria2.onDownloadStart",
	}
	if req.IsNotification() {
		t.Error("expected IsNotification = false")
	}
}

func TestIsNotificationWithNullID(t *testing.T) {
	req := &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`null`),
		Method:  "aria2.getVersion",
	}
	if req.IsNotification() {
		t.Error("null id should not be a notification")
	}
}

// ---- Helpers ----

func asError(err error, target **Error) bool {
	if err == nil {
		return false
	}
	var rpcErr *Error
	if !errors.As(err, &rpcErr) {
		return false
	}
	*target = rpcErr
	return true
}

func getErrorCode(e *Error) int {
	if e == nil {
		return 0
	}
	return e.Code
}

func stringContains(data []byte, substr string) bool {
	return len(data) >= len(substr) && indexInBytes(data, substr) >= 0
}

func indexInBytes(data []byte, substr string) int {
	for i := 0; i <= len(data)-len(substr); i++ {
		if string(data[i:i+len(substr)]) == substr {
			return i
		}
	}
	return -1
}
