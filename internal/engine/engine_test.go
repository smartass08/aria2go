package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/bencode"
	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	btprogress "github.com/smartass08/aria2go/internal/protocol/bittorrent/progress"
	"github.com/smartass08/aria2go/internal/sessionfile"
	"github.com/smartass08/aria2go/internal/torrent"
)

// testLogger returns a logger that writes to a buffer, suitable for testing.
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// testOpts returns a baseline Options suitable for tests.
func testOpts() *config.Options {
	return &config.Options{
		Dir:                    "/tmp/aria2go-test",
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
	}
}

// collectorSubscriber collects events for test assertions using the new
// channel-based subscription API.
type collectorSubscriber struct {
	ch     chan core.Event
	done   chan struct{}
	events []core.Event
	mu     sync.Mutex
}

func newCollectorSubscriber() *collectorSubscriber {
	c := &collectorSubscriber{
		ch:   make(chan core.Event, 64),
		done: make(chan struct{}),
	}
	go c.drain()
	return c
}

func (c *collectorSubscriber) drain() {
	for ev := range c.ch {
		c.mu.Lock()
		c.events = append(c.events, ev)
		c.mu.Unlock()
	}
	close(c.done)
}

func (c *collectorSubscriber) stop() {
	close(c.ch)
	<-c.done
}

func (c *collectorSubscriber) Events() []core.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]core.Event, len(c.events))
	copy(result, c.events)
	return result
}

func TestNew(t *testing.T) {
	logger := testLogger(t)
	e, err := New(testOpts(), logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if e == nil {
		t.Fatal("New() returned nil")
	}
	if e.SessionID() == "" {
		t.Error("SessionID is empty")
	}
	if e.Created().IsZero() {
		t.Error("Created time is zero")
	}
}

func TestNew_NilConfig(t *testing.T) {
	_, err := New(nil, testLogger(t))
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestNew_NilLogger(t *testing.T) {
	_, err := New(testOpts(), nil)
	if err == nil {
		t.Error("expected error for nil logger")
	}
}

func TestAdd(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, err := e.Add(AddSpec{
		URIs: []string{"http://example.com/file.iso"},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if gid == 0 {
		t.Error("Add() returned zero GID")
	}

	// Verify it's in the waiting queue.
	waiting := e.TellWaiting(0, 10)
	if len(waiting) != 1 {
		t.Fatalf("expected 1 waiting download, got %d", len(waiting))
	}
	if waiting[0].GID != gid {
		t.Errorf("waiting GID = %s, want %s", waiting[0].GID, gid)
	}
	if waiting[0].Status != core.StatusWaiting {
		t.Errorf("waiting status = %s, want waiting", waiting[0].Status)
	}
}

func TestAddPositionDefaultAppendVsExplicitPosition(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gidA, err := e.Add(AddSpec{URIs: []string{"http://example.com/a"}})
	if err != nil {
		t.Fatalf("Add(a) error = %v", err)
	}
	gidB, err := e.Add(AddSpec{
		URIs:     []string{"http://example.com/b"},
		Position: 0,
	})
	if err != nil {
		t.Fatalf("Add(b) error = %v", err)
	}
	gidC, err := e.Add(AddSpec{
		URIs:        []string{"http://example.com/c"},
		Position:    0,
		PositionSet: true,
	})
	if err != nil {
		t.Fatalf("Add(c) error = %v", err)
	}

	waiting := e.TellWaiting(0, 10)
	if len(waiting) != 3 {
		t.Fatalf("TellWaiting returned %d entries, want 3", len(waiting))
	}
	got := []core.GID{waiting[0].GID, waiting[1].GID, waiting[2].GID}
	want := []core.GID{gidC, gidA, gidB}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("waiting order = %v, want %v", got, want)
		}
	}
}

func TestAdd_PauseOption(t *testing.T) {
	opts := testOpts()
	opts.EnableRPC = true
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, err := e.Add(AddSpec{
		URIs:    []string{"http://example.com/file.iso"},
		Options: &config.Options{Pause: true},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	status, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	if status.Status != core.StatusPaused {
		t.Fatalf("status = %s, want paused", status.Status)
	}

	e.fillRequestGroupFromReserver()
	if active := e.TellActive(); len(active) != 0 {
		t.Fatalf("paused add promoted %d active downloads", len(active))
	}
}

func TestAdd_PauseOptionIgnoredWithoutRPC(t *testing.T) {
	opts := testOpts()
	opts.EnableRPC = false
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, err := e.Add(AddSpec{
		URIs:    []string{"http://example.com/file.iso"},
		Options: &config.Options{Pause: true},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	status, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	if status.Status != core.StatusWaiting {
		t.Fatalf("status = %s, want waiting when RPC is disabled", status.Status)
	}
}

func TestAdd_PauseOptionHonoredWithRPC(t *testing.T) {
	opts := testOpts()
	opts.EnableRPC = true
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, err := e.Add(AddSpec{
		URIs:    []string{"http://example.com/file.iso"},
		Options: &config.Options{Pause: true},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	status, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	if status.Status != core.StatusPaused {
		t.Fatalf("status = %s, want paused when RPC is enabled", status.Status)
	}
}

func TestAdd_UsesProvidedGIDAndRejectsDuplicate(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	const gidHex = "00000000000000ab"
	gid, err := e.Add(AddSpec{
		URIs:    []string{"http://example.com/file.iso"},
		Options: &config.Options{GID: gidHex},
	})
	if err != nil {
		t.Fatalf("Add() with GID error = %v", err)
	}
	if gid != core.GID(0xab) {
		t.Fatalf("gid = %s, want 171", gid)
	}

	_, err = e.Add(AddSpec{
		URIs:    []string{"http://example.com/other.iso"},
		Options: &config.Options{GID: gidHex},
	})
	if err == nil {
		t.Fatal("expected duplicate GID to be rejected")
	}
}

func TestAdd_NoURIs(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = e.Add(AddSpec{})
	if err == nil {
		t.Error("expected error for empty URIs and no torrent")
	}
}

func TestAdd_MonotonicGID(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid1, _ := e.Add(AddSpec{URIs: []string{"http://example.com/a"}})
	gid2, _ := e.Add(AddSpec{URIs: []string{"http://example.com/b"}})
	// aria2 uses random GIDs — they should be distinct, not necessarily ordered.
	if gid1 == gid2 {
		t.Errorf("GIDs should be distinct: %s == %s", gid2, gid1)
	}
	if gid1 == 0 || gid2 == 0 {
		t.Error("GIDs must be non-zero")
	}
}

func TestPause(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})

	if err := e.Pause(gid, false); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	status, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	// aria2 model: paused downloads report "paused" in RPC but internally
	// are in the waiting queue with pauseRequested flag.
	if status.Status != core.StatusPaused {
		t.Errorf("expected paused status in RPC response, got %s", status.Status)
	}

	// Paused downloads remain in the waiting queue.
	waiting := e.TellWaiting(0, 10)
	found := false
	for _, w := range waiting {
		if w.GID == gid {
			found = true
			break
		}
	}
	if !found {
		t.Error("paused download should appear in waiting queue")
	}
}

func TestPause_AlreadyPaused(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	e.Pause(gid, false)

	// Second pause should error.
	if err := e.Pause(gid, false); err == nil {
		t.Error("expected error pausing already paused download")
	}
}

func TestPause_NotFound(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := e.Pause(99999, false); err == nil {
		t.Error("expected error for unknown GID")
	}
}

func TestResume(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	e.Pause(gid, false)

	if err := e.Resume(gid); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}

	status, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	// Resumed downloads go from paused -> waiting (matched by aria2 model).
	if status.Status != core.StatusWaiting {
		t.Errorf("expected waiting status after resume, got %s", status.Status)
	}

	rg, ok := e.groups.getLocked(gid)
	if !ok {
		t.Fatalf("group %s not found after Resume()", gid)
	}
	if rg.restartReq {
		e.groups.unlock(gid)
		t.Fatal("restartReq = true after Resume(), want false")
	}
	e.groups.unlock(gid)

	// It should still be in the waiting queue.
	waiting := e.TellWaiting(0, 10)
	found := false
	for _, w := range waiting {
		if w.GID == gid {
			found = true
			break
		}
	}
	if !found {
		t.Error("resumed download not in waiting queue")
	}
}

func TestResume_NotPaused(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	// Not paused — should error.
	if err := e.Resume(gid); err == nil {
		t.Error("expected error resuming non-paused download")
	}
}

func TestRemove(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	e.fillRequestGroupFromReserver()

	if err := e.Remove(gid, false); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Should be in stopped queue and queryable via TellStatus.
	s, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus on removed download returned error: %v", err)
	}
	if s.Status != core.StatusRemoved {
		t.Errorf("expected removed status, got %s", s.Status)
	}

	// Should be in stopped queue.
	stopped := e.TellStopped(0, 10)
	found := false
	for _, s := range stopped {
		if s.GID == gid {
			found = true
			if s.Status != core.StatusRemoved {
				t.Errorf("expected removed status, got %s", s.Status)
			}
			break
		}
	}
	if !found {
		t.Error("removed download not in stopped queue")
	}
}

func TestRemove_NotFound(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := e.Remove(99999, false); err == nil {
		t.Error("expected error for unknown GID")
	}
}

func TestTellStatus(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, _ := e.Add(AddSpec{
		URIs: []string{"http://example.com/file.iso"},
		Options: &config.Options{
			Dir: "/custom/dir",
		},
	})

	status, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	if status.GID != gid {
		t.Errorf("GID = %s, want %s", status.GID, gid)
	}
	if status.Status != core.StatusWaiting {
		t.Errorf("status = %s, want waiting", status.Status)
	}
	if status.Dir != "/custom/dir" {
		t.Errorf("dir = %s, want /custom/dir", status.Dir)
	}
}

func TestTellActive_Empty(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	active := e.TellActive()
	if len(active) != 0 {
		t.Errorf("expected 0 active, got %d", len(active))
	}
}

func TestTellWaiting_Range(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for i := 0; i < 5; i++ {
		e.Add(AddSpec{URIs: []string{fmt.Sprintf("http://example.com/file%d.iso", i)}})
	}

	// offset=1, num=2
	results := e.TellWaiting(1, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestTellWaiting_OffsetOutOfRange(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	results := e.TellWaiting(100, 10)
	if results != nil {
		t.Errorf("expected nil for out-of-range offset, got %v", results)
	}
}

func TestTellStopped(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	e.fillRequestGroupFromReserver()
	e.Remove(gid, false)

	stopped := e.TellStopped(0, 10)
	if len(stopped) != 1 {
		t.Fatalf("expected 1 stopped, got %d", len(stopped))
	}
	if stopped[0].GID != gid {
		t.Errorf("GID = %s, want %s", stopped[0].GID, gid)
	}
}

func TestPurgeDownloadResult(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid1, _ := e.Add(AddSpec{URIs: []string{"http://example.com/a"}})
	gid2, _ := e.Add(AddSpec{URIs: []string{"http://example.com/b"}})
	e.fillRequestGroupFromReserver()
	e.Remove(gid1, false)
	e.fillRequestGroupFromReserver()
	e.Remove(gid2, false)

	n := e.PurgeDownloadResult()
	if n != 2 {
		t.Errorf("expected 2 purged, got %d", n)
	}

	stopped := e.TellStopped(0, 10)
	if len(stopped) != 0 {
		t.Errorf("expected 0 stopped after purge, got %d", len(stopped))
	}
}

func TestRemoveDownloadResultRejectsActiveWaitingAndRemovesStopped(t *testing.T) {
	e, err := New(&config.Options{
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	activeGID, err := e.Add(AddSpec{URIs: []string{"http://example.com/active"}})
	if err != nil {
		t.Fatalf("Add(active) error = %v", err)
	}
	waitingGID, err := e.Add(AddSpec{URIs: []string{"http://example.com/waiting"}})
	if err != nil {
		t.Fatalf("Add(waiting) error = %v", err)
	}
	e.fillRequestGroupFromReserver()

	if err := e.RemoveDownloadResult(activeGID); err == nil {
		t.Fatal("RemoveDownloadResult(active) expected error")
	}
	if err := e.RemoveDownloadResult(waitingGID); err == nil {
		t.Fatal("RemoveDownloadResult(waiting) expected error")
	}

	if err := e.Remove(waitingGID, false); err != nil {
		t.Fatalf("Remove(waiting) error = %v", err)
	}
	if err := e.RemoveDownloadResult(waitingGID); err == nil {
		t.Fatal("RemoveDownloadResult(removed waiting) expected error")
	}
	if _, err := e.TellStatus(waitingGID); err == nil {
		t.Fatal("TellStatus removed result expected error")
	}
}

func TestChangeOption(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, _ := e.Add(AddSpec{
		URIs:    []string{"http://example.com/file.iso"},
		Options: &config.Options{Dir: "/old"},
	})

	if err := e.ChangeOption(gid, &config.Options{Dir: "/new", MaxTries: 5}); err != nil {
		t.Fatalf("ChangeOption() error = %v", err)
	}

	status, _ := e.TellStatus(gid)
	if status.Dir != "/new" {
		t.Errorf("dir = %s, want /new", status.Dir)
	}
}

func TestChangeGlobalOption(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := e.ChangeGlobalOption(&config.Options{MaxConcurrentDownloads: 3}); err != nil {
		t.Fatalf("ChangeGlobalOption() error = %v", err)
	}

	opts := e.GetGlobalOption()
	if opts.MaxConcurrentDownloads != 3 {
		t.Errorf("MaxConcurrentDownloads = %d, want 3", opts.MaxConcurrentDownloads)
	}
}

func TestChangeGlobalOption_RuntimeEffects(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	e.stoppedTotal.Store(7)
	e.stoppedRing.push(&downloadResult{
		gid:     0x11,
		state:   core.StatusError,
		errCode: core.ExitResourceNotFound,
		statusSnapshot: Status{
			GID:    0x11,
			Status: core.StatusError,
		},
	})
	e.stoppedRing.push(&downloadResult{
		gid:     0x12,
		state:   core.StatusRemoved,
		errCode: core.ExitRemoved,
		statusSnapshot: Status{
			GID:    0x12,
			Status: core.StatusRemoved,
		},
	})

	opts := &config.Options{
		AutoSaveInterval:            "7",
		SaveSessionInterval:         "13",
		MaxOverallDownloadLimit:     "200K",
		MaxOverallUploadLimit:       "100K",
		MaxDownloadResult:           1,
		OptimizeConcurrentDownloads: "true",
	}
	for _, name := range []string{
		"auto-save-interval",
		"save-session-interval",
		"max-overall-download-limit",
		"max-overall-upload-limit",
		"max-download-result",
		"optimize-concurrent-downloads",
	} {
		opts.MarkExplicit(name)
	}

	if err := e.ChangeGlobalOption(opts); err != nil {
		t.Fatalf("ChangeGlobalOption() error = %v", err)
	}

	if e.saveInterval != 7*time.Second {
		t.Fatalf("saveInterval = %v, want 7s", e.saveInterval)
	}
	if e.saveSessionInterval != 13*time.Second {
		t.Fatalf("saveSessionInterval = %v, want 13s", e.saveSessionInterval)
	}
	if e.rateGlobal.rate.Load() != 204800 {
		t.Fatalf("rateGlobal = %d, want 204800", e.rateGlobal.rate.Load())
	}
	if e.rateGlobalUp.rate.Load() != 102400 {
		t.Fatalf("rateGlobalUp = %d, want 102400", e.rateGlobalUp.rate.Load())
	}
	if got := e.stoppedRing.len(); got != 1 {
		t.Fatalf("stopped ring len = %d, want 1", got)
	}
	stopped := e.TellStopped(0, 10)
	if len(stopped) != 1 || stopped[0].GID != 0x12 {
		t.Fatalf("TellStopped() = %#v, want newest entry only", stopped)
	}
	if e.stoppedTotal.Load() != 7 {
		t.Fatalf("stoppedTotal = %d, want 7", e.stoppedTotal.Load())
	}
	if e.removedErrors.Load() != 1 {
		t.Fatalf("removedErrors = %d, want 1", e.removedErrors.Load())
	}
	if got := core.ErrorCode(e.removedLastErr.Load()); got != core.ExitResourceNotFound {
		t.Fatalf("removedLastErr = %v, want %v", got, core.ExitResourceNotFound)
	}
	select {
	case <-e.queueWake:
	default:
		t.Fatal("queueWake was not signaled")
	}
}

func TestSubscribe(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	sub := newCollectorSubscriber()
	defer sub.stop()

	result := e.Subscribe(sub.ch)
	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	e.fillRequestGroupFromReserver()
	e.Remove(gid, false)

	time.Sleep(10 * time.Millisecond)

	events := sub.Events()
	if len(events) == 0 {
		t.Error("expected at least one event after remove")
	}

	result.Unsubscribe()

	e.Add(AddSpec{URIs: []string{"http://example.com/another.iso"}})
	e.Remove(gid, false)

	events2 := sub.Events()
	if len(events2) != len(events) {
		t.Errorf("expected %d events after unsubscribe, got %d", len(events), len(events2))
	}
}

func TestEventKindMapping(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	sub := newCollectorSubscriber()
	defer sub.stop()
	e.Subscribe(sub.ch)

	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	e.Remove(gid, false)

	events := sub.Events()
	for _, ev := range events {
		if ev.Kind != core.EvStop {
			continue
		}
		if ev.GID != gid {
			t.Errorf("event GID = %s, want %s", ev.GID, gid)
		}
		if ev.Time.IsZero() {
			t.Error("event Time is zero")
		}
	}
}

func TestShutdown(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- e.Run(ctx)
	}()

	time.Sleep(10 * time.Millisecond)
	if err := e.Shutdown(false); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after Shutdown")
	}
}

func TestShutdown_Force(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	if err := e.Shutdown(true); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestShutdown_DoubleCall(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx)
	time.Sleep(10 * time.Millisecond)
	e.Shutdown(false)
	if err := e.Shutdown(false); err == nil {
		t.Error("expected error on second shutdown call")
	}
}

func TestShutdown_ForceEscalatesGracefulRequest(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.ctx = ctx
	e.cancel = cancel

	gid := core.GID(1)
	rg := &requestGroup{
		gid:    gid,
		state:  core.StatusActive,
		cancel: func() {},
	}
	e.groups.set(gid, rg)
	e.active = []core.GID{gid}

	if err := e.Shutdown(false); err != nil {
		t.Fatalf("Shutdown(false) error = %v", err)
	}
	if !rg.haltRequested {
		t.Fatal("haltRequested = false, want true after graceful shutdown")
	}
	if rg.forceHaltReq {
		t.Fatal("forceHaltReq = true after graceful shutdown, want false")
	}

	if err := e.Shutdown(true); err != nil {
		t.Fatalf("Shutdown(true) escalation error = %v", err)
	}
	if !rg.forceHaltReq {
		t.Fatal("forceHaltReq = false, want true after force escalation")
	}
}

func TestShutdown_PreventsAdd(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx)
	time.Sleep(10 * time.Millisecond)
	e.Shutdown(false)
	time.Sleep(10 * time.Millisecond)

	_, err = e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	if err == nil {
		t.Error("expected error adding after shutdown")
	}
}

func TestRunStopTimerRequestsShutdown(t *testing.T) {
	opts := testOpts()
	opts.EnableRPC = true
	opts.Stop = "1"
	opts.Quiet = true

	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- e.Run(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Run() did not exit after --stop timer elapsed")
	}

	if !e.shuttingDown.Load() {
		t.Fatal("expected stop timer to request shutdown")
	}
}

func TestRunStopWithProcessRequestsShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process watch semantics are Unix-focused in this test")
	}

	opts := testOpts()
	opts.EnableRPC = true
	opts.StopWithProcess = 999999999
	opts.Quiet = true

	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- e.Run(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Run() did not exit after watched process disappeared")
	}

	if !e.shuttingDown.Load() {
		t.Fatal("expected stop-with-process watcher to request shutdown")
	}
}

func TestRunExitsWhenQueuesDrainWithoutRPC(t *testing.T) {
	opts := testOpts()
	opts.DryRun = true
	opts.EnableRPC = false
	opts.Quiet = true

	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- e.Run(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2500 * time.Millisecond):
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("Run() did not return after dry-run download completed")
	}
}

func TestStatusContention(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.TellStatus(gid)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.TellActive()
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.TellWaiting(0, 10)
		}()
	}
	wg.Wait()
}

func TestGIDSeed(t *testing.T) {
	e1, _ := New(testOpts(), testLogger(t))
	e2, _ := New(testOpts(), testLogger(t))

	gid1, _ := e1.Add(AddSpec{URIs: []string{"http://a"}})
	gid2, _ := e2.Add(AddSpec{URIs: []string{"http://b"}})

	// aria2 uses random GIDs — just verify they're non-zero and distinct.
	if gid1 == 0 {
		t.Errorf("GID1 should be non-zero")
	}
	if gid2 == 0 {
		t.Errorf("GID2 should be non-zero")
	}
	if gid1 == gid2 {
		t.Errorf("GIDs should be distinct across engines")
	}
}

func TestSessionID(t *testing.T) {
	e1, _ := New(testOpts(), testLogger(t))
	e2, _ := New(testOpts(), testLogger(t))

	if e1.SessionID() == e2.SessionID() {
		t.Error("expected different session IDs")
	}
	if len(e1.SessionID()) != 40 {
		t.Errorf("expected 40-char hex session ID, got %d chars", len(e1.SessionID()))
	}
}

func TestTellStopped_RemovedInStopped(t *testing.T) {
	e, _ := New(testOpts(), testLogger(t))
	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	e.fillRequestGroupFromReserver()
	e.Remove(gid, false)

	s, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus on removed download returned error: %v", err)
	}
	if s.Status != core.StatusRemoved {
		t.Errorf("status = %s, want removed", s.Status)
	}
}

func TestMakeStatusLocked_StatePaused(t *testing.T) {
	e, _ := New(testOpts(), testLogger(t))
	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	e.Pause(gid, false)

	status, _ := e.TellStatus(gid)
	if status.Status != core.StatusPaused {
		t.Errorf("expected paused, got %s", status.Status)
	}
	if status.Seeder {
		t.Error("expected Seeder to default to false for non-BT download")
	}
}

func TestFillRequestGroupFromReserver_PromotesToActive(t *testing.T) {
	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 3,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
	}, testLogger(t))

	sub := newCollectorSubscriber()
	defer sub.stop()
	e.Subscribe(sub.ch)

	for i := 0; i < 5; i++ {
		e.Add(AddSpec{URIs: []string{fmt.Sprintf("http://example.com/file%d.iso", i)}})
	}

	e.queuesMu.Lock()
	allWaiting := len(e.waiting)
	e.queuesMu.Unlock()
	if allWaiting != 5 {
		t.Fatalf("expected 5 waiting, got %d", allWaiting)
	}

	e.fillRequestGroupFromReserver()

	time.Sleep(10 * time.Millisecond)

	e.queuesMu.Lock()
	activeCount := len(e.active)
	waitingCount := len(e.waiting)
	e.queuesMu.Unlock()

	if activeCount != 3 {
		t.Errorf("expected 3 active (max=3), got %d", activeCount)
	}
	if waitingCount != 2 {
		t.Errorf("expected 2 remaining waiting, got %d", waitingCount)
	}

	events := sub.Events()
	startCount := 0
	for _, ev := range events {
		if ev.Kind == core.EvStart {
			startCount++
		}
	}
	if startCount != 3 {
		t.Errorf("expected 3 EvStart events, got %d", startCount)
	}

	active := e.TellActive()
	if len(active) != 3 {
		t.Errorf("TellActive returned %d entries, want 3", len(active))
	}
	for _, s := range active {
		if s.Status != core.StatusActive {
			t.Errorf("expected active status, got %s", s.Status)
		}
	}
}

func TestFillRequestGroupFromReserver_SkipsPaused(t *testing.T) {
	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 2,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
	}, testLogger(t))

	gid1, _ := e.Add(AddSpec{URIs: []string{"http://example.com/a"}})
	e.Add(AddSpec{URIs: []string{"http://example.com/b"}})
	e.Add(AddSpec{URIs: []string{"http://example.com/c"}})
	e.Pause(gid1, false)

	e.fillRequestGroupFromReserver()

	e.queuesMu.Lock()
	activeCount := len(e.active)
	waitingCount := len(e.waiting)
	e.queuesMu.Unlock()

	if activeCount != 2 {
		t.Errorf("expected 2 active, got %d", activeCount)
	}
	if waitingCount != 1 {
		t.Errorf("expected 1 waiting (paused), got %d", waitingCount)
	}

	waiting := e.TellWaiting(0, 10)
	found := false
	for _, w := range waiting {
		if w.GID == gid1 && w.Status == core.StatusPaused {
			found = true
			break
		}
	}
	if !found {
		t.Error("paused download should appear in waiting queue with paused status")
	}
}

func TestChangeOption_ActiveUsesPending(t *testing.T) {
	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
	}, testLogger(t))

	gid, _ := e.Add(AddSpec{
		URIs:    []string{"http://example.com/file.iso"},
		Options: &config.Options{Dir: "/old"},
	})

	e.fillRequestGroupFromReserver()

	rg, ok := e.groups.getLocked(gid)
	if !ok {
		t.Fatal("group not found")
	}
	if rg.state != core.StatusActive {
		e.groups.unlock(gid)
		t.Fatalf("expected active, got %s", rg.state)
	}

	e.groups.unlock(gid)

	e.ChangeOption(gid, &config.Options{Dir: "/new"})

	rg, ok = e.groups.getLocked(gid)
	if !ok {
		t.Fatal("group not found")
	}
	defer e.groups.unlock(gid)
	if rg.pendingOpts == nil {
		t.Error("expected pendingOpts to be set for active download")
	} else if rg.pendingOpts.Dir != "/new" {
		t.Errorf("pending dir = %s, want /new", rg.pendingOpts.Dir)
	}
	if !rg.pauseReq {
		t.Error("expected pauseReq=true after ChangeOption on active")
	}
	if !rg.restartReq {
		t.Error("expected restartReq=true after ChangeOption on active")
	}
}

func TestChangeOption_WaitingAppliesImmediately(t *testing.T) {
	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
	}, testLogger(t))

	gid, _ := e.Add(AddSpec{
		URIs:    []string{"http://example.com/file.iso"},
		Options: &config.Options{Dir: "/old"},
	})

	e.ChangeOption(gid, &config.Options{Dir: "/new"})

	rg, ok := e.groups.getLocked(gid)
	if !ok {
		t.Fatal("group not found")
	}
	defer e.groups.unlock(gid)

	if rg.opts.Dir != "/new" {
		t.Errorf("dir = %s, want /new", rg.opts.Dir)
	}
	if rg.pendingOpts != nil {
		t.Error("pendingOpts should be nil for waiting group")
	}
	if rg.pauseReq {
		t.Error("pauseReq should be false for waiting group")
	}
}

func TestChangeOption_NotFound(t *testing.T) {
	e, _ := New(testOpts(), testLogger(t))
	if err := e.ChangeOption(99999, &config.Options{Dir: "/x"}); err == nil {
		t.Error("expected error for unknown GID")
	}
}

func TestChangeOptionStoppedResult(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, _ := e.Add(AddSpec{
		URIs:    []string{"http://example.com/file.iso"},
		Options: &config.Options{Dir: "/old"},
	})

	rg, ok := e.groups.getLocked(gid)
	if !ok {
		t.Fatal("group not found")
	}
	e.addStoppedLocked(rg, core.StatusError, core.ExitNetworkProblem, "network failed")
	e.groups.unlock(gid)
	e.groups.delete(gid)

	if err := e.ChangeOption(gid, &config.Options{MaxDownloadLimit: "1M"}); err != nil {
		t.Fatalf("ChangeOption() error = %v", err)
	}
	opts, err := e.GetOption(gid)
	if err != nil {
		t.Fatalf("GetOption() error = %v", err)
	}
	if opts.Dir != "/old" {
		t.Errorf("dir = %s, want /old", opts.Dir)
	}
	if opts.MaxDownloadLimit != "1M" {
		t.Errorf("max download limit = %s, want 1M", opts.MaxDownloadLimit)
	}
}

func TestOnEndOfRun_CleansUpActiveAndWaiting(t *testing.T) {
	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 2,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
	}, testLogger(t))

	sub := newCollectorSubscriber()
	defer sub.stop()
	e.Subscribe(sub.ch)

	e.Add(AddSpec{URIs: []string{"http://example.com/a"}})
	e.Add(AddSpec{URIs: []string{"http://example.com/b"}})
	e.Add(AddSpec{URIs: []string{"http://example.com/c"}})

	e.fillRequestGroupFromReserver()

	active := e.TellActive()
	if len(active) == 0 {
		t.Fatal("expected active downloads before shutdown")
	}

	waitingBefore := len(e.TellWaiting(0, 100))
	if waitingBefore == 0 {
		t.Fatal("expected waiting downloads before shutdown")
	}

	e.onEndOfRun()

	active = e.TellActive()
	if len(active) != 0 {
		t.Errorf("expected 0 active after onEndOfRun, got %d", len(active))
	}
	waiting := e.TellWaiting(0, 100)
	if len(waiting) != 0 {
		t.Errorf("expected 0 waiting after onEndOfRun, got %d", len(waiting))
	}

	stopped := e.TellStopped(0, 100)
	if len(stopped) < 3 {
		t.Errorf("expected at least 3 stopped entries, got %d", len(stopped))
	}
}

func TestOnEndOfRun_ActiveShutdownCountsAsInProgress(t *testing.T) {
	e, err := New(&config.Options{
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		Dir:                    t.TempDir(),
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, err := e.Add(AddSpec{URIs: []string{"http://example.com/active.iso"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	e.fillRequestGroupFromReserver()

	if len(e.TellActive()) != 1 {
		t.Fatalf("expected 1 active download before shutdown, got %d", len(e.TellActive()))
	}

	e.shuttingDown.Store(true)
	e.onEndOfRun()

	dr, ok := e.stoppedRing.getByGID(gid)
	if !ok {
		t.Fatalf("stopped result missing for gid %s", gid)
	}
	if dr.errCode != core.ExitInProgress {
		t.Fatalf("shutdown errCode = %v, want %v", dr.errCode, core.ExitInProgress)
	}
	if got := e.ExitCode(); got != core.ExitUnfinishedDownloads {
		t.Fatalf("ExitCode() = %v, want %v", got, core.ExitUnfinishedDownloads)
	}
}

func TestExitCode_FallsBackToShutdownInProgressWhenStoppedRingIsEmpty(t *testing.T) {
	e, err := New(&config.Options{
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		Dir:                    t.TempDir(),
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := e.Add(AddSpec{URIs: []string{"http://example.com/active.iso"}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	e.fillRequestGroupFromReserver()
	e.shuttingDown.Store(true)
	e.onEndOfRun()
	e.stoppedRing.purge()

	if got := e.ExitCode(); got != core.ExitUnfinishedDownloads {
		t.Fatalf("ExitCode() after purging stopped ring = %v, want %v", got, core.ExitUnfinishedDownloads)
	}
}

func TestExitCode_FallsBackToShutdownErrorWhenStoppedRingIsEmpty(t *testing.T) {
	e, err := New(&config.Options{
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		Dir:                    t.TempDir(),
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, err := e.Add(AddSpec{URIs: []string{"http://example.com/active.iso"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	e.fillRequestGroupFromReserver()

	rg, ok := e.groups.getLocked(gid)
	if !ok {
		t.Fatalf("group %s missing", gid)
	}
	rg.errCode = core.ExitNetworkProblem
	rg.errMsg = "network failed"
	e.groups.unlock(gid)

	e.shuttingDown.Store(true)
	e.onEndOfRun()
	e.stoppedRing.purge()

	if got := e.ExitCode(); got != core.ExitNetworkProblem {
		t.Fatalf("ExitCode() after purging stopped ring = %v, want %v", got, core.ExitNetworkProblem)
	}
}

func TestMarkTransferCanceledGracefulHaltLeavesResultUnspecified(t *testing.T) {
	rg := &requestGroup{
		haltRequested: true,
		errCode:       core.ExitSuccess,
		errMsg:        "old",
	}

	markTransferCanceled(rg)

	if rg.errCode != 0 {
		t.Fatalf("errCode = %v, want 0 for graceful halt", rg.errCode)
	}
	if rg.errMsg != "" {
		t.Fatalf("errMsg = %q, want empty for graceful halt", rg.errMsg)
	}
}

func TestMarkTransferCanceledRemovePreservesRemovedOutcome(t *testing.T) {
	rg := &requestGroup{
		forceHaltReq: true,
	}

	markTransferCanceled(rg)

	if rg.errCode != core.ExitRemoved {
		t.Fatalf("errCode = %v, want %v", rg.errCode, core.ExitRemoved)
	}
	if rg.errMsg != "download cancelled" {
		t.Fatalf("errMsg = %q, want %q", rg.errMsg, "download cancelled")
	}
}

func TestSaveSession_NoPath(t *testing.T) {
	e, _ := New(testOpts(), testLogger(t))
	if err := e.SaveSession(); err == nil {
		t.Error("SaveSession() expected error when save-session not configured")
	}
}

func TestChangePosition_ValidModes(t *testing.T) {
	e, _ := New(testOpts(), testLogger(t))
	e.Add(AddSpec{URIs: []string{"http://example.com/a"}})
	e.Add(AddSpec{URIs: []string{"http://example.com/b"}})
	c, _ := e.Add(AddSpec{URIs: []string{"http://example.com/c"}})

	pos, err := e.ChangePosition(c, 0, "POS_SET")
	if err != nil {
		t.Fatalf("POS_SET error: %v", err)
	}
	if pos != 0 {
		t.Errorf("POS_SET result = %d, want 0", pos)
	}

	_, err = e.ChangePosition(c, 0, "INVALID")
	if err == nil {
		t.Error("expected error for invalid how")
	}
}

func TestChangePosition_NotFound(t *testing.T) {
	e, _ := New(testOpts(), testLogger(t))
	_, err := e.ChangePosition(99999, 0, "POS_SET")
	if err == nil {
		t.Error("expected error for unknown GID")
	}
}

func TestChangeURIAppendInsertCountsAndValidation(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	gid, err := e.Add(AddSpec{URIs: []string{
		"http://example.com/a.iso",
		"http://example.com/b.iso",
	}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	deleted, added, err := e.ChangeURIWithPosition(gid, 1,
		[]string{"http://example.com/b.iso", "http://example.com/missing.iso"},
		[]string{"http://mirror.example.com/c.iso", "not a uri"},
		0,
		false,
	)
	if err != nil {
		t.Fatalf("ChangeURIWithPosition(append) error = %v", err)
	}
	if deleted != 1 || added != 1 {
		t.Fatalf("append counts = [%d %d], want [1 1]", deleted, added)
	}

	deleted, added, err = e.ChangeURIWithPosition(gid, 1,
		nil,
		[]string{"http://mirror.example.com/front.iso"},
		0,
		true,
	)
	if err != nil {
		t.Fatalf("ChangeURIWithPosition(insert) error = %v", err)
	}
	if deleted != 0 || added != 1 {
		t.Fatalf("insert counts = [%d %d], want [0 1]", deleted, added)
	}

	status, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	got := []string{
		status.Files[0].URIs[0].URI,
		status.Files[0].URIs[1].URI,
		status.Files[0].URIs[2].URI,
	}
	want := []string{
		"http://mirror.example.com/front.iso",
		"http://example.com/a.iso",
		"http://mirror.example.com/c.iso",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("URIs = %v, want %v", got, want)
		}
	}

	if _, _, err := e.ChangeURIWithPosition(gid, 0, nil, nil, 0, true); err == nil {
		t.Fatal("fileIndex 0 expected error")
	}
	if _, _, err := e.ChangeURIWithPosition(gid, 2, nil, nil, 0, true); err == nil {
		t.Fatal("fileIndex out of range expected error")
	}
	if _, _, err := e.ChangeURIWithPosition(gid, 1, nil, nil, -1, true); err == nil {
		t.Fatal("negative explicit position expected error")
	}
}

func TestURIStatusWaitingVsActive(t *testing.T) {
	e, err := New(&config.Options{
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	gid, err := e.Add(AddSpec{URIs: []string{
		"http://example.com/a.iso",
		"http://example.com/b.iso",
	}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	waiting, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus(waiting) error = %v", err)
	}
	for _, uri := range waiting.Files[0].URIs {
		if uri.Status != "waiting" {
			t.Fatalf("waiting URI status = %q, want waiting", uri.Status)
		}
	}

	e.fillRequestGroupFromReserver()
	active, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus(active) error = %v", err)
	}
	if active.Files[0].URIs[0].Status != "used" {
		t.Fatalf("active first URI status = %q, want used", active.Files[0].URIs[0].Status)
	}
	if active.Files[0].URIs[1].Status != "waiting" {
		t.Fatalf("active second URI status = %q, want waiting", active.Files[0].URIs[1].Status)
	}
}

func TestGetDownloadStat(t *testing.T) {
	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 3,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
	}, testLogger(t))

	completed, errors, inProgress, waiting := e.GetDownloadStat()
	if completed != 0 || errors != 0 || inProgress != 0 || waiting != 0 {
		t.Errorf("empty engine stats: completed=%d errors=%d inProgress=%d waiting=%d",
			completed, errors, inProgress, waiting)
	}

	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	e.fillRequestGroupFromReserver()
	e.Remove(gid, false)

	completed, errors, inProgress, waiting = e.GetDownloadStat()
	if inProgress != 0 {
		t.Errorf("inProgress = %d, want 0 after remove", inProgress)
	}
	if completed != 0 {
		t.Errorf("completed = %d, want 0 (removed)", completed)
	}
	if errors != 0 {
		t.Errorf("errors = %d, want 0", errors)
	}
}

func TestNumStopped(t *testing.T) {
	e, _ := New(testOpts(), testLogger(t))
	if n := e.NumStopped(); n != 0 {
		t.Errorf("NumStopped() = %d, want 0", n)
	}

	gid, _ := e.Add(AddSpec{URIs: []string{"http://example.com/a"}})
	e.fillRequestGroupFromReserver()
	e.Remove(gid, false)

	if n := e.NumStopped(); n != 1 {
		t.Errorf("NumStopped() = %d, want 1", n)
	}
}

func TestSaveSession_WithPath(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.txt")

	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
		SaveSession:            sessionPath,
	}, testLogger(t))

	e.Add(AddSpec{
		URIs:    []string{"http://example.com/file.iso"},
		Options: &config.Options{Dir: "/downloads", Out: "file.iso"},
	})
	e.Add(AddSpec{URIs: []string{"http://example.com/a", "http://example.com/b"}})

	if err := e.SaveSession(); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("session file not written: %v", err)
	}

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	if len(data) == 0 {
		t.Error("session file is empty")
	}
}

func TestSaveSession_EmptyQueueCreatesEmptyFileAndUpdatesHash(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.txt")
	if err := os.WriteFile(sessionPath, []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("seed session file: %v", err)
	}

	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
		Dir:                    dir,
		SaveSession:            sessionPath,
	}, testLogger(t))

	if err := e.SaveSession(); err != nil {
		t.Fatalf("first SaveSession() error = %v", err)
	}

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("session file len = %d, want 0", len(data))
	}

	wantHash, err := sessionfile.SerializedHash(nil)
	if err != nil {
		t.Fatalf("SerializedHash(nil): %v", err)
	}
	if !e.hasSessionHash {
		t.Fatal("hasSessionHash = false, want true")
	}
	if e.lastSessionHash != wantHash {
		t.Fatalf("lastSessionHash = %x, want %x", e.lastSessionHash, wantHash)
	}

	stat1, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("stat first session file: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := e.SaveSession(); err != nil {
		t.Fatalf("second SaveSession() error = %v", err)
	}
	stat2, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("stat second session file: %v", err)
	}
	if !stat2.ModTime().Equal(stat1.ModTime()) {
		t.Fatalf("empty SaveSession rewrote file: first=%s second=%s", stat1.ModTime(), stat2.ModTime())
	}
}

func TestSaveSessionAfterShutdownPreservesActiveAndWaiting(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.txt")

	e, err := New(&config.Options{
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		Dir:                    dir,
		SaveSession:            sessionPath,
		EnableRPC:              true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := e.Add(AddSpec{
		URIs:    []string{"http://example.com/active.iso"},
		Options: &config.Options{Dir: dir, Out: "active.iso"},
	}); err != nil {
		t.Fatalf("Add(active) error = %v", err)
	}
	if _, err := e.Add(AddSpec{
		URIs:    []string{"http://example.com/paused.iso"},
		Options: &config.Options{Dir: dir, Out: "paused.iso", Pause: true},
	}); err != nil {
		t.Fatalf("Add(paused) error = %v", err)
	}

	e.fillRequestGroupFromReserver()
	if len(e.TellActive()) != 1 {
		t.Fatalf("expected 1 active download before shutdown snapshot")
	}
	if len(e.TellWaiting(0, 10)) != 1 {
		t.Fatalf("expected 1 waiting download before shutdown snapshot")
	}

	e.shuttingDown.Store(true)
	e.onEndOfRun()

	if err := e.SaveSession(); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	f, err := os.Open(sessionPath)
	if err != nil {
		t.Fatalf("open session file: %v", err)
	}
	defer f.Close()

	entries, err := sessionfile.Read(f)
	if err != nil {
		t.Fatalf("sessionfile.Read() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("saved entries = %d, want 2", len(entries))
	}
	if got := entries[0].URIs[0]; got != "http://example.com/active.iso" {
		t.Fatalf("first saved entry = %q, want active URI first", got)
	}
	if got := entries[1].URIs[0]; got != "http://example.com/paused.iso" {
		t.Fatalf("second saved entry = %q, want paused URI second", got)
	}

	seen := map[string]core.Status{}
	for _, entry := range entries {
		if len(entry.URIs) == 0 {
			t.Fatalf("saved entry missing URIs: %+v", entry)
		}
		seen[entry.URIs[0]] = entry.Status
	}
	if seen["http://example.com/active.iso"] != core.StatusWaiting {
		t.Fatalf("active shutdown entry status = %s, want waiting", seen["http://example.com/active.iso"])
	}
	if seen["http://example.com/paused.iso"] != core.StatusPaused {
		t.Fatalf("paused shutdown entry status = %s, want paused", seen["http://example.com/paused.iso"])
	}
}

func TestSaveSessionSkipsUnchangedSerializedContent(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.txt")

	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
		SaveSession:            sessionPath,
	}, testLogger(t))

	e.Add(AddSpec{URIs: []string{"http://example.com/file.iso"}})
	if err := e.SaveSession(); err != nil {
		t.Fatalf("first SaveSession() error = %v", err)
	}
	stat1, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("stat first session file: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := e.SaveSession(); err != nil {
		t.Fatalf("second SaveSession() error = %v", err)
	}
	stat2, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("stat second session file: %v", err)
	}
	if !stat2.ModTime().Equal(stat1.ModTime()) {
		t.Fatalf("unchanged SaveSession rewrote file: first=%s second=%s", stat1.ModTime(), stat2.ModTime())
	}
}

func TestRemoveWaitingDropsWithoutStoppedResult(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, err := e.Add(AddSpec{URIs: []string{"http://example.com/waiting.iso"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := e.Remove(gid, false); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := e.TellStatus(gid); err == nil {
		t.Fatal("TellStatus() on removed waiting download unexpectedly succeeded")
	}
	if stopped := e.TellStopped(0, 10); len(stopped) != 0 {
		t.Fatalf("TellStopped len = %d, want 0", len(stopped))
	}
	if got := e.NumStopped(); got != 0 {
		t.Fatalf("NumStopped() = %d, want 0", got)
	}
}

func TestLoadSession_RestoresDownloads(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.txt")

	e1, _ := New(&config.Options{
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
		SaveSession:            sessionPath,
	}, testLogger(t))

	e1.Add(AddSpec{
		URIs:    []string{"http://example.com/file.iso"},
		Options: &config.Options{Dir: "/dl", Out: "file.iso"},
	})
	e1.Add(AddSpec{URIs: []string{"http://example.com/a"}})

	if err := e1.SaveSession(); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	e2, _ := New(testOpts(), testLogger(t))
	if err := e2.LoadSession(sessionPath); err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}

	waiting := e2.TellWaiting(0, 100)
	if len(waiting) != 2 {
		t.Errorf("expected 2 restored downloads, got %d", len(waiting))
	}
}

func TestLoadSession_UsesSavedGIDAndRejectsDuplicateReload(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.txt")
	data := "http://example.com/file.iso\n gid=00000000000000ab\n dir=" + dir + "\n out=file.iso\n"
	if err := os.WriteFile(sessionPath, []byte(data), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := e.LoadSession(sessionPath); err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if err := e.LoadSession(sessionPath); err != nil {
		t.Fatalf("LoadSession() second call error = %v", err)
	}

	if _, err := e.TellStatus(core.GID(0xab)); err != nil {
		t.Fatalf("saved GID was not restored: %v", err)
	}
	waiting := e.TellWaiting(0, 10)
	if len(waiting) != 1 {
		t.Fatalf("waiting entries = %d, want 1 after duplicate reload", len(waiting))
	}
}

func TestLoadInputFileParsesSessionOptions(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.session")
	data := "http://example.com/file.iso\n  dir=" + dir + "\n  out=file.iso\n"
	if err := os.WriteFile(inputPath, []byte(data), 0644); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := e.loadInputFile(inputPath); err != nil {
		t.Fatalf("loadInputFile() error = %v", err)
	}

	waiting := e.TellWaiting(0, 10)
	if len(waiting) != 1 {
		t.Fatalf("waiting entries = %d, want 1", len(waiting))
	}
	rg, ok := e.groups.getLocked(waiting[0].GID)
	if !ok {
		t.Fatal("requestGroup not found")
	}
	defer e.groups.unlock(waiting[0].GID)

	if len(rg.uris) != 1 || rg.uris[0] != "http://example.com/file.iso" {
		t.Fatalf("uris = %v, want only the URI line", rg.uris)
	}
	if rg.opts.Dir != dir {
		t.Fatalf("Dir = %q, want %q", rg.opts.Dir, dir)
	}
	if rg.opts.Out != "file.iso" {
		t.Fatalf("Out = %q, want file.iso", rg.opts.Out)
	}
}

func TestLoadInputFileReadsStdinDash(t *testing.T) {
	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})

	if _, err := w.WriteString("http://example.com/stdin.bin\n"); err != nil {
		t.Fatalf("write stdin pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin pipe writer: %v", err)
	}

	if err := e.loadInputFile("-"); err != nil {
		t.Fatalf("loadInputFile(-) error = %v", err)
	}
	waiting := e.TellWaiting(0, 10)
	if len(waiting) != 1 {
		t.Fatalf("waiting entries = %d, want 1", len(waiting))
	}
	if len(waiting[0].Files) != 1 || filepath.Base(waiting[0].Files[0].Path) != "stdin.bin" {
		t.Fatalf("waiting file status = %+v, want basename stdin.bin", waiting[0].Files)
	}
}

func TestLoadInputFileExpandsParameterizedForceSequential(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.session")
	data := "http://example.com/asset-[1-2].bin\n  parameterized-uri=true\n  force-sequential=true\n"
	if err := os.WriteFile(inputPath, []byte(data), 0644); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := e.loadInputFile(inputPath); err != nil {
		t.Fatalf("loadInputFile() error = %v", err)
	}

	waiting := e.TellWaiting(0, 10)
	if len(waiting) != 2 {
		t.Fatalf("waiting entries = %d, want 2", len(waiting))
	}
	got := []string{filepath.Base(waiting[0].Files[0].Path), filepath.Base(waiting[1].Files[0].Path)}
	want := []string{"asset-1.bin", "asset-2.bin"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("waiting paths = %v, want %v", got, want)
		}
	}
}

func TestLoadInputFileSkipsUnrecognizedEntries(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.session")
	data := "not-a-uri\nhttp://example.com/file.iso\n  dir=" + dir + "\n  out=file.iso\n"
	if err := os.WriteFile(inputPath, []byte(data), 0644); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := e.loadInputFile(inputPath); err != nil {
		t.Fatalf("loadInputFile() error = %v", err)
	}

	waiting := e.TellWaiting(0, 10)
	if len(waiting) != 1 {
		t.Fatalf("waiting entries = %d, want 1", len(waiting))
	}
	if len(waiting[0].Files) != 1 || filepath.Base(waiting[0].Files[0].Path) != "file.iso" {
		t.Fatalf("waiting file status = %+v, want basename file.iso", waiting[0].Files)
	}
}

func TestLoadInputFileClassifiesLocalMetadataFiles(t *testing.T) {
	dir := t.TempDir()
	torrentPath := filepath.Join(dir, "single.torrent")
	if err := os.WriteFile(torrentPath, testSingleFileTorrent(t, "payload.bin", 32), 0644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}
	metalinkPath := filepath.Join(dir, "fixture.meta4")
	metalinkDoc := `<?xml version="1.0" encoding="utf-8"?><metalink xmlns="urn:ietf:params:xml:ns:metalink"><file name="payload.bin"><url>http://example.com/payload.bin</url></file></metalink>`
	if err := os.WriteFile(metalinkPath, []byte(metalinkDoc), 0644); err != nil {
		t.Fatalf("write metalink: %v", err)
	}
	inputPath := filepath.Join(dir, "input.session")
	data := torrentPath + "\n" + metalinkPath + "\n"
	if err := os.WriteFile(inputPath, []byte(data), 0644); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	e, err := New(testOpts(), testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := e.loadInputFile(inputPath); err != nil {
		t.Fatalf("loadInputFile() error = %v", err)
	}

	waiting := e.TellWaiting(0, 10)
	if len(waiting) != 2 {
		t.Fatalf("waiting entries = %d, want 2", len(waiting))
	}

	var sawTorrent, sawMetalink bool
	for _, status := range waiting {
		rg, ok := e.groups.getLocked(status.GID)
		if !ok {
			t.Fatalf("requestGroup %s not found", status.GID)
		}
		if rg.metadataURI == torrentPath {
			sawTorrent = len(rg.torrent) > 0 && len(rg.uris) == 0
		}
		if rg.metadataURI == metalinkPath {
			sawMetalink = len(rg.metalinkData) > 0 && len(rg.uris) == 0
		}
		e.groups.unlock(status.GID)
	}
	if !sawTorrent {
		t.Fatal("local torrent input entry was not classified as torrent metadata")
	}
	if !sawMetalink {
		t.Fatal("local metalink input entry was not classified as metalink metadata")
	}
}

func TestDeferredInputLoadsEntriesIncrementally(t *testing.T) {
	e, err := New(&config.Options{
		Dir:                    t.TempDir(),
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	parser, err := sessionfile.NewParser(strings.NewReader("http://example.com/one.iso\nhttp://example.com/two.iso\n"))
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}
	defer parser.Close()
	e.inputParser = parser

	e.fillRequestGroupFromReserver()

	if len(e.active) != 1 {
		t.Fatalf("active entries = %d, want 1", len(e.active))
	}
	if len(e.waiting) != 0 {
		t.Fatalf("waiting entries = %d, want 0 after first deferred promotion", len(e.waiting))
	}
	if e.inputParser == nil {
		t.Fatal("input parser should still contain deferred entries")
	}
	if _, ok := e.groups.get(e.active[0]); !ok {
		t.Fatal("active deferred entry was not added to the engine")
	}
}

func TestSaveSessionWritesExplicitRequestOptionsAndMetadataURI(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.txt")
	torrentPath := filepath.Join(dir, "single.torrent")
	if err := os.WriteFile(torrentPath, testSingleFileTorrent(t, "payload.bin", 32), 0644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}

	e, err := New(&config.Options{
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		Dir:                    dir,
		SaveSession:            sessionPath,
		EnableRPC:              true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	localOpts := &config.Options{
		Dir:              dir,
		Out:              "payload.bin",
		Pause:            true,
		Header:           []string{"X-Test: one", "X-Test: two"},
		MaxDownloadLimit: "2K",
	}
	for _, name := range []string{"dir", "out", "pause", "header", "max-download-limit"} {
		localOpts.MarkExplicit(name)
	}

	if _, err := e.Add(AddSpec{
		Torrent:     testSingleFileTorrent(t, "payload.bin", 32),
		Options:     localOpts,
		MetadataURI: torrentPath,
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := e.SaveSession(); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	out := string(data)
	if !strings.HasPrefix(out, torrentPath+"\t\n") {
		t.Fatalf("session file should start with metadata URI %q, got:\n%s", torrentPath, out)
	}
	for _, want := range []string{
		" pause=true\n",
		" dir=" + dir + "\n",
		" out=payload.bin\n",
		" header=X-Test: one\n",
		" header=X-Test: two\n",
		" max-download-limit=2048\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("session file missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " save-session=") {
		t.Fatalf("session file should not serialize global save-session option:\n%s", out)
	}
	if strings.Count(out, " gid=") != 1 {
		t.Fatalf("gid line count = %d, want 1:\n%s", strings.Count(out, " gid="), out)
	}
	if strings.Count(out, " pause=true\n") != 1 {
		t.Fatalf("pause line count = %d, want 1:\n%s", strings.Count(out, " pause=true\n"), out)
	}
}

func TestLoadSession_NotFound(t *testing.T) {
	e, _ := New(testOpts(), testLogger(t))
	if err := e.LoadSession("/nonexistent/session.txt"); err != nil {
		t.Errorf("LoadSession() error = %v", err)
	}
}

// TestTryAutoFileRenaming tests the 9 extension-splitting patterns from aria2's
// RequestGroup::tryAutoFileRenaming.
func TestTryAutoFileRenaming(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"foo.txt", "foo.1.txt"},
		{".dotfile", ".dotfile.1"},
		{"dir/foo", "dir/foo.1"},
		{"dir/foo.txt", "dir/foo.1.txt"},
		{"dir/.hidden", "dir/.hidden.1"},
		{"foo.tar.gz", "foo.tar.1.gz"},
		{"a/b/c.txt", "a/b/c.1.txt"},
		{"file", "file.1"},
		{"/absolute/path/file.txt", "/absolute/path/file.1.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := tryAutoFileRenaming(tt.input)
			if got != tt.expected {
				t.Errorf("tryAutoFileRenaming(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTryAutoFileRenaming_Empty(t *testing.T) {
	got := tryAutoFileRenaming("")
	if got != "" {
		t.Errorf("tryAutoFileRenaming(\"\") = %q, want \"\"", got)
	}
}

// TestGetFirstFilePath tests the first file path logic for both regular and
// in-memory downloads, matching aria2's RequestGroup::getFirstFilePath.
func TestGetFirstFilePath(t *testing.T) {
	t.Run("regular download", func(t *testing.T) {
		rg := &requestGroup{
			filePath: "/downloads/foo.txt",
			inMemory: false,
		}
		got := rg.getFirstFilePath()
		if got != "/downloads/foo.txt" {
			t.Errorf("getFirstFilePath() = %q, want %q", got, "/downloads/foo.txt")
		}
	})

	t.Run("memory download", func(t *testing.T) {
		rg := &requestGroup{
			filePath: "/downloads/foo.txt",
			inMemory: true,
		}
		got := rg.getFirstFilePath()
		expected := "[MEMORY]foo.txt"
		if got != expected {
			t.Errorf("getFirstFilePath() = %q, want %q", got, expected)
		}
	})

	t.Run("memory download with subdir", func(t *testing.T) {
		rg := &requestGroup{
			filePath: "/a/b/c/file.iso",
			inMemory: true,
		}
		got := rg.getFirstFilePath()
		expected := "[MEMORY]file.iso"
		if got != expected {
			t.Errorf("getFirstFilePath() = %q, want %q", got, expected)
		}
	})

	t.Run("regular download empty path", func(t *testing.T) {
		rg := &requestGroup{
			filePath: "",
			inMemory: false,
		}
		got := rg.getFirstFilePath()
		if got != "" {
			t.Errorf("getFirstFilePath() = %q, want \"\"", got)
		}
	})

	t.Run("memory download empty path", func(t *testing.T) {
		rg := &requestGroup{
			filePath: "",
			inMemory: true,
		}
		got := rg.getFirstFilePath()
		if got != "[MEMORY]." {
			t.Errorf("getFirstFilePath() = %q, want %q", got, "[MEMORY].")
		}
	})
}

// TestIsSameFileBeingDownloaded verifies the same-file detection logic
// matching aria2's RequestGroupMan::isSameFileBeingDownloaded.
func TestIsSameFileBeingDownloaded(t *testing.T) {
	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
	}, testLogger(t))

	// No downloads yet.
	if e.isSameFileBeingDownloaded("/tmp/test.iso", 0) {
		t.Error("expected false with no active/waiting downloads")
	}

	// Add a download with a specific path.
	gid, _ := e.Add(AddSpec{
		URIs:    []string{"http://example.com/test.iso"},
		Options: &config.Options{Dir: "/tmp", Out: "test.iso"},
	})
	e.fillRequestGroupFromReserver()

	// Same path should be detected.
	if !e.isSameFileBeingDownloaded("/tmp/test.iso", 0) {
		t.Error("expected true for same path as active download")
	}

	// Different path should not be detected.
	if e.isSameFileBeingDownloaded("/tmp/other.iso", 0) {
		t.Error("expected false for different path")
	}

	// Case sensitivity test — paths are compared exactly.
	if e.isSameFileBeingDownloaded("/TMP/test.iso", 0) {
		t.Error("expected false for case-different path")
	}

	// After removing the download, it should no longer be found.
	e.Remove(gid, false)
	if e.isSameFileBeingDownloaded("/tmp/test.iso", 0) {
		t.Error("expected false after removing all downloads")
	}
}

func TestIsSameFileBeingDownloaded_Waiting(t *testing.T) {
	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 2,
		MaxDownloadResult:      10,
		Dir:                    "/tmp/aria2go-test",
	}, testLogger(t))

	// Add downloads limited by max=2 — third stays waiting.
	e.Add(AddSpec{URIs: []string{"http://a.com/a.iso"}, Options: &config.Options{Dir: "/tmp", Out: "a.iso"}})
	e.Add(AddSpec{URIs: []string{"http://b.com/b.iso"}, Options: &config.Options{Dir: "/tmp", Out: "b.iso"}})
	e.Add(AddSpec{URIs: []string{"http://c.com/c.iso"}, Options: &config.Options{Dir: "/tmp", Out: "c.iso"}})
	e.fillRequestGroupFromReserver()

	// Should detect both active and waiting paths.
	if !e.isSameFileBeingDownloaded("/tmp/a.iso", 0) {
		t.Error("expected true for active download")
	}
	if !e.isSameFileBeingDownloaded("/tmp/b.iso", 0) {
		t.Error("expected true for active download")
	}
	if !e.isSameFileBeingDownloaded("/tmp/c.iso", 0) {
		t.Error("expected true for waiting download")
	}
}

// TestResolveProxyURI verifies the proxy URI resolution logic matching
// aria2's getProxyUri + getProxyOptionFor in AbstractCommand.cc.
func TestResolveProxyURI(t *testing.T) {
	t.Run("http proxy", func(t *testing.T) {
		opts := &config.Options{
			HTTPProxy: "http://proxy.example.com:8080",
		}
		got := resolveProxyURI("http", opts)
		if got != "http://proxy.example.com:8080" {
			t.Errorf("resolveProxyURI(http) = %q, want %q", got, "http://proxy.example.com:8080")
		}
	})

	t.Run("https proxy", func(t *testing.T) {
		opts := &config.Options{
			HTTPSProxy: "http://ssl-proxy.example.com:8443",
		}
		got := resolveProxyURI("https", opts)
		if got != "http://ssl-proxy.example.com:8443" {
			t.Errorf("resolveProxyURI(https) = %q, want %q", got, "http://ssl-proxy.example.com:8443")
		}
	})

	t.Run("ftp proxy", func(t *testing.T) {
		opts := &config.Options{
			FTPProxy: "http://ftp-proxy.example.com:2121",
		}
		got := resolveProxyURI("ftp", opts)
		if got != "http://ftp-proxy.example.com:2121" {
			t.Errorf("resolveProxyURI(ftp) = %q, want %q", got, "http://ftp-proxy.example.com:2121")
		}
	})

	t.Run("sftp proxy", func(t *testing.T) {
		opts := &config.Options{
			FTPProxy: "http://sftp-proxy.example.com:2222",
		}
		got := resolveProxyURI("sftp", opts)
		if got != "http://sftp-proxy.example.com:2222" {
			t.Errorf("resolveProxyURI(sftp) = %q, want %q", got, "http://sftp-proxy.example.com:2222")
		}
	})

	t.Run("socks proxy via all-proxy", func(t *testing.T) {
		opts := &config.Options{
			AllProxy: "socks5://localhost:1080",
		}
		got := resolveProxyURI("http", opts)
		if got != "socks5://localhost:1080" {
			t.Errorf("resolveProxyURI(http via all-proxy) = %q, want %q", got, "socks5://localhost:1080")
		}
	})

	t.Run("http proxy with user", func(t *testing.T) {
		opts := &config.Options{
			HTTPProxy:     "http://proxy.example.com:8080",
			HTTPProxyUser: "alice",
		}
		got := resolveProxyURI("http", opts)
		expected := "http://alice@proxy.example.com:8080"
		if got != expected {
			t.Errorf("resolveProxyURI(http with user) = %q, want %q", got, expected)
		}
	})

	t.Run("http proxy with user and passwd", func(t *testing.T) {
		opts := &config.Options{
			HTTPProxy:       "http://proxy.example.com:8080",
			HTTPProxyUser:   "alice",
			HTTPProxyPasswd: "s3cret",
		}
		got := resolveProxyURI("http", opts)
		expected := "http://alice:s3cret@proxy.example.com:8080"
		if got != expected {
			t.Errorf("resolveProxyURI(http with user+pass) = %q, want %q", got, expected)
		}
	})

	t.Run("all-proxy override", func(t *testing.T) {
		opts := &config.Options{
			AllProxy:       "http://global-proxy.example.com:3128",
			AllProxyUser:   "bob",
			AllProxyPasswd: "pass",
		}
		// No http-proxy set, so fallback to all-proxy.
		got := resolveProxyURI("http", opts)
		expected := "http://bob:pass@global-proxy.example.com:3128"
		if got != expected {
			t.Errorf("resolveProxyURI(http fallback to all-proxy) = %q, want %q", got, expected)
		}
	})

	t.Run("protocol-specific proxy takes precedence", func(t *testing.T) {
		opts := &config.Options{
			HTTPProxy: "http://http-proxy.example.com:8080",
			AllProxy:  "http://all-proxy.example.com:3128",
		}
		got := resolveProxyURI("http", opts)
		expected := "http://http-proxy.example.com:8080"
		if got != expected {
			t.Errorf("resolveProxyURI(http precedence) = %q, want %q", got, expected)
		}
	})

	t.Run("unsupported protocol returns empty", func(t *testing.T) {
		opts := &config.Options{
			AllProxy: "http://proxy.example.com:3128",
		}
		got := resolveProxyURI("bittorrent", opts)
		if got != "" {
			t.Errorf("resolveProxyURI(bittorrent) = %q, want \"\"", got)
		}
	})

	t.Run("empty proxy returns empty", func(t *testing.T) {
		opts := &config.Options{}
		got := resolveProxyURI("http", opts)
		if got != "" {
			t.Errorf("resolveProxyURI(http empty) = %q, want \"\"", got)
		}
	})

	t.Run("invalid proxy URI returns empty", func(t *testing.T) {
		opts := &config.Options{
			HTTPProxy: "not a valid url",
		}
		got := resolveProxyURI("http", opts)
		if got != "" {
			t.Errorf("resolveProxyURI(http invalid) = %q, want \"\"", got)
		}
	})

	t.Run("proxy without scheme falls back", func(t *testing.T) {
		// A proxy URI without a scheme is invalid per url.Parse,
		// so it should fall back to all-proxy.
		opts := &config.Options{
			HTTPProxy: "proxy.example.com:8080",
			AllProxy:  "http://fallback.example.com:3128",
		}
		got := resolveProxyURI("http", opts)
		if got != "http://fallback.example.com:3128" {
			t.Errorf("resolveProxyURI(http no-scheme fallback) = %q, want %q", got, "http://fallback.example.com:3128")
		}
	})

	t.Run("proxy user without passwd does not set empty password", func(t *testing.T) {
		opts := &config.Options{
			HTTPProxy:     "http://proxy.example.com:8080",
			HTTPProxyUser: "alice",
			// No HTTPProxyPasswd set — URL should have user but no password.
		}
		got := resolveProxyURI("http", opts)
		expected := "http://alice@proxy.example.com:8080"
		if got != expected {
			t.Errorf("resolveProxyURI(http user no pass) = %q, want %q", got, expected)
		}
	})
}

func TestResolveProxyURIForTargetHonorsNoProxy(t *testing.T) {
	opts := &config.Options{
		HTTPProxy: "http://proxy.example.com:8080",
		NoProxy:   "example.com,.bypass.test,127.0.0.0/8",
	}

	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "exact host bypass", host: "example.com", want: ""},
		{name: "exact host does not match subdomain", host: "www.example.com", want: "http://proxy.example.com:8080"},
		{name: "domain suffix bypass", host: "api.bypass.test", want: ""},
		{name: "cidr bypass", host: "127.0.0.1", want: ""},
		{name: "non-match uses proxy", host: "other.test", want: "http://proxy.example.com:8080"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveProxyURIForTarget("http", tc.host, opts); got != tc.want {
				t.Fatalf("resolveProxyURIForTarget(%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
	}
}

func TestHTTPDriverForURINoProxyBypassesInvalidProxy(t *testing.T) {
	opts := testOpts()
	opts.HTTPProxy = "http://%"
	opts.NoProxy = "example.com"

	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := e.httpDriverForURI(nil, "http://example.com/file.iso"); err != nil {
		t.Fatalf("httpDriverForURI() error = %v", err)
	}
}

func TestResolveHTTPProxyMethod(t *testing.T) {
	t.Run("http defaults to get", func(t *testing.T) {
		if got := resolveHTTPProxyMethod("http", &config.Options{}); got != "get" {
			t.Fatalf("resolveHTTPProxyMethod(http) = %q, want get", got)
		}
	})

	t.Run("https always tunnels", func(t *testing.T) {
		if got := resolveHTTPProxyMethod("https", &config.Options{}); got != "tunnel" {
			t.Fatalf("resolveHTTPProxyMethod(https) = %q, want tunnel", got)
		}
	})

	t.Run("explicit tunnel overrides http default", func(t *testing.T) {
		if got := resolveHTTPProxyMethod("http", &config.Options{ProxyMethod: "tunnel"}); got != "tunnel" {
			t.Fatalf("resolveHTTPProxyMethod(http tunnel) = %q, want tunnel", got)
		}
	})
}

func TestPerDownloadThrottle(t *testing.T) {
	t.Run("throttle created when limit set", func(t *testing.T) {
		e, _ := New(testOpts(), testLogger(t))
		gid, _ := e.Add(AddSpec{
			URIs: []string{"http://example.com/file.iso"},
			Options: &config.Options{
				MaxDownloadLimit: "100K",
			},
		})

		rg, ok := e.groups.getLocked(gid)
		if !ok {
			t.Fatal("requestGroup not found")
		}
		defer e.groups.unlock(gid)

		if rg.downloadLimit == nil {
			t.Fatal("downloadLimit should not be nil when MaxDownloadLimit is set")
		}
		if rg.downloadLimit.rate.Load() != 102400 {
			t.Errorf("downloadLimit rate = %d, want 102400", rg.downloadLimit.rate.Load())
		}
	})

	t.Run("throttle not created when limit is zero", func(t *testing.T) {
		e, _ := New(testOpts(), testLogger(t))
		gid, _ := e.Add(AddSpec{
			URIs: []string{"http://example.com/file.iso"},
			Options: &config.Options{
				MaxDownloadLimit: "0",
			},
		})

		rg, ok := e.groups.getLocked(gid)
		if !ok {
			t.Fatal("requestGroup not found")
		}
		defer e.groups.unlock(gid)

		if rg.downloadLimit != nil {
			t.Error("downloadLimit should be nil when MaxDownloadLimit is 0")
		}
	})

	t.Run("throttle rate is parsed correctly", func(t *testing.T) {
		tests := []struct {
			input    string
			expected int64
		}{
			{"1K", 1024},
			{"10K", 10240},
			{"1M", 1048576},
			{"100", 100},
		}
		for _, tt := range tests {
			e, _ := New(testOpts(), testLogger(t))
			gid, _ := e.Add(AddSpec{
				URIs: []string{"http://example.com/file.iso"},
				Options: &config.Options{
					MaxDownloadLimit: tt.input,
				},
			})

			rg, ok := e.groups.getLocked(gid)
			if !ok {
				e.groups.unlock(gid)
				t.Fatalf("requestGroup not found for input %s", tt.input)
			}
			if rg.downloadLimit == nil {
				e.groups.unlock(gid)
				t.Fatalf("downloadLimit is nil for input %s", tt.input)
			}
			if rg.downloadLimit.rate.Load() != tt.expected {
				e.groups.unlock(gid)
				t.Errorf("downloadLimit rate = %d, want %d for input %s", rg.downloadLimit.rate.Load(), tt.expected, tt.input)
			}
			e.groups.unlock(gid)
		}
	})

	t.Run("global and per-download throttles both created", func(t *testing.T) {
		e, _ := New(&config.Options{
			MaxOverallDownloadLimit: "200K",
			MaxConcurrentDownloads:  5,
			MaxDownloadResult:       10,
			Dir:                     "/tmp/aria2go-test",
		}, testLogger(t))

		gid, _ := e.Add(AddSpec{
			URIs: []string{"http://example.com/file.iso"},
			Options: &config.Options{
				MaxDownloadLimit: "50K",
			},
		})

		if e.rateGlobal.rate.Load() != 204800 {
			t.Errorf("global rate = %d, want 204800", e.rateGlobal.rate.Load())
		}

		rg, ok := e.groups.getLocked(gid)
		if !ok {
			t.Fatal("requestGroup not found")
		}
		defer e.groups.unlock(gid)

		if rg.downloadLimit == nil {
			t.Fatal("per-download throttle should not be nil")
		}
		if rg.downloadLimit.rate.Load() != 51200 {
			t.Errorf("per-download rate = %d, want 51200", rg.downloadLimit.rate.Load())
		}
	})
}

func TestUploadThrottleWired(t *testing.T) {
	t.Run("upload throttle created in engine", func(t *testing.T) {
		e, _ := New(&config.Options{
			MaxOverallUploadLimit:  "100K",
			MaxConcurrentDownloads: 5,
			MaxDownloadResult:      10,
			Dir:                    "/tmp/aria2go-test",
		}, testLogger(t))

		if e.rateGlobalUp == nil {
			t.Fatal("rateGlobalUp should not be nil")
		}
		if e.rateGlobalUp.rate.Load() != 102400 {
			t.Errorf("upload throttle rate = %d, want 102400", e.rateGlobalUp.rate.Load())
		}
	})

	t.Run("upload throttle unlimited by default", func(t *testing.T) {
		e, _ := New(testOpts(), testLogger(t))
		if e.rateGlobalUp == nil {
			t.Fatal("rateGlobalUp should not be nil")
		}
		if e.rateGlobalUp.rate.Load() != 0 {
			t.Errorf("upload throttle rate = %d, want 0", e.rateGlobalUp.rate.Load())
		}
	})
}

func TestSpeedEMA_FeedbackLoop(t *testing.T) {
	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
		Dir:                    t.TempDir(),
	}, testLogger(t))

	closeTo := func(a, b, tolerance int64) bool {
		diff := a - b
		if diff < 0 {
			diff = -diff
		}
		return diff <= tolerance
	}

	// Add download 1
	gid1, _ := e.Add(AddSpec{
		URIs: []string{"http://example.com/file1.bin"},
	})
	// Add download 2
	gid2, _ := e.Add(AddSpec{
		URIs: []string{"http://example.com/file2.bin"},
	})

	e.fillRequestGroupFromReserver()

	rg1, _ := e.groups.get(gid1)
	rg2, _ := e.groups.get(gid2)

	// Simulate both downloads actively running and updating lastSpeedSample
	now := time.Now()
	rg1.lastSpeedSample = now.Add(-1 * time.Second)
	rg1.bytesDownloaded = 1000 // 1000 bytes/sec
	rg1.state = core.StatusActive

	rg2.lastSpeedSample = now.Add(-1 * time.Second)
	rg2.bytesDownloaded = 2000 // 2000 bytes/sec
	rg2.state = core.StatusActive

	// First tick of refreshStats:
	// dlInstant1 = 1000. smoothed1 = 0.25 * 1000 + 0.75 * 0 = 250
	// dlInstant2 = 2000. smoothed2 = 0.25 * 2000 + 0.75 * 0 = 500
	// totalDL = 750
	e.refreshStats()

	// Verify global speed
	globalSpeed := e.downloadSpeed.Load()
	if !closeTo(globalSpeed, 750, 5) {
		t.Errorf("First tick: expected global speed close to 750, got %d", globalSpeed)
	}

	// Verify individual speeds
	s1 := e.makeStatus(rg1)
	s2 := e.makeStatus(rg2)
	if !closeTo(s1.DownloadSpeed, 250, 5) {
		t.Errorf("First tick: expected download 1 speed close to 250, got %d", s1.DownloadSpeed)
	}
	if !closeTo(s2.DownloadSpeed, 500, 5) {
		t.Errorf("First tick: expected download 2 speed close to 500, got %d", s2.DownloadSpeed)
	}

	// Second tick with same download rate:
	rg1.lastSpeedSample = now.Add(-1 * time.Second)
	rg1.bytesDownloaded = 1000
	rg2.lastSpeedSample = now.Add(-1 * time.Second)
	rg2.bytesDownloaded = 2000

	e.refreshStats()

	// Verify global speed
	globalSpeed = e.downloadSpeed.Load()
	if !closeTo(globalSpeed, 1312, 5) { // 437 + 875 = 1312
		t.Errorf("Second tick: expected global speed close to 1312, got %d", globalSpeed)
	}

	// Verify individual speeds
	s1 = e.makeStatus(rg1)
	s2 = e.makeStatus(rg2)
	if !closeTo(s1.DownloadSpeed, 437, 5) {
		t.Errorf("Second tick: expected download 1 speed close to 437, got %d", s1.DownloadSpeed)
	}
	if !closeTo(s2.DownloadSpeed, 875, 5) {
		t.Errorf("Second tick: expected download 2 speed close to 875, got %d", s2.DownloadSpeed)
	}
}

func TestFileCollision_AutoRenaming(t *testing.T) {
	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
		Dir:                    t.TempDir(),
		AutoFileRenaming:       true,
	}, testLogger(t))

	// 1. Concurrent active downloads collision
	gid1, _ := e.Add(AddSpec{
		URIs:    []string{"http://127.0.0.1:1/test.bin"},
		Options: &config.Options{Out: "test.bin"},
	})
	gid2, _ := e.Add(AddSpec{
		URIs:    []string{"http://127.0.0.1:1/test.bin"},
		Options: &config.Options{Out: "test.bin"},
	})

	e.fillRequestGroupFromReserver()

	rg1, _ := e.groups.get(gid1)
	rg1.state = core.StatusActive

	rg2, _ := e.groups.get(gid2)
	rg2.state = core.StatusActive

	// Run runDownload for rg1 (it shouldn't collide since it is the first one)
	// We'll pass a cancelable context to it
	ctx, cancel := context.WithCancel(context.Background())
	rg1.ctx = ctx
	rg1.cancel = cancel
	e.wg.Add(1)
	go e.runDownload(rg1)

	// Sleep briefly for rg1's runDownload to run. It will update rg1's filePath
	time.Sleep(20 * time.Millisecond)

	// Now run runDownload for rg2. It should collide and auto-rename to test.1.bin
	ctx2, cancel2 := context.WithCancel(context.Background())
	rg2.ctx = ctx2
	rg2.cancel = cancel2
	e.wg.Add(1)
	e.runDownload(rg2)

	// One of them must be test.bin, and the other must be test.1.bin
	path1 := rg1.filePath
	path2 := rg2.filePath
	expectedPathNormal := filepath.Join(e.cfg.Dir, "test.bin")
	expectedPathRenamed := filepath.Join(e.cfg.Dir, "test.1.bin")

	if (path1 == expectedPathNormal && path2 == expectedPathRenamed) || (path1 == expectedPathRenamed && path2 == expectedPathNormal) {
		// Pass!
	} else {
		t.Errorf("Expected one path to be %s and other to be %s, but got path1=%s, path2=%s", expectedPathNormal, expectedPathRenamed, path1, path2)
	}

	// 2. File exists on disk collision
	dir := t.TempDir()
	e2, _ := New(&config.Options{
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
		Dir:                    dir,
		AutoFileRenaming:       true,
	}, testLogger(t))

	// Pre-create the file on disk
	filePath := filepath.Join(dir, "exists.bin")
	_ = os.WriteFile(filePath, []byte("hello"), 0644)

	gid3, _ := e2.Add(AddSpec{
		URIs:    []string{"http://127.0.0.1:1/exists.bin"},
		Options: &config.Options{Out: "exists.bin", Continue: false},
	})
	e2.fillRequestGroupFromReserver()
	rg3, _ := e2.groups.get(gid3)
	rg3.state = core.StatusActive
	ctx3, cancel3 := context.WithCancel(context.Background())
	rg3.ctx = ctx3
	rg3.cancel = cancel3

	e2.wg.Add(1)
	e2.runDownload(rg3)

	expectedPath3 := filepath.Join(dir, "exists.1.bin")
	if rg3.filePath != expectedPath3 {
		t.Errorf("Expected rg3 filePath renamed to %s, got %s", expectedPath3, rg3.filePath)
	}
}

func TestFileCollision_AutoRenamingDisabled(t *testing.T) {
	dir := t.TempDir()
	e, _ := New(&config.Options{
		MaxConcurrentDownloads: 5,
		MaxDownloadResult:      10,
		Dir:                    dir,
		AutoFileRenaming:       false,
	}, testLogger(t))

	// Pre-create the file on disk
	filePath := filepath.Join(dir, "exists.bin")
	_ = os.WriteFile(filePath, []byte("hello"), 0644)

	gid, _ := e.Add(AddSpec{
		URIs:    []string{"http://127.0.0.1:1/exists.bin"},
		Options: &config.Options{Out: "exists.bin", Continue: false},
	})
	e.fillRequestGroupFromReserver()
	rg, _ := e.groups.get(gid)
	rg.state = core.StatusActive
	ctx, cancel := context.WithCancel(context.Background())
	rg.ctx = ctx
	rg.cancel = cancel

	e.wg.Add(1)
	e.runDownload(rg)

	if rg.errCode != core.ExitFileAlreadyExists {
		t.Errorf("Expected errCode ExitFileAlreadyExists, got %d", rg.errCode)
	}
}

func TestFileCollisionAllowOverwriteKeepsOriginalPath(t *testing.T) {
	content := []byte("new")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "exists.bin")
	if err := os.WriteFile(outPath, []byte("old-data"), 0644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		AutoFileRenaming:       true,
		AllowOverwrite:         true,
		UseHead:                true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, err := e.Add(AddSpec{
		URIs:    []string{server.URL + "/exists.bin"},
		Options: &config.Options{Out: "exists.bin"},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	e.fillRequestGroupFromReserver()
	rg, _ := e.groups.get(gid)
	rg.state = core.StatusActive
	ctx, cancel := context.WithCancel(context.Background())
	rg.ctx = ctx
	rg.cancel = cancel

	e.wg.Add(1)
	e.runDownload(rg)

	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if rg.filePath != outPath {
		t.Fatalf("filePath = %q, want %q", rg.filePath, outPath)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("output = %q, want %q", got, content)
	}
	if _, err := os.Stat(filepath.Join(dir, "exists.1.bin")); !os.IsNotExist(err) {
		t.Fatalf("renamed output exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunBTDownloadAllowOverwriteKeepsOriginalPath(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "btfile.bin")
	if err := os.WriteFile(outPath, []byte("old-data"), 0644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		AutoFileRenaming:       true,
		AllowOverwrite:         true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{
		gid:    1,
		opts:   e.cfg,
		state:  core.StatusActive,
		ctx:    context.Background(),
		cancel: func() {},
	}
	e.groups.set(rg.gid, rg)

	err = e.runBTDownload(context.Background(), rg, testSingleFileTorrent(t, "btfile.bin", 8))
	if err == nil || rg.errCode != core.ExitResourceNotFound {
		t.Fatalf("runBTDownload error = %v, errCode = %d; want no peers resource-not-found", err, rg.errCode)
	}
	if rg.filePath != outPath {
		t.Fatalf("filePath = %q, want %q", rg.filePath, outPath)
	}
	if _, err := os.Stat(filepath.Join(dir, "btfile.1.bin")); !os.IsNotExist(err) {
		t.Fatalf("renamed BT output exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunBTDownloadAllowOverwriteDoesNotTruncateLoadedControlFile(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "btfile.bin")
	original := []byte("old-data")
	if err := os.WriteFile(outPath, original, 0644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	torrentData := testSingleFileTorrent(t, "btfile.bin", int64(len(original)))
	meta, err := torrent.Load(torrentData)
	if err != nil {
		t.Fatalf("load torrent: %v", err)
	}
	infoHash, err := meta.InfoHash()
	if err != nil {
		t.Fatalf("info hash: %v", err)
	}
	if err := btprogress.Save(outPath, &btprogress.Info{InfoHash: infoHash[:], PieceLength: int64(len(original)), TotalLength: int64(len(original)), Bitfield: []byte{0x00}}); err != nil {
		t.Fatalf("save progress: %v", err)
	}

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		AutoFileRenaming:       true,
		AllowOverwrite:         true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{
		gid:    1,
		opts:   e.cfg,
		state:  core.StatusActive,
		ctx:    context.Background(),
		cancel: func() {},
	}
	e.groups.set(rg.gid, rg)

	err = e.runBTDownload(context.Background(), rg, torrentData)
	if err == nil || rg.errCode != core.ExitResourceNotFound {
		t.Fatalf("runBTDownload error = %v, errCode = %d; want no peers resource-not-found", err, rg.errCode)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("output was truncated or changed: got %q, want %q", got, original)
	}
}

func TestRunBTDownloadInvalidControlFileDoesNotBypassCollision(t *testing.T) {
	tests := []struct {
		name string
		info *btprogress.Info
	}{
		{
			name: "total length mismatch",
			info: &btprogress.Info{PieceLength: 8, TotalLength: 9, Bitfield: []byte{0x00}},
		},
		{
			name: "piece length mismatch",
			info: &btprogress.Info{PieceLength: 4, TotalLength: 8, Bitfield: []byte{0x00}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			outPath := filepath.Join(dir, "btfile.bin")
			if err := os.WriteFile(outPath, []byte("old-data"), 0644); err != nil {
				t.Fatalf("write existing file: %v", err)
			}
			torrentData := testSingleFileTorrent(t, "btfile.bin", 8)
			meta, err := torrent.Load(torrentData)
			if err != nil {
				t.Fatalf("load torrent: %v", err)
			}
			infoHash, err := meta.InfoHash()
			if err != nil {
				t.Fatalf("info hash: %v", err)
			}
			tt.info.InfoHash = infoHash[:]
			if err := btprogress.Save(outPath, tt.info); err != nil {
				t.Fatalf("save progress: %v", err)
			}

			e, err := New(&config.Options{
				Dir:                    dir,
				MaxConcurrentDownloads: 1,
				MaxDownloadResult:      10,
				AutoFileRenaming:       false,
			}, testLogger(t))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			rg := &requestGroup{
				gid:    1,
				opts:   e.cfg,
				state:  core.StatusActive,
				ctx:    context.Background(),
				cancel: func() {},
			}
			e.groups.set(rg.gid, rg)

			err = e.runBTDownload(context.Background(), rg, torrentData)
			if err == nil || rg.errCode != core.ExitFileAlreadyExists {
				t.Fatalf("runBTDownload error = %v, errCode = %d; want file-already-exists", err, rg.errCode)
			}
			if _, err := os.Stat(outPath + btprogress.Suffix); !os.IsNotExist(err) {
				t.Fatalf("invalid control file remains or stat failed unexpectedly: %v", err)
			}
		})
	}
}

func TestRunDownloadHTTPCorruptControlFileDoesNotBypassCollision(t *testing.T) {
	content := []byte("abcdefgh")
	server, _ := newHTTPRangeTestServer(t, content)
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(outPath, []byte("old-data"), 0644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}
	if err := os.WriteFile(outPath+btprogress.Suffix, []byte("not a control file"), 0644); err != nil {
		t.Fatalf("write corrupt progress: %v", err)
	}

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		AutoFileRenaming:       false,
		UseHead:                true,
		PieceLength:            "4",
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	rg := &requestGroup{
		gid:      1,
		opts:     e.cfg,
		uris:     []string{server.URL + "/file.bin"},
		filePath: outPath,
		state:    core.StatusActive,
		ctx:      ctx,
		cancel:   cancel,
	}
	e.groups.set(rg.gid, rg)
	e.wg.Add(1)
	e.runDownload(rg)

	if rg.errCode != core.ExitFileAlreadyExists {
		t.Fatalf("errCode = %d, errMsg = %q; want file-already-exists", rg.errCode, rg.errMsg)
	}
	if _, err := os.Stat(outPath + btprogress.Suffix); !os.IsNotExist(err) {
		t.Fatalf("corrupt control file remains or stat failed unexpectedly: %v", err)
	}
}

func TestControlFileAllowsResumeMissingControlFileIsQuiet(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "fresh.bin")
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e, err := New(&config.Options{Dir: dir, MaxConcurrentDownloads: 1, MaxDownloadResult: 10}, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if e.controlFileAllowsResume(outPath, e.cfg, 8, 4, nil) {
		t.Fatal("missing control file allowed resume")
	}
	if _, err := os.Stat(outPath + btprogress.Suffix); !os.IsNotExist(err) {
		t.Fatalf("control file exists or stat failed unexpectedly: %v", err)
	}
	if log := logBuf.String(); strings.Contains(log, "removing invalid control file") || strings.Contains(log, "load failed") {
		t.Fatalf("missing control file logged as invalid: %s", log)
	}
}

type rangeRequestRecorder struct {
	mu     sync.Mutex
	ranges []string
	gets   int
	heads  int
}

func (r *rangeRequestRecorder) Ranges() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.ranges))
	copy(out, r.ranges)
	return out
}

func (r *rangeRequestRecorder) GETs() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gets
}

func (r *rangeRequestRecorder) HEADs() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.heads
}

func newHTTPRangeTestServer(t *testing.T, content []byte) (*httptest.Server, *rangeRequestRecorder) {
	t.Helper()
	rec := &rangeRequestRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if r.Method == http.MethodHead {
			rec.mu.Lock()
			rec.heads++
			rec.mu.Unlock()
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		rangeHeader := r.Header.Get("Range")
		rec.mu.Lock()
		rec.ranges = append(rec.ranges, rangeHeader)
		rec.gets++
		rec.mu.Unlock()

		start, end := testRangeBounds(t, rangeHeader, len(content))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		if rangeHeader != "" {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(content[start : end+1])
	}))
	return server, rec
}

func testRangeBounds(t *testing.T, header string, total int) (int, int) {
	t.Helper()
	if header == "" {
		return 0, total - 1
	}
	if !strings.HasPrefix(header, "bytes=") {
		t.Fatalf("Range header = %q, want bytes=...", header)
	}
	startText, endText, ok := strings.Cut(strings.TrimPrefix(header, "bytes="), "-")
	if !ok {
		t.Fatalf("Range header = %q, want start-end", header)
	}
	start, err := strconv.Atoi(startText)
	if err != nil {
		t.Fatalf("Range start parse: %v", err)
	}
	end := total - 1
	if endText != "" {
		end, err = strconv.Atoi(endText)
		if err != nil {
			t.Fatalf("Range end parse: %v", err)
		}
	}
	if start < 0 || end < start || end >= total {
		t.Fatalf("Range header = %q outside total %d", header, total)
	}
	return start, end
}

func TestRunHTTPDownloadContinuesSingleConnection(t *testing.T) {
	content := []byte("0123456789")
	server, rec := newHTTPRangeTestServer(t, content)
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(outPath, content[:4], 0644); err != nil {
		t.Fatalf("write partial file: %v", err)
	}

	opts := &config.Options{
		Dir:                    dir,
		Continue:               true,
		Split:                  1,
		UseHead:                true,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{gid: 1, opts: opts, filePath: outPath}

	e.runHTTPDownload(context.Background(), rg, server.URL+"/file.bin", outPath)
	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if rg.completedLength != int64(len(content)) {
		t.Fatalf("completedLength = %d, want %d", rg.completedLength, len(content))
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("output = %q, want %q", got, content)
	}
	ranges := rec.Ranges()
	if len(ranges) != 1 || ranges[0] != "bytes=4-9" {
		t.Fatalf("GET ranges = %v, want [bytes=4-9]", ranges)
	}
}

func TestRunHTTPDownloadContinueSkipsCompleteFile(t *testing.T) {
	content := []byte("0123456789")
	server, rec := newHTTPRangeTestServer(t, content)
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(outPath, content, 0644); err != nil {
		t.Fatalf("write complete file: %v", err)
	}

	opts := &config.Options{
		Dir:                    dir,
		Continue:               true,
		Split:                  1,
		UseHead:                true,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{gid: 1, opts: opts, filePath: outPath}

	e.runHTTPDownload(context.Background(), rg, server.URL+"/file.bin", outPath)
	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if rg.completedLength != int64(len(content)) {
		t.Fatalf("completedLength = %d, want %d", rg.completedLength, len(content))
	}
	if rec.GETs() != 0 {
		t.Fatalf("GET count = %d, want 0 for already-complete file", rec.GETs())
	}
}

func TestRunDownloadConditionalGetSendsIfModifiedSince(t *testing.T) {
	content := []byte("conditional payload")
	localMTime := time.Date(2014, 10, 21, 7, 28, 0, 0, time.UTC)
	var mu sync.Mutex
	var ifModifiedSince []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ifModifiedSince = append(ifModifiedSince, r.Header.Get("If-Modified-Since"))
		mu.Unlock()
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("Last-Modified", time.Date(2015, 10, 21, 7, 28, 0, 0, time.UTC).Format(http.TimeFormat))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "conditional.bin")
	if err := os.WriteFile(outPath, []byte("cached"), 0644); err != nil {
		t.Fatalf("write cached file: %v", err)
	}
	if err := os.Chtimes(outPath, localMTime, localMTime); err != nil {
		t.Fatalf("set cached mtime: %v", err)
	}

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		AutoFileRenaming:       true,
		AllowOverwrite:         true,
		ConditionalGet:         true,
		UseHead:                true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	gid, err := e.Add(AddSpec{
		URIs:    []string{server.URL + "/conditional.bin"},
		Options: &config.Options{Out: "conditional.bin"},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	e.fillRequestGroupFromReserver()
	rg, _ := e.groups.get(gid)
	rg.state = core.StatusActive
	rg.ctx = context.Background()
	rg.cancel = func() {}
	e.wg.Add(1)
	e.runDownload(rg)

	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	want := localMTime.Format(http.TimeFormat)
	mu.Lock()
	defer mu.Unlock()
	for _, got := range ifModifiedSince {
		if got == want {
			return
		}
	}
	t.Fatalf("If-Modified-Since headers = %v, want one %q", ifModifiedSince, want)
}

func TestRunDownloadRemoteTimeAppliesLastModified(t *testing.T) {
	content := []byte("remote time payload")
	lastModified := time.Date(2016, 7, 8, 9, 10, 11, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("Last-Modified", lastModified.Format(http.TimeFormat))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dir := t.TempDir()
	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		AutoFileRenaming:       true,
		AllowOverwrite:         true,
		RemoteTime:             true,
		UseHead:                true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	gid, err := e.Add(AddSpec{
		URIs:    []string{server.URL + "/remote-time.bin"},
		Options: &config.Options{Out: "remote-time.bin"},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	e.fillRequestGroupFromReserver()
	rg, _ := e.groups.get(gid)
	rg.state = core.StatusActive
	rg.ctx = context.Background()
	rg.cancel = func() {}
	e.wg.Add(1)
	e.runDownload(rg)

	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	st, err := os.Stat(filepath.Join(dir, "remote-time.bin"))
	if err != nil {
		t.Fatalf("stat downloaded file: %v", err)
	}
	if !st.ModTime().Truncate(time.Second).Equal(lastModified) {
		t.Fatalf("mtime = %s, want %s", st.ModTime(), lastModified)
	}
}

func TestRunDownloadContentDispositionSelectsFilename(t *testing.T) {
	content := []byte("content disposition payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("Content-Disposition", `attachment; filename="server-name.bin"`)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dir := t.TempDir()
	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		AutoFileRenaming:       true,
		AllowOverwrite:         true,
		UseHead:                true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	gid, err := e.Add(AddSpec{URIs: []string{server.URL + "/download"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	e.fillRequestGroupFromReserver()
	rg, _ := e.groups.get(gid)
	rg.state = core.StatusActive
	rg.ctx = context.Background()
	rg.cancel = func() {}
	e.wg.Add(1)
	e.runDownload(rg)

	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	wantPath := filepath.Join(dir, "server-name.bin")
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read content-disposition output: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("output = %q, want %q", got, content)
	}
	if rg.filePath != wantPath {
		t.Fatalf("filePath = %q, want %q", rg.filePath, wantPath)
	}
	if _, err := os.Stat(filepath.Join(dir, "download")); !os.IsNotExist(err) {
		t.Fatalf("URL fallback output exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunDownloadMagnetMissingLocalTorrent(t *testing.T) {
	dir := t.TempDir()
	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	rg := &requestGroup{
		gid:    1,
		opts:   e.cfg,
		uris:   []string{"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"},
		state:  core.StatusActive,
		ctx:    ctx,
		cancel: cancel,
	}
	e.groups.set(rg.gid, rg)
	e.wg.Add(1)
	e.runDownload(rg)

	if rg.errCode != core.ExitResourceNotFound {
		t.Fatalf("errCode = %d, want resource-not-found", rg.errCode)
	}
	if rg.errMsg != "no peers available" {
		t.Fatalf("errMsg = %q", rg.errMsg)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("magnet failure did not cancel group context")
	}
}

func TestRunDownloadMagnetDoesNotLoadSavedMetadataWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	infoHash := "0123456789abcdef0123456789abcdef01234567"
	if err := os.WriteFile(filepath.Join(dir, infoHash+".torrent"), []byte("not a torrent"), 0644); err != nil {
		t.Fatalf("write bogus torrent: %v", err)
	}
	e, err := New(&config.Options{
		Dir:                    dir,
		BTLoadSavedMetadata:    false,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	rg := &requestGroup{
		gid:    1,
		opts:   e.cfg,
		uris:   []string{"magnet:?xt=urn:btih:" + infoHash},
		state:  core.StatusActive,
		ctx:    ctx,
		cancel: cancel,
	}
	e.groups.set(rg.gid, rg)
	e.wg.Add(1)
	e.runDownload(rg)

	if rg.errCode != core.ExitResourceNotFound {
		t.Fatalf("errCode = %d, want resource-not-found", rg.errCode)
	}
	if rg.errMsg != "no peers available" {
		t.Fatalf("errMsg = %q", rg.errMsg)
	}
}

func TestRunHTTPDownloadAlwaysResumeTrueAbortsWhenServerIgnoresRange(t *testing.T) {
	content := []byte("0123456789")
	var mu sync.Mutex
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if r.Method == http.MethodHead {
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	partial := []byte("0123")
	if err := os.WriteFile(outPath, partial, 0644); err != nil {
		t.Fatalf("write partial file: %v", err)
	}

	opts := &config.Options{
		Dir:                    dir,
		Continue:               true,
		AlwaysResume:           true,
		Split:                  1,
		UseHead:                true,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{gid: 1, opts: opts, filePath: outPath}

	e.runHTTPDownload(context.Background(), rg, server.URL+"/file.bin", outPath)
	if rg.errCode != core.ExitFileAlreadyExists {
		t.Fatalf("errCode = %d, errMsg = %q; want file-already-exists", rg.errCode, rg.errMsg)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, partial) {
		t.Fatalf("output = %q, want original partial %q", got, partial)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ranges) != 1 || ranges[0] != "bytes=4-9" {
		t.Fatalf("GET ranges = %v, want [bytes=4-9]", ranges)
	}
}

func TestRunHTTPDownloadAlwaysResumeFalseRestartsWhenServerIgnoresRange(t *testing.T) {
	content := []byte("0123456789")
	var mu sync.Mutex
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if r.Method == http.MethodHead {
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(outPath, []byte("0123"), 0644); err != nil {
		t.Fatalf("write partial file: %v", err)
	}

	opts := &config.Options{
		Dir:                    dir,
		Continue:               true,
		AlwaysResume:           false,
		Split:                  1,
		UseHead:                true,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{gid: 1, opts: opts, filePath: outPath}

	e.runHTTPDownload(context.Background(), rg, server.URL+"/file.bin", outPath)
	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("output = %q, want %q", got, content)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ranges) != 2 || ranges[0] != "bytes=4-9" || ranges[1] != "" {
		t.Fatalf("GET ranges = %v, want [bytes=4-9, empty]", ranges)
	}
}

func TestRunHTTPDownloadMaxResumeFailureTriesSingleURIRestartsImmediately(t *testing.T) {
	content := []byte("0123456789")
	var mu sync.Mutex
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if r.Method == http.MethodHead {
			return
		}
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	partial := []byte("0123")
	if err := os.WriteFile(outPath, partial, 0644); err != nil {
		t.Fatalf("write partial file: %v", err)
	}

	opts := &config.Options{
		Dir:                    dir,
		Continue:               true,
		AlwaysResume:           false,
		MaxResumeFailureTries:  2,
		Split:                  1,
		UseHead:                true,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{gid: 1, opts: opts, filePath: outPath}

	e.runHTTPDownload(context.Background(), rg, server.URL+"/file.bin", outPath)
	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read restarted output: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("restarted output = %q, want %q", got, content)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"bytes=4-9", ""}
	if len(ranges) != len(want) {
		t.Fatalf("GET ranges = %v, want %v", ranges, want)
	}
	for i := range want {
		if ranges[i] != want[i] {
			t.Fatalf("GET ranges = %v, want %v", ranges, want)
		}
	}
}

func TestRunHTTPDownloadMaxResumeFailureTriesSingleURIProbeWithoutRangeSupportStillAttemptsResume(t *testing.T) {
	content := []byte("0123456789")
	var mu sync.Mutex
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if r.Method == http.MethodHead {
			return
		}
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(outPath, content[:4], 0644); err != nil {
		t.Fatalf("write partial file: %v", err)
	}

	opts := &config.Options{
		Dir:                    dir,
		Continue:               true,
		AlwaysResume:           false,
		MaxResumeFailureTries:  2,
		Split:                  1,
		UseHead:                false,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{gid: 1, opts: opts, filePath: outPath}

	e.runHTTPDownload(context.Background(), rg, server.URL+"/file.bin", outPath)
	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read restarted output: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("restarted output = %q, want %q", got, content)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"bytes=0-0", "bytes=4-9", ""}
	if len(ranges) != len(want) {
		t.Fatalf("GET ranges = %v, want %v", ranges, want)
	}
	for i := range want {
		if ranges[i] != want[i] {
			t.Fatalf("GET ranges = %v, want %v", ranges, want)
		}
	}
}

func TestRunHTTPDownloadRetriesGatewayTimeoutBeforeBody(t *testing.T) {
	content := []byte("retry payload")
	var mu sync.Mutex
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if r.Method == http.MethodHead {
			return
		}
		mu.Lock()
		gets++
		attempt := gets
		mu.Unlock()
		if attempt == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "retry.bin")
	opts := &config.Options{
		Dir:                    dir,
		Split:                  1,
		UseHead:                true,
		MaxTries:               2,
		RetryWait:              "0",
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{gid: 1, opts: opts, filePath: outPath}

	e.runHTTPDownload(context.Background(), rg, server.URL+"/retry.bin", outPath)
	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("output = %q, want %q", got, content)
	}
	mu.Lock()
	defer mu.Unlock()
	if gets != 2 {
		t.Fatalf("GET count = %d, want 2", gets)
	}
}

func TestRunHTTPDownloadInflatedContentEncodingDisablesResumeAndSplit(t *testing.T) {
	payload := []byte("inflated response payload")
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(payload); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	var mu sync.Mutex
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.Itoa(compressed.Len()))
		if r.Method == http.MethodHead {
			return
		}
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		_, _ = w.Write(compressed.Bytes())
	}))
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "gzip.bin")
	if err := os.WriteFile(outPath, []byte("stale"), 0644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	opts := &config.Options{
		Dir:                    dir,
		Continue:               true,
		Split:                  3,
		MinSplitSize:           "1",
		HTTPAcceptGzip:         true,
		UseHead:                true,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{gid: 1, opts: opts, filePath: outPath}

	e.runHTTPDownload(context.Background(), rg, server.URL+"/gzip.bin", outPath)
	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if rg.totalLength != 0 {
		t.Fatalf("totalLength = %d, want 0 for inflated response", rg.totalLength)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("output = %q, want %q", got, payload)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ranges) != 1 || ranges[0] != "" {
		t.Fatalf("GET ranges = %v, want [\"\"]", ranges)
	}
}

func testSingleFileTorrent(t *testing.T, name string, length int64) []byte {
	t.Helper()
	pieces := make([]byte, 20)
	info := testBencodeDict(
		"name", bencode.StringVal{S: name},
		"piece length", bencode.IntVal{I: length},
		"pieces", bencode.StringVal{S: string(pieces)},
		"length", bencode.IntVal{I: length},
	)
	raw, err := bencode.Marshal(testBencodeDict(
		"announce", bencode.StringVal{S: ""},
		"info", info,
	))
	if err != nil {
		t.Fatalf("marshal torrent: %v", err)
	}
	return raw
}

func testBencodeDict(pairs ...any) *bencode.DictVal {
	d := &bencode.DictVal{Keys: make([]string, 0, len(pairs)/2), Values: make(map[string]bencode.Value, len(pairs)/2)}
	for i := 0; i < len(pairs); i += 2 {
		d.Set(pairs[i].(string), pairs[i+1].(bencode.Value))
	}
	return d
}

func TestRunHTTPDownloadContinuesMultiConnectionAfterExistingBytes(t *testing.T) {
	content := []byte("0123456789")
	server, rec := newHTTPRangeTestServer(t, content)
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(outPath, content[:4], 0644); err != nil {
		t.Fatalf("write partial file: %v", err)
	}

	opts := &config.Options{
		Dir:                    dir,
		Continue:               true,
		Split:                  3,
		MinSplitSize:           "1",
		UseHead:                true,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}
	e, err := New(opts, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{gid: 1, opts: opts, filePath: outPath}

	e.runHTTPDownload(context.Background(), rg, server.URL+"/file.bin", outPath)
	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	ranges := rec.Ranges()
	if len(ranges) == 0 {
		t.Fatal("no ranged GETs recorded")
	}
	for _, h := range ranges {
		start, _ := testRangeBounds(t, h, len(content))
		if start < 4 {
			t.Fatalf("range %q started before existing bytes", h)
		}
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("output = %q, want %q", got, content)
	}
}

func TestRunDownloadHTTPWritesControlFileWhileActive(t *testing.T) {
	content := []byte("abcdefgh")
	server, firstChunkWritten, release := newBlockingHTTPServer(t, content, 4)
	defer server.Close()
	defer release()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	_, _, cancel, done := startHTTPRunDownload(t, dir, server.URL+"/file.bin", outPath)

	<-firstChunkWritten
	waitForPath(t, outPath+btprogress.Suffix)

	cancel()
	release()
	<-done
}

func TestRunDownloadHTTPCancelKeepsControlProgress(t *testing.T) {
	content := []byte("abcdefgh")
	server, firstChunkWritten, release := newBlockingHTTPServer(t, content, 4)
	defer server.Close()
	defer release()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	_, rg, cancel, done := startHTTPRunDownload(t, dir, server.URL+"/file.bin", outPath)

	<-firstChunkWritten
	waitForFileSize(t, outPath, 4)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		release()
		<-done
	}

	if rg.errCode != core.ExitRemoved {
		t.Fatalf("errCode = %d, errMsg = %q; want removed", rg.errCode, rg.errMsg)
	}
	info, err := btprogress.Load(outPath)
	if err != nil {
		t.Fatalf("Load progress: %v", err)
	}
	if info.TotalLength != int64(len(content)) {
		t.Fatalf("TotalLength = %d, want %d", info.TotalLength, len(content))
	}
	if info.PieceLength != 4 {
		t.Fatalf("PieceLength = %d, want 4", info.PieceLength)
	}
	if len(info.Bitfield) != 1 || info.Bitfield[0]&0x80 == 0 {
		t.Fatalf("Bitfield = %08b, want first 4-byte piece complete", info.Bitfield)
	}
}

func TestRemoveStoppedGroup_PausedActiveRunsPauseHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook script uses POSIX shell")
	}

	content := []byte("abcdefgh")
	server, firstChunkWritten, release := newBlockingHTTPServer(t, content, 4)
	defer server.Close()
	defer release()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "hooks.log")
	hookPath := filepath.Join(dir, "pause-hook.sh")
	script := fmt.Sprintf("#!/bin/sh\nprintf 'pause|%%s|%%s|%%s\\n' \"$1\" \"$2\" \"$3\" >> %q\n", logPath)
	if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
		t.Fatalf("write hook script: %v", err)
	}

	e, rg, _, done := startHTTPRunDownload(t, dir, server.URL+"/file.bin", filepath.Join(dir, "file.bin"))
	e.cfg.OnDownloadPause = hookPath

	e.queuesMu.Lock()
	e.active = append(e.active, rg.gid)
	e.queuesMu.Unlock()

	<-firstChunkWritten

	if err := e.Pause(rg.gid, false); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cancelled download to stop")
	}

	e.removeStoppedGroup()

	status, err := e.TellStatus(rg.gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	if status.Status != core.StatusPaused {
		t.Fatalf("status = %s, want paused", status.Status)
	}

	wantLine := fmt.Sprintf("pause|%s|1|%s", rg.gid.Hex(), rg.filePath)
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(logPath)
		if err == nil && strings.TrimSpace(string(data)) == wantLine {
			break
		}
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read hook log: %v", err)
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(logPath)
			t.Fatalf("hook log = %q, want %q", strings.TrimSpace(string(data)), wantLine)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRemoveStoppedGroup_UserRemovedActiveBecomesRemovedResult(t *testing.T) {
	content := []byte("abcdefgh")
	server, firstChunkWritten, release := newBlockingHTTPServer(t, content, 4)
	defer server.Close()
	defer release()

	dir := t.TempDir()
	e, rg, _, done := startHTTPRunDownload(t, dir, server.URL+"/file.bin", filepath.Join(dir, "file.bin"))

	e.queuesMu.Lock()
	e.active = append(e.active, rg.gid)
	e.queuesMu.Unlock()

	<-firstChunkWritten

	if err := e.Remove(rg.gid, true); err != nil {
		t.Fatalf("Remove(force) error = %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for force-removed download to stop")
	}

	e.removeStoppedGroup()

	status, err := e.TellStatus(rg.gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	if status.Status != core.StatusRemoved {
		t.Fatalf("status = %s, want removed", status.Status)
	}
	if status.ErrorCode != core.ExitRemoved {
		t.Fatalf("errorCode = %d, want %d", status.ErrorCode, core.ExitRemoved)
	}
	if status.ErrorMessage != "" {
		t.Fatalf("errorMessage = %q, want empty string", status.ErrorMessage)
	}
}

func TestRunDownloadHTTPControlFileBypassesCollisionAndResumes(t *testing.T) {
	content := []byte("abcdefgh")
	server, rec := newHTTPRangeTestServer(t, content)
	defer server.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(outPath, content[:4], 0644); err != nil {
		t.Fatalf("write partial output: %v", err)
	}
	if err := btprogress.Save(outPath, &btprogress.Info{PieceLength: 4, TotalLength: int64(len(content)), Bitfield: []byte{0x80}}); err != nil {
		t.Fatalf("save progress: %v", err)
	}

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		AutoFileRenaming:       true,
		Continue:               false,
		UseHead:                true,
		PieceLength:            "4",
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	rg := &requestGroup{
		gid:      1,
		opts:     e.cfg,
		uris:     []string{server.URL + "/file.bin"},
		filePath: outPath,
		state:    core.StatusActive,
		ctx:      ctx,
		cancel:   cancel,
	}
	e.groups.set(rg.gid, rg)
	e.wg.Add(1)
	e.runDownload(rg)

	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if rg.filePath != outPath {
		t.Fatalf("filePath = %q, want %q", rg.filePath, outPath)
	}
	if _, err := os.Stat(filepath.Join(dir, "file.1.bin")); !os.IsNotExist(err) {
		t.Fatalf("renamed output exists or stat failed unexpectedly: %v", err)
	}
	if ranges := rec.Ranges(); len(ranges) != 1 || ranges[0] != "bytes=4-7" {
		t.Fatalf("GET ranges = %v, want [bytes=4-7]", ranges)
	}
	if rec.HEADs() != 1 {
		t.Fatalf("HEAD count = %d, want 1 cached probe", rec.HEADs())
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("output = %q, want %q", got, content)
	}
}

func TestRunDownloadHTTPSuccessDeletesControlFile(t *testing.T) {
	content := []byte("abcdefgh")
	server, rec := newHTTPRangeTestServer(t, content)
	defer server.Close()
	_ = rec

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	if err := btprogress.Save(outPath, &btprogress.Info{PieceLength: 4, TotalLength: int64(len(content)), Bitfield: []byte{0x00}}); err != nil {
		t.Fatalf("save progress: %v", err)
	}

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		AutoFileRenaming:       true,
		UseHead:                true,
		PieceLength:            "4",
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	rg := &requestGroup{
		gid:      1,
		opts:     e.cfg,
		uris:     []string{server.URL + "/file.bin"},
		filePath: outPath,
		state:    core.StatusActive,
		ctx:      ctx,
		cancel:   cancel,
	}
	e.groups.set(rg.gid, rg)
	e.wg.Add(1)
	e.runDownload(rg)

	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	if _, err := os.Stat(outPath + btprogress.Suffix); !os.IsNotExist(err) {
		t.Fatalf("control file remains after success or stat failed unexpectedly: %v", err)
	}
}

func TestRunDownloadHTTPSuccessForceSaveKeepsControlFile(t *testing.T) {
	content := []byte("abcdefgh")
	server, rec := newHTTPRangeTestServer(t, content)
	defer server.Close()
	_ = rec

	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	if err := btprogress.Save(outPath, &btprogress.Info{PieceLength: 4, TotalLength: int64(len(content)), Bitfield: []byte{0x00}}); err != nil {
		t.Fatalf("save progress: %v", err)
	}

	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		AutoFileRenaming:       true,
		UseHead:                true,
		PieceLength:            "4",
		ForceSave:              true,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	rg := &requestGroup{
		gid:      1,
		opts:     e.cfg,
		uris:     []string{server.URL + "/file.bin"},
		filePath: outPath,
		state:    core.StatusActive,
		ctx:      ctx,
		cancel:   cancel,
	}
	e.groups.set(rg.gid, rg)
	e.wg.Add(1)
	e.runDownload(rg)

	if rg.errCode != core.ExitSuccess {
		t.Fatalf("errCode = %d, errMsg = %q", rg.errCode, rg.errMsg)
	}
	info, err := btprogress.Load(outPath)
	if err != nil {
		t.Fatalf("control file was not kept after force-save success: %v", err)
	}
	if len(info.Bitfield) != 1 || info.Bitfield[0] != 0xc0 {
		t.Fatalf("Bitfield = %08b, want both 4-byte pieces complete", info.Bitfield)
	}
}

func TestSaveControlFileSyncsAdaptorBeforeProgressSave(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "file.bin")
	adaptor := &syncBeforeSaveAdaptor{controlPath: outPath + btprogress.Suffix}
	e, err := New(&config.Options{Dir: dir, MaxConcurrentDownloads: 1, MaxDownloadResult: 10}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{
		gid:         1,
		opts:        e.cfg,
		controlPath: outPath,
		controlInfo: &btprogress.Info{PieceLength: 4, TotalLength: 4, Bitfield: []byte{0x00}},
		adaptor:     adaptor,
	}

	e.saveControlFile(rg)

	if !adaptor.synced {
		t.Fatal("adaptor was not synced before saving control file")
	}
	if adaptor.controlExistedAtSync {
		t.Fatal("control file existed before adaptor sync; want sync before save")
	}
	if _, err := btprogress.Load(outPath); err != nil {
		t.Fatalf("Load progress: %v", err)
	}
}

func TestDownloadToAdaptorContextCanceledReadReturnsCleanly(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e, err := New(testOpts(), logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	rg := &requestGroup{gid: 1}

	written := e.downloadToAdaptor(context.Background(), rg, &syncBeforeSaveAdaptor{}, canceledReadBody{}, 0, nil)

	if written != -1 {
		t.Fatalf("written = %d, want -1", written)
	}
	if rg.errCode != core.ExitRemoved {
		t.Fatalf("errCode = %d, want removed", rg.errCode)
	}
	if rg.errMsg != "download cancelled" {
		t.Fatalf("errMsg = %q, want download cancelled", rg.errMsg)
	}
	if strings.Contains(logBuf.String(), "read failed") {
		t.Fatalf("unexpected read failure log: %s", logBuf.String())
	}
}

type canceledReadBody struct{}

func (canceledReadBody) Read([]byte) (int, error) {
	return 0, context.Canceled
}

type syncBeforeSaveAdaptor struct {
	controlPath          string
	synced               bool
	controlExistedAtSync bool
}

func (a *syncBeforeSaveAdaptor) OpenForWrite() error                { return nil }
func (a *syncBeforeSaveAdaptor) WriteAt([]byte, int64) (int, error) { return 0, nil }
func (a *syncBeforeSaveAdaptor) ReadAt([]byte, int64) (int, error)  { return 0, nil }
func (a *syncBeforeSaveAdaptor) Size() int64                        { return 0 }
func (a *syncBeforeSaveAdaptor) Sync() error {
	a.synced = true
	if _, err := os.Stat(a.controlPath); err == nil {
		a.controlExistedAtSync = true
	}
	return nil
}
func (a *syncBeforeSaveAdaptor) Close() error        { return nil }
func (a *syncBeforeSaveAdaptor) SetPieceCount(int)   {}
func (a *syncBeforeSaveAdaptor) MarkPiece(int, bool) {}
func (a *syncBeforeSaveAdaptor) Have(int) bool       { return false }
func (a *syncBeforeSaveAdaptor) Bitfield() []byte    { return nil }
func (a *syncBeforeSaveAdaptor) Missing() []int      { return nil }

func newBlockingHTTPServer(t *testing.T, content []byte, firstChunk int) (*httptest.Server, <-chan struct{}, func()) {
	t.Helper()
	firstChunkWritten := make(chan struct{})
	releaseCh := make(chan struct{})
	var wroteOnce sync.Once
	var releaseOnce sync.Once

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if r.Method == http.MethodHead {
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		start, end := testRangeBounds(t, r.Header.Get("Range"), len(content))
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
			w.WriteHeader(http.StatusPartialContent)
		}

		chunkEnd := start + firstChunk
		if chunkEnd > end+1 {
			chunkEnd = end + 1
		}
		_, _ = w.Write(content[start:chunkEnd])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		wroteOnce.Do(func() { close(firstChunkWritten) })

		select {
		case <-releaseCh:
		case <-r.Context().Done():
			return
		}
		if chunkEnd <= end {
			_, _ = w.Write(content[chunkEnd : end+1])
		}
	}))

	release := func() { releaseOnce.Do(func() { close(releaseCh) }) }
	return server, firstChunkWritten, release
}

func startHTTPRunDownload(t *testing.T, dir, uri, outPath string) (*Engine, *requestGroup, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	e, err := New(&config.Options{
		Dir:                    dir,
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
		AutoFileRenaming:       true,
		UseHead:                true,
		PieceLength:            "4",
		AutoSaveInterval:       "1",
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	rg := &requestGroup{
		gid:      1,
		opts:     e.cfg,
		uris:     []string{uri},
		filePath: outPath,
		state:    core.StatusActive,
		ctx:      ctx,
		cancel:   cancel,
	}
	e.groups.set(rg.gid, rg)
	done := make(chan struct{})
	e.wg.Add(1)
	go func() {
		e.runDownload(rg)
		close(done)
	}()
	return e, rg, cancel, done
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func waitForFileSize(t *testing.T, path string, size int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, err := os.Stat(path)
		if err == nil && st.Size() >= size {
			return
		}
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to reach %d bytes", path, size)
}
