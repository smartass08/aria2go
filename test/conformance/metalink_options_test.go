package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type metalinkFixtureURL struct {
	URL      string
	Priority int
	Location string
}

type metalinkFixtureFile struct {
	Name     string
	Data     []byte
	HashHex  string
	Version  string
	Language string
	OS       string
	URLs     []metalinkFixtureURL
}

func TestMetalink_ShowFilesParity(t *testing.T) {
	SkipIfNoRef(t)

	files := []metalinkFixtureFile{
		{
			Name: "a.bin",
			Data: protocolPayload("metalink-show-a", 500),
			URLs: []metalinkFixtureURL{{URL: "http://example.invalid/a.bin", Priority: 1}},
		},
		{
			Name: "sub/b.bin",
			Data: protocolPayload("metalink-show-b", 700),
			URLs: []metalinkFixtureURL{{URL: "http://example.invalid/sub/b.bin", Priority: 1}},
		},
	}

	dir := t.TempDir()
	metalinkPath := filepath.Join(dir, "show.meta4")
	if err := os.WriteFile(metalinkPath, protocolMetalinkV4Custom(t, files), 0o644); err != nil {
		t.Fatalf("write metalink: %v", err)
	}

	args := append(protocolBaseArgs(t.TempDir()), "--show-files=true", metalinkPath)
	ref := protocolRun(t, true, args)
	impl := protocolRun(t, false, args)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref metalink show-files", ref)
	protocolRequireExitZero(t, "impl metalink show-files", impl)
	assertStableTextEqual(t, "metalink show-files stdout",
		withoutShowFilesBanner(ref.Stdout),
		withoutShowFilesBanner(impl.Stdout),
	)
}

func TestMetalink_BaseURIParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("metalink-base-uri", 32*1024)
	refHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/base/payload.bin": payload})
	implHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/base/payload.bin": payload})
	defer refHTTP.Close()
	defer implHTTP.Close()

	refDir := t.TempDir()
	implDir := t.TempDir()
	refMetalink := filepath.Join(refDir, "fixture.meta4")
	implMetalink := filepath.Join(implDir, "fixture.meta4")
	refDoc := []metalinkFixtureFile{{
		Name: "payload.bin",
		Data: payload,
		URLs: []metalinkFixtureURL{{URL: "payload.bin", Priority: 1}},
	}}
	implDoc := []metalinkFixtureFile{{
		Name: "payload.bin",
		Data: payload,
		URLs: []metalinkFixtureURL{{URL: "payload.bin", Priority: 1}},
	}}
	if err := os.WriteFile(refMetalink, protocolMetalinkV4Custom(t, refDoc), 0o644); err != nil {
		t.Fatalf("write ref metalink: %v", err)
	}
	if err := os.WriteFile(implMetalink, protocolMetalinkV4Custom(t, implDoc), 0o644); err != nil {
		t.Fatalf("write impl metalink: %v", err)
	}

	refArgs := append(protocolBaseArgs(refDir),
		"--metalink-base-uri="+refHTTP.URL+"/base/",
		"--metalink-file="+refMetalink,
	)
	implArgs := append(protocolBaseArgs(implDir),
		"--metalink-base-uri="+implHTTP.URL+"/base/",
		"--metalink-file="+implMetalink,
	)
	ref := protocolRun(t, true, refArgs)
	impl := protocolRun(t, false, implArgs)

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref metalink base-uri", ref)
	protocolRequireExitZero(t, "impl metalink base-uri", impl)
	protocolRequireFile(t, filepath.Join(refDir, "payload.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "payload.bin"), payload)
}

func TestMetalink_MirrorFallbackParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("metalink-mirror-fallback", 48*1024)
	refHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/good.bin": payload})
	implHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/good.bin": payload})
	defer refHTTP.Close()
	defer implHTTP.Close()

	refDir := t.TempDir()
	implDir := t.TempDir()
	refMetalink := filepath.Join(refDir, "fallback.meta4")
	implMetalink := filepath.Join(implDir, "fallback.meta4")
	refDoc := []metalinkFixtureFile{{
		Name: "good.bin",
		Data: payload,
		URLs: []metalinkFixtureURL{
			{URL: refHTTP.URLPath("/missing.bin"), Priority: 1},
			{URL: refHTTP.URLPath("/good.bin"), Priority: 10},
		},
	}}
	implDoc := []metalinkFixtureFile{{
		Name: "good.bin",
		Data: payload,
		URLs: []metalinkFixtureURL{
			{URL: implHTTP.URLPath("/missing.bin"), Priority: 1},
			{URL: implHTTP.URLPath("/good.bin"), Priority: 10},
		},
	}}
	if err := os.WriteFile(refMetalink, protocolMetalinkV4Custom(t, refDoc), 0o644); err != nil {
		t.Fatalf("write ref metalink: %v", err)
	}
	if err := os.WriteFile(implMetalink, protocolMetalinkV4Custom(t, implDoc), 0o644); err != nil {
		t.Fatalf("write impl metalink: %v", err)
	}

	ref := protocolRun(t, true, append(protocolBaseArgs(refDir), "--metalink-file="+refMetalink))
	impl := protocolRun(t, false, append(protocolBaseArgs(implDir), "--metalink-file="+implMetalink))

	AssertEqualExit(t, ref, impl)
	protocolRequireExitZero(t, "ref metalink mirror fallback", ref)
	protocolRequireExitZero(t, "impl metalink mirror fallback", impl)
	protocolRequireFile(t, filepath.Join(refDir, "good.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "good.bin"), payload)
}

func TestMetalink_HashVerificationParity(t *testing.T) {
	SkipIfNoRef(t)

	payload := protocolPayload("metalink-hash-failure", 24*1024)
	refHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/payload.bin": payload})
	implHTTP := startProtocolHTTPFixture(t, map[string][]byte{"/payload.bin": payload})
	defer refHTTP.Close()
	defer implHTTP.Close()

	refDir := t.TempDir()
	implDir := t.TempDir()
	refMetalink := filepath.Join(refDir, "bad-hash.meta4")
	implMetalink := filepath.Join(implDir, "bad-hash.meta4")
	refDoc := []metalinkFixtureFile{{
		Name:    "payload.bin",
		Data:    payload,
		HashHex: strings.Repeat("0", sha256.Size*2),
		URLs:    []metalinkFixtureURL{{URL: refHTTP.URLPath("/payload.bin"), Priority: 1}},
	}}
	implDoc := []metalinkFixtureFile{{
		Name:    "payload.bin",
		Data:    payload,
		HashHex: strings.Repeat("0", sha256.Size*2),
		URLs:    []metalinkFixtureURL{{URL: implHTTP.URLPath("/payload.bin"), Priority: 1}},
	}}
	if err := os.WriteFile(refMetalink, protocolMetalinkV4Custom(t, refDoc), 0o644); err != nil {
		t.Fatalf("write ref metalink: %v", err)
	}
	if err := os.WriteFile(implMetalink, protocolMetalinkV4Custom(t, implDoc), 0o644); err != nil {
		t.Fatalf("write impl metalink: %v", err)
	}

	ref := protocolRun(t, true, append(protocolBaseArgs(refDir), "--metalink-file="+refMetalink))
	impl := protocolRun(t, false, append(protocolBaseArgs(implDir), "--metalink-file="+implMetalink))

	AssertEqualExit(t, ref, impl)
	if ref.ExitCode == 0 || impl.ExitCode == 0 {
		t.Fatalf("expected checksum failure, got ref=%d impl=%d", ref.ExitCode, impl.ExitCode)
	}
	protocolRequireFile(t, filepath.Join(refDir, "payload.bin"), payload)
	protocolRequireFile(t, filepath.Join(implDir, "payload.bin"), payload)
}

func protocolMetalinkV4Custom(t *testing.T, files []metalinkFixtureFile) []byte {
	t.Helper()

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">` + "\n")
	for _, file := range files {
		sumHex := file.HashHex
		if sumHex == "" {
			sum := sha256.Sum256(file.Data)
			sumHex = hex.EncodeToString(sum[:])
		}
		b.WriteString(`  <file name="`)
		xmlEscape(&b, file.Name)
		b.WriteString(`">` + "\n")
		b.WriteString("    <size>")
		b.WriteString(strconv.Itoa(len(file.Data)))
		b.WriteString("</size>\n")
		if file.Version != "" {
			b.WriteString("    <version>")
			xmlEscape(&b, file.Version)
			b.WriteString("</version>\n")
		}
		if file.Language != "" {
			b.WriteString("    <language>")
			xmlEscape(&b, file.Language)
			b.WriteString("</language>\n")
		}
		if file.OS != "" {
			b.WriteString("    <os>")
			xmlEscape(&b, file.OS)
			b.WriteString("</os>\n")
		}
		b.WriteString(`    <hash type="sha-256">`)
		b.WriteString(sumHex)
		b.WriteString("</hash>\n")
		for _, uri := range file.URLs {
			b.WriteString(`    <url`)
			if uri.Priority > 0 {
				b.WriteString(` priority="`)
				b.WriteString(strconv.Itoa(uri.Priority))
				b.WriteString(`"`)
			}
			if uri.Location != "" {
				b.WriteString(` location="`)
				xmlEscape(&b, uri.Location)
				b.WriteString(`"`)
			}
			b.WriteString(`>`)
			xmlEscape(&b, uri.URL)
			b.WriteString("</url>\n")
		}
		b.WriteString("  </file>\n")
	}
	b.WriteString("</metalink>\n")
	return []byte(b.String())
}
