package xmlrpc

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeCallMethodName(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>aria2.addUri</methodName><params/></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	if call.MethodName != "aria2.addUri" {
		t.Errorf("MethodName = %q, want %q", call.MethodName, "aria2.addUri")
	}
	if len(call.Params) != 0 {
		t.Errorf("len(Params) = %d, want 0", len(call.Params))
	}
}

func TestDecodeCallStringParam(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>aria2.addUri</methodName><params><param><value><string>token:secret</string></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	if len(call.Params) != 1 {
		t.Fatalf("len(Params) = %d, want 1", len(call.Params))
	}
	s, ok := call.Params[0].(string)
	if !ok {
		t.Fatalf("Param[0] type = %T, want string", call.Params[0])
	}
	if s != "token:secret" {
		t.Errorf("Param[0] = %q, want %q", s, "token:secret")
	}
}

func TestDecodeCallMultipleParams(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>aria2.addUri</methodName><params><param><value><string>token:secret</string></value></param><param><value><array><data><value><string>http://example.com/file</string></value></data></array></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	if len(call.Params) != 2 {
		t.Fatalf("len(Params) = %d, want 2", len(call.Params))
	}
}

func TestDecodeCallIntParam(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>aria2.tellStatus</methodName><params><param><value><i4>42</i4></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	if len(call.Params) != 1 {
		t.Fatalf("len(Params) = %d, want 1", len(call.Params))
	}
	v, ok := call.Params[0].(int64)
	if !ok {
		t.Fatalf("Param[0] type = %T, want int64", call.Params[0])
	}
	if v != 42 {
		t.Errorf("Param[0] = %d, want 42", v)
	}
}

func TestDecodeCallIntTag(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><int>-1</int></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	v, ok := call.Params[0].(int64)
	if !ok {
		t.Fatalf("Param[0] type = %T, want int64", call.Params[0])
	}
	if v != -1 {
		t.Errorf("Param[0] = %d, want -1", v)
	}
}

func TestDecodeCallEmptyStringParam(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><string></string></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	s, ok := call.Params[0].(string)
	if !ok {
		t.Fatalf("Param[0] type = %T, want string", call.Params[0])
	}
	if s != "" {
		t.Errorf("Param[0] = %q, want empty", s)
	}
}

func TestDecodeCallBooleanParam(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><boolean>1</boolean></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	b, ok := call.Params[0].(bool)
	if !ok {
		t.Fatalf("Param[0] type = %T, want bool", call.Params[0])
	}
	if !b {
		t.Errorf("Param[0] = false, want true")
	}
}

func TestDecodeCallBooleanFalse(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><boolean>0</boolean></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	b, ok := call.Params[0].(bool)
	if !ok {
		t.Fatalf("Param[0] type = %T, want bool", call.Params[0])
	}
	if b {
		t.Errorf("Param[0] = true, want false")
	}
}

func TestDecodeCallStructParam(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><struct><member><name>key</name><value><string>val</string></value></member></struct></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	m, ok := call.Params[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Param[0] type = %T, want map[string]interface{}", call.Params[0])
	}
	if v, ok := m["key"].(string); !ok || v != "val" {
		t.Errorf("m[key] = %v (%T), want string \"val\"", m["key"], m["key"])
	}
}

func TestDecodeCallNestedStruct(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><struct><member><name>outer</name><value><struct><member><name>inner</name><value><i4>99</i4></value></member></struct></value></member></struct></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	m, ok := call.Params[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Param[0] type = %T, want map[string]interface{}", call.Params[0])
	}
	inner, ok := m["outer"].(map[string]interface{})
	if !ok {
		t.Fatalf("m[outer] type = %T, want map[string]interface{}", m["outer"])
	}
	if v, ok := inner["inner"].(int64); !ok || v != 99 {
		t.Errorf("inner[inner] = %v (%T), want int64 99", inner["inner"], inner["inner"])
	}
}

func TestDecodeCallArrayParam(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><array><data><value><string>a</string></value><value><string>b</string></value></data></array></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	arr, ok := call.Params[0].([]interface{})
	if !ok {
		t.Fatalf("Param[0] type = %T, want []interface{}", call.Params[0])
	}
	if len(arr) != 2 {
		t.Fatalf("len(arr) = %d, want 2", len(arr))
	}
	if s, ok := arr[0].(string); !ok || s != "a" {
		t.Errorf("arr[0] = %v, want \"a\"", arr[0])
	}
	if s, ok := arr[1].(string); !ok || s != "b" {
		t.Errorf("arr[1] = %v, want \"b\"", arr[1])
	}
}

func TestDecodeCallDoubleParam(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><double>3.14</double></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	// In aria2, double is treated as string for RPC purposes
	s, ok := call.Params[0].(string)
	if !ok {
		t.Fatalf("Param[0] type = %T, want string", call.Params[0])
	}
	if s != "3.14" {
		t.Errorf("Param[0] = %q, want %q", s, "3.14")
	}
}

func TestDecodeCallBase64Param(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0xFF}
	encoded := base64.StdEncoding.EncodeToString(data)
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><base64>` + encoded + `</base64></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	// aria2 decodes base64 and stores as a string (with raw bytes)
	s, ok := call.Params[0].(string)
	if !ok {
		t.Fatalf("Param[0] type = %T, want string", call.Params[0])
	}
	if s != string(data) {
		t.Errorf("Param[0] = %q, want %q", s, string(data))
	}
}

func TestDecodeCallImplicitString(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value>hello</value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	s, ok := call.Params[0].(string)
	if !ok {
		t.Fatalf("Param[0] type = %T, want string", call.Params[0])
	}
	if s != "hello" {
		t.Errorf("Param[0] = %q, want %q", s, "hello")
	}
}

func TestDecodeCallEmptyValue(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value/></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	if len(call.Params) != 1 {
		t.Fatalf("len(Params) = %d, want 1", len(call.Params))
	}
	// Empty value without type tag: no value stored (nil)
	if call.Params[0] != nil {
		t.Errorf("Param[0] = %v, want nil", call.Params[0])
	}
}

func TestDecodeCallEmptyArray(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><array><data/></array></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	arr, ok := call.Params[0].([]interface{})
	if !ok {
		t.Fatalf("Param[0] type = %T, want []interface{}", call.Params[0])
	}
	if len(arr) != 0 {
		t.Errorf("len(arr) = %d, want 0", len(arr))
	}
}

func TestDecodeCallRealWorldAddUri(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>aria2.addUri</methodName><params><param><value><string>token:secret</string></value></param><param><value><array><data><value><string>http://example.com/file</string></value></data></array></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	if call.MethodName != "aria2.addUri" {
		t.Errorf("MethodName = %q, want %q", call.MethodName, "aria2.addUri")
	}
	if len(call.Params) != 2 {
		t.Fatalf("len(Params) = %d, want 2", len(call.Params))
	}
	if s, ok := call.Params[0].(string); !ok || s != "token:secret" {
		t.Errorf("Param[0] = %v, want \"token:secret\"", call.Params[0])
	}
	arr, ok := call.Params[1].([]interface{})
	if !ok {
		t.Fatalf("Param[1] type = %T, want []interface{}", call.Params[1])
	}
	if len(arr) != 1 {
		t.Fatalf("len(arr) = %d, want 1", len(arr))
	}
}

func TestDecodeCallWithXMLDeclaration(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
  <methodName>aria2.getVersion</methodName>
  <params/>
</methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	if call.MethodName != "aria2.getVersion" {
		t.Errorf("MethodName = %q, want %q", call.MethodName, "aria2.getVersion")
	}
}

func TestDecodeCallMultipleParamsWithNils(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><nil/></value></param><param><value><string>hello</string></value></param><param><value><nil/></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	if len(call.Params) != 3 {
		t.Fatalf("len(Params) = %d, want 3", len(call.Params))
	}
	if call.Params[0] != nil {
		t.Errorf("Param[0] = %v, want nil", call.Params[0])
	}
	if s, ok := call.Params[1].(string); !ok || s != "hello" {
		t.Errorf("Param[1] = %v, want \"hello\"", call.Params[1])
	}
	if call.Params[2] != nil {
		t.Errorf("Param[2] = %v, want nil", call.Params[2])
	}
}

func TestDecodeCallWhitespaceInText(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><string>  spaced  </string></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	s, ok := call.Params[0].(string)
	if !ok {
		t.Fatalf("Param[0] type = %T, want string", call.Params[0])
	}
	if s != "  spaced  " {
		t.Errorf("Param[0] = %q, want %q", s, "  spaced  ")
	}
}

func TestDecodeCallNotMethodCall(t *testing.T) {
	input := `<?xml version="1.0"?><methodResponse><params><param><value><string>ok</string></value></param></params></methodResponse>`
	_, err := DecodeCall(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for non-methodCall root element")
	}
}

func TestDecodeCallLeadingWhitespaceBeforeTypeTag(t *testing.T) {
	// C++ Expat pushes a new character buffer per state. Leading text
	// between <value> and <string> must not pollute the string value.
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value>  <string>hello</string></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	s, ok := call.Params[0].(string)
	if !ok {
		t.Fatalf("Param[0] type = %T, want string", call.Params[0])
	}
	if s != "hello" {
		t.Errorf("Param[0] = %q, want %q", s, "hello")
	}
}

func TestDecodeCallTrailingTextAfterTypeTag(t *testing.T) {
	// Text between </string> and </value> must be ignored (C++ valueState
	// buffer is isolated from stringState buffer; currentFrameValue already set).
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><string>hello</string> tail ignored</value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	s, ok := call.Params[0].(string)
	if !ok {
		t.Fatalf("Param[0] type = %T, want string", call.Params[0])
	}
	if s != "hello" {
		t.Errorf("Param[0] = %q, want %q", s, "hello")
	}
}

func TestDecodeCallMixedContentAroundTypeTag(t *testing.T) {
	// Both leading and trailing text around a type tag must be excluded.
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value>before<string>inner</string>after</value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	s, ok := call.Params[0].(string)
	if !ok {
		t.Fatalf("Param[0] type = %T, want string", call.Params[0])
	}
	if s != "inner" {
		t.Errorf("Param[0] = %q, want %q", s, "inner")
	}
}

func TestDecodeCallMixedContentBeforeInt(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value>  <int>42</int></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	v, ok := call.Params[0].(int64)
	if !ok {
		t.Fatalf("Param[0] type = %T, want int64", call.Params[0])
	}
	if v != 42 {
		t.Errorf("Param[0] = %d, want 42", v)
	}
}

func TestDecodeCallMixedContentBeforeBoolean(t *testing.T) {
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value>\n  <boolean>1</boolean></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	b, ok := call.Params[0].(bool)
	if !ok {
		t.Fatalf("Param[0] type = %T, want bool", call.Params[0])
	}
	if !b {
		t.Errorf("Param[0] = false, want true")
	}
}

func TestDecodeCallMixedContentBeforeBase64(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02}
	encoded := base64.StdEncoding.EncodeToString(data)
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value>  <base64>` + encoded + `</base64></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	s, ok := call.Params[0].(string)
	if !ok {
		t.Fatalf("Param[0] type = %T, want string", call.Params[0])
	}
	if s != string(data) {
		t.Errorf("Param[0] = %q, want %q", s, string(data))
	}
}

func TestDecodeCallNestedTypeTagIsolated(t *testing.T) {
	// Leading/trailing text in struct context must not pollute member values.
	input := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><struct>
  <member>
    <name>key</name>
    <value>  <string>val</string></value>
  </member>
</struct></value></param></params></methodCall>`
	call, err := DecodeCall(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCall failed: %v", err)
	}
	m, ok := call.Params[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Param[0] type = %T, want map[string]interface{}", call.Params[0])
	}
	if v, ok := m["key"].(string); !ok || v != "val" {
		t.Errorf("m[key] = %q (%T), want string %q", m["key"], m["key"], "val")
	}
}

func TestDecodeCallInvalidXML(t *testing.T) {
	input := `not xml`
	_, err := DecodeCall(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for invalid XML")
	}
}

func TestEncodeReplyStringResult(t *testing.T) {
	var buf bytes.Buffer
	err := EncodeReply(&buf, Reply{Result: "2089b05ecca3d829"})
	if err != nil {
		t.Fatalf("EncodeReply failed: %v", err)
	}
	// Check that the XML is valid
	var result xmlResponse
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response XML failed: %v", err)
	}
	if len(result.Params) != 1 {
		t.Fatalf("len(Params) = %d, want 1", len(result.Params))
	}
	s, err := getStringValue(result.Params[0].Value)
	if err != nil {
		t.Fatalf("getStringValue: %v", err)
	}
	if s != "2089b05ecca3d829" {
		t.Errorf("result = %q, want %q", s, "2089b05ecca3d829")
	}
}

func TestEncodeReplyFault(t *testing.T) {
	var buf bytes.Buffer
	err := EncodeReply(&buf, Reply{
		Fault: &Fault{Code: 1, String: "error message"},
	})
	if err != nil {
		t.Fatalf("EncodeReply failed: %v", err)
	}
	var result xmlResponse
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal fault response XML failed: %v\nbody: %s", err, buf.String())
	}
	if result.Fault == nil {
		t.Fatal("expected fault element")
	}
}

func TestEncodeReplyIntResult(t *testing.T) {
	var buf bytes.Buffer
	err := EncodeReply(&buf, Reply{Result: int64(42)})
	if err != nil {
		t.Fatalf("EncodeReply failed: %v", err)
	}
	var result xmlResponse
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response XML failed: %v\nbody: %s", err, buf.String())
	}
	got := result.Params[0].Value.Int
	if got == nil {
		got = result.Params[0].Value.IntAlt
	}
	if got == nil || *got != 42 {
		t.Errorf("result = %v, want int 42", got)
	}
}

func TestEncodeReplyArrayResult(t *testing.T) {
	var buf bytes.Buffer
	err := EncodeReply(&buf, Reply{Result: []interface{}{"a", "b"}})
	if err != nil {
		t.Fatalf("EncodeReply failed: %v", err)
	}
	var result xmlResponse
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response XML failed: %v\nbody: %s", err, buf.String())
	}
	if result.Params[0].Value.Array == nil {
		t.Fatal("expected array in result")
	}
	if len(result.Params[0].Value.Array.Data.Values) != 2 {
		t.Fatalf("array len = %d, want 2", len(result.Params[0].Value.Array.Data.Values))
	}
}

func TestEncodeReplyStructResult(t *testing.T) {
	var buf bytes.Buffer
	err := EncodeReply(&buf, Reply{
		Result: map[string]interface{}{"gid": "abc123", "status": "active"},
	})
	if err != nil {
		t.Fatalf("EncodeReply failed: %v", err)
	}
	var result xmlResponse
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response XML failed: %v\nbody: %s", err, buf.String())
	}
	if result.Params[0].Value.Struct == nil {
		t.Fatal("expected struct in result")
	}
}

func TestEncodeReplyNilResult(t *testing.T) {
	var buf bytes.Buffer
	err := EncodeReply(&buf, Reply{})
	if err != nil {
		t.Fatalf("EncodeReply failed: %v", err)
	}
	var result xmlResponse
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response XML failed: %v\nbody: %s", err, buf.String())
	}
}

func TestEncodeReplyBooleanResult(t *testing.T) {
	var buf bytes.Buffer
	err := EncodeReply(&buf, Reply{Result: true})
	if err != nil {
		t.Fatalf("EncodeReply failed: %v", err)
	}
	var result xmlResponse
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response XML failed: %v\nbody: %s", err, buf.String())
	}
	if result.Params[0].Value.Boolean == nil || *result.Params[0].Value.Boolean != 1 {
		t.Errorf("result = %v, want boolean 1", result.Params[0].Value.Boolean)
	}
}

func TestEncodeReplyBooleanFalse(t *testing.T) {
	var buf bytes.Buffer
	err := EncodeReply(&buf, Reply{Result: false})
	if err != nil {
		t.Fatalf("EncodeReply failed: %v", err)
	}
	var result xmlResponse
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response XML failed: %v\nbody: %s", err, buf.String())
	}
	if result.Params[0].Value.Boolean == nil || *result.Params[0].Value.Boolean != 0 {
		t.Errorf("result = %v, want boolean 0", result.Params[0].Value.Boolean)
	}
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		call Call
	}{
		{
			name: "string params",
			call: Call{
				MethodName: "aria2.addUri",
				Params:     []interface{}{"token:secret", []interface{}{"http://example.com/file"}},
			},
		},
		{
			name: "mixed params",
			call: Call{
				MethodName: "aria2.changeOption",
				Params: []interface{}{
					"2089b05ecca3d829",
					map[string]interface{}{
						"max-download-limit": "100K",
					},
				},
			},
		},
		{
			name: "int and string",
			call: Call{
				MethodName: "aria2.tellStatus",
				Params:     []interface{}{"2089b05ecca3d829", int64(0), int64(1)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode the call as a methodCall (we can reuse EncodeReply-like encoding)
			xml, err := encodeCall(tt.call)
			if err != nil {
				t.Fatalf("encodeCall failed: %v", err)
			}

			// Decode the call back
			decoded, err := DecodeCall(strings.NewReader(xml))
			if err != nil {
				t.Fatalf("DecodeCall failed: %v", err)
			}

			if decoded.MethodName != tt.call.MethodName {
				t.Errorf("MethodName = %q, want %q", decoded.MethodName, tt.call.MethodName)
			}
			if len(decoded.Params) != len(tt.call.Params) {
				t.Fatalf("len(Params) = %d, want %d", len(decoded.Params), len(tt.call.Params))
			}
			for i := range tt.call.Params {
				if !deepEqual(decoded.Params[i], tt.call.Params[i]) {
					t.Errorf("Param[%d] = %#v, want %#v", i, decoded.Params[i], tt.call.Params[i])
				}
			}
		})
	}
}

// xmlResponse is a helper to unmarshal methodResponse for testing.
type xmlResponse struct {
	XMLName xml.Name      `xml:"methodResponse"`
	Params  []xmlRPCParam `xml:"params>param"`
	Fault   *xmlRPCValue  `xml:"fault>value"`
}

type xmlRPCParam struct {
	Value xmlRPCValue `xml:"value"`
}

type xmlRPCValue struct {
	String  *string       `xml:"string"`
	Int     *int64        `xml:"i4"`
	IntAlt  *int64        `xml:"int"`
	Boolean *int          `xml:"boolean"`
	Array   *xmlRPCArray  `xml:"array"`
	Struct  *xmlRPCStruct `xml:"struct"`
	Double  *string       `xml:"double"`
	Base64  *string       `xml:"base64"`
	Nil     *struct{}     `xml:"nil"`
	// Raw text content for implicit string values
	InnerXML string `xml:",innerxml"`
}

type xmlRPCArray struct {
	Data xmlRPCData `xml:"data"`
}

type xmlRPCData struct {
	Values []xmlRPCValue `xml:"value"`
}

type xmlRPCStruct struct {
	Members []xmlRPCMember `xml:"member"`
}

type xmlRPCMember struct {
	Name  string      `xml:"name"`
	Value xmlRPCValue `xml:"value"`
}

func getStringValue(v xmlRPCValue) (string, error) {
	if v.String != nil {
		return *v.String, nil
	}
	// Try innerxml stripped of whitespace
	return strings.TrimSpace(v.InnerXML), nil
}

// encodeCall is a test helper that encodes a Call into XML-RPC methodCall XML.
// It uses the production writeValue for parameter encoding.
func encodeCall(c Call) (string, error) {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0"?>`)
	buf.WriteString(`<methodCall>`)
	buf.WriteString(`<methodName>`)
	if err := xml.EscapeText(&buf, []byte(c.MethodName)); err != nil {
		return "", err
	}
	buf.WriteString(`</methodName>`)
	if len(c.Params) > 0 {
		buf.WriteString(`<params>`)
		for _, p := range c.Params {
			buf.WriteString(`<param>`)
			if err := writeValue(&buf, p); err != nil {
				return "", err
			}
			buf.WriteString(`</param>`)
		}
		buf.WriteString(`</params>`)
	} else {
		buf.WriteString(`<params/>`)
	}
	buf.WriteString(`</methodCall>`)
	return buf.String(), nil
}

func deepEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	switch va := a.(type) {
	case string:
		vb, ok := b.(string)
		return ok && va == vb
	case int64:
		vb, ok := b.(int64)
		return ok && va == vb
	case bool:
		vb, ok := b.(bool)
		return ok && va == vb
	case []interface{}:
		vb, ok := b.([]interface{})
		if !ok || len(va) != len(vb) {
			return false
		}
		for i := range va {
			if !deepEqual(va[i], vb[i]) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		vb, ok := b.(map[string]interface{})
		if !ok || len(va) != len(vb) {
			return false
		}
		for k, v := range va {
			if !deepEqual(v, vb[k]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}
