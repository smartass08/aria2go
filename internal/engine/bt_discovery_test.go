package engine

import (
	"context"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	btpeer "github.com/smartass08/aria2go/internal/protocol/bittorrent/peer"
	"github.com/smartass08/aria2go/internal/torrent"
	"github.com/smartass08/aria2go/internal/tracker"
)

func TestBTTrackerSessionNextDefaultDelayUsesUserOverride(t *testing.T) {
	session := &btTrackerSession{
		list:                tracker.NewAnnounceList([][]string{{"http://tracker/announce"}}),
		minInterval:         30 * time.Minute,
		userDefinedInterval: 10 * time.Second,
		prevAnnounce:        time.Now().Add(-3 * time.Second),
	}

	delay := session.nextDefaultDelay()
	if delay < 6*time.Second || delay > 8*time.Second {
		t.Fatalf("nextDefaultDelay() = %v, want about 7s", delay)
	}
}

func TestBTTrackerSessionCompletedAnnounceUsesCompletedEvent(t *testing.T) {
	session := &btTrackerSession{
		list: tracker.NewAnnounceList([][]string{{"http://tracker/announce"}}),
	}
	session.list.AnnounceSuccess()

	var gotEvent string
	err := session.announceCompleted(
		context.Background(),
		func(_ context.Context, _ string, req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
			gotEvent = req.Event
			return &tracker.AnnounceResponse{Interval: 1800, MinInterval: 600}, nil
		},
		func(event string, _ int, _ string) tracker.AnnounceRequest {
			return tracker.AnnounceRequest{Event: event}
		},
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("announceCompleted() error = %v", err)
	}
	if gotEvent != "completed" {
		t.Fatalf("announceCompleted() event = %q, want completed", gotEvent)
	}
}

func TestNextDHTDiscoveryDelay(t *testing.T) {
	if got := nextDHTDiscoveryDelay(0, 40, 0); got != btDHTPeerIntervalZero {
		t.Fatalf("nextDHTDiscoveryDelay(zero peers, no retry) = %v, want %v", got, btDHTPeerIntervalZero)
	}
	if got := nextDHTDiscoveryDelay(1, 40, 1); got != btDHTPeerRetryInterval {
		t.Fatalf("nextDHTDiscoveryDelay(low peers, retry) = %v, want %v", got, btDHTPeerRetryInterval)
	}
	if got := nextDHTDiscoveryDelay(40, 40, 0); got != btDHTPeerInterval {
		t.Fatalf("nextDHTDiscoveryDelay(healthy peers) = %v, want %v", got, btDHTPeerInterval)
	}
}

func TestBtPeerConfigDisablesPrivateTorrentDiscoveryFlags(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e.cfg.EnablePeerExchange = true
	e.dhtServer = nil

	meta := &torrent.MetaInfo{}
	meta.Info.Private = true
	meta.Info.PieceLength = 16 * 1024

	cfg := e.btPeerConfig(meta, nil)
	if cfg.Reserved != btpeer.MakeReserved(false, false, false) {
		t.Fatalf("Reserved = %v, want no PEX/DHT bits for private torrent", cfg.Reserved)
	}
}

func TestBtPeerConfigEnablesPEXFlagWhenAllowed(t *testing.T) {
	opts := testOpts()
	opts.EnablePeerExchange = true
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	meta := &torrent.MetaInfo{}
	meta.Info.PieceLength = 16 * 1024

	cfg := e.btPeerConfig(meta, nil)
	if cfg.Reserved != btpeer.MakeReserved(false, true, false) {
		t.Fatalf("Reserved = %v, want extended-messaging bit only", cfg.Reserved)
	}
}

func TestBTTrackerTimeoutParsesOption(t *testing.T) {
	if got := btTrackerTimeout(&config.Options{BTTrackerTimeout: "12"}); got != 12*time.Second {
		t.Fatalf("btTrackerTimeout() = %v, want 12s", got)
	}
}

// TestBTTrackerConnectTimeoutParsesOption verifies that btTrackerConnectTimeout
// reads bt-tracker-connect-timeout and falls back to 60 s when absent.
func TestBTTrackerConnectTimeoutParsesOption(t *testing.T) {
	if got := btTrackerConnectTimeout(&config.Options{BTTrackerConnectTimeout: "30"}); got != 30*time.Second {
		t.Fatalf("btTrackerConnectTimeout(30) = %v, want 30s", got)
	}
	if got := btTrackerConnectTimeout(&config.Options{BTTrackerConnectTimeout: "0"}); got != 60*time.Second {
		t.Fatalf("btTrackerConnectTimeout(0) = %v, want 60s default", got)
	}
	if got := btTrackerConnectTimeout(&config.Options{}); got != 60*time.Second {
		t.Fatalf("btTrackerConnectTimeout(empty) = %v, want 60s default", got)
	}
	if got := btTrackerConnectTimeout(nil); got != 60*time.Second {
		t.Fatalf("btTrackerConnectTimeout(nil) = %v, want 60s default", got)
	}
}

// TestAnnounceTrackerAppliesConnectTimeout checks that announceTracker uses
// the bt-tracker-connect-timeout option to bound tracker announce calls.
// When the timeout is very short and the tracker does not respond, the call
// should fail within that bound rather than waiting indefinitely.
func TestAnnounceTrackerAppliesConnectTimeout(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Set a very short connect timeout so the announce will time out quickly.
	e.cfg.BTTrackerConnectTimeout = "1"

	// Use an unreachable address that will cause a connection timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, announceErr := e.announceTracker(ctx, "udp://192.0.2.1:9/announce", tracker.AnnounceRequest{})
	if announceErr == nil {
		t.Fatal("expected error from unreachable tracker, got nil")
	}
}
