package dashboard

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServeStatic(t *testing.T) {
	s, err := New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	resp, err := http.Get(s.URL() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestSSE(t *testing.T) {
	s, err := New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL()+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}

	// Publish an event
	go func() {
		time.Sleep(200 * time.Millisecond)
		s.Publish(Event{Type: "meta", Meta: map[string]string{"host": "test"}})
	}()

	scanner := bufio.NewScanner(resp.Body)
	if !scanner.Scan() {
		t.Fatal("no event received")
	}
	line := scanner.Text()
	if !strings.HasPrefix(line, "data: ") {
		t.Errorf("expected 'data: ...', got %q", line)
	}
	if !strings.Contains(line, "test") {
		t.Errorf("expected 'test' in payload, got %q", line)
	}
}
