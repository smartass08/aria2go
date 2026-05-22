package conformance

// Task C.5 — ftp.advanced-options: ftp-reuse-connection conformance parity.
//
// With --ftp-reuse-connection=true --max-connection-per-server=1, aria2 is
// expected to reuse the same FTP control connection across multiple sequential
// file downloads from the same server.
//
// Single-file test: verifies basic success + exactly one accept.
// Two-file test: uses --input-file to enqueue two separate downloads from the
// same FTP server, then asserts parity in both exit code and accept count.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProtocol_FTPReuseConnectionParity verifies that a single-file FTP
// download with --ftp-reuse-connection=true succeeds for both binaries and
// opens exactly one control connection.
func TestProtocol_FTPReuseConnectionParity(t *testing.T) {
	SkipIfNoRef(t)

	payload1 := protocolPayload("ftp-reuse-connection-1", 48*1024+7)

	refFTP := startProtocolFTPFixture(t, map[string][]byte{"/file1.bin": payload1})
	implFTP := startProtocolFTPFixture(t, map[string][]byte{"/file1.bin": payload1})

	refDir := t.TempDir()
	implDir := t.TempDir()

	makeSingleArgs := func(dir string, ftp *protocolFTPFixture) []string {
		return []string{
			"--no-conf=true",
			"--dir=" + dir,
			"--allow-overwrite=true",
			"--auto-file-renaming=false",
			"--file-allocation=none",
			"--quiet=true",
			"--show-console-readout=false",
			"--summary-interval=0",
			"--no-netrc=true",
			"--check-certificate=false",
			"--no-proxy=127.0.0.1,localhost",
			"--enable-dht=false",
			"--enable-dht6=false",
			"--bt-enable-lpd=false",
			"--ftp-reuse-connection=true",
			"--max-connection-per-server=1",
			"--split=1",
			"--ftp-pasv=true",
			ftp.URL("/file1.bin"),
		}
	}

	ref := protocolRun(t, true, makeSingleArgs(refDir, refFTP))
	impl := protocolRun(t, false, makeSingleArgs(implDir, implFTP))

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref FTP reuse single", ref)
	protocolRequireExitZero(t, "impl FTP reuse single", impl)
	protocolRequireFile(t, filepath.Join(refDir, "file1.bin"), payload1)
	protocolRequireFile(t, filepath.Join(implDir, "file1.bin"), payload1)

	// Each single-file download must have opened exactly one control connection.
	if refFTP.AcceptCount() != 1 {
		t.Errorf("ref FTP reuse single: accept count = %d, want 1", refFTP.AcceptCount())
	}
	if implFTP.AcceptCount() != 1 {
		t.Errorf("impl FTP reuse single: accept count = %d, want 1", implFTP.AcceptCount())
	}
}

// TestProtocol_FTPReuseConnectionTwoFileParity downloads two files
// sequentially from the same FTP server using --input-file with
// --ftp-reuse-connection=true and verifies that:
//   - Both binaries exit 0 and produce both files correctly.
//   - Both binaries accept the same number of control connections
//     (connection-reuse parity).
func TestProtocol_FTPReuseConnectionTwoFileParity(t *testing.T) {
	SkipIfNoRef(t)

	payload1 := protocolPayload("ftp-reuse-two-file-1", 64*1024+3)
	payload2 := protocolPayload("ftp-reuse-two-file-2", 48*1024+17)

	refFTP := startProtocolFTPFixture(t, map[string][]byte{
		"/file1.bin": payload1,
		"/file2.bin": payload2,
	})
	implFTP := startProtocolFTPFixture(t, map[string][]byte{
		"/file1.bin": payload1,
		"/file2.bin": payload2,
	})

	refDir := t.TempDir()
	implDir := t.TempDir()

	// Write input files with two sequential download URIs.
	writeInputFile := func(t *testing.T, dir string, ftp *protocolFTPFixture) string {
		t.Helper()
		p := filepath.Join(dir, "downloads.txt")
		content := ftp.URL("/file1.bin") + "\n" + ftp.URL("/file2.bin") + "\n"
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write input file: %v", err)
		}
		return p
	}

	refInputFile := writeInputFile(t, refDir, refFTP)
	implInputFile := writeInputFile(t, implDir, implFTP)

	makeInputArgs := func(dir, inputFile string) []string {
		return []string{
			"--no-conf=true",
			"--dir=" + dir,
			"--allow-overwrite=true",
			"--auto-file-renaming=false",
			"--file-allocation=none",
			"--quiet=true",
			"--show-console-readout=false",
			"--summary-interval=0",
			"--no-netrc=true",
			"--check-certificate=false",
			"--no-proxy=127.0.0.1,localhost",
			"--enable-dht=false",
			"--enable-dht6=false",
			"--bt-enable-lpd=false",
			"--ftp-reuse-connection=true",
			"--max-connection-per-server=1",
			"--split=1",
			"--ftp-pasv=true",
			"--input-file=" + inputFile,
		}
	}

	refArgs := makeInputArgs(refDir, refInputFile)
	implArgs := makeInputArgs(implDir, implInputFile)

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref FTP reuse two-file", ref)
	protocolRequireExitZero(t, "impl FTP reuse two-file", impl)
	protocolRequireFile(t, filepath.Join(refDir, "file1.bin"), payload1)
	protocolRequireFile(t, filepath.Join(refDir, "file2.bin"), payload2)
	protocolRequireFile(t, filepath.Join(implDir, "file1.bin"), payload1)
	protocolRequireFile(t, filepath.Join(implDir, "file2.bin"), payload2)

	// Both binaries should have the same accept count (reuse parity).
	refAccept := refFTP.AcceptCount()
	implAccept := implFTP.AcceptCount()
	if refAccept != implAccept {
		t.Errorf("ftp-reuse-connection accept count mismatch: ref=%d impl=%d", refAccept, implAccept)
	}
}
