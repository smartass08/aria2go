package config

import "testing"

func TestCloneExplicitOptionsCopiesOnlyExplicitFields(t *testing.T) {
	src := &Options{
		Dir:             "/downloads",
		Header:          []string{"X-Test: one"},
		SummaryInterval: "10",
	}
	src.MarkExplicit("dir")
	src.MarkExplicit("header")

	cloned := CloneExplicitOptions(src)
	if cloned == nil {
		t.Fatal("CloneExplicitOptions() returned nil")
	}
	if cloned.Dir != "/downloads" {
		t.Fatalf("Dir = %q, want /downloads", cloned.Dir)
	}
	if len(cloned.Header) != 1 || cloned.Header[0] != "X-Test: one" {
		t.Fatalf("Header = %v, want [X-Test: one]", cloned.Header)
	}
	if cloned.SummaryInterval != "" {
		t.Fatalf("SummaryInterval = %q, want empty because it was not explicit", cloned.SummaryInterval)
	}

	cloned.Header[0] = "X-Test: two"
	if src.Header[0] != "X-Test: one" {
		t.Fatal("header slice was not deep-copied")
	}
}

func TestSessionOptionMapIncludesOnlyInitialExplicitOptions(t *testing.T) {
	opts := &Options{
		Dir:                    "/downloads",
		Header:                 []string{"X-Test: one", "X-Test: two"},
		MaxDownloadLimit:       "2K",
		AllProxy:               "proxy.example:8080",
		SaveSession:            "/tmp/out.session",
		SummaryInterval:        "1",
		MaxConcurrentDownloads: 8,
	}
	opts.MarkExplicit("dir")
	opts.MarkExplicit("header")
	opts.MarkExplicit("max-download-limit")
	opts.MarkExplicit("all-proxy")
	opts.MarkExplicit("save-session")
	opts.MarkExplicit("summary-interval")
	opts.MarkExplicit("max-concurrent-downloads")

	got := SessionOptionMap(opts)
	if got["dir"] != "/downloads" {
		t.Fatalf("dir = %q, want /downloads", got["dir"])
	}
	if got["header"] != "X-Test: one\nX-Test: two" {
		t.Fatalf("header = %q", got["header"])
	}
	if got["max-download-limit"] != "2048" {
		t.Fatalf("max-download-limit = %q, want 2048", got["max-download-limit"])
	}
	if got["all-proxy"] != "http://proxy.example:8080/" {
		t.Fatalf("all-proxy = %q, want normalized proxy URI", got["all-proxy"])
	}
	if _, ok := got["save-session"]; ok {
		t.Fatal("save-session should not be session-serialized")
	}
	if _, ok := got["summary-interval"]; ok {
		t.Fatal("summary-interval should not be session-serialized")
	}
	if _, ok := got["max-concurrent-downloads"]; ok {
		t.Fatal("max-concurrent-downloads should not be session-serialized")
	}
}
