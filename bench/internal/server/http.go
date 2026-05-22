package server

import (
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

type InfiniteServer struct {
	lis     net.Listener
	srv     *http.Server
	buf     []byte

	bytesSent atomic.Int64
	requests  atomic.Int64
}

func NewInfinite(addr string) (*InfiniteServer, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 256*1024)
	for i := range buf {
		buf[i] = byte(i % 256)
	}
	s := &InfiniteServer{
		lis: lis,
		buf: buf,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/infinite", s.handleInfinite)
	mux.HandleFunc("/stats", s.handleStats)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	s.srv = &http.Server{Handler: mux, ReadTimeout: 0, WriteTimeout: 0, IdleTimeout: 0}
	return s, nil
}

func (s *InfiniteServer) Addr() string          { return s.lis.Addr().String() }
func (s *InfiniteServer) URL() string           { return "http://" + s.lis.Addr().String() + "/infinite" }
func (s *InfiniteServer) BytesSent() int64      { return s.bytesSent.Load() }
func (s *InfiniteServer) ActiveRequests() int64 { return s.requests.Load() }

func (s *InfiniteServer) Start() error {
	go s.srv.Serve(s.lis)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", s.Addr())
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("server not ready after 2s")
}

func (s *InfiniteServer) Close() error {
	return s.srv.Close()
}

func (s *InfiniteServer) handleInfinite(w http.ResponseWriter, r *http.Request) {
	s.requests.Add(1)
	defer s.requests.Add(-1)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	// Send a fake large Content-Length so aria2c/aria2go can start downloading.
	// Use 100GB — large enough for any benchmark duration.
	// aria2c/aria2go must use --file-allocation=none to skip preallocation.
	w.Header().Set("Content-Length", "107374182400") // 100 GiB
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	for {
		n, err := w.Write(s.buf)
		if err != nil {
			return
		}
		s.bytesSent.Add(int64(n))
		if canFlush {
			flusher.Flush()
		}
	}
}

func (s *InfiniteServer) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "bytes_sent=%d\nactive_requests=%d\n", s.bytesSent.Load(), s.requests.Load())
}
