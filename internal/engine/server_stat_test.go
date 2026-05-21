package engine

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestNewServerStat(t *testing.T) {
	s := NewServerStat("example.com", "http")
	if s.Hostname() != "example.com" {
		t.Errorf("Hostname = %q, want example.com", s.Hostname())
	}
	if s.Protocol() != "http" {
		t.Errorf("Protocol = %q, want http", s.Protocol())
	}
	if s.DownloadSpeed() != 0 {
		t.Errorf("DownloadSpeed = %d, want 0", s.DownloadSpeed())
	}
	if s.Counter() != 0 {
		t.Errorf("Counter = %d, want 0", s.Counter())
	}
	if !s.IsOK() {
		t.Error("new ServerStat should be OK")
	}
	if s.IsError() {
		t.Error("new ServerStat should not be Error")
	}
}

func TestServerStat_Less(t *testing.T) {
	a := NewServerStat("aaa.com", "http")
	b := NewServerStat("bbb.com", "http")
	c := NewServerStat("aaa.com", "ftp")

	if !a.Less(b) {
		t.Error("aaa.com < bbb.com should be true")
	}
	if b.Less(a) {
		t.Error("bbb.com < aaa.com should be false")
	}
	if a.Less(c) {
		t.Error("aaa.com:http < aaa.com:ftp should be false (http > ftp lexicographically)")
	}
	if !c.Less(a) {
		t.Error("aaa.com:ftp < aaa.com:http should be true")
	}
}

func TestServerStat_Equal(t *testing.T) {
	a := NewServerStat("example.com", "http")
	b := NewServerStat("example.com", "http")
	c := NewServerStat("example.com", "ftp")

	if !a.Equal(b) {
		t.Error("same host+protocol should be equal")
	}
	if a.Equal(c) {
		t.Error("different protocol should not be equal")
	}
}

func TestServerStat_UpdateDownloadSpeed(t *testing.T) {
	s := NewServerStat("host", "http")
	s.SetError()
	s.UpdateDownloadSpeed(5000)

	if s.DownloadSpeed() != 5000 {
		t.Errorf("DownloadSpeed = %d, want 5000", s.DownloadSpeed())
	}
	if !s.IsOK() {
		t.Error("update with >0 speed should set OK")
	}
	if s.LastUpdated().IsZero() {
		t.Error("lastUpdated should be set after update")
	}
}

func TestServerStat_SetStatus(t *testing.T) {
	s := NewServerStat("host", "http")
	s.SetStatus("ERROR")
	if !s.IsError() {
		t.Error("should be ERROR")
	}
	s.SetStatus("OK")
	if !s.IsOK() {
		t.Error("should be OK")
	}
	s.SetStatus("INVALID")
	if !s.IsOK() {
		t.Error("invalid status should default to OK")
	}
}

func TestServerStat_SetOK_SetError(t *testing.T) {
	s := NewServerStat("host", "http")
	s.SetOK()
	if !s.IsOK() {
		t.Error("should be OK")
	}
	s.SetError()
	if !s.IsError() {
		t.Error("should be ERROR")
	}
}

func TestServerStat_ToString(t *testing.T) {
	s := NewServerStat("example.com", "http")
	s.SetDownloadSpeed(5000)
	s.SetSingleConnectionAvgSpeed(3000)
	s.SetMultiConnectionAvgSpeed(4000)
	s.SetCounter(5)
	s.SetLastUpdated(time.Unix(1000, 0))

	str := s.toString()
	if !strings.Contains(str, "host=example.com") {
		t.Errorf("missing host: %s", str)
	}
	if !strings.Contains(str, "protocol=http") {
		t.Errorf("missing protocol: %s", str)
	}
	if !strings.Contains(str, "dl_speed=5000") {
		t.Errorf("missing dl_speed: %s", str)
	}
	if !strings.Contains(str, "sc_avg_speed=3000") {
		t.Errorf("missing sc_avg_speed: %s", str)
	}
	if !strings.Contains(str, "mc_avg_speed=4000") {
		t.Errorf("missing mc_avg_speed: %s", str)
	}
	if !strings.Contains(str, "last_updated=1000") {
		t.Errorf("missing last_updated: %s", str)
	}
	if !strings.Contains(str, "counter=5") {
		t.Errorf("missing counter: %s", str)
	}
	if !strings.Contains(str, "status=OK") {
		t.Errorf("missing status: %s", str)
	}
}

func TestServerStatMan_AddAndFind(t *testing.T) {
	m := NewServerStatMan()
	s := NewServerStat("example.com", "http")
	s.SetDownloadSpeed(5000)
	m.Add(s)

	found := m.Find("example.com", "http")
	if found == nil {
		t.Fatal("Find returned nil")
	}
	if found.DownloadSpeed() != 5000 {
		t.Errorf("DownloadSpeed = %d, want 5000", found.DownloadSpeed())
	}
}

func TestServerStatMan_Find_NotFound(t *testing.T) {
	m := NewServerStatMan()
	if got := m.Find("nonexistent", "http"); got != nil {
		t.Error("Find should return nil for unknown host")
	}
}

func TestServerStatMan_AddDuplicate(t *testing.T) {
	m := NewServerStatMan()
	s1 := NewServerStat("example.com", "http")
	s1.SetDownloadSpeed(100)
	m.Add(s1)

	s2 := NewServerStat("example.com", "http")
	s2.SetDownloadSpeed(200)
	m.Add(s2)

	if m.Len() != 1 {
		t.Fatalf("Len = %d, want 1 after duplicate add", m.Len())
	}

	found := m.Find("example.com", "http")
	if found.DownloadSpeed() != 100 {
		t.Errorf("should keep first entry's speed, got %d", found.DownloadSpeed())
	}
}

func TestServerStatMan_MultipleEntries(t *testing.T) {
	m := NewServerStatMan()
	m.Add(NewServerStat("a.com", "http"))
	m.Add(NewServerStat("a.com", "ftp"))
	m.Add(NewServerStat("b.com", "http"))

	if m.Len() != 3 {
		t.Fatalf("Len = %d, want 3", m.Len())
	}

	if m.Find("a.com", "http") == nil {
		t.Error("should find a.com/http")
	}
	if m.Find("a.com", "ftp") == nil {
		t.Error("should find a.com/ftp")
	}
	if m.Find("b.com", "http") == nil {
		t.Error("should find b.com/http")
	}
}

func TestServerStatMan_Save(t *testing.T) {
	m := NewServerStatMan()
	s := NewServerStat("example.com", "http")
	s.SetDownloadSpeed(5000)
	s.SetSingleConnectionAvgSpeed(3000)
	s.SetMultiConnectionAvgSpeed(4000)
	s.SetCounter(5)
	s.SetLastUpdated(time.Unix(1000, 0))
	m.Add(s)

	var buf bytes.Buffer
	if err := m.Save(&buf); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "host=example.com") {
		t.Errorf("missing host in output: %s", output)
	}
	if !strings.Contains(output, "protocol=http") {
		t.Errorf("missing protocol in output: %s", output)
	}
}

func TestServerStatMan_Load(t *testing.T) {
	input := `host=example.com, protocol=http, dl_speed=5000, sc_avg_speed=3000, mc_avg_speed=4000, last_updated=1000, counter=5, status=OK
host=other.com, protocol=ftp, dl_speed=100, sc_avg_speed=50, mc_avg_speed=60, last_updated=2000, counter=1, status=ERROR
`
	m := NewServerStatMan()
	if err := m.Load(strings.NewReader(input)); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if m.Len() != 2 {
		t.Fatalf("Len = %d, want 2", m.Len())
	}

	s1 := m.Find("example.com", "http")
	if s1 == nil {
		t.Fatal("example.com/http not found")
	}
	if s1.DownloadSpeed() != 5000 {
		t.Errorf("dl_speed = %d, want 5000", s1.DownloadSpeed())
	}
	if s1.SingleConnectionAvgSpeed() != 3000 {
		t.Errorf("sc_avg_speed = %d, want 3000", s1.SingleConnectionAvgSpeed())
	}
	if s1.MultiConnectionAvgSpeed() != 4000 {
		t.Errorf("mc_avg_speed = %d, want 4000", s1.MultiConnectionAvgSpeed())
	}
	if s1.Counter() != 5 {
		t.Errorf("counter = %d, want 5", s1.Counter())
	}
	if !s1.IsOK() {
		t.Error("should be OK")
	}

	s2 := m.Find("other.com", "ftp")
	if s2 == nil {
		t.Fatal("other.com/ftp not found")
	}
	if !s2.IsError() {
		t.Error("should be ERROR")
	}
}

func TestServerStatMan_Load_EmptyLines(t *testing.T) {
	input := `

host=example.com, protocol=http, dl_speed=100, last_updated=1, counter=0, status=OK

`
	m := NewServerStatMan()
	if err := m.Load(strings.NewReader(input)); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if m.Len() != 1 {
		t.Errorf("Len = %d, want 1", m.Len())
	}
}

func TestServerStatMan_Load_MissingFields(t *testing.T) {
	input := `host=, protocol=, dl_speed=100
host=only.dl_speed=100
`
	m := NewServerStatMan()
	if err := m.Load(strings.NewReader(input)); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if m.Len() != 0 {
		t.Errorf("Len = %d, want 0 (no valid entries)", m.Len())
	}
}

func TestServerStatMan_RemoveStale(t *testing.T) {
	m := NewServerStatMan()

	// Recent entry.
	s1 := NewServerStat("recent.com", "http")
	s1.SetLastUpdated(time.Now())
	m.Add(s1)

	// Old entry (25 hours ago).
	s2 := NewServerStat("old.com", "http")
	s2.SetLastUpdated(time.Now().Add(-25 * time.Hour))
	m.Add(s2)

	// Zero-time entry (never updated).
	s3 := NewServerStat("never.com", "http")
	m.Add(s3)

	if m.Len() != 3 {
		t.Fatalf("Len = %d, want 3 before removal", m.Len())
	}

	m.RemoveStale(24 * time.Hour)

	if m.Len() != 1 {
		t.Fatalf("Len = %d, want 1 after stale removal", m.Len())
	}
	if m.Find("recent.com", "http") == nil {
		t.Error("recent entry should not be removed")
	}
	if m.Find("old.com", "http") != nil {
		t.Error("old entry should be removed")
	}
	if m.Find("never.com", "http") != nil {
		t.Error("zero-time entry should be removed")
	}
}

func TestServerStatMan_SaveLoadRoundTrip(t *testing.T) {
	m1 := NewServerStatMan()
	s1 := NewServerStat("host1.com", "http")
	s1.SetDownloadSpeed(1000)
	s1.SetSingleConnectionAvgSpeed(800)
	s1.SetMultiConnectionAvgSpeed(900)
	s1.SetCounter(3)
	s1.SetLastUpdated(time.Unix(5000, 0))
	s1.SetStatus("OK")
	m1.Add(s1)

	s2 := NewServerStat("host2.com", "ftp")
	s2.SetDownloadSpeed(200)
	s2.SetSingleConnectionAvgSpeed(150)
	s2.SetMultiConnectionAvgSpeed(180)
	s2.SetCounter(1)
	s2.SetLastUpdated(time.Unix(6000, 0))
	s2.SetStatus("ERROR")
	m1.Add(s2)

	var buf bytes.Buffer
	if err := m1.Save(&buf); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	m2 := NewServerStatMan()
	if err := m2.Load(&buf); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if m2.Len() != 2 {
		t.Fatalf("round-trip Len = %d, want 2", m2.Len())
	}

	check := func(name string, got, want *ServerStat) {
		t.Helper()
		if got == nil {
			t.Fatalf("%s: not found", name)
		}
		if got.DownloadSpeed() != want.DownloadSpeed() {
			t.Errorf("%s: DownloadSpeed = %d, want %d", name, got.DownloadSpeed(), want.DownloadSpeed())
		}
		if got.SingleConnectionAvgSpeed() != want.SingleConnectionAvgSpeed() {
			t.Errorf("%s: SingleConnectionAvgSpeed = %d, want %d", name, got.SingleConnectionAvgSpeed(), want.SingleConnectionAvgSpeed())
		}
		if got.MultiConnectionAvgSpeed() != want.MultiConnectionAvgSpeed() {
			t.Errorf("%s: MultiConnectionAvgSpeed = %d, want %d", name, got.MultiConnectionAvgSpeed(), want.MultiConnectionAvgSpeed())
		}
		if got.Counter() != want.Counter() {
			t.Errorf("%s: Counter = %d, want %d", name, got.Counter(), want.Counter())
		}
		if got.Status() != want.Status() {
			t.Errorf("%s: Status = %v, want %v", name, got.Status(), want.Status())
		}
		if !got.LastUpdated().Equal(want.LastUpdated()) {
			t.Errorf("%s: LastUpdated = %v, want %v", name, got.LastUpdated(), want.LastUpdated())
		}
	}

	check("host1.com/http", m2.Find("host1.com", "http"), s1)
	check("host2.com/ftp", m2.Find("host2.com", "ftp"), s2)
}

func TestServerStatMan_IncreaseCounter(t *testing.T) {
	s := NewServerStat("host", "http")
	if s.Counter() != 0 {
		t.Errorf("initial counter = %d, want 0", s.Counter())
	}
	s.IncreaseCounter()
	if s.Counter() != 1 {
		t.Errorf("counter after increase = %d, want 1", s.Counter())
	}
	s.IncreaseCounter()
	if s.Counter() != 2 {
		t.Errorf("counter after second increase = %d, want 2", s.Counter())
	}
}
