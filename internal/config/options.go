package config

// Options holds every aria2 configuration option as typed fields.
// Field types follow config-keys.md: string for text/size/duration/enum,
// int for integers, bool for booleans, []string for accumulative options.
// json tags use exact aria2 option names with hyphens.
type Options struct {
	explicit map[string]bool

	// Basic
	Dir                               string `json:"dir"`
	InputFile                         string `json:"input-file"`
	Log                               string `json:"log"`
	MaxConcurrentDownloads            int    `json:"max-concurrent-downloads"`
	CheckIntegrity                    bool   `json:"check-integrity"`
	Continue                          bool   `json:"continue"`
	LogLevel                          string `json:"log-level"`
	ConsoleLogLevel                   string `json:"console-log-level"`
	Daemon                            bool   `json:"daemon"`
	Split                             int    `json:"split"`
	MaxConnectionPerServer            int    `json:"max-connection-per-server"`
	MinSplitSize                      string `json:"min-split-size"`
	MaxOverallDownloadLimit           string `json:"max-overall-download-limit"`
	MaxDownloadLimit                  string `json:"max-download-limit"`
	MaxOverallUploadLimit             string `json:"max-overall-upload-limit"`
	MaxUploadLimit                    string `json:"max-upload-limit"`
	Out                               string `json:"out"`
	Quiet                             bool   `json:"quiet"`
	ShowConsoleReadout                bool   `json:"show-console-readout"`
	TruncateConsoleReadout            bool   `json:"truncate-console-readout"`
	HumanReadable                     bool   `json:"human-readable"`
	SummaryInterval                   string `json:"summary-interval"`
	DownloadResult                    string `json:"download-result"`
	OptimizeConcurrentDownloads       string `json:"optimize-concurrent-downloads"`
	OptimizeConcurrentDownloadsCoeffA string `json:"optimize-concurrent-downloads-coeffA"`
	OptimizeConcurrentDownloadsCoeffB string `json:"optimize-concurrent-downloads-coeffB"`
	ForceSequential                   bool   `json:"force-sequential"`
	Stderr                            bool   `json:"stderr"`

	// HTTP
	HTTPUser             string   `json:"http-user"`
	HTTPPasswd           string   `json:"http-passwd"`
	UserAgent            string   `json:"user-agent"`
	Referer              string   `json:"referer"`
	EnableHTTPKeepAlive  bool     `json:"enable-http-keep-alive"`
	EnableHTTPPipelining bool     `json:"enable-http-pipelining"`
	HTTPAcceptGzip       bool     `json:"http-accept-gzip"`
	HTTPAuthChallenge    bool     `json:"http-auth-challenge"`
	HTTPNoCache          bool     `json:"http-no-cache"`
	NoWantDigestHeader   bool     `json:"no-want-digest-header"`
	UseHead              bool     `json:"use-head"`
	MaxHTTPPipelining    string   `json:"max-http-pipelining"` // hidden
	Header               []string `json:"header"`
	LoadCookies          string   `json:"load-cookies"`
	SaveCookies          string   `json:"save-cookies"`
	CACertificate        string   `json:"ca-certificate"` // OS-specific default: CA_BUNDLE on some builds, else ""
	Certificate          string   `json:"certificate"`
	CheckCertificate     bool     `json:"check-certificate"`
	CheckCertificateSet  bool     `json:"-"`
	PrivateKey           string   `json:"private-key"`
	HTTPProxy            string   `json:"http-proxy"`
	HTTPProxyUser        string   `json:"http-proxy-user"`
	HTTPProxyPasswd      string   `json:"http-proxy-passwd"`
	HTTPSProxy           string   `json:"https-proxy"`
	HTTPSProxyUser       string   `json:"https-proxy-user"`
	HTTPSProxyPasswd     string   `json:"https-proxy-passwd"`

	// FTP/SFTP
	FTPUser            string `json:"ftp-user"`
	FTPPasswd          string `json:"ftp-passwd"`
	FTPPasv            bool   `json:"ftp-pasv"`
	FTPType            string `json:"ftp-type"`
	FTPReuseConnection bool   `json:"ftp-reuse-connection"`
	FTPProxy           string `json:"ftp-proxy"`
	FTPProxyUser       string `json:"ftp-proxy-user"`
	FTPProxyPasswd     string `json:"ftp-proxy-passwd"`
	SSHHostKeyMD       string `json:"ssh-host-key-md"`
	NetrcPath          string `json:"netrc-path"`
	NoNetrc            bool   `json:"no-netrc"`

	// HTTP/FTP/SFTP Shared
	ConnectTimeout      string `json:"connect-timeout"`
	DNSTimeout          string `json:"dns-timeout"` // hidden
	Timeout             string `json:"timeout"`
	MaxTries            int    `json:"max-tries"`
	RetryWait           string `json:"retry-wait"`
	MaxFileNotFound     int    `json:"max-file-not-found"`
	LowestSpeedLimit    string `json:"lowest-speed-limit"`
	RemoteTime          bool   `json:"remote-time"`
	ReuseURI            bool   `json:"reuse-uri"`
	URISelector         string `json:"uri-selector"`
	StreamPieceSelector string `json:"stream-piece-selector"`
	ServerStatOf        string `json:"server-stat-of"`
	ServerStatIf        string `json:"server-stat-if"`
	ServerStatTimeout   string `json:"server-stat-timeout"`
	ProxyMethod         string `json:"proxy-method"`
	AllProxy            string `json:"all-proxy"`
	AllProxyUser        string `json:"all-proxy-user"`
	AllProxyPasswd      string `json:"all-proxy-passwd"`
	NoProxy             string `json:"no-proxy"`
	DryRun              bool   `json:"dry-run"`
	ParameterizedURI    bool   `json:"parameterized-uri"`

	// Checksum
	Checksum              string `json:"checksum"`
	RealtimeChunkChecksum bool   `json:"realtime-chunk-checksum"`

	// BitTorrent
	BTMetadataOnly             bool     `json:"bt-metadata-only"`
	BTSaveMetadata             bool     `json:"bt-save-metadata"`
	BTLoadSavedMetadata        bool     `json:"bt-load-saved-metadata"`
	BTEnableLPD                bool     `json:"bt-enable-lpd"`
	BTLPDInterface             string   `json:"bt-lpd-interface"`
	BTTracker                  []string `json:"bt-tracker"`
	BTExcludeTracker           []string `json:"bt-exclude-tracker"`
	BTTrackerConnectTimeout    string   `json:"bt-tracker-connect-timeout"`
	BTTrackerTimeout           string   `json:"bt-tracker-timeout"`
	BTTrackerInterval          string   `json:"bt-tracker-interval"`
	BTMaxPeers                 int      `json:"bt-max-peers"`
	BTRequestPeerSpeedLimit    string   `json:"bt-request-peer-speed-limit"`
	BTStopTimeout              string   `json:"bt-stop-timeout"`
	BTTimeout                  string   `json:"bt-timeout"`              // hidden
	BTRequestTimeout           string   `json:"bt-request-timeout"`      // hidden
	BTKeepAliveInterval        string   `json:"bt-keep-alive-interval"`  // hidden
	PeerConnectionTimeout      string   `json:"peer-connection-timeout"` // hidden
	BTPrioritizePiece          string   `json:"bt-prioritize-piece"`
	BTHashCheckSeed            bool     `json:"bt-hash-check-seed"`
	BTSeedUnverified           bool     `json:"bt-seed-unverified"`
	BTRemoveUnselectedFile     bool     `json:"bt-remove-unselected-file"`
	BTMaxOpenFiles             int      `json:"bt-max-open-files"`
	BTDetachSeedOnly           bool     `json:"bt-detach-seed-only"`
	BTEnableHookAfterHashCheck bool     `json:"bt-enable-hook-after-hash-check"`
	BTForceEncryption          bool     `json:"bt-force-encryption"`
	BTRequireCrypto            bool     `json:"bt-require-crypto"`
	BTMinCryptoLevel           string   `json:"bt-min-crypto-level"`
	BTExternalIP               string   `json:"bt-external-ip"`
	PeerIDPrefix               string   `json:"peer-id-prefix"`
	PeerAgent                  string   `json:"peer-agent"`
	SeedRatio                  string   `json:"seed-ratio"`
	SeedTime                   string   `json:"seed-time"`
	ListenPort                 string   `json:"listen-port"`
	TorrentFile                string   `json:"torrent-file"`
	FollowTorrent              string   `json:"follow-torrent"`
	SelectFile                 string   `json:"select-file"`
	ShowFiles                  bool     `json:"show-files"`
	IndexOut                   []string `json:"index-out"`
	DSCP                       string   `json:"dscp"`
	EnableDHT                  bool     `json:"enable-dht"`
	EnableDHT6                 bool     `json:"enable-dht6"`
	DHTListenPort              string   `json:"dht-listen-port"`
	DHTListenAddr              string   `json:"dht-listen-addr"` // hidden
	DHTListenAddr6             string   `json:"dht-listen-addr6"`
	DHTEntryPointHost          string   `json:"dht-entry-point-host"` // hidden
	DHTEntryPointPort          string   `json:"dht-entry-point-port"` // hidden
	DHTEntryPoint              []string `json:"dht-entry-point"`
	DHTEntryPointHost6         string   `json:"dht-entry-point-host6"` // hidden
	DHTEntryPointPort6         string   `json:"dht-entry-point-port6"` // hidden
	DHTEntryPoint6             []string `json:"dht-entry-point6"`
	DHTFilePath                string   `json:"dht-file-path"`
	DHTFilePath6               string   `json:"dht-file-path6"`
	DHTMessageTimeout          string   `json:"dht-message-timeout"`
	EnablePeerExchange         bool     `json:"enable-peer-exchange"`

	// Metalink
	FollowMetalink               string `json:"follow-metalink"`
	MetalinkBaseURI              string `json:"metalink-base-uri"`
	MetalinkFile                 string `json:"metalink-file"`
	MetalinkLanguage             string `json:"metalink-language"`
	MetalinkLocation             string `json:"metalink-location"`
	MetalinkOS                   string `json:"metalink-os"`
	MetalinkVersion              string `json:"metalink-version"`
	MetalinkPreferredProtocol    string `json:"metalink-preferred-protocol"`
	MetalinkEnableUniqueProtocol bool   `json:"metalink-enable-unique-protocol"`

	// RPC
	EnableRPC             bool   `json:"enable-rpc"`
	RPCListenPort         int    `json:"rpc-listen-port"`
	RPCListenAll          bool   `json:"rpc-listen-all"`
	RPCAllowOriginAll     bool   `json:"rpc-allow-origin-all"`
	RPCSecret             string `json:"rpc-secret"`
	RPCUser               string `json:"rpc-user"`
	RPCPasswd             string `json:"rpc-passwd"`
	RPCSecure             bool   `json:"rpc-secure"`
	RPCCertificate        string `json:"rpc-certificate"`
	RPCPrivateKey         string `json:"rpc-private-key"`
	RPCMaxRequestSize     string `json:"rpc-max-request-size"`
	RPCSaveUploadMetadata bool   `json:"rpc-save-upload-metadata"`
	Pause                 bool   `json:"pause"`
	PauseMetadata         bool   `json:"pause-metadata"`

	// Advanced
	ConfPath                      string `json:"conf-path"`
	NoConf                        bool   `json:"no-conf"`
	AllowOverwrite                bool   `json:"allow-overwrite"`
	AllowPieceLengthChange        bool   `json:"allow-piece-length-change"`
	AlwaysResume                  bool   `json:"always-resume"`
	AlwaysResumeSet               bool   `json:"-"`
	MaxResumeFailureTries         int    `json:"max-resume-failure-tries"`
	AutoFileRenaming              bool   `json:"auto-file-renaming"`
	ConditionalGet                bool   `json:"conditional-get"`
	SelectLeastUsedHost           bool   `json:"select-least-used-host"` // hidden
	ContentDispositionDefaultUTF8 bool   `json:"content-disposition-default-utf8"`
	DiskCache                     string `json:"disk-cache"`
	FileAllocation                string `json:"file-allocation"`
	NoFileAllocationLimit         string `json:"no-file-allocation-limit"`
	EnableMmap                    bool   `json:"enable-mmap"`
	MaxMmapLimit                  string `json:"max-mmap-limit"`
	ForceSave                     bool   `json:"force-save"`
	SaveNotFound                  bool   `json:"save-not-found"`
	SaveSession                   string `json:"save-session"`
	SaveSessionInterval           string `json:"save-session-interval"`
	AutoSaveInterval              string `json:"auto-save-interval"`
	StartupIdleTime               string `json:"startup-idle-time"` // hidden
	RemoveControlFile             bool   `json:"remove-control-file"`
	HashCheckOnly                 bool   `json:"hash-check-only"`
	GID                           string `json:"gid"`
	Stop                          string `json:"stop"`
	StopWithProcess               int    `json:"stop-with-process"`
	Interface                     string `json:"interface"`
	MultipleInterface             string `json:"multiple-interface"`
	DisableIPv6                   bool   `json:"disable-ipv6"`
	AsyncDNS                      bool   `json:"async-dns"`
	EnableAsyncDNS6               bool   `json:"enable-async-dns6"` // deprecated
	AsyncDNSServer                string `json:"async-dns-server"`
	MinTLSVersion                 string `json:"min-tls-version"`
	EventPoll                     string `json:"event-poll"`
	PieceLength                   string `json:"piece-length"`
	SocketRecvBufferSize          string `json:"socket-recv-buffer-size"`
	RlimitNofile                  string `json:"rlimit-nofile"`
	DeferredInput                 bool   `json:"deferred-input"`
	MaxDownloadResult             int    `json:"max-download-result"`
	KeepUnfinishedDownloadResult  bool   `json:"keep-unfinished-download-result"`
	EnableColor                   bool   `json:"enable-color"`
	OnDownloadStart               string `json:"on-download-start"`
	OnDownloadPause               string `json:"on-download-pause"`
	OnDownloadStop                string `json:"on-download-stop"`
	OnDownloadComplete            string `json:"on-download-complete"`
	OnDownloadError               string `json:"on-download-error"`
	OnBTDownloadComplete          string `json:"on-bt-download-complete"`
}

// fieldsSorted is the pre-computed ordered list of all option names.
// Returns a slice that must not be mutated by callers.
var fieldsSorted []string

func init() {
	fieldsSorted = []string{
		// Basic
		"dir",
		"input-file",
		"log",
		"max-concurrent-downloads",
		"check-integrity",
		"continue",
		"log-level",
		"console-log-level",
		"daemon",
		"split",
		"max-connection-per-server",
		"min-split-size",
		"max-overall-download-limit",
		"max-download-limit",
		"max-overall-upload-limit",
		"max-upload-limit",
		"out",
		"quiet",
		"show-console-readout",
		"truncate-console-readout",
		"human-readable",
		"summary-interval",
		"download-result",
		"optimize-concurrent-downloads",
		"optimize-concurrent-downloads-coeffA",
		"optimize-concurrent-downloads-coeffB",
		"force-sequential",
		"stderr",
		// HTTP
		"http-user",
		"http-passwd",
		"user-agent",
		"referer",
		"enable-http-keep-alive",
		"enable-http-pipelining",
		"http-accept-gzip",
		"http-auth-challenge",
		"http-no-cache",
		"no-want-digest-header",
		"use-head",
		"max-http-pipelining",
		"header",
		"load-cookies",
		"save-cookies",
		"ca-certificate",
		"certificate",
		"check-certificate",
		"private-key",
		"http-proxy",
		"http-proxy-user",
		"http-proxy-passwd",
		"https-proxy",
		"https-proxy-user",
		"https-proxy-passwd",
		// FTP/SFTP
		"ftp-user",
		"ftp-passwd",
		"ftp-pasv",
		"ftp-type",
		"ftp-reuse-connection",
		"ftp-proxy",
		"ftp-proxy-user",
		"ftp-proxy-passwd",
		"ssh-host-key-md",
		"netrc-path",
		"no-netrc",
		// HTTP/FTP/SFTP Shared
		"connect-timeout",
		"dns-timeout",
		"timeout",
		"max-tries",
		"retry-wait",
		"max-file-not-found",
		"lowest-speed-limit",
		"remote-time",
		"reuse-uri",
		"uri-selector",
		"stream-piece-selector",
		"server-stat-of",
		"server-stat-if",
		"server-stat-timeout",
		"proxy-method",
		"all-proxy",
		"all-proxy-user",
		"all-proxy-passwd",
		"no-proxy",
		"dry-run",
		"parameterized-uri",
		// Checksum
		"checksum",
		"realtime-chunk-checksum",
		// BitTorrent
		"bt-metadata-only",
		"bt-save-metadata",
		"bt-load-saved-metadata",
		"bt-enable-lpd",
		"bt-lpd-interface",
		"bt-tracker",
		"bt-exclude-tracker",
		"bt-tracker-connect-timeout",
		"bt-tracker-timeout",
		"bt-tracker-interval",
		"bt-max-peers",
		"bt-request-peer-speed-limit",
		"bt-stop-timeout",
		"bt-timeout",
		"bt-request-timeout",
		"bt-keep-alive-interval",
		"peer-connection-timeout",
		"bt-prioritize-piece",
		"bt-hash-check-seed",
		"bt-seed-unverified",
		"bt-remove-unselected-file",
		"bt-max-open-files",
		"bt-detach-seed-only",
		"bt-enable-hook-after-hash-check",
		"bt-force-encryption",
		"bt-require-crypto",
		"bt-min-crypto-level",
		"bt-external-ip",
		"peer-id-prefix",
		"peer-agent",
		"seed-ratio",
		"seed-time",
		"listen-port",
		"torrent-file",
		"follow-torrent",
		"select-file",
		"show-files",
		"index-out",
		"dscp",
		"enable-dht",
		"enable-dht6",
		"dht-listen-port",
		"dht-listen-addr",
		"dht-listen-addr6",
		"dht-entry-point-host",
		"dht-entry-point-port",
		"dht-entry-point",
		"dht-entry-point-host6",
		"dht-entry-point-port6",
		"dht-entry-point6",
		"dht-file-path",
		"dht-file-path6",
		"dht-message-timeout",
		"enable-peer-exchange",
		// Metalink
		"follow-metalink",
		"metalink-base-uri",
		"metalink-file",
		"metalink-language",
		"metalink-location",
		"metalink-os",
		"metalink-version",
		"metalink-preferred-protocol",
		"metalink-enable-unique-protocol",
		// RPC
		"enable-rpc",
		"rpc-listen-port",
		"rpc-listen-all",
		"rpc-allow-origin-all",
		"rpc-secret",
		"rpc-user",
		"rpc-passwd",
		"rpc-secure",
		"rpc-certificate",
		"rpc-private-key",
		"rpc-max-request-size",
		"rpc-save-upload-metadata",
		"pause",
		"pause-metadata",
		// Advanced
		"conf-path",
		"no-conf",
		"allow-overwrite",
		"allow-piece-length-change",
		"always-resume",
		"max-resume-failure-tries",
		"auto-file-renaming",
		"conditional-get",
		"select-least-used-host",
		"content-disposition-default-utf8",
		"disk-cache",
		"file-allocation",
		"no-file-allocation-limit",
		"enable-mmap",
		"max-mmap-limit",
		"force-save",
		"save-not-found",
		"save-session",
		"save-session-interval",
		"auto-save-interval",
		"startup-idle-time",
		"remove-control-file",
		"hash-check-only",
		"gid",
		"stop",
		"stop-with-process",
		"interface",
		"multiple-interface",
		"disable-ipv6",
		"async-dns",
		"enable-async-dns6",
		"async-dns-server",
		"min-tls-version",
		"event-poll",
		"piece-length",
		"socket-recv-buffer-size",
		"rlimit-nofile",
		"deferred-input",
		"max-download-result",
		"keep-unfinished-download-result",
		"enable-color",
		"on-download-start",
		"on-download-pause",
		"on-download-stop",
		"on-download-complete",
		"on-download-error",
		"on-bt-download-complete",
	}
}

// Fields returns the ordered list of all option names (json tag values)
// for iteration. The returned slice is shared and must not be mutated.
func (o *Options) Fields() []string {
	return fieldsSorted
}

func (o *Options) markExplicit(name string) {
	if o.explicit == nil {
		o.explicit = make(map[string]bool)
	}
	o.explicit[name] = true
}

// MarkExplicit records that name was explicitly provided by a caller that
// constructs Options outside the parser package.
func (o *Options) MarkExplicit(name string) {
	o.markExplicit(name)
}

// ClearExplicit removes an explicit marker for name.
func (o *Options) ClearExplicit(name string) {
	if o == nil || o.explicit == nil {
		return
	}
	delete(o.explicit, name)
}

// ExplicitNames returns the option names explicitly provided to this Options.
func (o *Options) ExplicitNames() []string {
	if len(o.explicit) == 0 {
		return nil
	}
	names := make([]string, 0, len(o.explicit))
	for name := range o.explicit {
		names = append(names, name)
	}
	return names
}
