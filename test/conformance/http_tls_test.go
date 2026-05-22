package conformance

import (
	"crypto/tls"
	"database/sql"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestHTTPEdge_HTTPSCertificateMatrix(t *testing.T) {
	SkipIfNoRef(t)

	payload := []byte("https tls payload\n")
	refTLS13DownloadSupported := refSupportsTLS13Download(t, payload)

	t.Run("check_certificate_false_allows_self_signed", func(t *testing.T) {
		refSrv := newHTTPSPayloadServer(t, payload, tls.VersionTLS12, tls.VersionTLS13)
		defer refSrv.Close()
		implSrv := newHTTPSPayloadServer(t, payload, tls.VersionTLS12, tls.VersionTLS13)
		defer implSrv.Close()
		refDir, implDir := t.TempDir(), t.TempDir()

		tc := CommandMatrixCase{
			Name:    "https_check_certificate_false",
			Timeout: 20 * time.Second,
			Env:     httpEdgeProxylessEnv(),
			ArgsFor: func(target RunnerTarget) []string {
				dir, rawURL := refDir, refSrv.URL+"/tls.bin"
				if target == RunnerImpl {
					dir, rawURL = implDir, implSrv.URL+"/tls.bin"
				}
				args := append(httpEdgeBaseArgs(dir, "tls.bin"), "--check-certificate=false")
				return append(args, rawURL)
			},
		}
		result := RunCommandPair(t, tc)

		AssertEqualExit(t, result.Ref, result.Impl)
		requireExitSuccess(t, "ref https check-certificate=false", result.Ref)
		requireExitSuccess(t, "impl https check-certificate=false", result.Impl)
		AssertFileBytes(t, filepath.Join(refDir, "tls.bin"), payload)
		AssertFileBytes(t, filepath.Join(implDir, "tls.bin"), payload)
	})

	t.Run("ca_certificate_trusts_self_signed_server", func(t *testing.T) {
		if refUsesAppleTLS(t) {
			t.Skip("reference aria2c uses AppleTLS; source-truth docs and AppleTLSContext disable --ca-certificate in favor of the OS trust store")
		}
		refSrv := newHTTPSPayloadServer(t, payload, tls.VersionTLS12, tls.VersionTLS13)
		defer refSrv.Close()
		implSrv := newHTTPSPayloadServer(t, payload, tls.VersionTLS12, tls.VersionTLS13)
		defer implSrv.Close()
		refDir, implDir := t.TempDir(), t.TempDir()
		refCA := filepath.Join(refDir, "ca.pem")
		implCA := filepath.Join(implDir, "ca.pem")
		writeServerCertPEM(t, refCA, refSrv)
		writeServerCertPEM(t, implCA, implSrv)

		tc := CommandMatrixCase{
			Name:    "https_ca_certificate",
			Timeout: 20 * time.Second,
			Env:     httpEdgeProxylessEnv(),
			ArgsFor: func(target RunnerTarget) []string {
				dir, rawURL, caPath := refDir, refSrv.URL+"/tls-ca.bin", refCA
				if target == RunnerImpl {
					dir, rawURL, caPath = implDir, implSrv.URL+"/tls-ca.bin", implCA
				}
				args := append(httpEdgeBaseArgs(dir, "tls-ca.bin"), "--ca-certificate="+caPath)
				return append(args, rawURL)
			},
		}
		result := RunCommandPair(t, tc)

		AssertEqualExit(t, result.Ref, result.Impl)
		requireExitSuccess(t, "ref https ca-certificate", result.Ref)
		requireExitSuccess(t, "impl https ca-certificate", result.Impl)
		AssertFileBytes(t, filepath.Join(refDir, "tls-ca.bin"), payload)
		AssertFileBytes(t, filepath.Join(implDir, "tls-ca.bin"), payload)
	})

	t.Run("min_tls_version_rejects_tls12_only_server", func(t *testing.T) {
		if !refTLS13DownloadSupported {
			t.Skip("reference aria2c cannot complete a TLS 1.3-only download in this environment, so min-tls-version conformance is not observable")
		}
		refSrv := newHTTPSPayloadServer(t, payload, tls.VersionTLS12, tls.VersionTLS12)
		defer refSrv.Close()
		implSrv := newHTTPSPayloadServer(t, payload, tls.VersionTLS12, tls.VersionTLS12)
		defer implSrv.Close()
		refDir, implDir := t.TempDir(), t.TempDir()

		tc := CommandMatrixCase{
			Name:    "https_min_tls_version_rejects_tls12",
			Timeout: 20 * time.Second,
			Env:     httpEdgeProxylessEnv(),
			ArgsFor: func(target RunnerTarget) []string {
				dir, rawURL := refDir, refSrv.URL+"/tls12.bin"
				if target == RunnerImpl {
					dir, rawURL = implDir, implSrv.URL+"/tls12.bin"
				}
				args := append(httpEdgeBaseArgs(dir, "tls12.bin"),
					"--check-certificate=false",
					"--min-tls-version=TLSv1.3",
				)
				return append(args, rawURL)
			},
		}
		result := RunCommandPair(t, tc)

		AssertEqualExit(t, result.Ref, result.Impl)
		if result.Ref.ExitCode == 0 {
			t.Fatalf("reference unexpectedly succeeded against TLS 1.2-only server\nstdout=%s\nstderr=%s", result.Ref.Stdout, result.Ref.Stderr)
		}
		AssertNoFile(t, filepath.Join(refDir, "tls12.bin"))
		AssertNoFile(t, filepath.Join(implDir, "tls12.bin"))
	})

	t.Run("min_tls_version_allows_tls13_server", func(t *testing.T) {
		if !refTLS13DownloadSupported {
			t.Skip("reference aria2c cannot complete a TLS 1.3-only download in this environment, so min-tls-version conformance is not observable")
		}
		refSrv := newHTTPSPayloadServer(t, payload, tls.VersionTLS13, tls.VersionTLS13)
		defer refSrv.Close()
		implSrv := newHTTPSPayloadServer(t, payload, tls.VersionTLS13, tls.VersionTLS13)
		defer implSrv.Close()
		refDir, implDir := t.TempDir(), t.TempDir()

		tc := CommandMatrixCase{
			Name:    "https_min_tls_version_allows_tls13",
			Timeout: 20 * time.Second,
			Env:     httpEdgeProxylessEnv(),
			ArgsFor: func(target RunnerTarget) []string {
				dir, rawURL := refDir, refSrv.URL+"/tls13.bin"
				if target == RunnerImpl {
					dir, rawURL = implDir, implSrv.URL+"/tls13.bin"
				}
				args := append(httpEdgeBaseArgs(dir, "tls13.bin"),
					"--check-certificate=false",
					"--min-tls-version=TLSv1.3",
				)
				return append(args, rawURL)
			},
		}
		result := RunCommandPair(t, tc)

		AssertEqualExit(t, result.Ref, result.Impl)
		requireExitSuccess(t, "ref https min-tls-version=TLSv1.3", result.Ref)
		requireExitSuccess(t, "impl https min-tls-version=TLSv1.3", result.Impl)
		AssertFileBytes(t, filepath.Join(refDir, "tls13.bin"), payload)
		AssertFileBytes(t, filepath.Join(implDir, "tls13.bin"), payload)
	})
}

func TestHTTPEdge_CookieLoadSQLiteFirefox(t *testing.T) {
	SkipIfNoRef(t)

	payload := []byte("sqlite cookie payload\n")
	refFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{
		payload:        payload,
		requiredCookie: "session=sqlite",
		setCookie:      "saved=from-sqlite; Path=/",
	})
	implFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{
		payload:        payload,
		requiredCookie: "session=sqlite",
		setCookie:      "saved=from-sqlite; Path=/",
	})
	refDir, implDir := t.TempDir(), t.TempDir()
	refCookieIn := filepath.Join(refDir, "cookies.sqlite")
	implCookieIn := filepath.Join(implDir, "cookies.sqlite")
	refCookieOut := filepath.Join(refDir, "cookies-out.txt")
	implCookieOut := filepath.Join(implDir, "cookies-out.txt")
	writeFirefoxCookieSQLite(t, refCookieIn, refFixture.server.URL, "session", "sqlite")
	writeFirefoxCookieSQLite(t, implCookieIn, implFixture.server.URL, "session", "sqlite")

	tc := CommandMatrixCase{
		Name:    "cookie_load_sqlite_firefox",
		Timeout: 20 * time.Second,
		Env:     httpEdgeProxylessEnv(),
		ArgsFor: func(target RunnerTarget) []string {
			dir, in, out, rawURL := refDir, refCookieIn, refCookieOut, refFixture.url(httpEdgePathCookie)
			if target == RunnerImpl {
				dir, in, out, rawURL = implDir, implCookieIn, implCookieOut, implFixture.url(httpEdgePathCookie)
			}
			args := append(httpEdgeBaseArgs(dir, "cookie-sqlite.bin"),
				"--load-cookies="+in,
				"--save-cookies="+out,
			)
			return append(args, rawURL)
		},
	}
	result := RunCommandPair(t, tc)

	AssertEqualExit(t, result.Ref, result.Impl)
	requireExitSuccess(t, "ref sqlite cookies", result.Ref)
	requireExitSuccess(t, "impl sqlite cookies", result.Impl)
	AssertFileBytes(t, filepath.Join(refDir, "cookie-sqlite.bin"), payload)
	AssertFileBytes(t, filepath.Join(implDir, "cookie-sqlite.bin"), payload)
	if !refFixture.sawCookie(httpEdgePathCookie, "session=sqlite") {
		t.Fatalf("reference did not send sqlite-loaded cookie; records=%#v", refFixture.recordsFor(httpEdgePathCookie))
	}
	if !implFixture.sawCookie(httpEdgePathCookie, "session=sqlite") {
		t.Fatalf("implementation did not send sqlite-loaded cookie; records=%#v", implFixture.recordsFor(httpEdgePathCookie))
	}
	requireCookieFileContains(t, refCookieOut, "saved", "from-sqlite")
	requireCookieFileContains(t, implCookieOut, "saved", "from-sqlite")
}

func refSupportsTLS13Download(t *testing.T, payload []byte) bool {
	t.Helper()

	refSrv := newHTTPSPayloadServer(t, payload, tls.VersionTLS13, tls.VersionTLS13)
	defer refSrv.Close()
	refDir := t.TempDir()
	args := append(httpEdgeBaseArgs(refDir, "tls13-probe.bin"),
		"--check-certificate=false",
		"--min-tls-version=TLSv1.3",
		refSrv.URL+"/tls13-probe.bin",
	)
	result, err := RunRefWithOptions(t, args, "", RunOptions{
		Env:     httpEdgeProxylessEnv(),
		Timeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("run reference TLS 1.3 probe: %v", err)
	}
	return result.ExitCode == 0
}

func newHTTPSPayloadServer(t *testing.T, payload []byte, minVersion, maxVersion uint16) *httptest.Server {
	t.Helper()

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if r.Method != http.MethodHead {
			_, _ = w.Write(payload)
		}
	}))
	srv.TLS = &tls.Config{
		MinVersion: minVersion,
		MaxVersion: maxVersion,
	}
	srv.StartTLS()
	return srv
}

func writeServerCertPEM(t *testing.T, path string, srv *httptest.Server) {
	t.Helper()

	if srv == nil || srv.Certificate() == nil {
		t.Fatal("server certificate is nil")
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write certificate %s: %v", path, err)
	}
}

func writeFirefoxCookieSQLite(t *testing.T, path, rawURL, name, value string) {
	t.Helper()

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("Parse(%q): %v", rawURL, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", path, err)
	}
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
		VALUES(?, '/', 0, 1893456000, ?, ?, 1700000000);
	`, u.Hostname(), name, value); err != nil {
		t.Fatalf("seed firefox sqlite cookie %s: %v", path, err)
	}
}
