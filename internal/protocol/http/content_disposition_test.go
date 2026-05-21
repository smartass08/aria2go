package http

import (
	"testing"
)

func TestParseContentDispositionFilename(t *testing.T) {
	tests := []struct {
		header string
		want   string
		wantOK bool
	}{
		// Basic attachment with quoted filename
		{`attachment; filename="test.txt"`, "test.txt", true},
		// Basic inline with quoted filename
		{`inline; filename="image.png"`, "image.png", true},
		// Unquoted filename token
		{`attachment; filename=hello.zip`, "hello.zip", true},
		// filename* with UTF-8 encoding (RFC 5987)
		{`attachment; filename*=UTF-8''foo-%c3%a4.html`, "foo-ä.html", true},
		// filename* with ISO-8859-1 encoding
		{`attachment; filename*=iso-8859-1''foo%20bar.txt`, "foo bar.txt", true},
		// No filename parameter
		{`attachment`, "", false},
		// Empty header
		{"", "", false},
		// Empty filename (returned as empty string with ok=true)
		{`attachment; filename=""`, "", true},
		// filename* second, overrides empty filename
		{`attachment; filename=""; filename*=UTF-8''realname.pdf`, "realname.pdf", true},
		// Multiple semicolons (just whitespace after last semicolon)
		{`attachment; filename="file.txt"; size=12345`, "file.txt", true},
		// Whitespace handling
		{` attachment ; filename = "spaced.txt" `, "spaced.txt", true},
		// Path separators stripped (directory traversal protection)
		{`attachment; filename="/etc/passwd"`, "", false},
		{`attachment; filename="../../../etc/passwd"`, "", false},
		// Backslash in filename rejected (path traversal protection)
		{`attachment; filename="file\\name.txt"`, "", false},
		// Just basename is OK
		{`attachment; filename="justfile"`, "justfile", true},
		// Percent-encoded filename* with no charset prefix
		{`attachment; filename*=UTF-8''My%20Document.pdf`, "My Document.pdf", true},
		// filename* with percent encoding and unicode
		{`attachment; filename*=UTF-8''%e2%82%ac%20rates.txt`, "€ rates.txt", true},
		// Only disposition type, no params
		{`inline`, "", false},
		// RFC 5987 with language tag
		{`attachment; filename*=UTF-8'en'hello-world.txt`, "hello-world.txt", true},
		// filename*=iso-8859-1 with encoded char (ä in Latin-1)
		{`attachment; filename*=ISO-8859-1''%e4%20test.txt`, "ä test.txt", true},
	}

	for _, tt := range tests {
		filename, ok := ParseContentDisposition(tt.header)
		if ok != tt.wantOK {
			t.Errorf("ParseContentDisposition(%q): ok=%v, want ok=%v", tt.header, ok, tt.wantOK)
		}
		if ok && filename != tt.want {
			t.Errorf("ParseContentDisposition(%q): filename=%q, want=%q", tt.header, filename, tt.want)
		}
		if !ok && filename != "" {
			t.Errorf("ParseContentDisposition(%q): filename should be empty when ok=false, got=%q", tt.header, filename)
		}
	}
}

func TestParseContentDispositionEdgeCases(t *testing.T) {
	tests := []struct {
		header string
		want   string
		wantOK bool
	}{
		// Quoted-pair: single backslash before char — backslash is stripped
		{`attachment; filename="test\nt.txt"`, "testnt.txt", true},
		// Quoted-pair with double backslash — one backslash remains, path traversal rejects
		{`attachment; filename="test\\n.txt"`, "", false},
		// filename*= with unknown charset (treated as raw bytes)
		{`attachment; filename*=unknown''somefile.txt`, "somefile.txt", true},
		// Multiple params with filename at end
		{`attachment; size=1024; filename="end.txt"`, "end.txt", true},
		// filename with no value after equals (parser fails)
		{`attachment; filename=`, "", false},
		// Just semicolon after attachment (parser fails)
		{`attachment;`, "", false},
		// Trailing semicolon after complete param (Go accepts for robustness)
		{`attachment; filename="f1";`, "f1", true},
		// Spaces around equals in param (Go accepts for robustness)
		{`attachment; filename* = UTF-8''spaced.txt`, "spaced.txt", true},
		// No quotes, unquoted token filename
		{`attachment; filename=download.tar.gz`, "download.tar.gz", true},
	}

	for _, tt := range tests {
		filename, ok := ParseContentDisposition(tt.header)
		if ok != tt.wantOK {
			t.Errorf("ParseContentDisposition(%q): ok=%v, want ok=%v", tt.header, ok, tt.wantOK)
		}
		if ok && filename != tt.want {
			t.Errorf("ParseContentDisposition(%q): filename=%q, want=%q", tt.header, filename, tt.want)
		}
	}
}

func TestContentDispositionRealWorld(t *testing.T) {
	tests := []struct {
		header string
		want   string
		wantOK bool
	}{
		// Typical Apache/Nginx download response
		{`attachment; filename="aria2-1.37.0.tar.xz"`, "aria2-1.37.0.tar.xz", true},
		// S3/CDN with encoded filename
		{`attachment; filename*=UTF-8''report%202024.pdf`, "report 2024.pdf", true},
		// GitHub release asset
		{`attachment; filename=source.zip`, "source.zip", true},
		// Complex header with both filename and filename*
		{`attachment; filename="fallback.zip"; filename*=UTF-8''real%C3%A5me.zip`, "realåme.zip", true},
	}

	for _, tt := range tests {
		filename, ok := ParseContentDisposition(tt.header)
		if ok != tt.wantOK {
			t.Errorf("ParseContentDisposition(%q): ok=%v, want ok=%v", tt.header, ok, tt.wantOK)
		}
		if ok && filename != tt.want {
			t.Errorf("ParseContentDisposition(%q): filename=%q, want=%q", tt.header, filename, tt.want)
		}
	}
}

func TestISO88591ToUTF8(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"caf\xe9", "café"},
		{"M\xfcnchen", "München"},
		{"", ""},
	}

	for _, tt := range tests {
		got := iso88591ToUTF8(tt.input)
		if got != tt.want {
			t.Errorf("iso88591ToUTF8(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
