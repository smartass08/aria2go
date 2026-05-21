package engine

import (
	"net/url"
	"testing"

	"github.com/smartass08/aria2go/internal/config"
)

func TestFTPCredentialsPreferURLUserInfo(t *testing.T) {
	u, err := url.Parse("ftp://alice:secret@example.com/file")
	if err != nil {
		t.Fatal(err)
	}
	user, pass := ftpCredentials(u, &config.Options{
		FTPUser:   "option-user",
		FTPPasswd: "option-pass",
	}, &config.Options{
		FTPUser:   "global-user",
		FTPPasswd: "global-pass",
	})
	if user != "alice" || pass != "secret" {
		t.Fatalf("credentials = %q/%q, want alice/secret", user, pass)
	}
}

func TestFTPCredentialsFallbackToOptionsThenGlobal(t *testing.T) {
	u, err := url.Parse("ftp://example.com/file")
	if err != nil {
		t.Fatal(err)
	}
	user, pass := ftpCredentials(u, &config.Options{FTPUser: "option-user"}, &config.Options{
		FTPUser:   "global-user",
		FTPPasswd: "global-pass",
	})
	if user != "option-user" || pass != "global-pass" {
		t.Fatalf("credentials = %q/%q, want option-user/global-pass", user, pass)
	}
}
