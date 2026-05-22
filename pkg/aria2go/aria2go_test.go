package aria2c

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/config"
)

const testMetalinkV4 = `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <url>http://example.com/example.iso</url>
    <url>http://mirror.example.com/example.iso</url>
  </file>
</metalink>`

const testMetalinkMultiFile = `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="a.bin">
    <url>http://example.com/a.bin</url>
  </file>
  <file name="b.bin">
    <url>http://example.com/b.bin</url>
  </file>
</metalink>`

func testOpts() *config.Options {
	cfg := config.Default()
	cfg.Dir = "/tmp/aria2go-test"
	cfg.MaxConcurrentDownloads = 5
	cfg.MaxDownloadResult = 100
	cfg.EnableDHT = false
	cfg.RPCListenPort = 0
	cfg.DryRun = true
	return cfg
}

func TestNew(t *testing.T) {
	d, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if d == nil {
		t.Fatal("New() returned nil")
	}
	if d.eng == nil {
		t.Fatal("Daemon engine is nil")
	}
	if d.cfg == nil {
		t.Fatal("Daemon config is nil")
	}
}

func TestNew_WithOptions(t *testing.T) {
	opts := testOpts()
	d, err := New(Config{Options: opts})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if d.cfg.Dir != opts.Dir {
		t.Errorf("Dir = %q, want %q", d.cfg.Dir, opts.Dir)
	}
}

func TestAddURI(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, err := d.AddURI("http://example.com/file.iso", nil)
	if err != nil {
		t.Fatalf("AddURI() error = %v", err)
	}
	if gid == 0 {
		t.Error("AddURI() returned zero GID")
	}

	st := d.Status()
	if st.Waiting != 1 {
		t.Errorf("Waiting = %d, want 1", st.Waiting)
	}
}

func TestAddTorrent(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gid, err := d.AddTorrent([]byte("dummy torrent data"), nil)
	if err != nil {
		t.Fatalf("AddTorrent() error = %v", err)
	}
	if gid == 0 {
		t.Error("AddTorrent() returned zero GID")
	}
}

func TestAddMetalink(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gids, err := d.AddMetalink([]byte(testMetalinkV4), nil)
	if err != nil {
		t.Fatalf("AddMetalink() error = %v", err)
	}
	if len(gids) != 1 {
		t.Fatalf("len(gids) = %d, want 1", len(gids))
	}
	if gids[0] == 0 {
		t.Error("AddMetalink() returned zero GID")
	}
}

func TestAddMetalink_MultiFileReturnsMultipleGIDs(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	gids, err := d.AddMetalink([]byte(testMetalinkMultiFile), nil)
	if err != nil {
		t.Fatalf("AddMetalink() error = %v", err)
	}
	if len(gids) != 2 {
		t.Fatalf("len(gids) = %d, want 2", len(gids))
	}
	if gids[0] == 0 || gids[1] == 0 {
		t.Fatalf("AddMetalink() returned zero gid(s): %v", gids)
	}
	if gids[0] == gids[1] {
		t.Fatalf("AddMetalink() returned duplicate gids: %v", gids)
	}
}

func TestAddMetalink_InvalidXML(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = d.AddMetalink([]byte("not xml"), nil)
	if err == nil {
		t.Error("expected error for invalid metalink XML")
	}
}

func TestRPCAddr_Disabled(t *testing.T) {
	d, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	addr := d.RPCAddr()
	if addr != "" {
		t.Errorf("RPCAddr() = %q, want empty (RPC disabled)", addr)
	}
}

func TestRPCAddr_Enabled(t *testing.T) {
	d, err := New(Config{
		Options: &config.Options{
			EnableRPC:     true,
			RPCListenPort: 6800,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	addr := d.RPCAddr()
	if addr != "http://127.0.0.1:6800/jsonrpc" {
		t.Errorf("RPCAddr() = %q, want http://127.0.0.1:6800/jsonrpc", addr)
	}
}

func TestRPCAddr_CustomPort(t *testing.T) {
	d, err := New(Config{
		Options: &config.Options{
			EnableRPC:     true,
			RPCListenPort: 6999,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	addr := d.RPCAddr()
	if addr != "http://127.0.0.1:6999/jsonrpc" {
		t.Errorf("RPCAddr() = %q, want http://127.0.0.1:6999/jsonrpc", addr)
	}
}

func TestRPCAddr_ListenAll(t *testing.T) {
	d, err := New(Config{
		Options: &config.Options{
			EnableRPC:     true,
			RPCListenPort: 6800,
			RPCListenAll:  true,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	addr := d.RPCAddr()
	if addr != "http://0.0.0.0:6800/jsonrpc" {
		t.Errorf("RPCAddr() = %q, want http://0.0.0.0:6800/jsonrpc", addr)
	}
}

func TestRPCAddr_DefaultPort(t *testing.T) {
	d, err := New(Config{
		Options: &config.Options{
			EnableRPC: true,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	addr := d.RPCAddr()
	if addr != "http://127.0.0.1:6800/jsonrpc" {
		t.Errorf("RPCAddr() = %q, want http://127.0.0.1:6800/jsonrpc (default port)", addr)
	}
}

func TestShutdown(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	time.Sleep(10 * time.Millisecond)

	if err := d.Shutdown(false); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after Shutdown")
	}
}

func TestShutdown_Force(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	if err := d.Shutdown(true); err != nil {
		t.Fatalf("Shutdown(true) error = %v", err)
	}
}

func TestShutdown_DoubleCall(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	d.Shutdown(false)
	if err := d.Shutdown(false); err == nil {
		t.Error("expected error on second shutdown call")
	}
}

func TestShutdown_Concurrent(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	// Add downloads before shutdown
	for i := 0; i < 10; i++ {
		d.AddURI("http://example.com/"+string(rune('a'+i%26)), nil)
	}

	var wg sync.WaitGroup

	// Concurrent shutdown calls
	var shutdownErrors int32
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.Shutdown(false); err != nil {
				atomic.AddInt32(&shutdownErrors, 1)
			}
		}()
	}

	// Concurrent status reads during shutdown
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.Status()
		}()
	}

	// Concurrent AddURI during shutdown (should fail gracefully)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = d.AddURI("http://example.com/new-"+string(rune('a'+i%26)), nil)
		}(i)
	}

	wg.Wait()

	// At least 4 of 5 concurrent shutdowns should have been rejected (only first succeeds).
	if shutdownErrors < 3 {
		t.Errorf("expected most concurrent shutdowns to fail, got %d failures out of 5", shutdownErrors)
	}
}

func TestStatus(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// No downloads yet.
	st := d.Status()
	if st.Active != 0 {
		t.Errorf("Active = %d, want 0", st.Active)
	}
	if st.Waiting != 0 {
		t.Errorf("Waiting = %d, want 0", st.Waiting)
	}
	if st.Stopped != 0 {
		t.Errorf("Stopped = %d, want 0", st.Stopped)
	}
	if st.Speed != 0 {
		t.Errorf("Speed = %d, want 0", st.Speed)
	}
}

func TestStatus_WithDownloads(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	d.AddURI("http://example.com/a", nil)
	d.AddURI("http://example.com/b", nil)
	d.AddURI("http://example.com/c", nil)

	st := d.Status()
	if st.Waiting != 3 {
		t.Errorf("Waiting = %d, want 3", st.Waiting)
	}
	if st.Active != 0 {
		t.Errorf("Active = %d, want 0 (no Run called)", st.Active)
	}
}

func TestStatus_Concurrent(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	d.AddURI("http://example.com/file.iso", nil)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.Status()
		}()
	}
	wg.Wait()
}

func TestAddURI_Concurrent(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := d.AddURI("http://example.com/"+string(rune('a'+i%26)), nil)
			if err != nil {
				t.Errorf("AddURI concurrent error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	d.Shutdown(true)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after Shutdown")
	}

	st := d.Status()
	if st.Stopped != 20 {
		t.Errorf("Stopped = %d, want 20 (dry-run + onEndOfRun)", st.Stopped)
	}
}

func TestAddMetalink_Concurrent(t *testing.T) {
	d, err := New(Config{Options: testOpts()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := d.AddMetalink([]byte(testMetalinkV4), nil)
			if err != nil {
				t.Errorf("AddMetalink concurrent error: %v", err)
			}
		}()
	}
	wg.Wait()

	st := d.Status()
	if st.Waiting != 10 {
		t.Errorf("Waiting = %d, want 10", st.Waiting)
	}
}
