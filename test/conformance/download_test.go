package conformance

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDownload_HTTPQuietFileParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := bytes.Repeat([]byte("aria2go-conformance-full-download\n"), 32*1024)
	srv := newHTTPPayloadServer(t, payload)
	defer srv.Close()

	refDir, implDir := t.TempDir(), t.TempDir()
	ref := runHTTPDownload(t, true, srv.URL+"/file.bin", refDir, "payload.bin", nil)
	impl := runHTTPDownload(t, false, srv.URL+"/file.bin", implDir, "payload.bin", nil)

	AssertEqualExit(t, ref, impl)
	requireDownloadedBytes(t, filepath.Join(refDir, "payload.bin"), payload)
	requireDownloadedBytes(t, filepath.Join(implDir, "payload.bin"), payload)
	requireQuietOutput(t, "ref", ref)
	requireQuietOutput(t, "impl", impl)
}

func TestDownload_HTTPRangeSplitParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := bytes.Repeat([]byte("0123456789abcdef"), 256*1024)
	refSrv := newRecordingHTTPPayloadServer(t, payload)
	defer refSrv.Close()
	implSrv := newRecordingHTTPPayloadServer(t, payload)
	defer implSrv.Close()

	extra := []string{
		"--split=4",
		"--max-connection-per-server=4",
		"--min-split-size=1M",
	}
	refDir, implDir := t.TempDir(), t.TempDir()
	ref := runHTTPDownload(t, true, refSrv.URL+"/file.bin", refDir, "range.bin", extra)
	impl := runHTTPDownload(t, false, implSrv.URL+"/file.bin", implDir, "range.bin", extra)

	AssertEqualExit(t, ref, impl)
	requireDownloadedBytes(t, filepath.Join(refDir, "range.bin"), payload)
	requireDownloadedBytes(t, filepath.Join(implDir, "range.bin"), payload)
	requireRangeRequested(t, "ref", refSrv.snapshot())
	requireRangeRequested(t, "impl", implSrv.snapshot())
}

func TestDownload_HTTPAuthAndHeaderParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := []byte("authenticated conformance payload\n")
	refSrv := newRecordingHTTPPayloadServer(t, payload)
	defer refSrv.Close()
	implSrv := newRecordingHTTPPayloadServer(t, payload)
	defer implSrv.Close()

	extra := []string{
		"--http-user=user",
		"--http-passwd=secret",
		"--http-auth-challenge=true",
		"--header=X-Conformance: yes",
	}
	refDir, implDir := t.TempDir(), t.TempDir()
	ref := runHTTPDownload(t, true, refSrv.URL+"/auth.bin", refDir, "auth.bin", extra)
	impl := runHTTPDownload(t, false, implSrv.URL+"/auth.bin", implDir, "auth.bin", extra)

	AssertEqualExit(t, ref, impl)
	requireDownloadedBytes(t, filepath.Join(refDir, "auth.bin"), payload)
	requireDownloadedBytes(t, filepath.Join(implDir, "auth.bin"), payload)
	requireAuthChallengeFlow(t, "ref", refSrv.snapshot())
	requireAuthChallengeFlow(t, "impl", implSrv.snapshot())
}

func TestDownload_ResultHideSuppressesConsoleOutput(t *testing.T) {
	SkipIfNoRef(t)

	payload := []byte("hidden result output\n")
	srv := newHTTPPayloadServer(t, payload)
	defer srv.Close()

	extra := []string{
		"--quiet=false",
		"--console-log-level=error",
		"--download-result=hide",
		"--show-console-readout=false",
		"--summary-interval=0",
	}
	refDir, implDir := t.TempDir(), t.TempDir()
	ref := runHTTPDownload(t, true, srv.URL+"/file.bin", refDir, "hide.bin", extra)
	impl := runHTTPDownload(t, false, srv.URL+"/file.bin", implDir, "hide.bin", extra)

	AssertEqualExit(t, ref, impl)
	for label, result := range map[string]RunResult{"ref": ref, "impl": impl} {
		combined := result.Stdout + result.Stderr
		if strings.Contains(combined, "Download Results") {
			t.Errorf("%s output contains Download Results despite --download-result=hide:\n%s", label, combined)
		}
		if strings.Contains(combined, "Download Progress Summary") {
			t.Errorf("%s output contains progress summary despite --summary-interval=0:\n%s", label, combined)
		}
	}
}

func TestDownload_HelpDerivedOverwriteRenameResumeMatrix(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t,
		"dir",
		"out",
		"allow-overwrite",
		"auto-file-renaming",
		"continue",
	)

	payload := []byte("replacement payload from local server\n")
	srv := newRecordingHTTPPayloadServer(t, payload)
	defer srv.Close()

	t.Run("allow_overwrite_replaces_existing_file", func(t *testing.T) {
		refDir, implDir := t.TempDir(), t.TempDir()
		for _, dir := range []string{refDir, implDir} {
			if err := os.WriteFile(filepath.Join(dir, "target.bin"), []byte("old payload\n"), 0o644); err != nil {
				t.Fatalf("seed existing file: %v", err)
			}
		}

		args := append(baseDownloadArgs(refDir, "target.bin"),
			"--allow-overwrite=true",
			"--auto-file-renaming=false",
			srv.URL+"/overwrite.bin",
		)
		ref := runDownloadProcess(t, true, args, "")
		args = append(baseDownloadArgs(implDir, "target.bin"),
			"--allow-overwrite=true",
			"--auto-file-renaming=false",
			srv.URL+"/overwrite.bin",
		)
		impl := runDownloadProcess(t, false, args, "")

		AssertEqualExit(t, ref, impl)
		requireExitSuccess(t, "ref overwrite", ref)
		requireExitSuccess(t, "impl overwrite", impl)
		requireDownloadedBytes(t, filepath.Join(refDir, "target.bin"), payload)
		requireDownloadedBytes(t, filepath.Join(implDir, "target.bin"), payload)
	})

	t.Run("auto_file_renaming_preserves_existing_file", func(t *testing.T) {
		refDir, implDir := t.TempDir(), t.TempDir()
		original := []byte("existing payload\n")
		for _, dir := range []string{refDir, implDir} {
			if err := os.WriteFile(filepath.Join(dir, "collision.bin"), original, 0o644); err != nil {
				t.Fatalf("seed collision file: %v", err)
			}
		}

		args := append(baseDownloadArgs(refDir, "collision.bin"),
			"--allow-overwrite=false",
			"--auto-file-renaming=true",
			srv.URL+"/rename.bin",
		)
		ref := runDownloadProcess(t, true, args, "")
		args = append(baseDownloadArgs(implDir, "collision.bin"),
			"--allow-overwrite=false",
			"--auto-file-renaming=true",
			srv.URL+"/rename.bin",
		)
		impl := runDownloadProcess(t, false, args, "")

		AssertEqualExit(t, ref, impl)
		requireExitSuccess(t, "ref auto rename", ref)
		requireExitSuccess(t, "impl auto rename", impl)
		requireDownloadedBytes(t, filepath.Join(refDir, "collision.bin"), original)
		requireDownloadedBytes(t, filepath.Join(implDir, "collision.bin"), original)
		requireDownloadedBytes(t, filepath.Join(refDir, "collision.1.bin"), payload)
		requireDownloadedBytes(t, filepath.Join(implDir, "collision.1.bin"), payload)
	})

	t.Run("continue_resumes_existing_prefix", func(t *testing.T) {
		resumePayload := bytes.Repeat([]byte("0123456789abcdef"), 128*1024)
		refSrv := newRecordingHTTPPayloadServer(t, resumePayload)
		defer refSrv.Close()
		implSrv := newRecordingHTTPPayloadServer(t, resumePayload)
		defer implSrv.Close()

		const partial = 32768
		refDir, implDir := t.TempDir(), t.TempDir()
		for _, dir := range []string{refDir, implDir} {
			if err := os.WriteFile(filepath.Join(dir, "resume.bin"), resumePayload[:partial], 0o644); err != nil {
				t.Fatalf("seed partial file: %v", err)
			}
		}

		args := append(baseDownloadArgs(refDir, "resume.bin"),
			"--continue=true",
			"--allow-overwrite=false",
			"--auto-file-renaming=false",
			"--split=1",
			refSrv.URL+"/resume.bin",
		)
		ref := runDownloadProcess(t, true, args, "")
		args = append(baseDownloadArgs(implDir, "resume.bin"),
			"--continue=true",
			"--allow-overwrite=false",
			"--auto-file-renaming=false",
			"--split=1",
			implSrv.URL+"/resume.bin",
		)
		impl := runDownloadProcess(t, false, args, "")

		AssertEqualExit(t, ref, impl)
		requireExitSuccess(t, "ref resume", ref)
		requireExitSuccess(t, "impl resume", impl)
		requireDownloadedBytes(t, filepath.Join(refDir, "resume.bin"), resumePayload)
		requireDownloadedBytes(t, filepath.Join(implDir, "resume.bin"), resumePayload)
		requireRangeStartingAt(t, "ref resume", refSrv.snapshot(), partial)
		requireRangeStartingAt(t, "impl resume", implSrv.snapshot(), partial)
	})
}

func TestDownload_HelpDerivedInputFileAndStdinMatrix(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "input-file", "dir", "out", "allow-overwrite")

	payload := []byte("input file payload\n")
	srv := newHTTPPayloadServer(t, payload)
	defer srv.Close()

	t.Run("input_file_loads_uri_and_per_download_options", func(t *testing.T) {
		refDir, implDir := t.TempDir(), t.TempDir()
		refInput := filepath.Join(refDir, "uris.txt")
		implInput := filepath.Join(implDir, "uris.txt")
		refData := inputFileStanza(srv.URL+"/from-file.bin", refDir, "from-input.bin")
		implData := inputFileStanza(srv.URL+"/from-file.bin", implDir, "from-input.bin")
		if err := os.WriteFile(refInput, []byte(refData), 0o644); err != nil {
			t.Fatalf("write ref input file: %v", err)
		}
		if err := os.WriteFile(implInput, []byte(implData), 0o644); err != nil {
			t.Fatalf("write impl input file: %v", err)
		}

		ref := runDownloadProcess(t, true, append(inputFileArgs(), "--input-file="+refInput), "")
		impl := runDownloadProcess(t, false, append(inputFileArgs(), "--input-file="+implInput), "")

		AssertEqualExit(t, ref, impl)
		requireExitSuccess(t, "ref input file", ref)
		requireExitSuccess(t, "impl input file", impl)
		requireDownloadedBytes(t, filepath.Join(refDir, "from-input.bin"), payload)
		requireDownloadedBytes(t, filepath.Join(implDir, "from-input.bin"), payload)
	})

	t.Run("stdin_loads_input_file_format", func(t *testing.T) {
		refDir, implDir := t.TempDir(), t.TempDir()
		refStdin := inputFileStanza(srv.URL+"/from-stdin.bin", refDir, "from-stdin.bin")
		implStdin := inputFileStanza(srv.URL+"/from-stdin.bin", implDir, "from-stdin.bin")

		ref := runDownloadProcess(t, true, append(inputFileArgs(), "--input-file=-"), refStdin)
		impl := runDownloadProcess(t, false, append(inputFileArgs(), "--input-file=-"), implStdin)

		AssertEqualExit(t, ref, impl)
		requireExitSuccess(t, "ref stdin input", ref)
		requireExitSuccess(t, "impl stdin input", impl)
		requireDownloadedBytes(t, filepath.Join(refDir, "from-stdin.bin"), payload)
		requireDownloadedBytes(t, filepath.Join(implDir, "from-stdin.bin"), payload)
	})
}

func TestDownload_HelpDerivedOutputRoutingMatrix(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t,
		"download-result",
		"quiet",
		"stderr",
		"summary-interval",
		"show-console-readout",
	)

	payload := []byte("output routing payload\n")
	srv := newHTTPPayloadServer(t, payload)
	defer srv.Close()

	refDir, implDir := t.TempDir(), t.TempDir()
	ref := runDownloadProcess(t, true, append(baseDownloadArgs(refDir, "routing.bin"),
		"--quiet=false",
		"--stderr=true",
		"--download-result=default",
		"--summary-interval=0",
		"--show-console-readout=false",
		"--console-log-level=error",
		"--allow-overwrite=true",
		srv.URL+"/routing.bin",
	), "")
	impl := runDownloadProcess(t, false, append(baseDownloadArgs(implDir, "routing.bin"),
		"--quiet=false",
		"--stderr=true",
		"--download-result=default",
		"--summary-interval=0",
		"--show-console-readout=false",
		"--console-log-level=error",
		"--allow-overwrite=true",
		srv.URL+"/routing.bin",
	), "")

	AssertEqualExit(t, ref, impl)
	requireExitSuccess(t, "ref output routing", ref)
	requireExitSuccess(t, "impl output routing", impl)
	requireDownloadedBytes(t, filepath.Join(refDir, "routing.bin"), payload)
	requireDownloadedBytes(t, filepath.Join(implDir, "routing.bin"), payload)
	requireOutputRoutedToStderr(t, "ref", ref)
	requireOutputRoutedToStderr(t, "impl", impl)
}

func TestDownload_HelpDerivedHTTPHeaderMatrix(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "header", "user-agent", "referer")

	payload := []byte("header matrix payload\n")
	refSrv := newRecordingHTTPPayloadServer(t, payload)
	defer refSrv.Close()
	implSrv := newRecordingHTTPPayloadServer(t, payload)
	defer implSrv.Close()

	refDir, implDir := t.TempDir(), t.TempDir()
	common := []string{
		"--user-agent=aria2go-conformance-agent",
		"--referer=http://127.0.0.1/referrer",
		"--header=X-Conformance-Matrix: present",
		"--allow-overwrite=true",
	}
	ref := runDownloadProcess(t, true, append(append(baseDownloadArgs(refDir, "headers.bin"), common...), refSrv.URL+"/headers.bin"), "")
	impl := runDownloadProcess(t, false, append(append(baseDownloadArgs(implDir, "headers.bin"), common...), implSrv.URL+"/headers.bin"), "")

	AssertEqualExit(t, ref, impl)
	requireExitSuccess(t, "ref headers", ref)
	requireExitSuccess(t, "impl headers", impl)
	requireDownloadedBytes(t, filepath.Join(refDir, "headers.bin"), payload)
	requireDownloadedBytes(t, filepath.Join(implDir, "headers.bin"), payload)
	requireRequestWithHeaders(t, "ref", refSrv.snapshot())
	requireRequestWithHeaders(t, "impl", implSrv.snapshot())
}

func TestDownload_HelpDerivedConditionalRemoteTimeContentDispositionMatrix(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t,
		"conditional-get",
		"remote-time",
		"content-disposition-default-utf8",
	)

	t.Run("conditional_get_sends_if_modified_since", func(t *testing.T) {
		payload := []byte("conditional payload\n")
		lastModified := "Wed, 21 Oct 2015 07:28:00 GMT"
		refSrv := newRecordingHTTPPayloadServerWithOptions(t, payload, httpPayloadOptions{lastModified: lastModified})
		defer refSrv.Close()
		implSrv := newRecordingHTTPPayloadServerWithOptions(t, payload, httpPayloadOptions{lastModified: lastModified})
		defer implSrv.Close()

		refDir, implDir := t.TempDir(), t.TempDir()
		for _, dir := range []string{refDir, implDir} {
			path := filepath.Join(dir, "conditional.bin")
			if err := os.WriteFile(path, []byte("cached\n"), 0o644); err != nil {
				t.Fatalf("seed conditional file: %v", err)
			}
			if err := os.Chtimes(path, time.Date(2014, 10, 21, 7, 28, 0, 0, time.UTC), time.Date(2014, 10, 21, 7, 28, 0, 0, time.UTC)); err != nil {
				t.Fatalf("set conditional mtime: %v", err)
			}
		}

		ref := runDownloadProcess(t, true, append(baseDownloadArgs(refDir, "conditional.bin"),
			"--conditional-get=true",
			"--allow-overwrite=true",
			refSrv.URL+"/conditional.bin",
		), "")
		impl := runDownloadProcess(t, false, append(baseDownloadArgs(implDir, "conditional.bin"),
			"--conditional-get=true",
			"--allow-overwrite=true",
			implSrv.URL+"/conditional.bin",
		), "")

		AssertEqualExit(t, ref, impl)
		requireExitSuccess(t, "ref conditional", ref)
		requireExitSuccess(t, "impl conditional", impl)
		requireDownloadedBytes(t, filepath.Join(refDir, "conditional.bin"), payload)
		requireDownloadedBytes(t, filepath.Join(implDir, "conditional.bin"), payload)
		requireIfModifiedSinceSeen(t, "ref", refSrv.snapshot())
		requireIfModifiedSinceSeen(t, "impl", implSrv.snapshot())
	})

	t.Run("remote_time_applies_last_modified", func(t *testing.T) {
		payload := []byte("remote time payload\n")
		lastModifiedTime := time.Date(2016, 7, 8, 9, 10, 11, 0, time.UTC)
		lastModified := lastModifiedTime.Format(http.TimeFormat)
		srv := newRecordingHTTPPayloadServerWithOptions(t, payload, httpPayloadOptions{lastModified: lastModified})
		defer srv.Close()

		refDir, implDir := t.TempDir(), t.TempDir()
		ref := runDownloadProcess(t, true, append(baseDownloadArgs(refDir, "remote-time.bin"),
			"--remote-time=true",
			"--allow-overwrite=true",
			srv.URL+"/remote-time.bin",
		), "")
		impl := runDownloadProcess(t, false, append(baseDownloadArgs(implDir, "remote-time.bin"),
			"--remote-time=true",
			"--allow-overwrite=true",
			srv.URL+"/remote-time.bin",
		), "")

		AssertEqualExit(t, ref, impl)
		requireExitSuccess(t, "ref remote time", ref)
		requireExitSuccess(t, "impl remote time", impl)
		requireDownloadedBytes(t, filepath.Join(refDir, "remote-time.bin"), payload)
		requireDownloadedBytes(t, filepath.Join(implDir, "remote-time.bin"), payload)
		requireFileModTimeNear(t, "ref remote-time", filepath.Join(refDir, "remote-time.bin"), lastModifiedTime)
		requireFileModTimeNear(t, "impl remote-time", filepath.Join(implDir, "remote-time.bin"), lastModifiedTime)
	})

	t.Run("content_disposition_selects_response_filename", func(t *testing.T) {
		payload := []byte("content disposition payload\n")
		srv := newRecordingHTTPPayloadServerWithOptions(t, payload, httpPayloadOptions{
			contentDisposition: `attachment; filename="server-name.bin"`,
		})
		defer srv.Close()

		refDir, implDir := t.TempDir(), t.TempDir()
		ref := runDownloadProcess(t, true, append(baseDownloadArgs(refDir, ""),
			"--content-disposition-default-utf8=true",
			"--allow-overwrite=true",
			srv.URL+"/download",
		), "")
		impl := runDownloadProcess(t, false, append(baseDownloadArgs(implDir, ""),
			"--content-disposition-default-utf8=true",
			"--allow-overwrite=true",
			srv.URL+"/download",
		), "")

		AssertEqualExit(t, ref, impl)
		requireExitSuccess(t, "ref content-disposition", ref)
		requireExitSuccess(t, "impl content-disposition", impl)
		requireDownloadedBytes(t, filepath.Join(refDir, "server-name.bin"), payload)
		requireDownloadedBytes(t, filepath.Join(implDir, "server-name.bin"), payload)
	})
}

func TestDownload_HelpDerivedParameterizedURIMatrix(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "parameterized-uri", "force-sequential", "dir")

	payloads := map[string][]byte{
		"/asset-1.bin": []byte("asset one\n"),
		"/asset-2.bin": []byte("asset two\n"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, ok := payloads[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Accept-Ranges", "bytes")
		start, end, partial, rangeOK := parseRangeHeader(r.Header.Get("Range"), int64(len(payload)))
		if !rangeOK {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		body := payload[start : end+1]
		if partial {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
		}
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}))
	defer srv.Close()

	refDir, implDir := t.TempDir(), t.TempDir()
	ref := runDownloadProcess(t, true, append(baseDownloadArgs(refDir, ""),
		"--parameterized-uri=true",
		"--force-sequential=true",
		srv.URL+"/asset-[1-2].bin",
	), "")
	impl := runDownloadProcess(t, false, append(baseDownloadArgs(implDir, ""),
		"--parameterized-uri=true",
		"--force-sequential=true",
		srv.URL+"/asset-[1-2].bin",
	), "")

	AssertEqualExit(t, ref, impl)
	requireExitSuccess(t, "ref parameterized URI", ref)
	requireExitSuccess(t, "impl parameterized URI", impl)
	requireDownloadedBytes(t, filepath.Join(refDir, "asset-1.bin"), payloads["/asset-1.bin"])
	requireDownloadedBytes(t, filepath.Join(refDir, "asset-2.bin"), payloads["/asset-2.bin"])
	requireDownloadedBytes(t, filepath.Join(implDir, "asset-1.bin"), payloads["/asset-1.bin"])
	requireDownloadedBytes(t, filepath.Join(implDir, "asset-2.bin"), payloads["/asset-2.bin"])
}

func runHTTPDownload(t *testing.T, ref bool, url, dir, out string, extra []string) RunResult {
	t.Helper()

	args := []string{
		"--dir=" + dir,
		"--out=" + out,
		"--allow-overwrite=true",
		"--file-allocation=none",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--enable-dht=false",
		"--enable-dht6=false",
	}
	args = append(args, extra...)
	args = append(args, url)

	var result RunResult
	var err error
	if ref {
		result, err = RunRefWithOptions(t, args, "", RunOptions{Timeout: 20 * time.Second})
	} else {
		result, err = RunImplWithOptions(t, args, "", RunOptions{Timeout: 20 * time.Second})
	}
	if err != nil {
		t.Fatalf("run download ref=%v: %v\nstdout=%s\nstderr=%s", ref, err, result.Stdout, result.Stderr)
	}
	if result.ExitCode != 0 {
		t.Fatalf("download ref=%v exit=%d\nstdout=%s\nstderr=%s", ref, result.ExitCode, result.Stdout, result.Stderr)
	}
	return result
}

func baseDownloadArgs(dir, out string) []string {
	args := []string{
		"--file-allocation=none",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--enable-dht=false",
		"--enable-dht6=false",
	}
	if dir != "" {
		args = append(args, "--dir="+dir)
	}
	if out != "" {
		args = append(args, "--out="+out)
	}
	return args
}

func inputFileArgs() []string {
	return []string{
		"--file-allocation=none",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--enable-dht=false",
		"--enable-dht6=false",
	}
}

func inputFileStanza(uri, dir, out string) string {
	return strings.Join([]string{
		uri,
		"  dir=" + dir,
		"  out=" + out,
		"  allow-overwrite=true",
		"  file-allocation=none",
		"",
	}, "\n")
}

func runDownloadProcess(t *testing.T, ref bool, args []string, stdin string) RunResult {
	t.Helper()

	var result RunResult
	var err error
	if ref {
		result, err = RunRefWithOptions(t, args, stdin, RunOptions{Timeout: 20 * time.Second})
	} else {
		result, err = RunImplWithOptions(t, args, stdin, RunOptions{Timeout: 20 * time.Second})
	}
	if err != nil {
		t.Fatalf("run download ref=%v: %v\nargs=%v\nstdout=%s\nstderr=%s", ref, err, args, result.Stdout, result.Stderr)
	}
	return result
}

func requireExitSuccess(t *testing.T, label string, result RunResult) {
	t.Helper()
	if result.ExitCode != 0 {
		t.Fatalf("%s exit=%d\nstdout=%s\nstderr=%s", label, result.ExitCode, result.Stdout, result.Stderr)
	}
}

func requireDownloadedBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded file %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("downloaded bytes mismatch for %s: got %d bytes want %d", path, len(got), len(want))
	}
}

func requireQuietOutput(t *testing.T, label string, result RunResult) {
	t.Helper()
	if strings.TrimSpace(normalizeOutput(result.Stdout)) != "" {
		t.Errorf("%s quiet stdout not empty:\n%s", label, result.Stdout)
	}
	if strings.TrimSpace(normalizeOutput(result.Stderr)) != "" {
		t.Errorf("%s quiet stderr not empty:\n%s", label, result.Stderr)
	}
}

func newHTTPPayloadServer(t *testing.T, payload []byte) *httptest.Server {
	t.Helper()

	return newRecordingHTTPPayloadServerWithOptions(t, payload, httpPayloadOptions{}).Server
}

type recordingHTTPPayloadServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests []httpRequestRecord

	ranges         []string
	unauthorized   int
	authorized     int
	customHeaderOK int
}

type httpPayloadSnapshot struct {
	requests []httpRequestRecord

	ranges         []string
	unauthorized   int
	authorized     int
	customHeaderOK int
}

type httpRequestRecord struct {
	method          string
	path            string
	rangeHeader     string
	userAgent       string
	referer         string
	matrixHeader    string
	ifModifiedSince string
}

type httpPayloadOptions struct {
	lastModified       string
	contentDisposition string
}

func newRecordingHTTPPayloadServer(t *testing.T, payload []byte) *recordingHTTPPayloadServer {
	t.Helper()
	return newRecordingHTTPPayloadServerWithOptions(t, payload, httpPayloadOptions{})
}

func newRecordingHTTPPayloadServerWithOptions(t *testing.T, payload []byte, opts httpPayloadOptions) *recordingHTTPPayloadServer {
	t.Helper()

	rec := &recordingHTTPPayloadServer{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.recordRequest(r)
		if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
			rec.mu.Lock()
			rec.ranges = append(rec.ranges, rangeHeader)
			rec.mu.Unlock()
		}
		if r.URL.Path == "/auth.bin" {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "user" || pass != "secret" {
				rec.mu.Lock()
				rec.unauthorized++
				rec.mu.Unlock()
				w.Header().Set("WWW-Authenticate", `Basic realm="conformance"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			rec.mu.Lock()
			rec.authorized++
			if r.Header.Get("X-Conformance") == "yes" {
				rec.customHeaderOK++
			}
			rec.mu.Unlock()
			if r.Header.Get("X-Conformance") != "yes" {
				http.Error(w, "missing conformance header", http.StatusForbidden)
				return
			}
		}

		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"`+base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", len(payload))))+`"`)
		if opts.lastModified != "" {
			w.Header().Set("Last-Modified", opts.lastModified)
		}
		if opts.contentDisposition != "" {
			w.Header().Set("Content-Disposition", opts.contentDisposition)
		}

		start, end, partial, ok := parseRangeHeader(r.Header.Get("Range"), int64(len(payload)))
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		body := payload[start : end+1]
		if partial {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
		}
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}))
	return rec
}

func (s *recordingHTTPPayloadServer) recordRequest(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests = append(s.requests, httpRequestRecord{
		method:          r.Method,
		path:            r.URL.Path,
		rangeHeader:     r.Header.Get("Range"),
		userAgent:       r.Header.Get("User-Agent"),
		referer:         r.Header.Get("Referer"),
		matrixHeader:    r.Header.Get("X-Conformance-Matrix"),
		ifModifiedSince: r.Header.Get("If-Modified-Since"),
	})
}

func (s *recordingHTTPPayloadServer) snapshot() httpPayloadSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	ranges := append([]string(nil), s.ranges...)
	requests := append([]httpRequestRecord(nil), s.requests...)
	return httpPayloadSnapshot{
		requests:       requests,
		ranges:         ranges,
		unauthorized:   s.unauthorized,
		authorized:     s.authorized,
		customHeaderOK: s.customHeaderOK,
	}
}

func requireRangeRequested(t *testing.T, label string, snap httpPayloadSnapshot) {
	t.Helper()

	if len(snap.ranges) == 0 {
		t.Fatalf("%s server saw no Range requests", label)
	}
	for _, rangeHeader := range snap.ranges {
		if !strings.HasPrefix(rangeHeader, "bytes=") {
			t.Errorf("%s server saw malformed Range header %q", label, rangeHeader)
		}
	}
}

func requireAuthChallengeFlow(t *testing.T, label string, snap httpPayloadSnapshot) {
	t.Helper()

	if snap.unauthorized == 0 {
		t.Errorf("%s server saw no unauthenticated challenge request", label)
	}
	if snap.authorized == 0 {
		t.Errorf("%s server saw no authorized retry request", label)
	}
	if snap.customHeaderOK == 0 {
		t.Errorf("%s server saw no authorized request with custom header", label)
	}
}

func requireRangeStartingAt(t *testing.T, label string, snap httpPayloadSnapshot, offset int) {
	t.Helper()

	want := fmt.Sprintf("bytes=%d-", offset)
	for _, rangeHeader := range snap.ranges {
		if rangeHeader == want || strings.HasPrefix(rangeHeader, want) {
			return
		}
	}
	t.Fatalf("%s server saw no resume range starting at %d; ranges=%v", label, offset, snap.ranges)
}

func requireOutputRoutedToStderr(t *testing.T, label string, result RunResult) {
	t.Helper()

	if strings.TrimSpace(normalizeOutput(result.Stdout)) != "" {
		t.Errorf("%s stdout not empty with --stderr=true:\n%s", label, result.Stdout)
	}
	if !strings.Contains(result.Stderr, "Download Results") {
		t.Errorf("%s stderr missing Download Results:\n%s", label, result.Stderr)
	}
	if strings.Contains(result.Stderr, "Download Progress Summary") {
		t.Errorf("%s stderr contains progress summary despite --summary-interval=0:\n%s", label, result.Stderr)
	}
}

func requireRequestWithHeaders(t *testing.T, label string, snap httpPayloadSnapshot) {
	t.Helper()

	for _, req := range snap.requests {
		if req.userAgent == "aria2go-conformance-agent" &&
			req.referer == "http://127.0.0.1/referrer" &&
			req.matrixHeader == "present" {
			return
		}
	}
	t.Fatalf("%s server saw no request with expected user-agent/referer/custom header: %#v", label, snap.requests)
}

func requireIfModifiedSinceSeen(t *testing.T, label string, snap httpPayloadSnapshot) {
	t.Helper()

	for _, req := range snap.requests {
		if req.ifModifiedSince != "" {
			return
		}
	}
	t.Fatalf("%s server saw no If-Modified-Since header: %#v", label, snap.requests)
}

func requireFileModTimeNear(t *testing.T, label, path string, want time.Time) {
	t.Helper()

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s stat %s: %v", label, path, err)
	}
	got := st.ModTime()
	if got.Before(want.Add(-2*time.Second)) || got.After(want.Add(2*time.Second)) {
		t.Fatalf("%s modtime = %s, want near %s", label, got.UTC().Format(time.RFC3339), want.UTC().Format(time.RFC3339))
	}
}

func parseRangeHeader(header string, size int64) (start, end int64, partial, ok bool) {
	if header == "" {
		return 0, size - 1, false, true
	}
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false, false
	}
	parts := strings.SplitN(strings.TrimPrefix(header, "bytes="), "-", 2)
	if len(parts) != 2 || parts[0] == "" {
		return 0, 0, false, false
	}
	parsedStart, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || parsedStart < 0 || parsedStart >= size {
		return 0, 0, false, false
	}
	parsedEnd := size - 1
	if parts[1] != "" {
		parsedEnd, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, false, false
		}
	}
	if parsedEnd < parsedStart || parsedEnd >= size {
		return 0, 0, false, false
	}
	return parsedStart, parsedEnd, true, true
}
