// Package http provides HTTP protocol helpers including Content-Disposition
// parsing per RFC 6266 and RFC 5987.
package http

import (
	"strings"
	"unicode/utf8"
)

type cdState int

const (
	cdBeforeType cdState = iota
	cdType
	cdAfterType
	cdBeforeParamName
	cdParamName
	cdAfterParamName
	cdBeforeValue
	cdQuotedString
	cdQuotedBackslash
	cdToken
	cdAfterValue
	cdFinished
	cdBeforeExtValue
	cdCharset
	cdLanguage
	cdExtValueChars
	cdExtPct1
	cdExtPct2
)

// ParseContentDisposition parses a Content-Disposition header value using
// UTF-8 as the default charset for plain filename parameters.
func ParseContentDisposition(header string) (filename string, ok bool) {
	return ParseContentDispositionWithOptions(header, true)
}

// ParseContentDispositionWithOptions parses a Content-Disposition header value
// per RFC 6266 and extracts the filename parameter. It handles both the
// 'filename' parameter (token or quoted-string) and the 'filename*'
// parameter with RFC 5987 encoding (charset'language'percent-encoded-value).
// Path separators are stripped (only the basename is returned). When
// defaultUTF8 is false, plain filename parameters are interpreted as
// ISO-8859-1 bytes to match aria2's default
// --content-disposition-default-utf8=false behavior. Returns empty string and
// false if no valid filename is found.
func ParseContentDispositionWithOptions(header string, defaultUTF8 bool) (filename string, ok bool) {
	s := cdBeforeType
	var paramNameStart, paramNameEnd int
	var filenameCharset string
	inFileParm := false
	fileNameFound := false
	var sb strings.Builder
	pctVal := byte(0)

	p := 0
	n := len(header)

	for p < n {
		c := header[p]

		switch s {
		case cdBeforeType:
			if isRFC2616Token(c) {
				s = cdType
			} else if !isLwsChar(c) {
				return "", false
			}

		case cdType, cdAfterType:
			if c == ';' {
				s = cdBeforeParamName
			} else if isLwsChar(c) {
				s = cdAfterType
			} else if s == cdAfterType || !isRFC2616Token(c) {
				return "", false
			}

		case cdBeforeParamName:
			if isRFC2616Token(c) {
				paramNameStart = p
				s = cdParamName
			} else if !isLwsChar(c) {
				return "", false
			}

		case cdParamName, cdAfterParamName:
			if c == '=' {
				if s == cdParamName {
					paramNameEnd = p
				}
				// Strip trailing LWS from param name end
				for paramNameEnd > paramNameStart && isLwsChar(header[paramNameEnd-1]) {
					paramNameEnd--
				}
				inFileParm = false
				paramName := strings.ToLower(header[paramNameStart:paramNameEnd])
				plen := paramNameEnd - paramNameStart
				if paramName == "filename*" {
					inFileParm = true
					s = cdBeforeExtValue
				} else if paramName == "filename" {
					inFileParm = true
					s = cdBeforeValue
				} else {
					if plen > 1 && header[paramNameEnd-1] == '*' {
						s = cdBeforeExtValue
					} else {
						s = cdBeforeValue
					}
				}
				if inFileParm {
					sb.Reset()
				}
			} else if isLwsChar(c) {
				paramNameEnd = p
				s = cdAfterParamName
			} else if s == cdAfterParamName || !isRFC2616Token(c) {
				return "", false
			}

		case cdBeforeValue:
			if c == '"' {
				s = cdQuotedString
			} else if isRFC2616Token(c) {
				if inFileParm {
					sb.WriteByte(c)
				}
				s = cdToken
			} else if !isLwsChar(c) {
				return "", false
			}

		case cdAfterValue:
			if c == ';' {
				s = cdBeforeParamName
			} else if !isLwsChar(c) {
				return "", false
			}

		case cdQuotedString:
			if c == '\\' {
				s = cdQuotedBackslash
			} else if c == '"' {
				if inFileParm {
					fileNameFound = true
				}
				s = cdAfterValue
			} else {
				if !isQDText(c) {
					return "", false
				}
				if inFileParm {
					sb.WriteByte(c)
				}
			}

		case cdQuotedBackslash:
			if inFileParm {
				sb.WriteByte(c)
			}
			s = cdQuotedString

		case cdToken:
			if isRFC2616Token(c) {
				if inFileParm {
					sb.WriteByte(c)
				}
			} else if c == ';' {
				if inFileParm {
					fileNameFound = true
				}
				s = cdBeforeParamName
			} else if isLwsChar(c) {
				if inFileParm {
					fileNameFound = true
				}
				s = cdAfterValue
			} else {
				return "", false
			}

		case cdBeforeExtValue:
			if c == '\'' {
				return "", false // empty charset
			} else if isRFC2978MIMECharset(c) {
				paramNameStart = p
				s = cdCharset
			} else if !isLwsChar(c) {
				return "", false
			}

		case cdCharset:
			if c == '\'' {
				paramNameEnd = p
				filenameCharset = strings.ToLower(header[paramNameStart:paramNameEnd])
				s = cdLanguage
			} else if !isRFC2978MIMECharset(c) {
				return "", false
			}

		case cdLanguage:
			if c == '\'' {
				if inFileParm {
					sb.Reset()
				}
				s = cdExtValueChars
			} else if c != '-' && !isAlpha(c) && !isDigit(c) {
				return "", false
			}

		case cdExtValueChars:
			if isRFC5987AttrChar(c) {
				if inFileParm {
					sb.WriteByte(c)
				}
			} else if c == '%' {
				pctVal = 0
				s = cdExtPct1
			} else if c == ';' {
				if inFileParm {
					fileNameFound = true
				}
				s = cdBeforeParamName
			} else if isLwsChar(c) {
				if inFileParm {
					fileNameFound = true
				}
				s = cdAfterValue
			} else {
				return "", false
			}

		case cdExtPct1:
			if isHex(c) {
				pctVal = hexVal(c) << 4
				s = cdExtPct2
			} else {
				return "", false
			}

		case cdExtPct2:
			if isHex(c) {
				pctVal |= hexVal(c)
				if inFileParm {
					sb.WriteByte(pctVal)
				}
				s = cdExtValueChars
			} else {
				return "", false
			}

		case cdFinished:
			return "", false
		}

		p++
	}

	// Validate final state per C++ behavior.
	// cdBeforeParamName is accepted for robustness (trailing semicolons).
	switch s {
	case cdBeforeType, cdAfterType, cdType, cdAfterValue, cdToken, cdExtValueChars, cdBeforeParamName:
		// OK
	default:
		return "", false
	}

	// Set fileNameFound if we ended in a value-producing state
	if s == cdExtValueChars && inFileParm {
		fileNameFound = true
	}
	if s == cdToken && inFileParm {
		fileNameFound = true
	}

	if !fileNameFound {
		return "", false
	}

	filename = sb.String()

	// aria2 treats plain filename parameters as ISO-8859-1 unless
	// --content-disposition-default-utf8=true is enabled.
	if filenameCharset == "iso-8859-1" || (filenameCharset == "" && !defaultUTF8) {
		filename = iso88591ToUTF8(filename)
	}

	// Directory traversal: strip path separators
	if strings.ContainsAny(filename, "/\\") {
		return "", false
	}

	return filename, true
}

func isRFC2616Token(c byte) bool {
	if c <= 31 || c == 127 {
		return false
	}
	switch c {
	case '(', ')', '<', '>', '@', ',', ';', ':', '\\', '"', '/', '[', ']', '?', '=', '{', '}', ' ', '\t':
		return false
	}
	return true
}

func isLwsChar(c byte) bool {
	return c == ' ' || c == '\t'
}

func isQDText(c byte) bool {
	return c > 31 && c != 127
}

func isRFC2978MIMECharset(c byte) bool {
	return isRFC2616Token(c)
}

func isRFC5987AttrChar(c byte) bool {
	if isAlpha(c) || isDigit(c) {
		return true
	}
	switch c {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

func iso88591ToUTF8(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 2)
	for i := 0; i < len(s); i++ {
		r := rune(s[i])
		if r < 128 {
			b.WriteByte(byte(r))
		} else {
			var buf [utf8.UTFMax]byte
			n := utf8.EncodeRune(buf[:], r)
			b.Write(buf[:n])
		}
	}
	return b.String()
}
