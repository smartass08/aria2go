package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/smartass08/aria2go/bench/internal/types"
)

type Sampler struct {
	mu       sync.Mutex
	samples  []types.MetricSample
	started  time.Time
	binary   types.Binary
	pid      int
	interval time.Duration
	stopFn   context.CancelFunc
	doneCh   chan struct{}
}

func NewSampler(pid int, binary types.Binary, interval time.Duration) *Sampler {
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	return &Sampler{
		pid:      pid,
		binary:   binary,
		interval: interval,
		started:  time.Now(),
		doneCh:   make(chan struct{}),
	}
}

func (s *Sampler) Start(ctx context.Context) {
	ctx, s.stopFn = context.WithCancel(ctx)
	go s.loop(ctx)
}

func (s *Sampler) Stop() {
	if s.stopFn != nil {
		s.stopFn()
	}
	<-s.doneCh
}

func (s *Sampler) Samples() []types.MetricSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]types.MetricSample, len(s.samples))
	copy(out, s.samples)
	return out
}

func (s *Sampler) loop(ctx context.Context) {
	defer close(s.doneCh)
	tick := time.NewTicker(s.interval)
	defer tick.Stop()

	var prevUser, prevSys uint64
	var prevTime time.Time
	first := true

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			stats, err := readProcessStats(s.pid)
			if err != nil {
				continue
			}
			var cpuUserPct, cpuSysPct float64
			if !first {
				dt := now.Sub(prevTime).Seconds()
				if dt > 0 {
					cpuUserPct = float64(stats.UserTicks-prevUser) / (dt * float64(ticksPerSecond)) * 100
					cpuSysPct = float64(stats.SysTicks-prevSys) / (dt * float64(ticksPerSecond)) * 100
				}
			}
			prevUser, prevSys = stats.UserTicks, stats.SysTicks
			prevTime = now
			first = false

			sample := types.MetricSample{
				Timestamp:  now,
				Offset:     now.Sub(s.started),
				Binary:     s.binary,
				PID:        s.pid,
				CpuUserPct: cpuUserPct,
				CpuSysPct:  cpuSysPct,
				RSSBytes:   stats.RSSBytes,
				VSSBytes:   stats.VSSBytes,
				Threads:    stats.Threads,
				OpenFDs:    stats.OpenFDs,
			}
			s.mu.Lock()
			s.samples = append(s.samples, sample)
			s.mu.Unlock()
		}
	}
}
