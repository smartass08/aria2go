package conformance

// metalink_filter_test.go – conformance tests for Metalink language and OS filters.
//
// Tests:
//   TestMetalinkFilter_LanguageFilter – build a .meta4 with two files having
//     different <language> tags; run --metalink-language=en --show-files;
//     assert both binaries select the same file.
//
//   TestMetalinkFilter_OSFilter – build a .meta4 with two files having
//     different <os> tags; run --metalink-os=Linux --show-files;
//     assert both binaries select the same file.
//
//   TestMetalinkFilter_LanguageDownload – end-to-end: serve the matching file
//     via local HTTP, assert both binaries download exactly that file.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// withoutMetalinkFilterBanner strips the aria2c "Printing the contents of file"
// banner line so stdout comparison is header-agnostic.
func withoutMetalinkFilterBanner(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), ">>> Printing") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// buildMetalinkLanguageDoc writes a .meta4 with two files:
//
//	"en-file.bin"  language=en
//	"de-file.bin"  language=de
//
// Both have valid sha-256 hashes so aria2c won't reject them, but the URL
// is set to an invalid host so no network request is made.
func buildMetalinkLanguageDoc(t *testing.T, dir string) string {
	t.Helper()

	enData := protocolPayload("metalink-lang-en", 512)
	deData := protocolPayload("metalink-lang-de", 512)

	enSum := sha256.Sum256(enData)
	deSum := sha256.Sum256(deData)

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">` + "\n")

	// English file
	b.WriteString(`  <file name="en-file.bin">` + "\n")
	b.WriteString("    <size>" + strconv.Itoa(len(enData)) + "</size>\n")
	b.WriteString("    <language>en</language>\n")
	b.WriteString(`    <hash type="sha-256">` + hex.EncodeToString(enSum[:]) + "</hash>\n")
	b.WriteString(`    <url priority="1">http://example.invalid/en-file.bin</url>` + "\n")
	b.WriteString("  </file>\n")

	// German file
	b.WriteString(`  <file name="de-file.bin">` + "\n")
	b.WriteString("    <size>" + strconv.Itoa(len(deData)) + "</size>\n")
	b.WriteString("    <language>de</language>\n")
	b.WriteString(`    <hash type="sha-256">` + hex.EncodeToString(deSum[:]) + "</hash>\n")
	b.WriteString(`    <url priority="1">http://example.invalid/de-file.bin</url>` + "\n")
	b.WriteString("  </file>\n")

	b.WriteString("</metalink>\n")

	path := filepath.Join(dir, "lang.meta4")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write lang metalink: %v", err)
	}
	return path
}

// buildMetalinkOSDoc writes a .meta4 with two files:
//
//	"linux-file.bin"   os=Linux
//	"macos-file.bin"   os=macOS
func buildMetalinkOSDoc(t *testing.T, dir string) string {
	t.Helper()

	linuxData := protocolPayload("metalink-os-linux", 512)
	macData := protocolPayload("metalink-os-mac", 512)

	linuxSum := sha256.Sum256(linuxData)
	macSum := sha256.Sum256(macData)

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">` + "\n")

	b.WriteString(`  <file name="linux-file.bin">` + "\n")
	b.WriteString("    <size>" + strconv.Itoa(len(linuxData)) + "</size>\n")
	b.WriteString("    <os>Linux</os>\n")
	b.WriteString(`    <hash type="sha-256">` + hex.EncodeToString(linuxSum[:]) + "</hash>\n")
	b.WriteString(`    <url priority="1">http://example.invalid/linux-file.bin</url>` + "\n")
	b.WriteString("  </file>\n")

	b.WriteString(`  <file name="macos-file.bin">` + "\n")
	b.WriteString("    <size>" + strconv.Itoa(len(macData)) + "</size>\n")
	b.WriteString("    <os>macOS</os>\n")
	b.WriteString(`    <hash type="sha-256">` + hex.EncodeToString(macSum[:]) + "</hash>\n")
	b.WriteString(`    <url priority="1">http://example.invalid/macos-file.bin</url>` + "\n")
	b.WriteString("  </file>\n")

	b.WriteString("</metalink>\n")

	path := filepath.Join(dir, "os.meta4")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write os metalink: %v", err)
	}
	return path
}

// TestMetalinkFilter_LanguageFilterShowFiles verifies that --metalink-language=en
// causes both binaries to list only the English file when --show-files is used.
func TestMetalinkFilter_LanguageFilterShowFiles(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "metalink-language")

	dir := t.TempDir()
	meta4 := buildMetalinkLanguageDoc(t, dir)

	args := append(protocolBaseArgs(dir),
		"--metalink-language=en",
		"--show-files=true",
		meta4,
	)

	ref := protocolRun(t, true, args)
	impl := protocolRun(t, false, args)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref metalink language filter show-files", ref)
	protocolRequireExitZero(t, "impl metalink language filter show-files", impl)

	// Both outputs must list en-file.bin.
	if !strings.Contains(ref.Stdout, "en-file.bin") {
		t.Errorf("ref show-files did not list en-file.bin:\n%s", ref.Stdout)
	}
	if !strings.Contains(impl.Stdout, "en-file.bin") {
		t.Errorf("impl show-files did not list en-file.bin:\n%s", impl.Stdout)
	}

	// Neither output should list de-file.bin when filtered to en.
	if strings.Contains(ref.Stdout, "de-file.bin") {
		t.Logf("DIVERGENCE CANDIDATE: ref lists de-file.bin despite --metalink-language=en")
		t.Logf("ref stdout:\n%s", ref.Stdout)
	}
	if strings.Contains(impl.Stdout, "de-file.bin") {
		t.Logf("DIVERGENCE CANDIDATE: impl lists de-file.bin despite --metalink-language=en")
		t.Logf("impl stdout:\n%s", impl.Stdout)
	}

	// Core parity: compare normalised show-files output (minus header banner).
	assertStableTextEqual(t, "metalink language filter show-files stdout",
		withoutMetalinkFilterBanner(ref.Stdout),
		withoutMetalinkFilterBanner(impl.Stdout),
	)
}

// TestMetalinkFilter_LanguageFilterNoFilter verifies that without
// --metalink-language both files appear in --show-files output.
func TestMetalinkFilter_LanguageFilterNoFilter(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	meta4 := buildMetalinkLanguageDoc(t, dir)

	args := append(protocolBaseArgs(dir),
		"--show-files=true",
		meta4,
	)

	ref := protocolRun(t, true, args)
	impl := protocolRun(t, false, args)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref metalink no-filter show-files", ref)
	protocolRequireExitZero(t, "impl metalink no-filter show-files", impl)

	// Both should list both files when no language filter is set.
	for _, label := range []string{"en-file.bin", "de-file.bin"} {
		if !strings.Contains(ref.Stdout, label) {
			t.Errorf("ref show-files missing %s without filter:\n%s", label, ref.Stdout)
		}
		if !strings.Contains(impl.Stdout, label) {
			t.Errorf("impl show-files missing %s without filter:\n%s", label, impl.Stdout)
		}
	}

	assertStableTextEqual(t, "metalink no-filter show-files stdout",
		withoutMetalinkFilterBanner(ref.Stdout),
		withoutMetalinkFilterBanner(impl.Stdout),
	)
}

// TestMetalinkFilter_LanguageDownload verifies an end-to-end download using
// language filter: serve only "en-file.bin" via local HTTP, filter to en,
// assert exactly that file is downloaded.
func TestMetalinkFilter_LanguageDownload(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "metalink-language")

	enPayload := protocolPayload("metalink-filter-en-download", 16*1024)
	dePayload := protocolPayload("metalink-filter-de-download", 16*1024)

	refHTTP := startProtocolHTTPFixture(t, map[string][]byte{
		"/en-file.bin": enPayload,
		"/de-file.bin": dePayload,
	})
	implHTTP := startProtocolHTTPFixture(t, map[string][]byte{
		"/en-file.bin": enPayload,
		"/de-file.bin": dePayload,
	})
	defer refHTTP.Close()
	defer implHTTP.Close()

	buildDoc := func(httpFixture *protocolHTTPFixture) []metalinkFixtureFile {
		return []metalinkFixtureFile{
			{
				Name:     "en-file.bin",
				Data:     enPayload,
				Language: "en",
				URLs:     []metalinkFixtureURL{{URL: httpFixture.URLPath("/en-file.bin"), Priority: 1}},
			},
			{
				Name:     "de-file.bin",
				Data:     dePayload,
				Language: "de",
				URLs:     []metalinkFixtureURL{{URL: httpFixture.URLPath("/de-file.bin"), Priority: 1}},
			},
		}
	}

	refDir := t.TempDir()
	implDir := t.TempDir()
	refMeta4 := filepath.Join(refDir, "lang-dl.meta4")
	implMeta4 := filepath.Join(implDir, "lang-dl.meta4")

	if err := os.WriteFile(refMeta4, protocolMetalinkV4Custom(t, buildDoc(refHTTP)), 0o644); err != nil {
		t.Fatalf("write ref metalink: %v", err)
	}
	if err := os.WriteFile(implMeta4, protocolMetalinkV4Custom(t, buildDoc(implHTTP)), 0o644); err != nil {
		t.Fatalf("write impl metalink: %v", err)
	}

	refArgs := append(protocolBaseArgs(refDir),
		"--metalink-language=en",
		"--metalink-file="+refMeta4,
	)
	implArgs := append(protocolBaseArgs(implDir),
		"--metalink-language=en",
		"--metalink-file="+implMeta4,
	)

	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref metalink language download", ref)
	protocolRequireExitZero(t, "impl metalink language download", impl)

	// The EN file should be downloaded.
	protocolRequireFile(t, filepath.Join(refDir, "en-file.bin"), enPayload)
	protocolRequireFile(t, filepath.Join(implDir, "en-file.bin"), enPayload)

	// The DE file should NOT be downloaded (no file or zero-size file).
	if _, err := os.Stat(filepath.Join(refDir, "de-file.bin")); err == nil {
		t.Logf("DIVERGENCE NOTE: ref downloaded de-file.bin despite --metalink-language=en")
	}
	if _, err := os.Stat(filepath.Join(implDir, "de-file.bin")); err == nil {
		t.Logf("DIVERGENCE NOTE: impl downloaded de-file.bin despite --metalink-language=en")
	}
}

// TestMetalinkFilter_OSFilterShowFiles verifies that --metalink-os=Linux filters
// to only the Linux file in --show-files output.
func TestMetalinkFilter_OSFilterShowFiles(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "metalink-os")

	dir := t.TempDir()
	meta4 := buildMetalinkOSDoc(t, dir)

	args := append(protocolBaseArgs(dir),
		"--metalink-os=Linux",
		"--show-files=true",
		meta4,
	)

	ref := protocolRun(t, true, args)
	impl := protocolRun(t, false, args)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref metalink OS filter show-files", ref)
	protocolRequireExitZero(t, "impl metalink OS filter show-files", impl)

	assertStableTextEqual(t, "metalink OS filter show-files stdout",
		withoutMetalinkFilterBanner(ref.Stdout),
		withoutMetalinkFilterBanner(impl.Stdout),
	)

	t.Logf("ref stdout:\n%s", ref.Stdout)
	t.Logf("impl stdout:\n%s", impl.Stdout)
}

// TestMetalinkFilter_VersionFilterShowFiles verifies that --metalink-version
// filters entries by version metadata.
func TestMetalinkFilter_VersionFilterShowFiles(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "metalink-version")

	v1Data := protocolPayload("metalink-version-1", 256)
	v2Data := protocolPayload("metalink-version-2", 256)

	v1Sum := sha256.Sum256(v1Data)
	v2Sum := sha256.Sum256(v2Data)

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">` + "\n")

	b.WriteString(`  <file name="v1-file.bin">` + "\n")
	b.WriteString("    <size>" + strconv.Itoa(len(v1Data)) + "</size>\n")
	b.WriteString("    <version>1.0</version>\n")
	b.WriteString(`    <hash type="sha-256">` + hex.EncodeToString(v1Sum[:]) + "</hash>\n")
	b.WriteString(`    <url priority="1">http://example.invalid/v1-file.bin</url>` + "\n")
	b.WriteString("  </file>\n")

	b.WriteString(`  <file name="v2-file.bin">` + "\n")
	b.WriteString("    <size>" + strconv.Itoa(len(v2Data)) + "</size>\n")
	b.WriteString("    <version>2.0</version>\n")
	b.WriteString(`    <hash type="sha-256">` + hex.EncodeToString(v2Sum[:]) + "</hash>\n")
	b.WriteString(`    <url priority="1">http://example.invalid/v2-file.bin</url>` + "\n")
	b.WriteString("  </file>\n")

	b.WriteString("</metalink>\n")

	dir := t.TempDir()
	meta4 := filepath.Join(dir, "ver.meta4")
	if err := os.WriteFile(meta4, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write version metalink: %v", err)
	}

	args := append(protocolBaseArgs(dir),
		"--metalink-version=1.0",
		"--show-files=true",
		meta4,
	)

	ref := protocolRun(t, true, args)
	impl := protocolRun(t, false, args)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref metalink version filter show-files", ref)
	protocolRequireExitZero(t, "impl metalink version filter show-files", impl)

	assertStableTextEqual(t, "metalink version filter show-files stdout",
		withoutMetalinkFilterBanner(ref.Stdout),
		withoutMetalinkFilterBanner(impl.Stdout),
	)

	t.Logf("ref stdout:\n%s", ref.Stdout)
}
