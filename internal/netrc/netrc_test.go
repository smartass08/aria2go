package netrc

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func findEntry(entries []Entry, machine string) *Entry {
	for i := range entries {
		if entries[i].Machine == machine {
			return &entries[i]
		}
	}
	return nil
}

func TestParseBasicMachine(t *testing.T) {
	input := `machine example.com login alice password secret`
	entries, def, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if def != nil {
		t.Fatal("unexpected default entry")
	}
	e := findEntry(entries, "example.com")
	if e == nil {
		t.Fatal("expected entry for example.com")
	}
	if e.Machine != "example.com" {
		t.Errorf("Machine = %q, want %q", e.Machine, "example.com")
	}
	if e.Login != "alice" {
		t.Errorf("Login = %q, want %q", e.Login, "alice")
	}
	if e.Password != "secret" {
		t.Errorf("Password = %q, want %q", e.Password, "secret")
	}
	if e.Account != "" {
		t.Errorf("Account = %q, want empty", e.Account)
	}
}

func TestParseWithAccount(t *testing.T) {
	input := `machine ftp.example.com login bob password p4ss account ftp`
	entries, def, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if def != nil {
		t.Fatal("unexpected default entry")
	}
	e := findEntry(entries, "ftp.example.com")
	if e == nil {
		t.Fatal("expected entry for ftp.example.com")
	}
	if e.Account != "ftp" {
		t.Errorf("Account = %q, want %q", e.Account, "ftp")
	}
	if e.Login != "bob" {
		t.Errorf("Login = %q, want %q", e.Login, "bob")
	}
}

func TestParseDefaultEntry(t *testing.T) {
	input := `default login anonymous password guest`
	entries, def, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %d machine entries, want 0", len(entries))
	}
	if def == nil {
		t.Fatal("expected default entry")
	}
	if def.Login != "anonymous" {
		t.Errorf("Login = %q, want %q", def.Login, "anonymous")
	}
	if def.Password != "guest" {
		t.Errorf("Password = %q, want %q", def.Password, "guest")
	}
}

func TestParseMultipleEntriesMultiline(t *testing.T) {
	input := `
machine a.example.com
login alice
password aaa

machine b.example.com
login bob
password bbb

default login anonymous password guest
`
	entries, def, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d machine entries, want 2", len(entries))
	}
	if def == nil {
		t.Fatal("expected default entry")
	}
	e := findEntry(entries, "a.example.com")
	if e == nil {
		t.Fatal("expected entry for a.example.com")
	}
	if e.Login != "alice" {
		t.Errorf("a.example.com login = %q, want alice", e.Login)
	}
	if e.Password != "aaa" {
		t.Errorf("a.example.com password = %q, want aaa", e.Password)
	}
	e = findEntry(entries, "b.example.com")
	if e == nil {
		t.Fatal("expected entry for b.example.com")
	}
	if e.Login != "bob" {
		t.Errorf("b.example.com login = %q, want bob", e.Login)
	}
	if def.Login != "anonymous" {
		t.Errorf("default login = %q, want anonymous", def.Login)
	}
}

func TestParseCommentsAndBlankLines(t *testing.T) {
	input := `
# This is a comment
machine example.com login alice password secret

# Another comment
default login anonymous password guest
`
	entries, def, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d machine entries, want 1", len(entries))
	}
	if def == nil {
		t.Fatal("expected default entry")
	}
}

func TestParseInlineComment(t *testing.T) {
	// C++ checks line[0] == '#', no trimming. So "#" inside a line
	// that starts with non-# is NOT a comment in aria2 C++.
	// However, "machine example.com # comment" splits on whitespace,
	// producing tokens: machine, example.com, #, comment.
	// The '#' token would be an unexpected token (GET_TOKEN state error).
	input := `machine example.com # not a comment`
	_, _, err := Parse(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for unexpected token '#'")
	}
}

func TestParseMalformedMachineToken(t *testing.T) {
	input := `machine`
	_, _, err := Parse(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for EOF where a token expected")
	}
	if !strings.Contains(err.Error(), "token expected") {
		t.Errorf("error %q should mention expected token", err)
	}
}

func TestParseUnknownToken(t *testing.T) {
	// "foo" is not a recognized keyword, not preceded by machine/default.
	input := `foo bar`
	_, _, err := Parse(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for unknown token")
	}
	if !strings.Contains(err.Error(), "expected") {
		t.Errorf("error %q should mention expected keyword", err)
	}
}

func TestParseDanglingToken(t *testing.T) {
	// "extra" is a dangling token — in GET_TOKEN state after entry tokens.
	// C++ would error because "extra" is not a known keyword.
	input := `machine example.com login alice extra`
	_, _, err := Parse(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for dangling/unexpected token")
	}
	if !strings.Contains(err.Error(), "expected") {
		t.Errorf("error %q should mention expected keyword", err)
	}
}

func TestParseUnexpectedMachineInEntry(t *testing.T) {
	// "machine" inside an entry starts a new entry in C++.
	// After "machine a.com", we're in GET_TOKEN state. Next token
	// "machine" triggers storeAuthenticator of existing + new entry.
	// Then "b.com" sets the machine name for the second entry.
	input := `machine a.com machine b.com`
	entries, _, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Machine != "a.com" {
		t.Errorf("first entry machine = %q, want a.com", entries[0].Machine)
	}
	if entries[1].Machine != "b.com" {
		t.Errorf("second entry machine = %q, want b.com", entries[1].Machine)
	}
}

func TestParseEmptyInput(t *testing.T) {
	entries, def, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %d entries, want 0", len(entries))
	}
	if def != nil {
		t.Fatal("expected no default entry")
	}
}

func TestParsePasswordOnly(t *testing.T) {
	input := `machine example.com password secret`
	entries, def, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if def != nil {
		t.Fatal("unexpected default entry")
	}
	e := findEntry(entries, "example.com")
	if e == nil {
		t.Fatal("expected entry for example.com")
	}
	if e.Password != "secret" {
		t.Errorf("Password = %q, want secret", e.Password)
	}
	if e.Login != "" {
		t.Errorf("Login = %q, want empty", e.Login)
	}
}

func TestParseMacdef(t *testing.T) {
	// macdef consumes all subsequent lines until blank line or EOF.
	// After macdef, blank line ends it, then normal parsing resumes.
	input := `machine example.com login alice macdef init
commands go here
more commands

password secret`
	entries, _, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	e := findEntry(entries, "example.com")
	if e == nil {
		t.Fatal("expected entry for example.com")
	}
	if e.Login != "alice" {
		t.Errorf("Login = %q, want alice", e.Login)
	}
	if e.Password != "secret" {
		t.Errorf("Password = %q, want secret", e.Password)
	}
}

func TestParseMacdefReadError(t *testing.T) {
	// I/O error during macdef body scanning must be propagated (not silently dropped).
	// C++ skipMacdef throws DL_ABORT_EX("Netrc:I/O error.") when stream enters
	// error state after getLine() returns a non-empty line.
	firstLine := strings.NewReader("machine example.com login alice macdef init\ncommands")
	errReader := &stubReader{err: errors.New("simulated I/O error")}
	r := io.MultiReader(firstLine, errReader)
	_, _, err := Parse(r)
	if err == nil {
		t.Fatal("expected I/O error during macdef parsing")
	}
}

type stubReader struct {
	err error
}

func (r *stubReader) Read(p []byte) (int, error) {
	return 0, r.err
}

func TestParseMacdefEofEnds(t *testing.T) {
	// macdef body extends to EOF. The machine entry still gets stored
	// with whatever tokens were set before macdef.
	input := `machine example.com login alice macdef init
commands go here`
	entries, _, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	e := findEntry(entries, "example.com")
	if e == nil {
		t.Fatal("expected entry for example.com")
	}
	if e.Login != "alice" {
		t.Errorf("Login = %q, want alice", e.Login)
	}
	if e.Password != "" {
		t.Errorf("Password = %q, want empty", e.Password)
	}
}

func TestLoadDefaultEmptyWhenMissing(t *testing.T) {
	entries, def, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault failed: %v", err)
	}
	if entries == nil {
		t.Fatal("expected non-nil entries slice even when file missing")
	}
	if def != nil {
		t.Fatal("expected nil default entry when file missing")
	}
}

func TestLoadDefaultWithTempFile(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	netrcPath := filepath.Join(tmpDir, ".netrc")
	content := "machine test.example.com login tester password testpass\n"
	if err := os.WriteFile(netrcPath, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, def, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault failed: %v", err)
	}
	if def != nil {
		t.Fatal("unexpected default entry")
	}
	e := findEntry(entries, "test.example.com")
	if e == nil {
		t.Fatal("expected entry for test.example.com")
	}
	if e.Login != "tester" || e.Password != "testpass" {
		t.Errorf("entry = %+v, want login=tester password=testpass", e)
	}
}

func TestParseLineNumberInError(t *testing.T) {
	input := "# comment\n\nmachine example.com login ok password ok\n\nmachine\n"
	_, _, err := Parse(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "token expected") {
		t.Errorf("error %q should mention expected token", err)
	}
}

func TestParseLoginWithoutMachine(t *testing.T) {
	input := `login myuser`
	_, _, err := Parse(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for login without preceding machine or default")
	}
	if !strings.Contains(err.Error(), "without preceding") {
		t.Errorf("error %q should mention missing machine/default", err)
	}
}

func TestParseSampleNetrc(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.netrc")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	entries, def, err := Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 machine entries, got %d", len(entries))
	}
	e := findEntry(entries, "ftp.example.com")
	if e == nil {
		t.Fatal("missing ftp.example.com")
	}
	if e.Login != "alice" || e.Password != "s3cret" || e.Account != "ftp" {
		t.Errorf("ftp.example.com entry = %+v", e)
	}
	e2 := findEntry(entries, "api.example.net")
	if e2 == nil {
		t.Fatal("missing api.example.net")
	}
	if e2.Login != "bob" || e2.Password != "p4ssw0rd" {
		t.Errorf("api.example.net entry = %+v", e2)
	}
	if def == nil || def.Login != "anonymous" || def.Password != "guest" {
		t.Errorf("default entry = %+v", def)
	}
}
