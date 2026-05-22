package config

import "strconv"

type fieldSetter func(o *Options, val string) error
type fieldMerger func(dst, src *Options)

var fieldSetters map[string]fieldSetter
var fieldMergers map[string]fieldMerger
var accumulativeFieldsMap map[string]bool

func init() {
	buildFieldMaps()
}

func buildFieldMaps() {
	accumulativeFieldsMap = map[string]bool{
		"header": true, "index-out": true, "bt-tracker": true,
		"bt-exclude-tracker": true, "dht-entry-point": true, "dht-entry-point6": true,
	}

	fieldSetters = map[string]fieldSetter{
		"dir":        func(o *Options, v string) error { o.Dir = v; return nil },
		"input-file": func(o *Options, v string) error { o.InputFile = v; return nil },
		"log":        func(o *Options, v string) error { o.Log = v; return nil },
		"max-concurrent-downloads": func(o *Options, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			o.MaxConcurrentDownloads = n
			return nil
		},
		"check-integrity": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.CheckIntegrity = b
			return nil
		},
		"continue": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.Continue = b
			return nil
		},
		"log-level":         func(o *Options, v string) error { o.LogLevel = v; return nil },
		"console-log-level": func(o *Options, v string) error { o.ConsoleLogLevel = v; return nil },
		"daemon": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.Daemon = b
			return nil
		},
		"split": func(o *Options, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			o.Split = n
			return nil
		},
		"max-connection-per-server": func(o *Options, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			o.MaxConnectionPerServer = n
			return nil
		},
		"min-split-size":             func(o *Options, v string) error { o.MinSplitSize = v; return nil },
		"max-overall-download-limit": func(o *Options, v string) error { o.MaxOverallDownloadLimit = v; return nil },
		"max-download-limit":         func(o *Options, v string) error { o.MaxDownloadLimit = v; return nil },
		"max-overall-upload-limit":   func(o *Options, v string) error { o.MaxOverallUploadLimit = v; return nil },
		"max-upload-limit":           func(o *Options, v string) error { o.MaxUploadLimit = v; return nil },
		"out":                        func(o *Options, v string) error { o.Out = v; return nil },
		"quiet": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.Quiet = b
			return nil
		},
		"show-console-readout": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.ShowConsoleReadout = b
			return nil
		},
		"truncate-console-readout": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.TruncateConsoleReadout = b
			return nil
		},
		"human-readable": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.HumanReadable = b
			return nil
		},
		"summary-interval":                     func(o *Options, v string) error { o.SummaryInterval = v; return nil },
		"download-result":                      func(o *Options, v string) error { o.DownloadResult = v; return nil },
		"optimize-concurrent-downloads":        func(o *Options, v string) error { o.OptimizeConcurrentDownloads = v; return nil },
		"optimize-concurrent-downloads-coeffA": func(o *Options, v string) error { o.OptimizeConcurrentDownloadsCoeffA = v; return nil },
		"optimize-concurrent-downloads-coeffB": func(o *Options, v string) error { o.OptimizeConcurrentDownloadsCoeffB = v; return nil },
		"force-sequential": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.ForceSequential = b
			return nil
		},
		"stderr": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.Stderr = b
			return nil
		},
		"http-user":   func(o *Options, v string) error { o.HTTPUser = v; return nil },
		"http-passwd": func(o *Options, v string) error { o.HTTPPasswd = v; return nil },
		"user-agent":  func(o *Options, v string) error { o.UserAgent = v; return nil },
		"referer":     func(o *Options, v string) error { o.Referer = v; return nil },
		"enable-http-keep-alive": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.EnableHTTPKeepAlive = b
			return nil
		},
		"enable-http-pipelining": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.EnableHTTPPipelining = b
			return nil
		},
		"http-accept-gzip": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.HTTPAcceptGzip = b
			return nil
		},
		"http-auth-challenge": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.HTTPAuthChallenge = b
			return nil
		},
		"http-no-cache": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.HTTPNoCache = b
			return nil
		},
		"no-want-digest-header": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.NoWantDigestHeader = b
			return nil
		},
		"use-head": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.UseHead = b
			return nil
		},
		"max-http-pipelining": func(o *Options, v string) error { o.MaxHTTPPipelining = v; return nil },
		"header":              func(o *Options, v string) error { o.Header = append(o.Header, v); return nil },
		"load-cookies":        func(o *Options, v string) error { o.LoadCookies = v; return nil },
		"save-cookies":        func(o *Options, v string) error { o.SaveCookies = v; return nil },
		"ca-certificate":      func(o *Options, v string) error { o.CACertificate = v; return nil },
		"certificate":         func(o *Options, v string) error { o.Certificate = v; return nil },
		"check-certificate": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.CheckCertificate = b
			o.CheckCertificateSet = true
			return nil
		},
		"private-key":        func(o *Options, v string) error { o.PrivateKey = v; return nil },
		"http-proxy":         func(o *Options, v string) error { o.HTTPProxy = v; return nil },
		"http-proxy-user":    func(o *Options, v string) error { o.HTTPProxyUser = v; return nil },
		"http-proxy-passwd":  func(o *Options, v string) error { o.HTTPProxyPasswd = v; return nil },
		"https-proxy":        func(o *Options, v string) error { o.HTTPSProxy = v; return nil },
		"https-proxy-user":   func(o *Options, v string) error { o.HTTPSProxyUser = v; return nil },
		"https-proxy-passwd": func(o *Options, v string) error { o.HTTPSProxyPasswd = v; return nil },
		"ftp-user":           func(o *Options, v string) error { o.FTPUser = v; return nil },
		"ftp-passwd":         func(o *Options, v string) error { o.FTPPasswd = v; return nil },
		"ftp-pasv": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.FTPPasv = b
			return nil
		},
		"ftp-type": func(o *Options, v string) error { o.FTPType = v; return nil },
		"ftp-reuse-connection": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.FTPReuseConnection = b
			return nil
		},
		"ftp-proxy":        func(o *Options, v string) error { o.FTPProxy = v; return nil },
		"ftp-proxy-user":   func(o *Options, v string) error { o.FTPProxyUser = v; return nil },
		"ftp-proxy-passwd": func(o *Options, v string) error { o.FTPProxyPasswd = v; return nil },
		"ssh-host-key-md":  func(o *Options, v string) error { o.SSHHostKeyMD = v; return nil },
		"netrc-path":       func(o *Options, v string) error { o.NetrcPath = v; return nil },
		"no-netrc": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.NoNetrc = b
			return nil
		},
		"connect-timeout": func(o *Options, v string) error { o.ConnectTimeout = v; return nil },
		"dns-timeout":     func(o *Options, v string) error { o.DNSTimeout = v; return nil },
		"timeout":         func(o *Options, v string) error { o.Timeout = v; return nil },
		"max-tries": func(o *Options, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			o.MaxTries = n
			return nil
		},
		"retry-wait": func(o *Options, v string) error { o.RetryWait = v; return nil },
		"max-file-not-found": func(o *Options, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			o.MaxFileNotFound = n
			return nil
		},
		"lowest-speed-limit": func(o *Options, v string) error { o.LowestSpeedLimit = v; return nil },
		"remote-time": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.RemoteTime = b
			return nil
		},
		"reuse-uri": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.ReuseURI = b
			return nil
		},
		"uri-selector":          func(o *Options, v string) error { o.URISelector = v; return nil },
		"stream-piece-selector": func(o *Options, v string) error { o.StreamPieceSelector = v; return nil },
		"server-stat-of":        func(o *Options, v string) error { o.ServerStatOf = v; return nil },
		"server-stat-if":        func(o *Options, v string) error { o.ServerStatIf = v; return nil },
		"server-stat-timeout":   func(o *Options, v string) error { o.ServerStatTimeout = v; return nil },
		"proxy-method":          func(o *Options, v string) error { o.ProxyMethod = v; return nil },
		"all-proxy":             func(o *Options, v string) error { o.AllProxy = v; return nil },
		"all-proxy-user":        func(o *Options, v string) error { o.AllProxyUser = v; return nil },
		"all-proxy-passwd":      func(o *Options, v string) error { o.AllProxyPasswd = v; return nil },
		"no-proxy":              func(o *Options, v string) error { o.NoProxy = v; return nil },
		"dry-run": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.DryRun = b
			return nil
		},
		"parameterized-uri": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.ParameterizedURI = b
			return nil
		},
		"checksum": func(o *Options, v string) error { o.Checksum = v; return nil },
		"realtime-chunk-checksum": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.RealtimeChunkChecksum = b
			return nil
		},
		"bt-metadata-only": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.BTMetadataOnly = b
			return nil
		},
		"bt-save-metadata": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.BTSaveMetadata = b
			return nil
		},
		"bt-load-saved-metadata": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.BTLoadSavedMetadata = b
			return nil
		},
		"bt-enable-lpd": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.BTEnableLPD = b
			return nil
		},
		"bt-lpd-interface":           func(o *Options, v string) error { o.BTLPDInterface = v; return nil },
		"bt-tracker":                 func(o *Options, v string) error { o.BTTracker = append(o.BTTracker, v); return nil },
		"bt-exclude-tracker":         func(o *Options, v string) error { o.BTExcludeTracker = append(o.BTExcludeTracker, v); return nil },
		"bt-tracker-connect-timeout": func(o *Options, v string) error { o.BTTrackerConnectTimeout = v; return nil },
		"bt-tracker-timeout":         func(o *Options, v string) error { o.BTTrackerTimeout = v; return nil },
		"bt-tracker-interval":        func(o *Options, v string) error { o.BTTrackerInterval = v; return nil },
		"bt-max-peers": func(o *Options, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			o.BTMaxPeers = n
			return nil
		},
		"bt-request-peer-speed-limit": func(o *Options, v string) error { o.BTRequestPeerSpeedLimit = v; return nil },
		"bt-stop-timeout":             func(o *Options, v string) error { o.BTStopTimeout = v; return nil },
		"bt-timeout":                  func(o *Options, v string) error { o.BTTimeout = v; return nil },
		"bt-request-timeout":          func(o *Options, v string) error { o.BTRequestTimeout = v; return nil },
		"bt-keep-alive-interval":      func(o *Options, v string) error { o.BTKeepAliveInterval = v; return nil },
		"peer-connection-timeout":     func(o *Options, v string) error { o.PeerConnectionTimeout = v; return nil },
		"bt-prioritize-piece":         func(o *Options, v string) error { o.BTPrioritizePiece = v; return nil },
		"bt-hash-check-seed": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.BTHashCheckSeed = b
			return nil
		},
		"bt-seed-unverified": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.BTSeedUnverified = b
			return nil
		},
		"bt-remove-unselected-file": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.BTRemoveUnselectedFile = b
			return nil
		},
		"bt-max-open-files": func(o *Options, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			o.BTMaxOpenFiles = n
			return nil
		},
		"bt-detach-seed-only": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.BTDetachSeedOnly = b
			return nil
		},
		"bt-enable-hook-after-hash-check": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.BTEnableHookAfterHashCheck = b
			return nil
		},
		"bt-force-encryption": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.BTForceEncryption = b
			return nil
		},
		"bt-require-crypto": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.BTRequireCrypto = b
			return nil
		},
		"bt-min-crypto-level": func(o *Options, v string) error { o.BTMinCryptoLevel = v; return nil },
		"bt-external-ip":      func(o *Options, v string) error { o.BTExternalIP = v; return nil },
		"peer-id-prefix":      func(o *Options, v string) error { o.PeerIDPrefix = v; return nil },
		"peer-agent":          func(o *Options, v string) error { o.PeerAgent = v; return nil },
		"seed-ratio":          func(o *Options, v string) error { o.SeedRatio = v; return nil },
		"seed-time":           func(o *Options, v string) error { o.SeedTime = v; return nil },
		"listen-port":         func(o *Options, v string) error { o.ListenPort = v; return nil },
		"torrent-file":        func(o *Options, v string) error { o.TorrentFile = v; return nil },
		"follow-torrent":      func(o *Options, v string) error { o.FollowTorrent = v; return nil },
		"select-file":         func(o *Options, v string) error { o.SelectFile = v; return nil },
		"show-files": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.ShowFiles = b
			return nil
		},
		"index-out": func(o *Options, v string) error { o.IndexOut = append(o.IndexOut, v); return nil },
		"dscp":      func(o *Options, v string) error { o.DSCP = v; return nil },
		"enable-dht": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.EnableDHT = b
			return nil
		},
		"enable-dht6": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.EnableDHT6 = b
			return nil
		},
		"dht-listen-port":  func(o *Options, v string) error { o.DHTListenPort = v; return nil },
		"dht-listen-addr":  func(o *Options, v string) error { o.DHTListenAddr = v; return nil },
		"dht-listen-addr6": func(o *Options, v string) error { o.DHTListenAddr6 = v; return nil },
		"dht-entry-point-host": func(o *Options, v string) error {
			o.DHTEntryPointHost = v
			return nil
		},
		"dht-entry-point-port": func(o *Options, v string) error {
			if err := validateDHTEntryPointPort("dht-entry-point", v); err != nil {
				return err
			}
			o.DHTEntryPointPort = v
			return nil
		},
		"dht-entry-point": func(o *Options, v string) error {
			host, port, err := parseDHTEntryPointValue("dht-entry-point", v)
			if err != nil {
				return err
			}
			o.DHTEntryPoint = append(o.DHTEntryPoint, v)
			o.DHTEntryPointHost = host
			o.DHTEntryPointPort = port
			return nil
		},
		"dht-entry-point-host6": func(o *Options, v string) error {
			o.DHTEntryPointHost6 = v
			return nil
		},
		"dht-entry-point-port6": func(o *Options, v string) error {
			if err := validateDHTEntryPointPort("dht-entry-point6", v); err != nil {
				return err
			}
			o.DHTEntryPointPort6 = v
			return nil
		},
		"dht-entry-point6": func(o *Options, v string) error {
			host, port, err := parseDHTEntryPointValue("dht-entry-point6", v)
			if err != nil {
				return err
			}
			o.DHTEntryPoint6 = append(o.DHTEntryPoint6, v)
			o.DHTEntryPointHost6 = host
			o.DHTEntryPointPort6 = port
			return nil
		},
		"dht-file-path":       func(o *Options, v string) error { o.DHTFilePath = v; return nil },
		"dht-file-path6":      func(o *Options, v string) error { o.DHTFilePath6 = v; return nil },
		"dht-message-timeout": func(o *Options, v string) error { o.DHTMessageTimeout = v; return nil },
		"enable-peer-exchange": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.EnablePeerExchange = b
			return nil
		},
		"follow-metalink":             func(o *Options, v string) error { o.FollowMetalink = v; return nil },
		"metalink-base-uri":           func(o *Options, v string) error { o.MetalinkBaseURI = v; return nil },
		"metalink-file":               func(o *Options, v string) error { o.MetalinkFile = v; return nil },
		"metalink-language":           func(o *Options, v string) error { o.MetalinkLanguage = v; return nil },
		"metalink-location":           func(o *Options, v string) error { o.MetalinkLocation = v; return nil },
		"metalink-os":                 func(o *Options, v string) error { o.MetalinkOS = v; return nil },
		"metalink-version":            func(o *Options, v string) error { o.MetalinkVersion = v; return nil },
		"metalink-preferred-protocol": func(o *Options, v string) error { o.MetalinkPreferredProtocol = v; return nil },
		"metalink-enable-unique-protocol": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.MetalinkEnableUniqueProtocol = b
			return nil
		},
		"enable-rpc": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.EnableRPC = b
			return nil
		},
		"rpc-listen-port": func(o *Options, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			o.RPCListenPort = n
			return nil
		},
		"rpc-listen-all": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.RPCListenAll = b
			return nil
		},
		"rpc-allow-origin-all": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.RPCAllowOriginAll = b
			return nil
		},
		"rpc-secret": func(o *Options, v string) error { o.RPCSecret = v; return nil },
		"rpc-user":   func(o *Options, v string) error { o.RPCUser = v; return nil },
		"rpc-passwd": func(o *Options, v string) error { o.RPCPasswd = v; return nil },
		"rpc-secure": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.RPCSecure = b
			return nil
		},
		"rpc-certificate":      func(o *Options, v string) error { o.RPCCertificate = v; return nil },
		"rpc-private-key":      func(o *Options, v string) error { o.RPCPrivateKey = v; return nil },
		"rpc-max-request-size": func(o *Options, v string) error { o.RPCMaxRequestSize = v; return nil },
		"rpc-save-upload-metadata": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.RPCSaveUploadMetadata = b
			return nil
		},
		"pause": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.Pause = b
			return nil
		},
		"pause-metadata": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.PauseMetadata = b
			return nil
		},
		"conf-path": func(o *Options, v string) error { o.ConfPath = v; return nil },
		"no-conf": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.NoConf = b
			return nil
		},
		"allow-overwrite": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.AllowOverwrite = b
			return nil
		},
		"allow-piece-length-change": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.AllowPieceLengthChange = b
			return nil
		},
		"always-resume": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.AlwaysResume = b
			o.AlwaysResumeSet = true
			return nil
		},
		"max-resume-failure-tries": func(o *Options, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			o.MaxResumeFailureTries = n
			return nil
		},
		"auto-file-renaming": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.AutoFileRenaming = b
			return nil
		},
		"conditional-get": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.ConditionalGet = b
			return nil
		},
		"select-least-used-host": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.SelectLeastUsedHost = b
			return nil
		},
		"content-disposition-default-utf8": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.ContentDispositionDefaultUTF8 = b
			return nil
		},
		"disk-cache":               func(o *Options, v string) error { o.DiskCache = v; return nil },
		"file-allocation":          func(o *Options, v string) error { o.FileAllocation = v; return nil },
		"no-file-allocation-limit": func(o *Options, v string) error { o.NoFileAllocationLimit = v; return nil },
		"enable-mmap": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.EnableMmap = b
			return nil
		},
		"max-mmap-limit": func(o *Options, v string) error { o.MaxMmapLimit = v; return nil },
		"force-save": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.ForceSave = b
			return nil
		},
		"save-not-found": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.SaveNotFound = b
			return nil
		},
		"save-session":          func(o *Options, v string) error { o.SaveSession = v; return nil },
		"save-session-interval": func(o *Options, v string) error { o.SaveSessionInterval = v; return nil },
		"auto-save-interval":    func(o *Options, v string) error { o.AutoSaveInterval = v; return nil },
		"startup-idle-time":     func(o *Options, v string) error { o.StartupIdleTime = v; return nil },
		"remove-control-file": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.RemoveControlFile = b
			return nil
		},
		"hash-check-only": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.HashCheckOnly = b
			return nil
		},
		"gid":  func(o *Options, v string) error { o.GID = v; return nil },
		"stop": func(o *Options, v string) error { o.Stop = v; return nil },
		"stop-with-process": func(o *Options, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			o.StopWithProcess = n
			return nil
		},
		"interface":          func(o *Options, v string) error { o.Interface = v; return nil },
		"multiple-interface": func(o *Options, v string) error { o.MultipleInterface = v; return nil },
		"disable-ipv6": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.DisableIPv6 = b
			return nil
		},
		"async-dns": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.AsyncDNS = b
			return nil
		},
		"enable-async-dns6": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.EnableAsyncDNS6 = b
			return nil
		},
		"async-dns-server":        func(o *Options, v string) error { o.AsyncDNSServer = v; return nil },
		"min-tls-version":         func(o *Options, v string) error { o.MinTLSVersion = v; return nil },
		"event-poll":              func(o *Options, v string) error { o.EventPoll = v; return nil },
		"piece-length":            func(o *Options, v string) error { o.PieceLength = v; return nil },
		"socket-recv-buffer-size": func(o *Options, v string) error { o.SocketRecvBufferSize = v; return nil },
		"rlimit-nofile":           func(o *Options, v string) error { o.RlimitNofile = v; return nil },
		"deferred-input": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.DeferredInput = b
			return nil
		},
		"max-download-result": func(o *Options, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			o.MaxDownloadResult = n
			return nil
		},
		"keep-unfinished-download-result": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.KeepUnfinishedDownloadResult = b
			return nil
		},
		"enable-color": func(o *Options, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			o.EnableColor = b
			return nil
		},
		"on-download-start":       func(o *Options, v string) error { o.OnDownloadStart = v; return nil },
		"on-download-pause":       func(o *Options, v string) error { o.OnDownloadPause = v; return nil },
		"on-download-stop":        func(o *Options, v string) error { o.OnDownloadStop = v; return nil },
		"on-download-complete":    func(o *Options, v string) error { o.OnDownloadComplete = v; return nil },
		"on-download-error":       func(o *Options, v string) error { o.OnDownloadError = v; return nil },
		"on-bt-download-complete": func(o *Options, v string) error { o.OnBTDownloadComplete = v; return nil },
	}

	fieldMergers = map[string]fieldMerger{
		"dir": func(d, s *Options) {
			if s.Dir != "" {
				d.Dir = s.Dir
			}
		},
		"input-file": func(d, s *Options) {
			if s.InputFile != "" {
				d.InputFile = s.InputFile
			}
		},
		"log": func(d, s *Options) {
			if s.Log != "" {
				d.Log = s.Log
			}
		},
		"max-concurrent-downloads": func(d, s *Options) {
			if s.MaxConcurrentDownloads != 0 {
				d.MaxConcurrentDownloads = s.MaxConcurrentDownloads
			}
		},
		"check-integrity": func(d, s *Options) {
			if s.CheckIntegrity {
				d.CheckIntegrity = true
			}
		},
		"continue": func(d, s *Options) {
			if s.Continue {
				d.Continue = true
			}
		},
		"log-level": func(d, s *Options) {
			if s.LogLevel != "" {
				d.LogLevel = s.LogLevel
			}
		},
		"console-log-level": func(d, s *Options) {
			if s.ConsoleLogLevel != "" {
				d.ConsoleLogLevel = s.ConsoleLogLevel
			}
		},
		"daemon": func(d, s *Options) {
			if s.Daemon {
				d.Daemon = true
			}
		},
		"split": func(d, s *Options) {
			if s.Split != 0 {
				d.Split = s.Split
			}
		},
		"max-connection-per-server": func(d, s *Options) {
			if s.MaxConnectionPerServer != 0 {
				d.MaxConnectionPerServer = s.MaxConnectionPerServer
			}
		},
		"min-split-size": func(d, s *Options) {
			if s.MinSplitSize != "" {
				d.MinSplitSize = s.MinSplitSize
			}
		},
		"max-overall-download-limit": func(d, s *Options) {
			if s.MaxOverallDownloadLimit != "" {
				d.MaxOverallDownloadLimit = s.MaxOverallDownloadLimit
			}
		},
		"max-download-limit": func(d, s *Options) {
			if s.MaxDownloadLimit != "" {
				d.MaxDownloadLimit = s.MaxDownloadLimit
			}
		},
		"max-overall-upload-limit": func(d, s *Options) {
			if s.MaxOverallUploadLimit != "" {
				d.MaxOverallUploadLimit = s.MaxOverallUploadLimit
			}
		},
		"max-upload-limit": func(d, s *Options) {
			if s.MaxUploadLimit != "" {
				d.MaxUploadLimit = s.MaxUploadLimit
			}
		},
		"out": func(d, s *Options) {
			if s.Out != "" {
				d.Out = s.Out
			}
		},
		"quiet": func(d, s *Options) {
			if s.Quiet {
				d.Quiet = true
			}
		},
		"show-console-readout": func(d, s *Options) {
			if s.ShowConsoleReadout {
				d.ShowConsoleReadout = true
			}
		},
		"truncate-console-readout": func(d, s *Options) {
			if s.TruncateConsoleReadout {
				d.TruncateConsoleReadout = true
			}
		},
		"human-readable": func(d, s *Options) {
			if s.HumanReadable {
				d.HumanReadable = true
			}
		},
		"summary-interval": func(d, s *Options) {
			if s.SummaryInterval != "" {
				d.SummaryInterval = s.SummaryInterval
			}
		},
		"download-result": func(d, s *Options) {
			if s.DownloadResult != "" {
				d.DownloadResult = s.DownloadResult
			}
		},
		"optimize-concurrent-downloads": func(d, s *Options) {
			if s.OptimizeConcurrentDownloads != "" {
				d.OptimizeConcurrentDownloads = s.OptimizeConcurrentDownloads
			}
		},
		"optimize-concurrent-downloads-coeffA": func(d, s *Options) {
			if s.OptimizeConcurrentDownloadsCoeffA != "" {
				d.OptimizeConcurrentDownloadsCoeffA = s.OptimizeConcurrentDownloadsCoeffA
			}
		},
		"optimize-concurrent-downloads-coeffB": func(d, s *Options) {
			if s.OptimizeConcurrentDownloadsCoeffB != "" {
				d.OptimizeConcurrentDownloadsCoeffB = s.OptimizeConcurrentDownloadsCoeffB
			}
		},
		"force-sequential": func(d, s *Options) {
			if s.ForceSequential {
				d.ForceSequential = true
			}
		},
		"stderr": func(d, s *Options) {
			if s.Stderr {
				d.Stderr = true
			}
		},
		"http-user": func(d, s *Options) {
			if s.HTTPUser != "" {
				d.HTTPUser = s.HTTPUser
			}
		},
		"http-passwd": func(d, s *Options) {
			if s.HTTPPasswd != "" {
				d.HTTPPasswd = s.HTTPPasswd
			}
		},
		"user-agent": func(d, s *Options) {
			if s.UserAgent != "" {
				d.UserAgent = s.UserAgent
			}
		},
		"referer": func(d, s *Options) {
			if s.Referer != "" {
				d.Referer = s.Referer
			}
		},
		"enable-http-keep-alive": func(d, s *Options) {
			if s.EnableHTTPKeepAlive {
				d.EnableHTTPKeepAlive = true
			}
		},
		"enable-http-pipelining": func(d, s *Options) {
			if s.EnableHTTPPipelining {
				d.EnableHTTPPipelining = true
			}
		},
		"http-accept-gzip": func(d, s *Options) {
			if s.HTTPAcceptGzip {
				d.HTTPAcceptGzip = true
			}
		},
		"http-auth-challenge": func(d, s *Options) {
			if s.HTTPAuthChallenge {
				d.HTTPAuthChallenge = true
			}
		},
		"http-no-cache": func(d, s *Options) {
			if s.HTTPNoCache {
				d.HTTPNoCache = true
			}
		},
		"no-want-digest-header": func(d, s *Options) {
			if s.NoWantDigestHeader {
				d.NoWantDigestHeader = true
			}
		},
		"use-head": func(d, s *Options) {
			if s.UseHead {
				d.UseHead = true
			}
		},
		"max-http-pipelining": func(d, s *Options) {
			if s.MaxHTTPPipelining != "" {
				d.MaxHTTPPipelining = s.MaxHTTPPipelining
			}
		},
		"header": func(d, s *Options) {
			if len(s.Header) > 0 {
				d.Header = append(d.Header, s.Header...)
			}
		},
		"load-cookies": func(d, s *Options) {
			if s.LoadCookies != "" {
				d.LoadCookies = s.LoadCookies
			}
		},
		"save-cookies": func(d, s *Options) {
			if s.SaveCookies != "" {
				d.SaveCookies = s.SaveCookies
			}
		},
		"ca-certificate": func(d, s *Options) {
			if s.CACertificate != "" {
				d.CACertificate = s.CACertificate
			}
		},
		"certificate": func(d, s *Options) {
			if s.Certificate != "" {
				d.Certificate = s.Certificate
			}
		},
		"check-certificate": func(d, s *Options) {
			if s.CheckCertificateSet || s.CheckCertificate {
				d.CheckCertificate = s.CheckCertificate
				d.CheckCertificateSet = true
			}
		},
		"private-key": func(d, s *Options) {
			if s.PrivateKey != "" {
				d.PrivateKey = s.PrivateKey
			}
		},
		"http-proxy": func(d, s *Options) {
			if s.HTTPProxy != "" {
				d.HTTPProxy = s.HTTPProxy
			}
		},
		"http-proxy-user": func(d, s *Options) {
			if s.HTTPProxyUser != "" {
				d.HTTPProxyUser = s.HTTPProxyUser
			}
		},
		"http-proxy-passwd": func(d, s *Options) {
			if s.HTTPProxyPasswd != "" {
				d.HTTPProxyPasswd = s.HTTPProxyPasswd
			}
		},
		"https-proxy": func(d, s *Options) {
			if s.HTTPSProxy != "" {
				d.HTTPSProxy = s.HTTPSProxy
			}
		},
		"https-proxy-user": func(d, s *Options) {
			if s.HTTPSProxyUser != "" {
				d.HTTPSProxyUser = s.HTTPSProxyUser
			}
		},
		"https-proxy-passwd": func(d, s *Options) {
			if s.HTTPSProxyPasswd != "" {
				d.HTTPSProxyPasswd = s.HTTPSProxyPasswd
			}
		},
		"ftp-user": func(d, s *Options) {
			if s.FTPUser != "" {
				d.FTPUser = s.FTPUser
			}
		},
		"ftp-passwd": func(d, s *Options) {
			if s.FTPPasswd != "" {
				d.FTPPasswd = s.FTPPasswd
			}
		},
		"ftp-pasv": func(d, s *Options) {
			if s.FTPPasv {
				d.FTPPasv = true
			}
		},
		"ftp-type": func(d, s *Options) {
			if s.FTPType != "" {
				d.FTPType = s.FTPType
			}
		},
		"ftp-reuse-connection": func(d, s *Options) {
			if s.FTPReuseConnection {
				d.FTPReuseConnection = true
			}
		},
		"ftp-proxy": func(d, s *Options) {
			if s.FTPProxy != "" {
				d.FTPProxy = s.FTPProxy
			}
		},
		"ftp-proxy-user": func(d, s *Options) {
			if s.FTPProxyUser != "" {
				d.FTPProxyUser = s.FTPProxyUser
			}
		},
		"ftp-proxy-passwd": func(d, s *Options) {
			if s.FTPProxyPasswd != "" {
				d.FTPProxyPasswd = s.FTPProxyPasswd
			}
		},
		"ssh-host-key-md": func(d, s *Options) {
			if s.SSHHostKeyMD != "" {
				d.SSHHostKeyMD = s.SSHHostKeyMD
			}
		},
		"netrc-path": func(d, s *Options) {
			if s.NetrcPath != "" {
				d.NetrcPath = s.NetrcPath
			}
		},
		"no-netrc": func(d, s *Options) {
			if s.NoNetrc {
				d.NoNetrc = true
			}
		},
		"connect-timeout": func(d, s *Options) {
			if s.ConnectTimeout != "" {
				d.ConnectTimeout = s.ConnectTimeout
			}
		},
		"dns-timeout": func(d, s *Options) {
			if s.DNSTimeout != "" {
				d.DNSTimeout = s.DNSTimeout
			}
		},
		"timeout": func(d, s *Options) {
			if s.Timeout != "" {
				d.Timeout = s.Timeout
			}
		},
		"max-tries": func(d, s *Options) {
			if s.MaxTries != 0 {
				d.MaxTries = s.MaxTries
			}
		},
		"retry-wait": func(d, s *Options) {
			if s.RetryWait != "" {
				d.RetryWait = s.RetryWait
			}
		},
		"max-file-not-found": func(d, s *Options) {
			if s.MaxFileNotFound != 0 {
				d.MaxFileNotFound = s.MaxFileNotFound
			}
		},
		"lowest-speed-limit": func(d, s *Options) {
			if s.LowestSpeedLimit != "" {
				d.LowestSpeedLimit = s.LowestSpeedLimit
			}
		},
		"remote-time": func(d, s *Options) {
			if s.RemoteTime {
				d.RemoteTime = true
			}
		},
		"reuse-uri": func(d, s *Options) {
			if s.ReuseURI {
				d.ReuseURI = true
			}
		},
		"uri-selector": func(d, s *Options) {
			if s.URISelector != "" {
				d.URISelector = s.URISelector
			}
		},
		"stream-piece-selector": func(d, s *Options) {
			if s.StreamPieceSelector != "" {
				d.StreamPieceSelector = s.StreamPieceSelector
			}
		},
		"server-stat-of": func(d, s *Options) {
			if s.ServerStatOf != "" {
				d.ServerStatOf = s.ServerStatOf
			}
		},
		"server-stat-if": func(d, s *Options) {
			if s.ServerStatIf != "" {
				d.ServerStatIf = s.ServerStatIf
			}
		},
		"server-stat-timeout": func(d, s *Options) {
			if s.ServerStatTimeout != "" {
				d.ServerStatTimeout = s.ServerStatTimeout
			}
		},
		"proxy-method": func(d, s *Options) {
			if s.ProxyMethod != "" {
				d.ProxyMethod = s.ProxyMethod
			}
		},
		"all-proxy": func(d, s *Options) {
			if s.AllProxy != "" {
				d.AllProxy = s.AllProxy
			}
		},
		"all-proxy-user": func(d, s *Options) {
			if s.AllProxyUser != "" {
				d.AllProxyUser = s.AllProxyUser
			}
		},
		"all-proxy-passwd": func(d, s *Options) {
			if s.AllProxyPasswd != "" {
				d.AllProxyPasswd = s.AllProxyPasswd
			}
		},
		"no-proxy": func(d, s *Options) {
			if s.NoProxy != "" {
				d.NoProxy = s.NoProxy
			}
		},
		"dry-run": func(d, s *Options) {
			if s.DryRun {
				d.DryRun = true
			}
		},
		"parameterized-uri": func(d, s *Options) {
			if s.ParameterizedURI {
				d.ParameterizedURI = true
			}
		},
		"checksum": func(d, s *Options) {
			if s.Checksum != "" {
				d.Checksum = s.Checksum
			}
		},
		"realtime-chunk-checksum": func(d, s *Options) {
			if s.RealtimeChunkChecksum {
				d.RealtimeChunkChecksum = true
			}
		},
		"bt-metadata-only": func(d, s *Options) {
			if s.BTMetadataOnly {
				d.BTMetadataOnly = true
			}
		},
		"bt-save-metadata": func(d, s *Options) {
			if s.BTSaveMetadata {
				d.BTSaveMetadata = true
			}
		},
		"bt-load-saved-metadata": func(d, s *Options) {
			if s.BTLoadSavedMetadata {
				d.BTLoadSavedMetadata = true
			}
		},
		"bt-enable-lpd": func(d, s *Options) {
			if s.BTEnableLPD {
				d.BTEnableLPD = true
			}
		},
		"bt-lpd-interface": func(d, s *Options) {
			if s.BTLPDInterface != "" {
				d.BTLPDInterface = s.BTLPDInterface
			}
		},
		"bt-tracker": func(d, s *Options) {
			if len(s.BTTracker) > 0 {
				d.BTTracker = append(d.BTTracker, s.BTTracker...)
			}
		},
		"bt-exclude-tracker": func(d, s *Options) {
			if len(s.BTExcludeTracker) > 0 {
				d.BTExcludeTracker = append(d.BTExcludeTracker, s.BTExcludeTracker...)
			}
		},
		"bt-tracker-connect-timeout": func(d, s *Options) {
			if s.BTTrackerConnectTimeout != "" {
				d.BTTrackerConnectTimeout = s.BTTrackerConnectTimeout
			}
		},
		"bt-tracker-timeout": func(d, s *Options) {
			if s.BTTrackerTimeout != "" {
				d.BTTrackerTimeout = s.BTTrackerTimeout
			}
		},
		"bt-tracker-interval": func(d, s *Options) {
			if s.BTTrackerInterval != "" {
				d.BTTrackerInterval = s.BTTrackerInterval
			}
		},
		"bt-max-peers": func(d, s *Options) {
			if s.BTMaxPeers != 0 {
				d.BTMaxPeers = s.BTMaxPeers
			}
		},
		"bt-request-peer-speed-limit": func(d, s *Options) {
			if s.BTRequestPeerSpeedLimit != "" {
				d.BTRequestPeerSpeedLimit = s.BTRequestPeerSpeedLimit
			}
		},
		"bt-stop-timeout": func(d, s *Options) {
			if s.BTStopTimeout != "" {
				d.BTStopTimeout = s.BTStopTimeout
			}
		},
		"bt-timeout": func(d, s *Options) {
			if s.BTTimeout != "" {
				d.BTTimeout = s.BTTimeout
			}
		},
		"bt-request-timeout": func(d, s *Options) {
			if s.BTRequestTimeout != "" {
				d.BTRequestTimeout = s.BTRequestTimeout
			}
		},
		"bt-keep-alive-interval": func(d, s *Options) {
			if s.BTKeepAliveInterval != "" {
				d.BTKeepAliveInterval = s.BTKeepAliveInterval
			}
		},
		"peer-connection-timeout": func(d, s *Options) {
			if s.PeerConnectionTimeout != "" {
				d.PeerConnectionTimeout = s.PeerConnectionTimeout
			}
		},
		"bt-prioritize-piece": func(d, s *Options) {
			if s.BTPrioritizePiece != "" {
				d.BTPrioritizePiece = s.BTPrioritizePiece
			}
		},
		"bt-hash-check-seed": func(d, s *Options) {
			if s.BTHashCheckSeed {
				d.BTHashCheckSeed = true
			}
		},
		"bt-seed-unverified": func(d, s *Options) {
			if s.BTSeedUnverified {
				d.BTSeedUnverified = true
			}
		},
		"bt-remove-unselected-file": func(d, s *Options) {
			if s.BTRemoveUnselectedFile {
				d.BTRemoveUnselectedFile = true
			}
		},
		"bt-max-open-files": func(d, s *Options) {
			if s.BTMaxOpenFiles != 0 {
				d.BTMaxOpenFiles = s.BTMaxOpenFiles
			}
		},
		"bt-detach-seed-only": func(d, s *Options) {
			if s.BTDetachSeedOnly {
				d.BTDetachSeedOnly = true
			}
		},
		"bt-enable-hook-after-hash-check": func(d, s *Options) {
			if s.BTEnableHookAfterHashCheck {
				d.BTEnableHookAfterHashCheck = true
			}
		},
		"bt-force-encryption": func(d, s *Options) {
			if s.BTForceEncryption {
				d.BTForceEncryption = true
			}
		},
		"bt-require-crypto": func(d, s *Options) {
			if s.BTRequireCrypto {
				d.BTRequireCrypto = true
			}
		},
		"bt-min-crypto-level": func(d, s *Options) {
			if s.BTMinCryptoLevel != "" {
				d.BTMinCryptoLevel = s.BTMinCryptoLevel
			}
		},
		"bt-external-ip": func(d, s *Options) {
			if s.BTExternalIP != "" {
				d.BTExternalIP = s.BTExternalIP
			}
		},
		"peer-id-prefix": func(d, s *Options) {
			if s.PeerIDPrefix != "" {
				d.PeerIDPrefix = s.PeerIDPrefix
			}
		},
		"peer-agent": func(d, s *Options) {
			if s.PeerAgent != "" {
				d.PeerAgent = s.PeerAgent
			}
		},
		"seed-ratio": func(d, s *Options) {
			if s.SeedRatio != "" {
				d.SeedRatio = s.SeedRatio
			}
		},
		"seed-time": func(d, s *Options) {
			if s.SeedTime != "" {
				d.SeedTime = s.SeedTime
			}
		},
		"listen-port": func(d, s *Options) {
			if s.ListenPort != "" {
				d.ListenPort = s.ListenPort
			}
		},
		"torrent-file": func(d, s *Options) {
			if s.TorrentFile != "" {
				d.TorrentFile = s.TorrentFile
			}
		},
		"follow-torrent": func(d, s *Options) {
			if s.FollowTorrent != "" {
				d.FollowTorrent = s.FollowTorrent
			}
		},
		"select-file": func(d, s *Options) {
			if s.SelectFile != "" {
				d.SelectFile = s.SelectFile
			}
		},
		"show-files": func(d, s *Options) {
			if s.ShowFiles {
				d.ShowFiles = true
			}
		},
		"index-out": func(d, s *Options) {
			if len(s.IndexOut) > 0 {
				d.IndexOut = append(d.IndexOut, s.IndexOut...)
			}
		},
		"dscp": func(d, s *Options) {
			if s.DSCP != "" {
				d.DSCP = s.DSCP
			}
		},
		"enable-dht": func(d, s *Options) {
			if s.EnableDHT {
				d.EnableDHT = true
			}
		},
		"enable-dht6": func(d, s *Options) {
			if s.EnableDHT6 {
				d.EnableDHT6 = true
			}
		},
		"dht-listen-port": func(d, s *Options) {
			if s.DHTListenPort != "" {
				d.DHTListenPort = s.DHTListenPort
			}
		},
		"dht-listen-addr": func(d, s *Options) {
			if s.DHTListenAddr != "" {
				d.DHTListenAddr = s.DHTListenAddr
			}
		},
		"dht-listen-addr6": func(d, s *Options) {
			if s.DHTListenAddr6 != "" {
				d.DHTListenAddr6 = s.DHTListenAddr6
			}
		},
		"dht-entry-point-host": func(d, s *Options) {
			if s.DHTEntryPointHost != "" {
				d.DHTEntryPointHost = s.DHTEntryPointHost
			}
		},
		"dht-entry-point-port": func(d, s *Options) {
			if s.DHTEntryPointPort != "" {
				d.DHTEntryPointPort = s.DHTEntryPointPort
			}
		},
		"dht-entry-point": func(d, s *Options) {
			if len(s.DHTEntryPoint) > 0 {
				d.DHTEntryPoint = append(d.DHTEntryPoint, s.DHTEntryPoint...)
			}
		},
		"dht-entry-point-host6": func(d, s *Options) {
			if s.DHTEntryPointHost6 != "" {
				d.DHTEntryPointHost6 = s.DHTEntryPointHost6
			}
		},
		"dht-entry-point-port6": func(d, s *Options) {
			if s.DHTEntryPointPort6 != "" {
				d.DHTEntryPointPort6 = s.DHTEntryPointPort6
			}
		},
		"dht-entry-point6": func(d, s *Options) {
			if len(s.DHTEntryPoint6) > 0 {
				d.DHTEntryPoint6 = append(d.DHTEntryPoint6, s.DHTEntryPoint6...)
			}
		},
		"dht-file-path": func(d, s *Options) {
			if s.DHTFilePath != "" {
				d.DHTFilePath = s.DHTFilePath
			}
		},
		"dht-file-path6": func(d, s *Options) {
			if s.DHTFilePath6 != "" {
				d.DHTFilePath6 = s.DHTFilePath6
			}
		},
		"dht-message-timeout": func(d, s *Options) {
			if s.DHTMessageTimeout != "" {
				d.DHTMessageTimeout = s.DHTMessageTimeout
			}
		},
		"enable-peer-exchange": func(d, s *Options) {
			if s.EnablePeerExchange {
				d.EnablePeerExchange = true
			}
		},
		"follow-metalink": func(d, s *Options) {
			if s.FollowMetalink != "" {
				d.FollowMetalink = s.FollowMetalink
			}
		},
		"metalink-base-uri": func(d, s *Options) {
			if s.MetalinkBaseURI != "" {
				d.MetalinkBaseURI = s.MetalinkBaseURI
			}
		},
		"metalink-file": func(d, s *Options) {
			if s.MetalinkFile != "" {
				d.MetalinkFile = s.MetalinkFile
			}
		},
		"metalink-language": func(d, s *Options) {
			if s.MetalinkLanguage != "" {
				d.MetalinkLanguage = s.MetalinkLanguage
			}
		},
		"metalink-location": func(d, s *Options) {
			if s.MetalinkLocation != "" {
				d.MetalinkLocation = s.MetalinkLocation
			}
		},
		"metalink-os": func(d, s *Options) {
			if s.MetalinkOS != "" {
				d.MetalinkOS = s.MetalinkOS
			}
		},
		"metalink-version": func(d, s *Options) {
			if s.MetalinkVersion != "" {
				d.MetalinkVersion = s.MetalinkVersion
			}
		},
		"metalink-preferred-protocol": func(d, s *Options) {
			if s.MetalinkPreferredProtocol != "" {
				d.MetalinkPreferredProtocol = s.MetalinkPreferredProtocol
			}
		},
		"metalink-enable-unique-protocol": func(d, s *Options) {
			if s.MetalinkEnableUniqueProtocol {
				d.MetalinkEnableUniqueProtocol = true
			}
		},
		"enable-rpc": func(d, s *Options) {
			if s.EnableRPC {
				d.EnableRPC = true
			}
		},
		"rpc-listen-port": func(d, s *Options) {
			if s.RPCListenPort != 0 {
				d.RPCListenPort = s.RPCListenPort
			}
		},
		"rpc-listen-all": func(d, s *Options) {
			if s.RPCListenAll {
				d.RPCListenAll = true
			}
		},
		"rpc-allow-origin-all": func(d, s *Options) {
			if s.RPCAllowOriginAll {
				d.RPCAllowOriginAll = true
			}
		},
		"rpc-secret": func(d, s *Options) {
			if s.RPCSecret != "" {
				d.RPCSecret = s.RPCSecret
			}
		},
		"rpc-user": func(d, s *Options) {
			if s.RPCUser != "" {
				d.RPCUser = s.RPCUser
			}
		},
		"rpc-passwd": func(d, s *Options) {
			if s.RPCPasswd != "" {
				d.RPCPasswd = s.RPCPasswd
			}
		},
		"rpc-secure": func(d, s *Options) {
			if s.RPCSecure {
				d.RPCSecure = true
			}
		},
		"rpc-certificate": func(d, s *Options) {
			if s.RPCCertificate != "" {
				d.RPCCertificate = s.RPCCertificate
			}
		},
		"rpc-private-key": func(d, s *Options) {
			if s.RPCPrivateKey != "" {
				d.RPCPrivateKey = s.RPCPrivateKey
			}
		},
		"rpc-max-request-size": func(d, s *Options) {
			if s.RPCMaxRequestSize != "" {
				d.RPCMaxRequestSize = s.RPCMaxRequestSize
			}
		},
		"rpc-save-upload-metadata": func(d, s *Options) {
			if s.RPCSaveUploadMetadata {
				d.RPCSaveUploadMetadata = true
			}
		},
		"pause": func(d, s *Options) {
			if s.Pause {
				d.Pause = true
			}
		},
		"pause-metadata": func(d, s *Options) {
			if s.PauseMetadata {
				d.PauseMetadata = true
			}
		},
		"conf-path": func(d, s *Options) {
			if s.ConfPath != "" {
				d.ConfPath = s.ConfPath
			}
		},
		"no-conf": func(d, s *Options) {
			if s.NoConf {
				d.NoConf = true
			}
		},
		"allow-overwrite": func(d, s *Options) {
			if s.AllowOverwrite {
				d.AllowOverwrite = true
			}
		},
		"allow-piece-length-change": func(d, s *Options) {
			if s.AllowPieceLengthChange {
				d.AllowPieceLengthChange = true
			}
		},
		"always-resume": func(d, s *Options) {
			if s.AlwaysResumeSet {
				d.AlwaysResume = s.AlwaysResume
				d.AlwaysResumeSet = true
			} else if s.AlwaysResume {
				d.AlwaysResume = true
			}
		},
		"max-resume-failure-tries": func(d, s *Options) {
			if s.MaxResumeFailureTries != 0 {
				d.MaxResumeFailureTries = s.MaxResumeFailureTries
			}
		},
		"auto-file-renaming": func(d, s *Options) {
			if s.AutoFileRenaming {
				d.AutoFileRenaming = true
			}
		},
		"conditional-get": func(d, s *Options) {
			if s.ConditionalGet {
				d.ConditionalGet = true
			}
		},
		"select-least-used-host": func(d, s *Options) {
			if s.SelectLeastUsedHost {
				d.SelectLeastUsedHost = true
			}
		},
		"content-disposition-default-utf8": func(d, s *Options) {
			if s.ContentDispositionDefaultUTF8 {
				d.ContentDispositionDefaultUTF8 = true
			}
		},
		"disk-cache": func(d, s *Options) {
			if s.DiskCache != "" {
				d.DiskCache = s.DiskCache
			}
		},
		"file-allocation": func(d, s *Options) {
			if s.FileAllocation != "" {
				d.FileAllocation = s.FileAllocation
			}
		},
		"no-file-allocation-limit": func(d, s *Options) {
			if s.NoFileAllocationLimit != "" {
				d.NoFileAllocationLimit = s.NoFileAllocationLimit
			}
		},
		"enable-mmap": func(d, s *Options) {
			if s.EnableMmap {
				d.EnableMmap = true
			}
		},
		"max-mmap-limit": func(d, s *Options) {
			if s.MaxMmapLimit != "" {
				d.MaxMmapLimit = s.MaxMmapLimit
			}
		},
		"force-save": func(d, s *Options) {
			if s.ForceSave {
				d.ForceSave = true
			}
		},
		"save-not-found": func(d, s *Options) {
			if s.SaveNotFound {
				d.SaveNotFound = true
			}
		},
		"save-session": func(d, s *Options) {
			if s.SaveSession != "" {
				d.SaveSession = s.SaveSession
			}
		},
		"save-session-interval": func(d, s *Options) {
			if s.SaveSessionInterval != "" {
				d.SaveSessionInterval = s.SaveSessionInterval
			}
		},
		"auto-save-interval": func(d, s *Options) {
			if s.AutoSaveInterval != "" {
				d.AutoSaveInterval = s.AutoSaveInterval
			}
		},
		"startup-idle-time": func(d, s *Options) {
			if s.StartupIdleTime != "" {
				d.StartupIdleTime = s.StartupIdleTime
			}
		},
		"remove-control-file": func(d, s *Options) {
			if s.RemoveControlFile {
				d.RemoveControlFile = true
			}
		},
		"hash-check-only": func(d, s *Options) {
			if s.HashCheckOnly {
				d.HashCheckOnly = true
			}
		},
		"gid": func(d, s *Options) {
			if s.GID != "" {
				d.GID = s.GID
			}
		},
		"stop": func(d, s *Options) {
			if s.Stop != "" {
				d.Stop = s.Stop
			}
		},
		"stop-with-process": func(d, s *Options) {
			if s.StopWithProcess != 0 {
				d.StopWithProcess = s.StopWithProcess
			}
		},
		"interface": func(d, s *Options) {
			if s.Interface != "" {
				d.Interface = s.Interface
			}
		},
		"multiple-interface": func(d, s *Options) {
			if s.MultipleInterface != "" {
				d.MultipleInterface = s.MultipleInterface
			}
		},
		"disable-ipv6": func(d, s *Options) {
			if s.DisableIPv6 {
				d.DisableIPv6 = true
			}
		},
		"async-dns": func(d, s *Options) {
			if s.AsyncDNS {
				d.AsyncDNS = true
			}
		},
		"enable-async-dns6": func(d, s *Options) {
			if s.EnableAsyncDNS6 {
				d.EnableAsyncDNS6 = true
			}
		},
		"async-dns-server": func(d, s *Options) {
			if s.AsyncDNSServer != "" {
				d.AsyncDNSServer = s.AsyncDNSServer
			}
		},
		"min-tls-version": func(d, s *Options) {
			if s.MinTLSVersion != "" {
				d.MinTLSVersion = s.MinTLSVersion
			}
		},
		"event-poll": func(d, s *Options) {
			if s.EventPoll != "" {
				d.EventPoll = s.EventPoll
			}
		},
		"piece-length": func(d, s *Options) {
			if s.PieceLength != "" {
				d.PieceLength = s.PieceLength
			}
		},
		"socket-recv-buffer-size": func(d, s *Options) {
			if s.SocketRecvBufferSize != "" {
				d.SocketRecvBufferSize = s.SocketRecvBufferSize
			}
		},
		"rlimit-nofile": func(d, s *Options) {
			if s.RlimitNofile != "" {
				d.RlimitNofile = s.RlimitNofile
			}
		},
		"deferred-input": func(d, s *Options) {
			if s.DeferredInput {
				d.DeferredInput = true
			}
		},
		"max-download-result": func(d, s *Options) {
			if s.MaxDownloadResult != 0 {
				d.MaxDownloadResult = s.MaxDownloadResult
			}
		},
		"keep-unfinished-download-result": func(d, s *Options) {
			if s.KeepUnfinishedDownloadResult {
				d.KeepUnfinishedDownloadResult = true
			}
		},
		"enable-color": func(d, s *Options) {
			if s.EnableColor {
				d.EnableColor = true
			}
		},
		"on-download-start": func(d, s *Options) {
			if s.OnDownloadStart != "" {
				d.OnDownloadStart = s.OnDownloadStart
			}
		},
		"on-download-pause": func(d, s *Options) {
			if s.OnDownloadPause != "" {
				d.OnDownloadPause = s.OnDownloadPause
			}
		},
		"on-download-stop": func(d, s *Options) {
			if s.OnDownloadStop != "" {
				d.OnDownloadStop = s.OnDownloadStop
			}
		},
		"on-download-complete": func(d, s *Options) {
			if s.OnDownloadComplete != "" {
				d.OnDownloadComplete = s.OnDownloadComplete
			}
		},
		"on-download-error": func(d, s *Options) {
			if s.OnDownloadError != "" {
				d.OnDownloadError = s.OnDownloadError
			}
		},
		"on-bt-download-complete": func(d, s *Options) {
			if s.OnBTDownloadComplete != "" {
				d.OnBTDownloadComplete = s.OnBTDownloadComplete
			}
		},
	}
}

func parseBool(v string) (bool, error) {
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, &Error{Code: ErrInvalidOption, Msg: "must be either 'true' or 'false'"}
}
