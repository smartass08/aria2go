package dashboard

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"time"
)

//go:embed static/*
var staticFS embed.FS

type Event struct {
	Type string `json:"type"`
	// Payload fields (varies by type)
	Meta      map[string]string `json:"meta,omitempty"`
	Kind      string            `json:"kind,omitempty"`
	Binary    string            `json:"binary,omitempty"`
	Duration  time.Duration     `json:"duration_ns,omitempty"`
	Summary   any               `json:"summary,omitempty"`
	Sample    any               `json:"sample,omitempty"`
}

type Server struct {
	mux       *http.ServeMux
	lis       net.Listener
	srv       *http.Server
	mu        sync.RWMutex
	clients   map[chan Event]struct{}
	msgCh     chan Event
}

func New(addr string) (*Server, error) {
	if addr == "" {
		addr = "127.0.0.1:7890"
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{
		lis:     lis,
		clients: make(map[chan Event]struct{}),
		msgCh:   make(chan Event, 256),
	}
	s.mux = http.NewServeMux()

	// Static files
	sub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))

	// SSE endpoint
	s.mux.HandleFunc("/events", s.handleSSE)

	s.srv = &http.Server{Handler: s.mux, ReadTimeout: 0, WriteTimeout: 0}

	// Background dispatcher
	go s.dispatch()

	return s, nil
}

func (s *Server) Start() error {
	go s.srv.Serve(s.lis)
	return nil
}

func (s *Server) Close() error {
	return s.srv.Close()
}

func (s *Server) Addr() string { return s.lis.Addr().String() }
func (s *Server) URL() string  { return "http://" + s.lis.Addr().String() }

// Publish sends an event to all connected SSE clients.
func (s *Server) Publish(ev Event) {
	select {
	case s.msgCh <- ev:
	default:
		// Channel full, drop oldest
		<-s.msgCh
		s.msgCh <- ev
	}
}

func (s *Server) dispatch() {
	for ev := range s.msgCh {
		s.mu.RLock()
		for ch := range s.clients {
			select {
			case ch <- ev:
			default:
				// Slow client, drop
			}
		}
		s.mu.RUnlock()
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flush", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := make(chan Event, 32)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(ev)
			w.Write([]byte("data: "))
			w.Write(data)
			w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}
