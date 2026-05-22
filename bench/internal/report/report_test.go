package report

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/smartass08/aria2go/bench/internal/types"
)

func TestComputeSummary(t *testing.T) {
	samples := []types.MetricSample{
		{CpuUserPct: 10, RSSBytes: 1000},
		{CpuUserPct: 20, RSSBytes: 2000},
		{CpuUserPct: 30, RSSBytes: 3000},
		{CpuUserPct: 40, RSSBytes: 4000},
		{CpuUserPct: 50, RSSBytes: 5000},
	}
	sum := ComputeSummary(samples)
	if sum.CPUUserPct.Min != 10 {
		t.Errorf("Min CPU = %v", sum.CPUUserPct.Min)
	}
	if sum.CPUUserPct.Max != 50 {
		t.Errorf("Max CPU = %v", sum.CPUUserPct.Max)
	}
	if sum.CPUUserPct.Mean != 30 {
		t.Errorf("Mean CPU = %v", sum.CPUUserPct.Mean)
	}
	if sum.RSSBytes.Last != 5000 {
		t.Errorf("Last RSS = %v", sum.RSSBytes.Last)
	}
}

func TestWriteReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	r := &types.Report{
		Host:      "test",
		Aria2cVer: "1.37.0",
	}
	if err := WriteReport(path, r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
