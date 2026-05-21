package config

import (
	"fmt"
	"strconv"
	"strings"
)

const maxParameterizedValue = 65535

// ExpandParameterizedURIs expands aria2 parameterized URI patterns in order.
func ExpandParameterizedURIs(uris []string) ([]string, error) {
	var expanded []string
	for _, uri := range uris {
		values, err := ExpandParameterizedURI(uri)
		if err != nil {
			return nil, err
		}
		expanded = append(expanded, values...)
	}
	return expanded, nil
}

// ExpandParameterizedURI expands aria2's choice and inclusive range URI syntax.
func ExpandParameterizedURI(uri string) ([]string, error) {
	parts := []string{""}
	for pos := 0; pos < len(uri); {
		next := strings.IndexAny(uri[pos:], "{[")
		if next < 0 {
			appendLiteral(parts, uri[pos:])
			break
		}
		next += pos
		appendLiteral(parts, uri[pos:next])
		var err error
		switch uri[next] {
		case '{':
			parts, pos, err = expandChoice(parts, uri, next)
		case '[':
			parts, pos, err = expandRange(parts, uri, next)
		}
		if err != nil {
			return nil, err
		}
	}
	if len(parts) == 1 && parts[0] == "" {
		return nil, nil
	}
	return parts, nil
}

func appendLiteral(parts []string, literal string) {
	if literal == "" {
		return
	}
	for i := range parts {
		parts[i] += literal
	}
}

func expandChoice(parts []string, uri string, start int) ([]string, int, error) {
	end := strings.IndexByte(uri[start+1:], '}')
	if end < 0 {
		return nil, 0, fmt.Errorf("config: parameterized URI missing '}'")
	}
	end += start + 1
	choices := strings.Split(uri[start+1:end], ",")
	next := make([]string, 0, len(parts)*len(choices))
	for _, prefix := range parts {
		for _, choice := range choices {
			next = append(next, prefix+choice)
		}
	}
	return next, end + 1, nil
}

func expandRange(parts []string, uri string, start int) ([]string, int, error) {
	end := strings.IndexByte(uri[start+1:], ']')
	if end < 0 {
		return nil, 0, fmt.Errorf("config: parameterized URI missing ']'")
	}
	end += start + 1
	body := uri[start+1 : end]
	rangePart, step, err := splitParameterizedRange(body)
	if err != nil {
		return nil, 0, err
	}
	dash := strings.IndexByte(rangePart, '-')
	if dash <= 0 || dash == len(rangePart)-1 {
		return nil, 0, fmt.Errorf("config: parameterized URI range missing")
	}
	from := rangePart[:dash]
	to := rangePart[dash+1:]
	values, err := parameterizedRangeValues(from, to, step)
	if err != nil {
		return nil, 0, err
	}
	if len(values) > 0 {
		next := make([]string, 0, len(parts)*len(values))
		for _, prefix := range parts {
			for _, value := range values {
				next = append(next, prefix+value)
			}
		}
		parts = next
	}
	return parts, end + 1, nil
}

func splitParameterizedRange(body string) (string, int, error) {
	colon := strings.IndexByte(body, ':')
	if colon < 0 {
		return body, 1, nil
	}
	step64, err := strconv.ParseUint(body[colon+1:], 10, 32)
	if err != nil || step64 == 0 {
		return "", 0, fmt.Errorf("config: parameterized URI step must be positive")
	}
	if step64 > maxParameterizedValue {
		return "", 0, fmt.Errorf("config: parameterized URI step overflow")
	}
	return body[:colon], int(step64), nil
}

func parameterizedRangeValues(from, to string, step int) ([]string, error) {
	switch {
	case asciiDigits(from) && asciiDigits(to):
		return numericParameterizedValues(from, to, step)
	case sameLetterRange(from, to, 'a', 'z'):
		return alphaParameterizedValues(from, to, step, 'a')
	case sameLetterRange(from, to, 'A', 'Z'):
		return alphaParameterizedValues(from, to, step, 'A')
	default:
		return nil, fmt.Errorf("config: invalid parameterized URI range")
	}
}

func numericParameterizedValues(from, to string, step int) ([]string, error) {
	start, err := parseParameterizedUint(from)
	if err != nil {
		return nil, err
	}
	end, err := parseParameterizedUint(to)
	if err != nil {
		return nil, err
	}
	if start > end {
		return nil, nil
	}
	width := 0
	if len(from) == len(to) {
		width = len(from)
	}
	values := make([]string, 0, ((end-start)/step)+1)
	for n := start; n <= end; n += step {
		if width > 0 {
			values = append(values, fmt.Sprintf("%0*d", width, n))
		} else {
			values = append(values, strconv.Itoa(n))
		}
	}
	return values, nil
}

func parseParameterizedUint(s string) (int, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("config: parameterized URI range missing")
	}
	if n > maxParameterizedValue {
		return 0, fmt.Errorf("config: parameterized URI range overflow")
	}
	return int(n), nil
}

func alphaParameterizedValues(from, to string, step int, zero byte) ([]string, error) {
	start, err := fromBase26(from, zero)
	if err != nil {
		return nil, err
	}
	end, err := fromBase26(to, zero)
	if err != nil {
		return nil, err
	}
	if start > end {
		return nil, nil
	}
	width := 0
	if len(from) == len(to) {
		width = len(from)
	}
	values := make([]string, 0, ((end-start)/step)+1)
	for n := start; n <= end; n += step {
		values = append(values, toBase26(n, zero, width))
	}
	return values, nil
}

func fromBase26(s string, zero byte) (int, error) {
	n := 0
	for i := 0; i < len(s); i++ {
		n = n*26 + int(s[i]-zero)
		if n > maxParameterizedValue {
			return 0, fmt.Errorf("config: parameterized URI range overflow")
		}
	}
	return n, nil
}

func toBase26(n int, zero byte, width int) string {
	if n == 0 && width == 0 {
		width = 1
	}
	var b []byte
	for n > 0 {
		b = append(b, zero+byte(n%26))
		n /= 26
	}
	for len(b) < width {
		b = append(b, zero)
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

func asciiDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func sameLetterRange(a, b string, first, last byte) bool {
	return asciiLetters(a, first, last) && asciiLetters(b, first, last)
}

func asciiLetters(s string, first, last byte) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < first || s[i] > last {
			return false
		}
	}
	return true
}
