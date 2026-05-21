package engine

import (
	"testing"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/netrc"
)

func defaultOpts() *config.Options {
	return &config.Options{}
}

func TestAuthConfig_New(t *testing.T) {
	ac := NewAuthConfig("user", "pass")
	if ac == nil {
		t.Fatal("NewAuthConfig returned nil")
	}
	if ac.User() != "user" {
		t.Errorf("User = %q, want user", ac.User())
	}
	if ac.Password() != "pass" {
		t.Errorf("Password = %q, want pass", ac.Password())
	}
	if ac.GetAuthText() != "user:pass" {
		t.Errorf("GetAuthText = %q, want user:pass", ac.GetAuthText())
	}
}

func TestAuthConfig_New_EmptyUser(t *testing.T) {
	ac := NewAuthConfig("", "pass")
	if ac != nil {
		t.Error("NewAuthConfig with empty user should return nil")
	}
}

func TestCreateAuthConfig_HTTP_UserInURI(t *testing.T) {
	f := NewAuthConfigFactory()
	opts := defaultOpts()
	ac := f.CreateAuthConfig("http://user:pass@example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig, got nil")
	}
	if ac.User() != "user" {
		t.Errorf("User = %q, want user", ac.User())
	}
	if ac.Password() != "pass" {
		t.Errorf("Password = %q, want pass", ac.Password())
	}
}

func TestCreateAuthConfig_HTTP_UserInURI_NoChallenge(t *testing.T) {
	f := NewAuthConfigFactory()
	opts := &config.Options{
		HTTPAuthChallenge: false,
	}
	ac := f.CreateAuthConfig("http://admin:secret@example.com/path", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig, got nil")
	}
	if ac.User() != "admin" {
		t.Errorf("User = %q, want admin", ac.User())
	}
	if ac.Password() != "secret" {
		t.Errorf("Password = %q, want secret", ac.Password())
	}
}

func TestCreateAuthConfig_HTTP_WithChallenge_UserInURI(t *testing.T) {
	f := NewAuthConfigFactory()
	opts := &config.Options{
		HTTPAuthChallenge: true,
	}
	ac := f.CreateAuthConfig("http://user:pass@example.com/path", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig, got nil")
	}
	if ac.User() != "user" {
		t.Errorf("User = %q, want user", ac.User())
	}
}

func TestCreateAuthConfig_HTTP_WithChallenge_NoUser_NoCred(t *testing.T) {
	f := NewAuthConfigFactory()
	opts := &config.Options{
		HTTPAuthChallenge: true,
	}
	ac := f.CreateAuthConfig("http://example.com/path", opts)
	if ac != nil {
		t.Error("expected nil (no basic cred and no user in URI)")
	}
}

func TestCreateAuthConfig_HTTP_NetrcFallback(t *testing.T) {
	f := NewAuthConfigFactory()
	f.SetNetrc([]netrc.Entry{
		{Machine: "example.com", Login: "netrcuser", Password: "netrcpass"},
	}, nil)
	opts := defaultOpts()
	ac := f.CreateAuthConfig("http://example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig from netrc, got nil")
	}
	if ac.User() != "netrcuser" {
		t.Errorf("User = %q, want netrcuser", ac.User())
	}
	if ac.Password() != "netrcpass" {
		t.Errorf("Password = %q, want netrcpass", ac.Password())
	}
}

func TestCreateAuthConfig_HTTP_NetrcFallback_NoNetrc(t *testing.T) {
	f := NewAuthConfigFactory()
	f.SetNetrc([]netrc.Entry{
		{Machine: "other.com", Login: "netrcuser", Password: "netrcpass"},
	}, nil)
	opts := &config.Options{
		HTTPUser:   "optuser",
		HTTPPasswd: "optpass",
	}
	ac := f.CreateAuthConfig("http://example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig from http-user option, got nil")
	}
	if ac.User() != "optuser" {
		t.Errorf("User = %q, want optuser", ac.User())
	}
	if ac.Password() != "optpass" {
		t.Errorf("Password = %q, want optpass", ac.Password())
	}
}

func TestCreateAuthConfig_HTTP_NoNetrcOption(t *testing.T) {
	f := NewAuthConfigFactory()
	f.SetNetrc([]netrc.Entry{
		{Machine: "example.com", Login: "netrcuser", Password: "netrcpass"},
	}, nil)
	opts := &config.Options{
		NoNetrc:    true,
		HTTPUser:   "optuser",
		HTTPPasswd: "optpass",
	}
	ac := f.CreateAuthConfig("http://example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig from http-user, got nil")
	}
	if ac.User() != "optuser" {
		t.Errorf("User = %q, want optuser", ac.User())
	}
}

func TestCreateAuthConfig_FTP_AnonymousDefault(t *testing.T) {
	f := NewAuthConfigFactory()
	opts := defaultOpts()
	ac := f.CreateAuthConfig("ftp://ftp.example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected anonymous AuthConfig for FTP, got nil")
	}
	if ac.User() != "anonymous" {
		t.Errorf("User = %q, want anonymous", ac.User())
	}
	if ac.Password() != "ARIA2USER@" {
		t.Errorf("Password = %q, want ARIA2USER@", ac.Password())
	}
}

func TestCreateAuthConfig_FTP_UserInURI(t *testing.T) {
	f := NewAuthConfigFactory()
	opts := defaultOpts()
	ac := f.CreateAuthConfig("ftp://ftpuser:ftppass@ftp.example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig, got nil")
	}
	if ac.User() != "ftpuser" {
		t.Errorf("User = %q, want ftpuser", ac.User())
	}
	if ac.Password() != "ftppass" {
		t.Errorf("Password = %q, want ftppass", ac.Password())
	}
}

func TestCreateAuthConfig_FTP_UserInURI_NoPass_NetrcFallback(t *testing.T) {
	f := NewAuthConfigFactory()
	f.SetNetrc([]netrc.Entry{
		{Machine: "ftp.example.com", Login: "ftpuser", Password: "netrcpass"},
	}, nil)
	opts := defaultOpts()
	ac := f.CreateAuthConfig("ftp://ftpuser@ftp.example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig from netrc for matching user, got nil")
	}
	if ac.User() != "ftpuser" {
		t.Errorf("User = %q, want ftpuser", ac.User())
	}
	if ac.Password() != "netrcpass" {
		t.Errorf("Password = %q, want netrcpass", ac.Password())
	}
}

func TestCreateAuthConfig_FTP_UserInURI_NoPass_Fallthrough(t *testing.T) {
	f := NewAuthConfigFactory()
	opts := &config.Options{
		FTPPasswd: "ftppass",
	}
	ac := f.CreateAuthConfig("ftp://ftpuser@ftp.example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig, got nil")
	}
	if ac.User() != "ftpuser" {
		t.Errorf("User = %q, want ftpuser", ac.User())
	}
	if ac.Password() != "ftppass" {
		t.Errorf("Password = %q, want ftppass", ac.Password())
	}
}

func TestCreateAuthConfig_FTP_UserInURI_NoPass_NoNetrcMismatch(t *testing.T) {
	f := NewAuthConfigFactory()
	f.SetNetrc([]netrc.Entry{
		{Machine: "ftp.example.com", Login: "otheruser", Password: "otherpass"},
	}, nil)
	opts := &config.Options{
		FTPPasswd: "optpasswd",
	}
	// netrc has "otheruser" but URI has "ftpuser" — should not match
	ac := f.CreateAuthConfig("ftp://ftpuser@ftp.example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig from ftp-passwd fallback, got nil")
	}
	if ac.User() != "ftpuser" {
		t.Errorf("User = %q, want ftpuser", ac.User())
	}
	if ac.Password() != "optpasswd" {
		t.Errorf("Password = %q, want optpasswd", ac.Password())
	}
}

func TestCreateAuthConfig_FTP_NetrcWithUserDefined(t *testing.T) {
	f := NewAuthConfigFactory()
	f.SetNetrc([]netrc.Entry{
		{Machine: "ftp.example.com", Login: "netrcuser", Password: "netrcpass"},
	}, nil)
	opts := &config.Options{
		FTPUser:   "optuser",
		FTPPasswd: "optpass",
	}
	ac := f.CreateAuthConfig("ftp://ftp.example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig, got nil")
	}
	if ac.User() != "optuser" {
		t.Errorf("User = %q, want optuser (user-defined takes priority)", ac.User())
	}
	if ac.Password() != "optpass" {
		t.Errorf("Password = %q, want optpass", ac.Password())
	}
}

func TestCreateAuthConfig_FTP_NoNetrc_WithUserDefined(t *testing.T) {
	f := NewAuthConfigFactory()
	opts := &config.Options{
		NoNetrc:   true,
		FTPUser:   "optuser",
		FTPPasswd: "optpass",
	}
	ac := f.CreateAuthConfig("ftp://ftp.example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig, got nil")
	}
	if ac.User() != "optuser" {
		t.Errorf("User = %q, want optuser", ac.User())
	}
	if ac.Password() != "optpass" {
		t.Errorf("Password = %q, want optpass", ac.Password())
	}
}

func TestCreateAuthConfig_InvalidURI(t *testing.T) {
	f := NewAuthConfigFactory()
	ac := f.CreateAuthConfig("://invalid", defaultOpts())
	if ac != nil {
		t.Error("expected nil for invalid URI")
	}
}

func TestCreateAuthConfig_HTTP_NoAuth(t *testing.T) {
	f := NewAuthConfigFactory()
	opts := defaultOpts()
	ac := f.CreateAuthConfig("http://example.com/file.iso", opts)
	if ac != nil {
		t.Error("expected nil (no auth configured)")
	}
}

func TestCreateAuthConfig_HTTPS_NoAuth(t *testing.T) {
	f := NewAuthConfigFactory()
	opts := defaultOpts()
	ac := f.CreateAuthConfig("https://example.com/file.iso", opts)
	if ac != nil {
		t.Error("expected nil (no auth configured)")
	}
}

func TestCreateAuthConfig_HTTP_DefaultNetrcIsIgnored(t *testing.T) {
	f := NewAuthConfigFactory()
	f.SetNetrc(nil, &netrc.DefaultEntry{
		Login:    "defaultuser",
		Password: "defaultpass",
	})
	opts := defaultOpts()
	// HTTP resolver ignores netrc default token (ignoreDefault=true).
	// Only machine-specific entries match.
	ac := f.CreateAuthConfig("http://unknown.example.com/file.iso", opts)
	if ac != nil {
		t.Error("expected nil (default netrc token is ignored for HTTP)")
	}
}

func TestCreateAuthConfig_HTTP_NetrcWithChallenge_NoUser_NoCred(t *testing.T) {
	f := NewAuthConfigFactory()
	opts := &config.Options{
		HTTPAuthChallenge: true,
	}
	ac := f.CreateAuthConfig("http://example.com/path", opts)
	if ac != nil {
		t.Error("expected nil (no basic cred and no URI user)")
	}
}

func TestCreateAuthConfig_HTTP_MachineSpecificNetrcEntryWorks(t *testing.T) {
	f := NewAuthConfigFactory()
	f.SetNetrc([]netrc.Entry{
		{Machine: "example.com", Login: "netrcuser", Password: "netrcpass"},
	}, &netrc.DefaultEntry{
		Login:    "defaultuser",
		Password: "defaultpass",
	})
	opts := defaultOpts()
	// Machine-specific entry matches, default is ignored.
	ac := f.CreateAuthConfig("http://example.com/file.iso", opts)
	if ac == nil {
		t.Fatal("expected AuthConfig from machine-specific netrc entry, got nil")
	}
	if ac.User() != "netrcuser" {
		t.Errorf("User = %q, want netrcuser", ac.User())
	}
}

func TestBasicCred_PathSuffix(t *testing.T) {
	bc := newBasicCred("user", "pass", "host", "80", "path", true)
	if bc.path != "path/" {
		t.Errorf("path = %q, want path/", bc.path)
	}

	bc2 := newBasicCred("user", "pass", "host", "80", "path/", true)
	if bc2.path != "path/" {
		t.Errorf("path = %q, want path/", bc2.path)
	}

	bc3 := newBasicCred("user", "pass", "host", "80", "", true)
	if bc3.path != "/" {
		t.Errorf("path = %q, want /", bc3.path)
	}
}

func TestBasicCred_Less(t *testing.T) {
	a := newBasicCred("", "", "aaa.com", "80", "/a", false)
	b := newBasicCred("", "", "bbb.com", "80", "/a", false)
	if !a.less(b) {
		t.Error("aaa.com < bbb.com should be true")
	}

	// Same host, different port.
	c := newBasicCred("", "", "aaa.com", "80", "/a", false)
	d := newBasicCred("", "", "aaa.com", "8080", "/a", false)
	if !c.less(d) {
		t.Error("port 80 < 8080 should be true")
	}

	// Same host and port, different path (reverse order matching C++).
	e := newBasicCred("", "", "aaa.com", "80", "/a", false)
	f2 := newBasicCred("", "", "aaa.com", "80", "/b", false)
	if !f2.less(e) {
		t.Error("path /b > /a so /b should be less (reverse order)")
	}
}

func TestMatchHost(t *testing.T) {
	tests := []struct {
		machine string
		host    string
		want    bool
	}{
		{"", "anything", true},
		{"example.com", "example.com", true},
		{"example.com", "EXAMPLE.COM", true},
		{"example.com", "other.com", false},
		{"example.com", "sub.example.com", true},
		{"sub.example.com", "example.com", false},
	}
	for _, tt := range tests {
		if got := matchHost(tt.machine, tt.host); got != tt.want {
			t.Errorf("matchHost(%q, %q) = %v, want %v", tt.machine, tt.host, got, tt.want)
		}
	}
}
