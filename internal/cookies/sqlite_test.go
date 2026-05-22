package cookies

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"
)

func TestLoadFileFirefoxSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cookies.sqlite")
	db := openSQLiteTestDB(t, dbPath)
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE moz_cookies (
			host TEXT,
			path TEXT,
			isSecure INTEGER,
			expiry INTEGER,
			name TEXT,
			value TEXT,
			lastAccessed INTEGER
		);
		INSERT INTO moz_cookies(host, path, isSecure, expiry, name, value, lastAccessed)
		VALUES('.example.com', '/', 0, 1893456000, 'session', 'firefox-db', 1700000000);
	`); err != nil {
		t.Fatalf("seed firefox sqlite cookies: %v", err)
	}

	jar := New()
	if err := jar.LoadFile(dbPath); err != nil {
		t.Fatalf("LoadFile(firefox sqlite) error = %v", err)
	}

	u, _ := url.Parse("https://www.example.com/")
	got := jar.Cookies(u)
	if len(got) != 1 {
		t.Fatalf("Cookies() len = %d, want 1", len(got))
	}
	if got[0].Name != "session" || got[0].Value != "firefox-db" {
		t.Fatalf("cookie = %s=%s, want session=firefox-db", got[0].Name, got[0].Value)
	}
}

func TestLoadFileChromiumSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "Cookies")
	db := openSQLiteTestDB(t, dbPath)
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE cookies (
			host_key TEXT,
			path TEXT,
			secure INTEGER,
			expires_utc INTEGER,
			name TEXT,
			value TEXT,
			last_access_utc INTEGER
		);
		INSERT INTO cookies(host_key, path, secure, expires_utc, name, value, last_access_utc)
		VALUES('.example.com', '/', 1, ?, 'token', 'chromium-db', ?);
	`, chromeUnixMicros(1893456000), chromeUnixMicros(1700000000)); err != nil {
		t.Fatalf("seed chromium sqlite cookies: %v", err)
	}

	jar := New()
	if err := jar.LoadFile(dbPath); err != nil {
		t.Fatalf("LoadFile(chromium sqlite) error = %v", err)
	}

	httpsURL, _ := url.Parse("https://www.example.com/")
	got := jar.Cookies(httpsURL)
	if len(got) != 1 {
		t.Fatalf("Cookies(https) len = %d, want 1", len(got))
	}
	if got[0].Name != "token" || got[0].Value != "chromium-db" {
		t.Fatalf("cookie = %s=%s, want token=chromium-db", got[0].Name, got[0].Value)
	}

	httpURL, _ := url.Parse("http://www.example.com/")
	if plain := jar.Cookies(httpURL); len(plain) != 0 {
		t.Fatalf("Cookies(http) len = %d, want 0 for secure cookie", len(plain))
	}
}

func openSQLiteTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", path, err)
	}
	return db
}

func chromeUnixMicros(unixSeconds int64) int64 {
	return (unixSeconds + 11644473600) * 1_000_000
}
