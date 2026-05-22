package server

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestInfiniteServerStreams(t *testing.T) {
	s, err := NewInfinite("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.ContentLength != 107374182400 {
		t.Errorf("expected ContentLength=107374182400, got %d", resp.ContentLength)
	}

	n, err := io.CopyN(io.Discard, resp.Body, 2*1024*1024)
	if err != nil {
		t.Fatalf("copy: %v (read %d)", err, n)
	}
	if n != 2*1024*1024 {
		t.Errorf("read %d bytes, want %d", n, 2*1024*1024)
	}
	if got := s.BytesSent(); got < 2*1024*1024 {
		t.Errorf("server bytes_sent=%d, want >= %d", got, 2*1024*1024)
	}
}
