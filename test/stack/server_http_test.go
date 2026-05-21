package stack

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// generateSelfSignedCert generates a self-signed key/cert pair in memory.
func generateSelfSignedCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Aria2go Integration stress-test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	var certPEM, keyPEM bytes.Buffer
	pem.Encode(&certPEM, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	pem.Encode(&keyPEM, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	return tls.X509KeyPair(certPEM.Bytes(), keyPEM.Bytes())
}

var (
	stackBinOnce sync.Once
	stackBinPath string
	stackBinErr  error
)

// findAria2goBinary rebuilds aria2go and returns the candidate binary under test.
func findAria2goBinary(t *testing.T) string {
	t.Helper()

	stackBinOnce.Do(func() {
		stackBinPath, stackBinErr = buildAria2goBinary()
	})
	if stackBinErr != nil {
		t.Fatalf("build aria2go stack-test binary: %v", stackBinErr)
	}
	return stackBinPath
}

func buildAria2goBinary() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find project root from %s", wd)
		}
		dir = parent
	}
	binPath := filepath.Join(os.TempDir(), "aria2go-test-stack", "aria2go")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		return "", fmt.Errorf("mkdir stack binary dir: %w", err)
	}
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/aria2go/")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build ./cmd/aria2go/: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return binPath, nil
}

// findAria2cBinary returns the official reference binary if available.
func findAria2cBinary() (string, error) {
	if p, err := exec.LookPath("aria2c"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("aria2c not found in PATH")
}

func TestHTTP_Download_Stress(t *testing.T) {
	bin := findAria2goBinary(t)

	// Set up plain HTTP mock server
	testPayload := make([]byte, 1024*1024*5) // 5 MB of random-like data
	for i := range testPayload {
		testPayload[i] = byte(i % 256)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/file.bin", func(w http.ResponseWriter, r *http.Request) {
		// Basic auth check
		if r.URL.Query().Get("auth") == "basic" {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "user" || pass != "secret" {
				w.Header().Set("WWW-Authenticate", `Basic realm="restricted"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		// Cookie check
		if r.URL.Query().Get("cookie") == "check" {
			cookie, err := r.Cookie("session")
			if err != nil || cookie.Value != "active" {
				http.Error(w, "Forbidden cookie", http.StatusForbidden)
				return
			}
		}

		// Custom Header check
		if r.URL.Query().Get("header") == "check" {
			if r.Header.Get("X-Aria2go-Test") != "verified" {
				http.Error(w, "Forbidden custom header", http.StatusForbidden)
				return
			}
		}

		// Range support
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")

		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(testPayload)))
			w.Write(testPayload)
			return
		}

		// Parse simple range header
		if !strings.HasPrefix(rangeHeader, "bytes=") {
			http.Error(w, "Invalid range", http.StatusBadRequest)
			return
		}
		parts := strings.Split(rangeHeader[6:], "-")
		start, _ := strconv.Atoi(parts[0])
		end := len(testPayload) - 1
		if parts[1] != "" {
			end, _ = strconv.Atoi(parts[1])
		}

		if start >= len(testPayload) || end >= len(testPayload) || start > end {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(testPayload)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(testPayload[start : end+1])
	})

	srv := &http.Server{
		Handler: mux,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())

	// Run concurrency stress test (1 to 20 concurrent downloads)
	concurrencies := []int{1, 5, 10, 20}
	for _, concat := range concurrencies {
		t.Run(fmt.Sprintf("Concurrency_%d", concat), func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "aria2go-http-stress-*")
			if err != nil {
				t.Fatalf("tempdir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			var wg sync.WaitGroup
			errs := make(chan error, concat)

			startTime := time.Now()
			for i := 0; i < concat; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					outFile := filepath.Join(tempDir, fmt.Sprintf("file_%d.bin", idx))
					cmd := exec.Command(bin,
						fmt.Sprintf("http://127.0.0.1:%d/file.bin", port),
						"-d", tempDir,
						"-o", fmt.Sprintf("file_%d.bin", idx),
						"--quiet=true",
					)
					var stderr bytes.Buffer
					cmd.Stderr = &stderr
					if err := cmd.Run(); err != nil {
						errs <- fmt.Errorf("download %d failed: %v (stderr: %s)", idx, err, stderr.String())
						return
					}

					// Validate content
					downloaded, err := os.ReadFile(outFile)
					if err != nil {
						errs <- fmt.Errorf("read file %d failed: %v", idx, err)
						return
					}
					if !bytes.Equal(downloaded, testPayload) {
						errs <- fmt.Errorf("mismatch on file %d", idx)
						return
					}
				}(i)
			}
			wg.Wait()
			close(errs)

			elapsed := time.Since(startTime)
			t.Logf("Downloaded %d files concurrently in %v", concat, elapsed)

			for err := range errs {
				t.Errorf("Error occurred: %v", err)
			}
		})
	}
}

func TestHTTP_Download_And_Auth_Cookies(t *testing.T) {
	bin := findAria2goBinary(t)

	testPayload := []byte("Hello World of Aria2go! Cookies, Custom Headers, and Authentication under heavy stress!")

	mux := http.NewServeMux()
	mux.HandleFunc("/secure.txt", func(w http.ResponseWriter, r *http.Request) {
		// Basic auth check
		user, pass, ok := r.BasicAuth()
		if !ok || user != "secure_user" || pass != "strong_pass" {
			w.Header().Set("WWW-Authenticate", `Basic realm="secure"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(testPayload)))
		w.Write(testPayload)
	})

	srv := &http.Server{
		Handler: mux,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())

	tempDir, err := os.MkdirTemp("", "aria2go-http-auth-*")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	outFile := filepath.Join(tempDir, "secure_download.txt")

	// Prepare commands
	logFile := filepath.Join(tempDir, "aria2go.log")
	args := []string{
		fmt.Sprintf("http://127.0.0.1:%d/secure.txt", port),
		"-d", tempDir,
		"-o", "secure_download.txt",
		"--http-user=secure_user",
		"--http-passwd=strong_pass",
		"--header=Cookie: auth_session=valid_token",
		"--header=X-Aria2-Test-Auth: passed",
		"--log=" + logFile,
	}

	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		logContent, _ := os.ReadFile(logFile)
		t.Fatalf("aria2go failed: %v (stdout: %s, stderr: %s, log: %s)", err, stdout.String(), stderr.String(), string(logContent))
	}

	downloaded, err := os.ReadFile(outFile)
	if err != nil {
		logContent, _ := os.ReadFile(logFile)
		t.Fatalf("read download: %v (stdout: %s, stderr: %s, log: %s)", err, stdout.String(), stderr.String(), string(logContent))
	}

	if !bytes.Equal(downloaded, testPayload) {
		t.Fatalf("payload mismatch:\ngot:  %q\nwant: %q", string(downloaded), string(testPayload))
	}
}
