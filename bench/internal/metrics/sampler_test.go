package metrics

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestSamplerCollectsSamples(t *testing.T) {
	s := NewSampler(os.Getpid(), "self", 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	s.Start(ctx)
	<-ctx.Done()
	s.Stop()

	samples := s.Samples()
	if len(samples) < 3 {
		t.Errorf("got %d samples, want >= 3", len(samples))
	}
	for _, sa := range samples {
		if sa.PID != os.Getpid() {
			t.Errorf("PID = %d, want %d", sa.PID, os.Getpid())
		}
		if sa.RSSBytes <= 0 {
			t.Errorf("RSSBytes = %d, want > 0", sa.RSSBytes)
		}
	}
}
