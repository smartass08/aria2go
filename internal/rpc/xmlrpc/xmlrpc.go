// Package xmlrpc provides XML-RPC request parsing and response encoding
// compatible with aria2 1.37.0's XML-RPC interface.
//
// The XML-RPC type system maps to Go types as follows:
//
//	<string> → string
//	<i4>/<int> → int64
//	<boolean> → bool
//	<double> → string (aria2 treats double as string)
//	<dateTime.iso8601> → string
//	<base64> → string (decoded bytes as string)
//	<struct> → map[string]interface{}
//	<array> → []interface{}
//	<nil/> → nil
package xmlrpc

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// Call represents a parsed XML-RPC method call request.
type Call struct {
	MethodName string
	Params     []interface{}
}

// Reply represents an XML-RPC method response.
type Reply struct {
	Result interface{}
	Fault  *Fault
}

// Fault represents an XML-RPC fault response.
type Fault struct {
	Code   int    `xml:"faultCode"`
	String string `xml:"faultString"`
}

// parserPool reuses parser state across DecodeCall invocations.
var parserPool = sync.Pool{
	New: func() any {
		p := &parser{
			valueStack:  make([]*valueContext, 0, 4),
			memberNames: make([]string, 0, 4),
		}
		return p
	},
}

// DecodeCall parses an XML-RPC request body from r and returns the
// method name and parameters. It only accepts top-level <methodCall>
// elements. Unknown elements are silently skipped (matching aria2
// behavior).
func DecodeCall(r io.Reader) (Call, error) {
	dec := xml.NewDecoder(r)
	p := parserPool.Get().(*parser)
	p.reset()
	if err := p.parse(dec); err != nil {
		parserPool.Put(p)
		return Call{}, err
	}
	call := Call{MethodName: p.methodName, Params: p.params}
	parserPool.Put(p)
	return call, nil
}

// parser is a SAX-like XML-RPC request parser.
type parser struct {
	methodName string
	params     []interface{}

	// valueStack tracks the chain of value contexts being built.
	// When a <struct> or <array> is encountered, we push a new
	// context. Child values add to the current top.
	valueStack []*valueContext

	// memberNames stack tracks struct member names at each nesting level.
	memberNames []string

	// charBuf collects text between tags for methodName, name elements, and scalar values.
	charBuf strings.Builder

	// collecting indicates whether charBuf is active.
	collecting bool
}

func (p *parser) reset() {
	p.methodName = ""
	p.params = p.params[:0]
	p.valueStack = p.valueStack[:0]
	p.memberNames = p.memberNames[:0]
	p.charBuf.Reset()
	p.collecting = false
}

// valueContext represents a single <value> being parsed.
type valueContext struct {
	kind    string // "string", "int", "boolean", "struct", "array", "base64", "double", "nil", "" (implicit)
	structM map[string]interface{}
	arrayV  []interface{}
}

func (p *parser) parse(dec *xml.Decoder) error {
	var seenMethodCall bool

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("xmlrpc: parse error: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local

			switch name {
			case "methodCall":
				seenMethodCall = true
			case "methodName":
				p.collecting = true
				p.charBuf.Reset()
			case "param":
				// value context pushed when <value> is encountered
			case "value":
				p.pushValue()
			case "string", "double", "dateTime.iso8601":
				p.setTypeTag(name, true)
			case "i4", "int":
				p.setTypeTag("int", true)
			case "boolean":
				p.setTypeTag("boolean", true)
			case "base64":
				p.setTypeTag("base64", true)
			case "struct":
				p.setTypeTag("struct", false)
			case "array":
				p.setTypeTag("array", false)
			case "nil":
				p.setTypeTag("nil", false)
			case "member":
				p.memberNames = append(p.memberNames, "")
			case "name":
				p.collecting = true
				p.charBuf.Reset()
			}

		case xml.EndElement:
			name := t.Name.Local

			switch name {
			case "methodName":
				p.collecting = false
				p.methodName = p.charBuf.String()
			case "value":
				p.popValue()
			case "name":
				p.collecting = false
				if len(p.memberNames) > 0 {
					p.memberNames[len(p.memberNames)-1] = p.charBuf.String()
				}
			case "string", "double", "dateTime.iso8601", "i4", "int", "boolean", "base64":
				p.collecting = false
			case "param":
				// param value set when </value> is processed
			case "member":
				if len(p.memberNames) > 0 {
					p.memberNames = p.memberNames[:len(p.memberNames)-1]
				}
			}

		case xml.CharData:
			text := string(t)
			if p.collecting {
				p.charBuf.WriteString(text)
			}
		}
	}

	if !seenMethodCall {
		return fmt.Errorf("xmlrpc: not a methodCall request")
	}

	return nil
}

func (p *parser) pushValue() {
	vc := &valueContext{}
	p.valueStack = append(p.valueStack, vc)
	p.collecting = true
	p.charBuf.Reset()
}

func (p *parser) setTypeTag(kind string, collectsChars bool) {
	if len(p.valueStack) > 0 {
		vc := p.valueStack[len(p.valueStack)-1]
		vc.kind = kind
		switch kind {
		case "struct":
			vc.structM = make(map[string]interface{})
		case "array":
			vc.arrayV = make([]interface{}, 0)
		}
	}
	p.collecting = collectsChars
	if collectsChars {
		p.charBuf.Reset()
	}
}

func (p *parser) popValue() {
	p.collecting = false
	if len(p.valueStack) == 0 {
		return
	}
	vc := p.valueStack[len(p.valueStack)-1]
	p.valueStack = p.valueStack[:len(p.valueStack)-1]

	val := p.finalize(vc)

	if len(p.valueStack) > 0 {
		parent := p.valueStack[len(p.valueStack)-1]
		switch parent.kind {
		case "array":
			parent.arrayV = append(parent.arrayV, val)
		case "struct":
			name := ""
			if len(p.memberNames) > 0 {
				name = p.memberNames[len(p.memberNames)-1]
			}
			parent.structM[name] = val
		}
	} else {
		p.params = append(p.params, val)
	}
}

func (p *parser) finalize(vc *valueContext) interface{} {
	if vc.kind == "" || vc.kind == "string" || vc.kind == "double" || vc.kind == "dateTime.iso8601" {
		s := p.charBuf.String()
		if vc.kind == "" && s == "" {
			return nil
		}
		return s
	}
	switch vc.kind {
	case "int":
		v, err := strconv.ParseInt(strings.TrimSpace(p.charBuf.String()), 10, 64)
		if err != nil {
			return nil
		}
		return v
	case "boolean":
		return strings.TrimSpace(p.charBuf.String()) == "1"
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(p.charBuf.String()))
		if err != nil {
			return ""
		}
		return string(decoded)
	case "struct":
		return vc.structM
	case "array":
		return vc.arrayV
	case "nil":
		return nil
	default:
		return nil
	}
}

// xmlBufPool is used by EncodeReply to buffer the XML output before
// writing to the final writer in one shot.
var xmlBufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// EncodeReply writes an XML-RPC method response to w.
// If r.Fault is non-nil, a fault response is generated.
// Otherwise, r.Result is encoded as the response value.
func EncodeReply(w io.Writer, r Reply) error {
	buf := xmlBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	bw := &bufWriter{buf: buf}

	bw.WriteString(`<?xml version="1.0"?>`)
	bw.WriteString(`<methodResponse>`)

	if r.Fault != nil {
		bw.WriteString(`<fault><value><struct>`)
		bw.WriteString(`<member><name>faultCode</name><value><int>`)
		bw.WriteString(strconv.Itoa(r.Fault.Code))
		bw.WriteString(`</int></value></member>`)
		bw.WriteString(`<member><name>faultString</name><value><string>`)
		xml.EscapeText(bw, []byte(r.Fault.String))
		bw.WriteString(`</string></value></member>`)
		bw.WriteString(`</struct></value></fault>`)
	} else {
		bw.WriteString(`<params><param>`)
		if err := writeValue(bw, r.Result); err != nil {
			xmlBufPool.Put(buf)
			return err
		}
		bw.WriteString(`</param></params>`)
	}

	bw.WriteString(`</methodResponse>`)
	if bw.err != nil {
		xmlBufPool.Put(buf)
		return bw.err
	}
	_, bw.err = w.Write(buf.Bytes())
	xmlBufPool.Put(buf)
	return bw.err
}

type bufWriter struct {
	buf *bytes.Buffer
	err error
}

func (bw *bufWriter) Write(p []byte) (int, error) {
	if bw.err != nil {
		return 0, bw.err
	}
	n, err := bw.buf.Write(p)
	if err != nil {
		bw.err = err
	}
	return n, err
}

// WriteString implements io.StringWriter for zero-copy string writes.
func (bw *bufWriter) WriteString(s string) (int, error) {
	if bw.err != nil {
		return 0, bw.err
	}
	n, err := bw.buf.WriteString(s)
	if err != nil {
		bw.err = err
	}
	return n, err
}

func (bw *bufWriter) Err() error {
	return bw.err
}

func writeValue(w io.Writer, v interface{}) error {
	if err := writeString(w, `<value>`); err != nil {
		return err
	}
	if err := encodeValue(w, v); err != nil {
		return err
	}
	return writeString(w, `</value>`)
}

func encodeValue(w io.Writer, v interface{}) error {
	switch val := v.(type) {
	case nil:
		return writeString(w, `<nil/>`)
	case string:
		if err := writeString(w, `<string>`); err != nil {
			return err
		}
		if err := xml.EscapeText(w, []byte(val)); err != nil {
			return err
		}
		if err := writeString(w, `</string>`); err != nil {
			return err
		}
	case int:
		return writeIntTag(w, int64(val))
	case int64:
		return writeIntTag(w, val)
	case int32:
		return writeIntTag(w, int64(val))
	case bool:
		if val {
			return writeString(w, `<boolean>1</boolean>`)
		}
		return writeString(w, `<boolean>0</boolean>`)
	case []interface{}:
		if err := writeString(w, `<array><data>`); err != nil {
			return err
		}
		for _, item := range val {
			if err := writeValue(w, item); err != nil {
				return err
			}
		}
		if err := writeString(w, `</data></array>`); err != nil {
			return err
		}
	case []string:
		if err := writeString(w, `<array><data>`); err != nil {
			return err
		}
		for _, item := range val {
			if err := writeValue(w, item); err != nil {
				return err
			}
		}
		if err := writeString(w, `</data></array>`); err != nil {
			return err
		}
	case map[string]interface{}:
		if err := writeString(w, `<struct>`); err != nil {
			return err
		}
		for k, vv := range val {
			if err := writeString(w, `<member><name>`); err != nil {
				return err
			}
			xml.EscapeText(w, []byte(k))
			if err := writeString(w, `</name>`); err != nil {
				return err
			}
			if err := writeValue(w, vv); err != nil {
				return err
			}
			if err := writeString(w, `</member>`); err != nil {
				return err
			}
		}
		if err := writeString(w, `</struct>`); err != nil {
			return err
		}
	case map[string]string:
		if err := writeString(w, `<struct>`); err != nil {
			return err
		}
		for k, vv := range val {
			if err := writeString(w, `<member><name>`); err != nil {
				return err
			}
			xml.EscapeText(w, []byte(k))
			if err := writeString(w, `</name>`); err != nil {
				return err
			}
			if err := writeValue(w, vv); err != nil {
				return err
			}
			if err := writeString(w, `</member>`); err != nil {
				return err
			}
		}
		if err := writeString(w, `</struct>`); err != nil {
			return err
		}
	default:
		if err := writeString(w, `<string>`); err != nil {
			return err
		}
		s := fmt.Sprintf("%v", val)
		xml.EscapeText(w, []byte(s))
		if err := writeString(w, `</string>`); err != nil {
			return err
		}
	}
	return nil
}

func writeString(w io.Writer, s string) error {
	_, err := io.WriteString(w, s)
	return err
}

func writeIntTag(w io.Writer, v int64) error {
	if err := writeString(w, `<int>`); err != nil {
		return err
	}
	if err := writeString(w, strconv.FormatInt(v, 10)); err != nil {
		return err
	}
	return writeString(w, `</int>`)
}
