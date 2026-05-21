package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewThrottle_Unlimited(t *testing.T) {
	th := NewThrottle(0)
	if th == nil {
		t.Fatal("NewThrottle(0) returned nil")
	}
	if th.rate.Load() != 0 {
		t.Errorf("expected rate 0, got %d", th.rate.Load())
	}
	if th.ticker != nil {
		t.Error("expected nil ticker for unlimited rate")
	}
}

func TestNewThrottle_Limited(t *testing.T) {
	th := NewThrottle(10240)
	defer th.Stop()
	if th.rate.Load() != 10240 {
		t.Errorf("expected rate 10240, got %d", th.rate.Load())
	}
	if th.ticker == nil {
		t.Error("expected ticker for limited rate")
	}
}

func TestThrottle_WaitUnlimited(t *testing.T) {
	th := NewThrottle(0)
	ctx := context.Background()
	if err := th.Wait(ctx, 1000000); err != nil {
		t.Errorf("unlimited throttle should return immediately: %v", err)
	}
}

func TestThrottle_WaitZeroBytes(t *testing.T) {
	th := NewThrottle(10240)
	defer th.Stop()
	ctx := context.Background()
	if err := th.Wait(ctx, 0); err != nil {
		t.Errorf("Wait(0) should return immediately: %v", err)
	}
}

func TestThrottle_WaitNegativeBytes(t *testing.T) {
	th := NewThrottle(10240)
	defer th.Stop()
	ctx := context.Background()
	if err := th.Wait(ctx, -1); err != nil {
		t.Errorf("Wait(-1) should return immediately: %v", err)
	}
}

func TestThrottle_TokenBucketFillsOverTime(t *testing.T) {
	th := NewThrottle(10240) // 10 KiB/s
	defer th.Stop()

	// Initially bucket should be empty.
	if th.bucket.Load() != 0 {
		t.Fatalf("bucket should start at 0, got %d", th.bucket.Load())
	}

	// Wait for tokens to accumulate.
	time.Sleep(500 * time.Millisecond)

	// After 500ms at 10 KiB/s, we should have ~5120 bytes.
	bucks := th.bucket.Load()
	if bucks < 4096 || bucks > 10240 {
		t.Errorf("expected ~5120 tokens after 500ms, got %d", bucks)
	}
}

func TestThrottle_TokenConsumption(t *testing.T) {
	th := NewThrottle(102400) // 100 KiB/s
	defer th.Stop()

	ctx := context.Background()

	// Wait for initial tokens.
	time.Sleep(200 * time.Millisecond)

	// Try to consume 10 KiB — should succeed quickly.
	start := time.Now()
	if err := th.Wait(ctx, 10240); err != nil {
		t.Fatalf("Wait(10240) error: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Errorf("Wait(10240) took too long: %v (rate=100KiB/s)", elapsed)
	}
}

func TestThrottle_WaitBlocksWhenEmpty(t *testing.T) {
	th := NewThrottle(10240) // 10 KiB/s
	defer th.Stop()

	ctx := context.Background()

	// Attempt to consume way more than available — should block and then time out.
	done := make(chan error, 1)
	go func() {
		done <- th.Wait(ctx, 100*1024) // 100 KiB
	}()

	select {
	case <-time.After(500 * time.Millisecond):
		// Wait should still be blocking — expected.
	case err := <-done:
		t.Errorf("Wait(100KiB) at 10KiB/s should block, got err=%v", err)
	}

	// Wait should eventually succeed after enough time for tokens to accumulate (~10s).
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Wait(100KiB) should succeed after refills: %v", err)
		}
	case <-time.After(12 * time.Second):
		t.Error("Wait(100KiB) at 10KiB/s did not return within 12s")
	}
}

func TestThrottle_WaitContextCancellation(t *testing.T) {
	th := NewThrottle(10240) // 10 KiB/s
	defer th.Stop()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- th.Wait(ctx, 100*1024*1024) // 100 MiB, will block
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Wait did not return after context cancellation")
	}
}

func TestThrottle_WaitContextTimeout(t *testing.T) {
	th := NewThrottle(10240) // 10 KiB/s
	defer th.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := th.Wait(ctx, 10*1024*1024) // 10 MiB, will block
	elapsed := time.Since(start)

	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Wait should return after deadline in ~500ms, took %v", elapsed)
	}
}

func TestThrottle_SetRate(t *testing.T) {
	th := NewThrottle(0) // Unlimited
	if th.rate.Load() != 0 {
		t.Fatalf("expected rate 0, got %d", th.rate.Load())
	}

	// Set to limited.
	th.SetRate(10240)
	defer th.Stop()
	if th.rate.Load() != 10240 {
		t.Errorf("expected rate 10240, got %d", th.rate.Load())
	}

	// Set back to unlimited.
	th.SetRate(0)
	if th.rate.Load() != 0 {
		t.Errorf("expected rate 0 after unsetting, got %d", th.rate.Load())
	}
}

func TestThrottle_ConcurrentWait(t *testing.T) {
	th := NewThrottle(102400) // 100 KiB/s, shared between 4 goroutines
	defer th.Stop()

	ctx := context.Background()

	time.Sleep(200 * time.Millisecond)

	var total atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if err := th.Wait(ctx, 1024); err != nil {
					t.Errorf("concurrent Wait error: %v", err)
					return
				}
				total.Add(1024)
			}
		}()
	}
	wg.Wait()

	if total.Load() != 4*10*1024 {
		t.Errorf("expected total %d, got %d", 4*10*1024, total.Load())
	}
}

func TestThrottle_Stop(t *testing.T) {
	th := NewThrottle(10240)
	th.Stop()

	ctx := context.Background()
	err := th.Wait(ctx, 10240)
	if err != nil {
		t.Errorf("Wait after Stop should return nil (unblocked), got %v", err)
	}

	th.Stop()
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"0", 0},
		{"100", 100},
		{"1K", 1024},
		{"1k", 1024},
		{"100K", 102400},
		{"100k", 102400},
		{"1M", 1048576},
		{"1m", 1048576},
		{"10M", 10485760},
		{"", 0},
		{"invalid", 0},
		{"-1", 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseSize(tt.input)
			if got != tt.expected {
				t.Errorf("parseSize(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}
