package report

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/smartass08/aria2go/bench/internal/types"
)

func ComputeSummary(samples []types.MetricSample) types.Summary {
	if len(samples) == 0 {
		return types.Summary{}
	}
	cpuUser := extract(samples, func(s types.MetricSample) float64 { return s.CpuUserPct })
	cpuSys := extract(samples, func(s types.MetricSample) float64 { return s.CpuSysPct })
	rss := extract(samples, func(s types.MetricSample) float64 { return float64(s.RSSBytes) })
	threads := extract(samples, func(s types.MetricSample) float64 { return float64(s.Threads) })
	fds := extract(samples, func(s types.MetricSample) float64 { return float64(s.OpenFDs) })

	return types.Summary{
		CPUUserPct: stats(cpuUser),
		CPUSysPct:  stats(cpuSys),
		RSSBytes:   stats(rss),
		Threads:    stats(threads),
		OpenFDs:    stats(fds),
	}
}

func extract(samples []types.MetricSample, fn func(types.MetricSample) float64) []float64 {
	out := make([]float64, len(samples))
	for i, s := range samples {
		out[i] = fn(s)
	}
	return out
}

func stats(vals []float64) types.SummaryStats {
	if len(vals) == 0 {
		return types.SummaryStats{}
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	p95Idx := int(float64(len(sorted)) * 0.95)
	if p95Idx >= len(sorted) {
		p95Idx = len(sorted) - 1
	}

	return types.SummaryStats{
		Min:  sorted[0],
		Max:  sorted[len(sorted)-1],
		Mean: mean,
		P95:  sorted[p95Idx],
		Last: vals[len(vals)-1],
	}
}

func WriteReport(path string, r *types.Report) error {
	r.GeneratedAt = time.Now().UTC()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func FormatDuration(d time.Duration) string {
	if d >= time.Minute {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
