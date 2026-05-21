package engine

import (
	"testing"

	"github.com/smartass08/aria2go/internal/config"
)

func TestNewBtSession(t *testing.T) {
	cfg := &config.Options{
		ListenPort: "6881-6999",
	}
	s := NewBtSession(cfg)
	if s == nil {
		t.Fatal("NewBtSession() returned nil")
	}
	if s.Port() == 0 {
		t.Error("BtSession.Port() returned 0, want non-zero")
	}
	pid := s.PeerID()
	if pid == [20]byte{} {
		t.Error("BtSession.PeerID() returned zero peer ID")
	}
}

func TestBtSession_DefaultPort(t *testing.T) {
	cfg := &config.Options{}
	s := NewBtSession(cfg)
	if s == nil {
		t.Fatal("NewBtSession() returned nil")
	}
	if s.Port() <= 0 || s.Port() > 65535 {
		t.Errorf("BtSession.Port() = %d, want between 1 and 65535", s.Port())
	}
}

func TestBtSession_DHTPort(t *testing.T) {
	cfg := &config.Options{
		ListenPort:    "6881",
		DHTListenPort: "6882",
	}
	s := NewBtSession(cfg)
	if s == nil {
		t.Fatal("NewBtSession() returned nil")
	}
	if s.Port() != 6881 {
		t.Errorf("BtSession.Port() = %d, want 6881", s.Port())
	}
}

func TestBtSession_PeerIDConsistency(t *testing.T) {
	cfg := &config.Options{}
	s := NewBtSession(cfg)
	pid1 := s.PeerID()
	pid2 := s.PeerID()
	if pid1 != pid2 {
		t.Error("PeerID() returned different values on successive calls")
	}
}
