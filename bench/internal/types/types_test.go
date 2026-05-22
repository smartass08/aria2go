package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestReportJSONRoundTrip(t *testing.T) {
	r := Report{
		GeneratedAt: time.Now().UTC(),
		Host:        "test-host",
		Aria2cVer:   "1.37.0",
		Aria2goVer:  "1.37.0",
		Scenarios: []ScenarioResult{{
			Kind:      ScenarioHTTPSingle,
			Binary:    BinaryAria2go,
			Protocol:  ProtocolHTTP,
			StartedAt: time.Now().UTC(),
			Duration:  5 * time.Second,
			Summary: Summary{
				CPUUserPct: SummaryStats{Mean: 42.5},
				RSSBytes:   SummaryStats{Mean: 100 * 1024 * 1024},
			},
		}},
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Host != "test-host" {
		t.Errorf("Host = %q", got.Host)
	}
	if len(got.Scenarios) != 1 {
		t.Fatalf("len(Scenarios) = %d", len(got.Scenarios))
	}
	if got.Scenarios[0].Summary.CPUUserPct.Mean != 42.5 {
		t.Errorf("Mean CPU = %v", got.Scenarios[0].Summary.CPUUserPct.Mean)
	}
}
