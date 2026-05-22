package conformance

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSFTPPasswordAuthDownloadParity(t *testing.T) {
	sftpSkipIfNoReference(t)

	payload := sftpPayload("password-auth-download", 96*1024+17)
	refFixture := startSFTPFixture(t, map[string][]byte{"/fixture.bin": payload})
	implFixture := startSFTPFixture(t, map[string][]byte{"/fixture.bin": payload})

	refDir := t.TempDir()
	implDir := t.TempDir()
	ref := sftpRun(t, true, sftpArgs(refDir, "fixture.bin", refFixture.URL("/fixture.bin")))
	impl := sftpRun(t, false, sftpArgs(implDir, "fixture.bin", implFixture.URL("/fixture.bin")))

	AssertEqualExit(t, ref, impl)
	sftpRequireExitZero(t, "ref SFTP", ref)
	sftpRequireExitZero(t, "impl SFTP", impl)
	sftpRequireFile(t, filepath.Join(refDir, "fixture.bin"), payload)
	sftpRequireFile(t, filepath.Join(implDir, "fixture.bin"), payload)
	sftpRequirePasswordAuth(t, "ref SFTP", refFixture)
	sftpRequirePasswordAuth(t, "impl SFTP", implFixture)
	sftpRequireRequestedPath(t, "ref SFTP", refFixture, "/fixture.bin")
	sftpRequireRequestedPath(t, "impl SFTP", implFixture, "/fixture.bin")
}

func TestSFTPHostKeyDigestParity(t *testing.T) {
	sftpSkipIfNoReference(t)

	t.Run("success", func(t *testing.T) {
		payload := sftpPayload("host-key-digest-success", 48*1024+9)
		refFixture := startSFTPFixture(t, map[string][]byte{"/digest.bin": payload})
		implFixture := startSFTPFixture(t, map[string][]byte{"/digest.bin": payload})

		refDir := t.TempDir()
		implDir := t.TempDir()
		refArgs := sftpArgs(refDir, "digest.bin", refFixture.URL("/digest.bin"),
			"--ssh-host-key-md=sha-1="+refFixture.HostKeyDigest("sha-1"))
		implArgs := sftpArgs(implDir, "digest.bin", implFixture.URL("/digest.bin"),
			"--ssh-host-key-md=sha-1="+implFixture.HostKeyDigest("sha-1"))

		ref := sftpRun(t, true, refArgs)
		impl := sftpRun(t, false, implArgs)

		AssertEqualExit(t, ref, impl)
		sftpRequireExitZero(t, "ref SFTP digest", ref)
		sftpRequireExitZero(t, "impl SFTP digest", impl)
		sftpRequireFile(t, filepath.Join(refDir, "digest.bin"), payload)
		sftpRequireFile(t, filepath.Join(implDir, "digest.bin"), payload)
	})

	t.Run("failure", func(t *testing.T) {
		payload := sftpPayload("host-key-digest-failure", 16*1024+3)
		refFixture := startSFTPFixture(t, map[string][]byte{"/digest.bin": payload})
		implFixture := startSFTPFixture(t, map[string][]byte{"/digest.bin": payload})

		refDir := t.TempDir()
		implDir := t.TempDir()
		refArgs := sftpArgs(refDir, "digest.bin", refFixture.URL("/digest.bin"),
			"--ssh-host-key-md=sha-1="+sftpWrongDigest(refFixture.HostKeyDigest("sha-1")))
		implArgs := sftpArgs(implDir, "digest.bin", implFixture.URL("/digest.bin"),
			"--ssh-host-key-md=sha-1="+sftpWrongDigest(implFixture.HostKeyDigest("sha-1")))

		ref := sftpRun(t, true, refArgs)
		impl := sftpRun(t, false, implArgs)

		if ref.ExitCode == 0 {
			t.Fatalf("ref SFTP digest failure unexpectedly succeeded\nstdout=%s\nstderr=%s", ref.Stdout, ref.Stderr)
		}
		AssertEqualExit(t, ref, impl)
		if impl.ExitCode == 0 {
			t.Fatalf("impl SFTP digest failure unexpectedly succeeded\nstdout=%s\nstderr=%s", impl.Stdout, impl.Stderr)
		}
	})
}

func TestSFTPPercentDecodedPathParity(t *testing.T) {
	sftpSkipIfNoReference(t)

	const name = "space # file.bin"
	payload := sftpPayload("percent-decoded-path", 40*1024+11)
	refFixture := startSFTPFixture(t, map[string][]byte{"/" + name: payload})
	implFixture := startSFTPFixture(t, map[string][]byte{"/" + name: payload})

	refDir := t.TempDir()
	implDir := t.TempDir()
	ref := sftpRun(t, true, sftpArgs(refDir, "", refFixture.URL("/"+name)))
	impl := sftpRun(t, false, sftpArgs(implDir, "", implFixture.URL("/"+name)))

	AssertEqualExit(t, ref, impl)
	sftpRequireExitZero(t, "ref SFTP percent-decoded path", ref)
	sftpRequireExitZero(t, "impl SFTP percent-decoded path", impl)
	sftpRequireFile(t, filepath.Join(refDir, name), payload)
	sftpRequireFile(t, filepath.Join(implDir, name), payload)
	sftpRequireRequestedPath(t, "ref SFTP percent-decoded path", refFixture, "/"+name)
	sftpRequireRequestedPath(t, "impl SFTP percent-decoded path", implFixture, "/"+name)
}

func TestSFTPRemoteTimeParity(t *testing.T) {
	sftpSkipIfNoReference(t)

	payload := sftpPayload("remote-time", 52*1024+5)
	refFixture := startSFTPFixture(t, map[string][]byte{"/remote-time.bin": payload})
	implFixture := startSFTPFixture(t, map[string][]byte{"/remote-time.bin": payload})

	refDir := t.TempDir()
	implDir := t.TempDir()
	ref := sftpRun(t, true, sftpArgs(refDir, "remote-time.bin", refFixture.URL("/remote-time.bin"), "--remote-time=true"))
	impl := sftpRun(t, false, sftpArgs(implDir, "remote-time.bin", implFixture.URL("/remote-time.bin"), "--remote-time=true"))

	AssertEqualExit(t, ref, impl)
	sftpRequireExitZero(t, "ref SFTP remote-time", ref)
	sftpRequireExitZero(t, "impl SFTP remote-time", impl)
	sftpRequireFile(t, filepath.Join(refDir, "remote-time.bin"), payload)
	sftpRequireFile(t, filepath.Join(implDir, "remote-time.bin"), payload)
	conformanceRequireFileModTimeNear(t, "ref SFTP remote-time", filepath.Join(refDir, "remote-time.bin"), time.Unix(sftpFixtureMTime, 0).UTC())
	conformanceRequireFileModTimeNear(t, "impl SFTP remote-time", filepath.Join(implDir, "remote-time.bin"), time.Unix(sftpFixtureMTime, 0).UTC())
}

func sftpSkipIfNoReference(t *testing.T) {
	t.Helper()
	if _, err := findRefBinary(); err != nil {
		t.Skipf("aria2c reference binary not available: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows aria2c reference builds are SFTP/libssh2 dependent and do not pass the local SFTP fixture in CI")
	}
}

func sftpRun(t *testing.T, ref bool, args []string) RunResult {
	t.Helper()

	opts := RunOptions{
		Timeout: 45 * time.Second,
		Env: []string{
			"http_proxy=",
			"HTTP_PROXY=",
			"https_proxy=",
			"HTTPS_PROXY=",
			"ftp_proxy=",
			"FTP_PROXY=",
			"all_proxy=",
			"ALL_PROXY=",
			"no_proxy=127.0.0.1,localhost",
			"NO_PROXY=127.0.0.1,localhost",
		},
	}
	var (
		result RunResult
		err    error
	)
	if ref {
		result, err = RunRefWithOptions(t, args, "", opts)
	} else {
		result, err = RunImplWithOptions(t, args, "", opts)
	}
	if err != nil {
		t.Fatalf("run ref=%v: %v\nstdout=%s\nstderr=%s", ref, err, result.Stdout, result.Stderr)
	}
	return result
}

func sftpArgs(dir, out, uri string, extra ...string) []string {
	args := []string{
		"--no-conf=true",
		"--dir=" + dir,
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--file-allocation=none",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--no-netrc=true",
		"--max-tries=1",
		"--connect-timeout=5",
		"--timeout=5",
	}
	if out != "" {
		args = append(args, "--out="+out)
	}
	args = append(args, extra...)
	args = append(args, uri)
	return args
}

func sftpPayload(label string, size int) []byte {
	seed := sha256.Sum256([]byte(label))
	out := make([]byte, size)
	for i := range out {
		out[i] = seed[i%len(seed)] ^ byte(i*17) ^ byte(i>>7)
	}
	return out
}

func sftpRequireExitZero(t *testing.T, label string, result RunResult) {
	t.Helper()
	if result.ExitCode != 0 {
		t.Fatalf("%s exit=%d\nstdout=%s\nstderr=%s", label, result.ExitCode, result.Stdout, result.Stderr)
	}
}

func sftpRequireFile(t *testing.T, file string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s bytes mismatch: got %d bytes want %d", file, len(got), len(want))
	}
}

func sftpRequirePasswordAuth(t *testing.T, label string, fixture *sftpFixture) {
	t.Helper()
	for _, auth := range fixture.AuthAttempts() {
		if auth.Method == "password" && auth.User == fixture.user && auth.Password == fixture.pass {
			return
		}
	}
	t.Fatalf("%s did not authenticate with expected password; attempts=%+v", label, fixture.AuthAttempts())
}

func sftpRequireRequestedPath(t *testing.T, label string, fixture *sftpFixture, want string) {
	t.Helper()
	for _, got := range fixture.RequestedPaths() {
		if got == want {
			if fixture.BytesServed() <= 0 {
				t.Fatalf("%s requested %s but served no bytes", label, want)
			}
			return
		}
	}
	t.Fatalf("%s did not request %s; paths=%v", label, want, fixture.RequestedPaths())
}

func sftpWrongDigest(digest string) string {
	if digest == "" {
		return digest
	}
	replacement := "0"
	if strings.HasSuffix(digest, "0") {
		replacement = "1"
	}
	return digest[:len(digest)-1] + replacement
}
