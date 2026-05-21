package engine

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ServerStatStatus represents the status of a server stat entry.
type ServerStatStatus int32

const (
	ServerStatOK    ServerStatStatus = 0
	ServerStatError ServerStatStatus = 1
)

var statusStrings = []string{"OK", "ERROR"}

func (s ServerStatStatus) String() string {
	if int(s) < len(statusStrings) {
		return statusStrings[s]
	}
	return "OK"
}

func parseStatus(s string) ServerStatStatus {
	for i, str := range statusStrings {
		if s == str {
			return ServerStatStatus(i)
		}
	}
	return ServerStatOK
}

// ServerStat tracks per-server performance statistics.
// It matches aria2's ServerStat class.
type ServerStat struct {
	hostname                 string
	protocol                 string
	downloadSpeed            int64
	singleConnectionAvgSpeed int64
	multiConnectionAvgSpeed  int64
	counter                  int32
	status                   ServerStatStatus
	lastUpdated              time.Time
}

// NewServerStat creates a new ServerStat with the given hostname and protocol.
// Matches aria2's ServerStat(hostname, protocol) constructor: speeds=0,
// counter=0, status=OK.
func NewServerStat(hostname, protocol string) *ServerStat {
	return &ServerStat{
		hostname: hostname,
		protocol: protocol,
		status:   ServerStatOK,
	}
}

// Hostname returns the server hostname.
func (s *ServerStat) Hostname() string { return s.hostname }

// Protocol returns the server protocol.
func (s *ServerStat) Protocol() string { return s.protocol }

// DownloadSpeed returns the current download speed.
func (s *ServerStat) DownloadSpeed() int64 { return s.downloadSpeed }

// SetDownloadSpeed sets the download speed without updating lastUpdated.
func (s *ServerStat) SetDownloadSpeed(v int64) { s.downloadSpeed = v }

// UpdateDownloadSpeed sets the download speed and resets lastUpdated to now.
// If speed > 0, status is set to OK. Matches aria2's updateDownloadSpeed.
func (s *ServerStat) UpdateDownloadSpeed(v int64) {
	s.downloadSpeed = v
	if v > 0 {
		s.status = ServerStatOK
	}
	s.lastUpdated = time.Now()
}

// SingleConnectionAvgSpeed returns the single-connection average speed.
func (s *ServerStat) SingleConnectionAvgSpeed() int64 { return s.singleConnectionAvgSpeed }

// SetSingleConnectionAvgSpeed sets the single-connection average speed.
func (s *ServerStat) SetSingleConnectionAvgSpeed(v int64) { s.singleConnectionAvgSpeed = v }

// MultiConnectionAvgSpeed returns the multi-connection average speed.
func (s *ServerStat) MultiConnectionAvgSpeed() int64 { return s.multiConnectionAvgSpeed }

// SetMultiConnectionAvgSpeed sets the multi-connection average speed.
func (s *ServerStat) SetMultiConnectionAvgSpeed(v int64) { s.multiConnectionAvgSpeed = v }

// Counter returns the use counter.
func (s *ServerStat) Counter() int32 { return s.counter }

// SetCounter sets the counter.
func (s *ServerStat) SetCounter(v int32) { s.counter = v }

// IncreaseCounter increments the counter by 1. Matches aria2's increaseCounter.
func (s *ServerStat) IncreaseCounter() { s.counter++ }

// Status returns the server status.
func (s *ServerStat) Status() ServerStatStatus { return s.status }

// SetStatus sets the status from a string ("OK" or "ERROR") without updating
// lastUpdated. Matches aria2's setStatus(const string&).
func (s *ServerStat) SetStatus(status string) { s.status = parseStatus(status) }

// IsOK returns true if the status is OK.
func (s *ServerStat) IsOK() bool { return s.status == ServerStatOK }

// SetOK sets status to OK and resets lastUpdated to now.
func (s *ServerStat) SetOK() {
	s.status = ServerStatOK
	s.lastUpdated = time.Now()
}

// IsError returns true if the status is ERROR.
func (s *ServerStat) IsError() bool { return s.status == ServerStatError }

// SetError sets status to ERROR and resets lastUpdated to now.
func (s *ServerStat) SetError() {
	s.status = ServerStatError
	s.lastUpdated = time.Now()
}

// LastUpdated returns the last updated time.
func (s *ServerStat) LastUpdated() time.Time { return s.lastUpdated }

// SetLastUpdated sets the last updated time without resetting.
func (s *ServerStat) SetLastUpdated(t time.Time) { s.lastUpdated = t }

// Less compares by hostname then protocol. Matches aria2's operator<.
func (s *ServerStat) Less(other *ServerStat) bool {
	if s.hostname != other.hostname {
		return s.hostname < other.hostname
	}
	return s.protocol < other.protocol
}

// Equal compares by hostname and protocol. Matches aria2's operator==.
func (s *ServerStat) Equal(other *ServerStat) bool {
	return s.hostname == other.hostname && s.protocol == other.protocol
}

// toString returns the CSV key=value representation matching aria2's
// toString(): host=H, protocol=P, dl_speed=D, sc_avg_speed=SC,
// mc_avg_speed=MC, last_updated=T, counter=C, status=S
func (s *ServerStat) toString() string {
	var b strings.Builder
	b.Grow(128)
	b.WriteString("host=")
	b.WriteString(s.hostname)
	b.WriteString(", protocol=")
	b.WriteString(s.protocol)
	b.WriteString(", dl_speed=")
	b.Write(strconv.AppendInt(nil, s.downloadSpeed, 10))
	b.WriteString(", sc_avg_speed=")
	b.Write(strconv.AppendInt(nil, s.singleConnectionAvgSpeed, 10))
	b.WriteString(", mc_avg_speed=")
	b.Write(strconv.AppendInt(nil, s.multiConnectionAvgSpeed, 10))
	b.WriteString(", last_updated=")
	b.Write(strconv.AppendInt(nil, s.lastUpdated.Unix(), 10))
	b.WriteString(", counter=")
	b.Write(strconv.AppendInt(nil, int64(s.counter), 10))
	b.WriteString(", status=")
	b.WriteString(s.status.String())
	return b.String()
}

func (s *ServerStat) writeToCSV(dst []byte) []byte {
	const (
		hostPrefix        = "host="
		protocolPrefix    = ", protocol="
		dlSpeedPrefix     = ", dl_speed="
		scAvgSpeedPrefix  = ", sc_avg_speed="
		mcAvgSpeedPrefix  = ", mc_avg_speed="
		lastUpdatedPrefix = ", last_updated="
		counterPrefix     = ", counter="
		statusPrefix      = ", status="
	)
	dst = append(dst, hostPrefix...)
	dst = append(dst, s.hostname...)
	dst = append(dst, protocolPrefix...)
	dst = append(dst, s.protocol...)
	dst = append(dst, dlSpeedPrefix...)
	dst = strconv.AppendInt(dst, s.downloadSpeed, 10)
	dst = append(dst, scAvgSpeedPrefix...)
	dst = strconv.AppendInt(dst, s.singleConnectionAvgSpeed, 10)
	dst = append(dst, mcAvgSpeedPrefix...)
	dst = strconv.AppendInt(dst, s.multiConnectionAvgSpeed, 10)
	dst = append(dst, lastUpdatedPrefix...)
	dst = strconv.AppendInt(dst, s.lastUpdated.Unix(), 10)
	dst = append(dst, counterPrefix...)
	dst = strconv.AppendInt(dst, int64(s.counter), 10)
	dst = append(dst, statusPrefix...)
	dst = append(dst, s.status.String()...)
	return dst
}

// csvFieldNames are the expected field names in the CSV format, matching
// aria2's FIELD_NAMES order: counter, dl_speed, host, last_updated,
// mc_avg_speed, protocol, sc_avg_speed, status.
var csvFieldNames = []string{
	"counter", "dl_speed", "host", "last_updated",
	"mc_avg_speed", "protocol", "sc_avg_speed", "status",
}

func fieldID(name string) int {
	for i, fn := range csvFieldNames {
		if name == fn {
			return i
		}
	}
	return len(csvFieldNames)
}

// ServerStatMan manages a set of ServerStat entries, supporting load/save
// and stale removal. Matches aria2's ServerStatMan.
type ServerStatMan struct {
	mu    sync.Mutex
	stats []*ServerStat
}

// NewServerStatMan creates a new ServerStatMan.
func NewServerStatMan() *ServerStatMan {
	return &ServerStatMan{}
}

// Add inserts a ServerStat. If an entry with the same hostname and protocol
// already exists, false is returned. Matches aria2's add which uses
// lower_bound and checks for equality.
func (m *ServerStatMan) Add(stat *ServerStat) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addLocked(stat)
}

func (m *ServerStatMan) addLocked(stat *ServerStat) bool {
	idx := sort.Search(len(m.stats), func(i int) bool {
		return !m.stats[i].Less(stat)
	})
	if idx < len(m.stats) && m.stats[idx].Equal(stat) {
		return false
	}
	m.stats = append(m.stats, nil)
	copy(m.stats[idx+1:], m.stats[idx:])
	m.stats[idx] = stat
	return true
}

// Find looks up a ServerStat by hostname and protocol. Returns nil if not
// found. Matches aria2's find which creates a temp ServerStat for lookup.
func (m *ServerStatMan) Find(hostname, protocol string) *ServerStat {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := NewServerStat(hostname, protocol)
	idx := sort.Search(len(m.stats), func(i int) bool {
		return !m.stats[i].Less(key)
	})
	if idx < len(m.stats) && m.stats[idx].Equal(key) {
		return m.stats[idx]
	}
	return nil
}

// Save writes all server stats to w in CSV format, one line per entry.
// Matches aria2's save which writes toString() + "\n" for each entry.
func (m *ServerStatMan) Save(w io.Writer) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var buf []byte
	for _, s := range m.stats {
		buf = s.writeToCSV(buf[:0])
		buf = append(buf, '\n')
		if _, err := w.Write(buf); err != nil {
			return fmt.Errorf("engine: save server stat: %w", err)
		}
	}
	return nil
}

// Load reads server stats from r in CSV format. Matches aria2's load which
// parses key=value pairs separated by commas, one entry per line.
func (m *ServerStatMan) Load(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("engine: load server stat: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		items := strings.Split(line, ",")
		fields := make([]string, len(csvFieldNames))
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			kv := strings.SplitN(item, "=", 2)
			if len(kv) != 2 {
				continue
			}
			id := fieldID(strings.TrimSpace(kv[0]))
			if id < len(fields) {
				fields[id] = strings.TrimSpace(kv[1])
			}
		}

		if fields[2] == "" || fields[5] == "" {
			continue
		}

		stat := NewServerStat(fields[2], fields[5])

		if v, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			stat.SetDownloadSpeed(v)
		}
		if fields[6] != "" {
			if v, err := strconv.ParseInt(fields[6], 10, 64); err == nil {
				stat.SetSingleConnectionAvgSpeed(v)
			}
		}
		if fields[4] != "" {
			if v, err := strconv.ParseInt(fields[4], 10, 64); err == nil {
				stat.SetMultiConnectionAvgSpeed(v)
			}
		}
		if fields[0] != "" {
			if v, err := strconv.ParseInt(fields[0], 10, 32); err == nil {
				stat.SetCounter(int32(v))
			}
		}
		if v, err := strconv.ParseInt(fields[3], 10, 64); err == nil {
			stat.SetLastUpdated(time.Unix(v, 0))
		}
		stat.SetStatus(fields[7])

		m.addLocked(stat)
	}
	return nil
}

// RemoveStale removes entries whose lastUpdated time is older than the
// given timeout duration. Matches aria2's removeStaleServerStat which
// compares Time::difference(now) >= timeout.
func (m *ServerStatMan) RemoveStale(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	deadline := time.Now().Add(-timeout)
	n := 0
	for _, s := range m.stats {
		if s.lastUpdated.After(deadline) && !s.lastUpdated.IsZero() {
			m.stats[n] = s
			n++
		}
	}
	m.stats = m.stats[:n]
}

// Len returns the number of stored server stats.
func (m *ServerStatMan) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.stats)
}

// Stats returns a copy of all stored server stats.
func (m *ServerStatMan) Stats() []*ServerStat {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*ServerStat, len(m.stats))
	copy(result, m.stats)
	return result
}
