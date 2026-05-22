package types

import "time"

type Binary string

const (
	BinaryAria2c  Binary = "aria2c"
	BinaryAria2go Binary = "aria2go"
)

type Protocol string

const (
	ProtocolHTTP    Protocol = "http"
	ProtocolTorrent Protocol = "torrent"
)

type ScenarioKind string

const (
	ScenarioHTTPSingle     ScenarioKind = "http-single"
	ScenarioHTTPConcurrent ScenarioKind = "http-concurrent"
	ScenarioTorrentSingle  ScenarioKind = "torrent-single"
	ScenarioTorrentBurst   ScenarioKind = "torrent-burst"
)

type MetricSample struct {
	Timestamp       time.Time     `json:"ts"`
	Offset          time.Duration `json:"offset_ns"`
	Binary          Binary        `json:"binary"`
	PID             int           `json:"pid"`
	CpuUserPct      float64       `json:"cpu_user_pct"`
	CpuSysPct       float64       `json:"cpu_sys_pct"`
	RSSBytes        int64         `json:"rss_bytes"`
	VSSBytes        int64         `json:"vss_bytes"`
	Threads         int           `json:"threads"`
	OpenFDs         int           `json:"open_fds"`
	BytesDownLoaded int64         `json:"bytes_downloaded,omitempty"`
}

type ScenarioResult struct {
	Kind      ScenarioKind   `json:"kind"`
	Binary    Binary         `json:"binary"`
	Protocol  Protocol       `json:"protocol"`
	StartedAt time.Time      `json:"started_at"`
	Duration  time.Duration  `json:"duration_ns"`
	Samples   []MetricSample `json:"samples"`
	Summary   Summary        `json:"summary"`
	Errors    []string       `json:"errors,omitempty"`
}

type Summary struct {
	CPUUserPct     SummaryStats `json:"cpu_user_pct"`
	CPUSysPct      SummaryStats `json:"cpu_sys_pct"`
	RSSBytes       SummaryStats `json:"rss_bytes"`
	Threads        SummaryStats `json:"threads"`
	OpenFDs        SummaryStats `json:"open_fds"`
	ThroughputMbps float64     `json:"throughput_mbps"`
	TotalBytes     int64       `json:"total_bytes"`
}

type SummaryStats struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Mean float64 `json:"mean"`
	P95  float64 `json:"p95"`
	Last float64 `json:"last"`
}

type Report struct {
	GeneratedAt time.Time        `json:"generated_at"`
	Host        string           `json:"host"`
	Aria2cVer   string           `json:"aria2c_version"`
	Aria2goVer  string           `json:"aria2go_version"`
	Scenarios   []ScenarioResult `json:"scenarios"`
}
