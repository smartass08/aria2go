package config

import (
	"testing"
)

func TestDefaultSetsAllRequired(t *testing.T) {
	o := Default()

	if o.Dir != "." {
		t.Errorf("Dir = %q, want %q", o.Dir, ".")
	}
	if o.MaxConcurrentDownloads != 5 {
		t.Errorf("MaxConcurrentDownloads = %d, want 5", o.MaxConcurrentDownloads)
	}
	if o.Split != 5 {
		t.Errorf("Split = %d, want 5", o.Split)
	}
	if o.MaxConnectionPerServer != 1 {
		t.Errorf("MaxConnectionPerServer = %d, want 1", o.MaxConnectionPerServer)
	}
	if o.MinSplitSize != "20M" {
		t.Errorf("MinSplitSize = %q, want %q", o.MinSplitSize, "20M")
	}
	if o.MaxOverallDownloadLimit != "0" {
		t.Errorf("MaxOverallDownloadLimit = %q, want %q", o.MaxOverallDownloadLimit, "0")
	}
	if o.MaxDownloadLimit != "0" {
		t.Errorf("MaxDownloadLimit = %q, want %q", o.MaxDownloadLimit, "0")
	}
	if o.MaxOverallUploadLimit != "0" {
		t.Errorf("MaxOverallUploadLimit = %q, want %q", o.MaxOverallUploadLimit, "0")
	}
	if o.MaxUploadLimit != "0" {
		t.Errorf("MaxUploadLimit = %q, want %q", o.MaxUploadLimit, "0")
	}
	if o.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", o.LogLevel, "debug")
	}
	if o.ConsoleLogLevel != "notice" {
		t.Errorf("ConsoleLogLevel = %q, want %q", o.ConsoleLogLevel, "notice")
	}
	if o.DownloadResult != "default" {
		t.Errorf("DownloadResult = %q, want %q", o.DownloadResult, "default")
	}
	if o.SummaryInterval != "60" {
		t.Errorf("SummaryInterval = %q, want %q", o.SummaryInterval, "60")
	}
	if o.FileAllocation != "prealloc" {
		t.Errorf("FileAllocation = %q, want %q", o.FileAllocation, "prealloc")
	}
	if o.DiskCache != "16M" {
		t.Errorf("DiskCache = %q, want %q", o.DiskCache, "16M")
	}
	if o.UserAgent != "aria2/1.37.0" {
		t.Errorf("UserAgent = %q, want %q", o.UserAgent, "aria2/1.37.0")
	}
	if o.RPCListenPort != 6800 {
		t.Errorf("RPCListenPort = %d, want 6800", o.RPCListenPort)
	}
	if o.BTMaxPeers != 55 {
		t.Errorf("BTMaxPeers = %d, want 55", o.BTMaxPeers)
	}
	if o.BTRequestPeerSpeedLimit != "50K" {
		t.Errorf("BTRequestPeerSpeedLimit = %q, want %q", o.BTRequestPeerSpeedLimit, "50K")
	}
	if o.BTMaxOpenFiles != 100 {
		t.Errorf("BTMaxOpenFiles = %d, want 100", o.BTMaxOpenFiles)
	}
	if o.BTMinCryptoLevel != "plain" {
		t.Errorf("BTMinCryptoLevel = %q, want %q", o.BTMinCryptoLevel, "plain")
	}
	if o.PeerIDPrefix != "A2-1-37-0-" {
		t.Errorf("PeerIDPrefix = %q, want %q", o.PeerIDPrefix, "A2-1-37-0-")
	}
	if o.PeerAgent != "aria2/1.37.0" {
		t.Errorf("PeerAgent = %q, want %q", o.PeerAgent, "aria2/1.37.0")
	}
	if o.SeedRatio != "1.0" {
		t.Errorf("SeedRatio = %q, want %q", o.SeedRatio, "1.0")
	}
	if o.ListenPort != "6881-6999" {
		t.Errorf("ListenPort = %q, want %q", o.ListenPort, "6881-6999")
	}
	if o.DHTListenPort != "6881-6999" {
		t.Errorf("DHTListenPort = %q, want %q", o.DHTListenPort, "6881-6999")
	}
	if o.DHTMessageTimeout != "10" {
		t.Errorf("DHTMessageTimeout = %q, want %q", o.DHTMessageTimeout, "10")
	}
	if o.ConnectTimeout != "60" {
		t.Errorf("ConnectTimeout = %q, want %q", o.ConnectTimeout, "60")
	}
	if o.Timeout != "60" {
		t.Errorf("Timeout = %q, want %q", o.Timeout, "60")
	}
	if o.MaxTries != 5 {
		t.Errorf("MaxTries = %d, want 5", o.MaxTries)
	}
	if o.ServerStatTimeout != "86400" {
		t.Errorf("ServerStatTimeout = %q, want %q", o.ServerStatTimeout, "86400")
	}
	if o.URISelector != "feedback" {
		t.Errorf("URISelector = %q, want %q", o.URISelector, "feedback")
	}
	if o.StreamPieceSelector != "default" {
		t.Errorf("StreamPieceSelector = %q, want %q", o.StreamPieceSelector, "default")
	}
	if o.ProxyMethod != "get" {
		t.Errorf("ProxyMethod = %q, want %q", o.ProxyMethod, "get")
	}
	if o.FTPUser != "" {
		t.Errorf("FTPUser = %q, want %q", o.FTPUser, "")
	}
	if o.FTPPasswd != "" {
		t.Errorf("FTPPasswd = %q, want %q", o.FTPPasswd, "")
	}
	if o.FTPType != "binary" {
		t.Errorf("FTPType = %q, want %q", o.FTPType, "binary")
	}
	if o.NetrcPath != "$(HOME)/.netrc" {
		t.Errorf("NetrcPath = %q, want %q", o.NetrcPath, "$(HOME)/.netrc")
	}
	if o.RPCMaxRequestSize != "2M" {
		t.Errorf("RPCMaxRequestSize = %q, want %q", o.RPCMaxRequestSize, "2M")
	}
	if o.MinTLSVersion != "TLSv1.2" {
		t.Errorf("MinTLSVersion = %q, want %q", o.MinTLSVersion, "TLSv1.2")
	}
	if o.PieceLength != "1M" {
		t.Errorf("PieceLength = %q, want %q", o.PieceLength, "1M")
	}
	if o.AutoSaveInterval != "60" {
		t.Errorf("AutoSaveInterval = %q, want %q", o.AutoSaveInterval, "60")
	}
	if o.MaxDownloadResult != 1000 {
		t.Errorf("MaxDownloadResult = %d, want 1000", o.MaxDownloadResult)
	}
	if o.NoFileAllocationLimit != "5M" {
		t.Errorf("NoFileAllocationLimit = %q, want %q", o.NoFileAllocationLimit, "5M")
	}
	if o.MetalinkPreferredProtocol != "none" {
		t.Errorf("MetalinkPreferredProtocol = %q, want %q", o.MetalinkPreferredProtocol, "none")
	}
	if o.DNSTimeout != "30" {
		t.Errorf("DNSTimeout = %q, want %q", o.DNSTimeout, "30")
	}
	if o.StartupIdleTime != "10" {
		t.Errorf("StartupIdleTime = %q, want %q", o.StartupIdleTime, "10")
	}
	if o.MaxHTTPPipelining != "2" {
		t.Errorf("MaxHTTPPipelining = %q, want %q", o.MaxHTTPPipelining, "2")
	}
	if !o.SelectLeastUsedHost {
		t.Error("SelectLeastUsedHost = false, want true")
	}
	if o.BTKeepAliveInterval != "120" {
		t.Errorf("BTKeepAliveInterval = %q, want %q", o.BTKeepAliveInterval, "120")
	}
	if o.BTTimeout != "180" {
		t.Errorf("BTTimeout = %q, want %q", o.BTTimeout, "180")
	}
	if o.BTRequestTimeout != "60" {
		t.Errorf("BTRequestTimeout = %q, want %q", o.BTRequestTimeout, "60")
	}
	if o.PeerConnectionTimeout != "20" {
		t.Errorf("PeerConnectionTimeout = %q, want %q", o.PeerConnectionTimeout, "20")
	}
	if o.DHTListenAddr != "" {
		t.Errorf("DHTListenAddr = %q, want %q", o.DHTListenAddr, "")
	}
	if o.OptimizeConcurrentDownloads != "false" {
		t.Errorf("OptimizeConcurrentDownloads = %q, want %q", o.OptimizeConcurrentDownloads, "false")
	}
	if o.SelectLeastUsedHost != true {
		t.Error("SelectLeastUsedHost = false, want true")
	}
	if o.EnableAsyncDNS6 != false {
		t.Error("EnableAsyncDNS6 = true, want false")
	}
}

func TestDefaultBooleans(t *testing.T) {
	o := Default()

	checkTrue := map[string]bool{
		"ShowConsoleReadout":           o.ShowConsoleReadout,
		"TruncateConsoleReadout":       o.TruncateConsoleReadout,
		"HumanReadable":                o.HumanReadable,
		"EnableHTTPKeepAlive":          o.EnableHTTPKeepAlive,
		"CheckCertificate":             o.CheckCertificate,
		"FTPPasv":                      o.FTPPasv,
		"FTPReuseConnection":           o.FTPReuseConnection,
		"ReuseURI":                     o.ReuseURI,
		"RealtimeChunkChecksum":        o.RealtimeChunkChecksum,
		"BTHashCheckSeed":              o.BTHashCheckSeed,
		"BTEnableHookAfterHashCheck":   o.BTEnableHookAfterHashCheck,
		"EnableDHT":                    o.EnableDHT,
		"EnablePeerExchange":           o.EnablePeerExchange,
		"MetalinkEnableUniqueProtocol": o.MetalinkEnableUniqueProtocol,
		"RPCSaveUploadMetadata":        o.RPCSaveUploadMetadata,
		"AlwaysResume":                 o.AlwaysResume,
		"AutoFileRenaming":             o.AutoFileRenaming,
		"SaveNotFound":                 o.SaveNotFound,
		"AsyncDNS":                     o.AsyncDNS,
		"KeepUnfinishedDownloadResult": o.KeepUnfinishedDownloadResult,
		"EnableColor":                  o.EnableColor,
		"SelectLeastUsedHost":          o.SelectLeastUsedHost,
	}
	for name, v := range checkTrue {
		if !v {
			t.Errorf("%s = false, want true", name)
		}
	}

	checkFalse := map[string]bool{
		"CheckIntegrity":                o.CheckIntegrity,
		"Continue":                      o.Continue,
		"Daemon":                        o.Daemon,
		"Quiet":                         o.Quiet,
		"ForceSequential":               o.ForceSequential,
		"Stderr":                        o.Stderr,
		"EnableHTTPPipelining":          o.EnableHTTPPipelining,
		"HTTPAcceptGzip":                o.HTTPAcceptGzip,
		"HTTPAuthChallenge":             o.HTTPAuthChallenge,
		"HTTPNoCache":                   o.HTTPNoCache,
		"NoWantDigestHeader":            o.NoWantDigestHeader,
		"UseHead":                       o.UseHead,
		"NoNetrc":                       o.NoNetrc,
		"RemoteTime":                    o.RemoteTime,
		"DryRun":                        o.DryRun,
		"ParameterizedURI":              o.ParameterizedURI,
		"BTMetadataOnly":                o.BTMetadataOnly,
		"BTSaveMetadata":                o.BTSaveMetadata,
		"BTLoadSavedMetadata":           o.BTLoadSavedMetadata,
		"BTEnableLPD":                   o.BTEnableLPD,
		"BTSeedUnverified":              o.BTSeedUnverified,
		"BTRemoveUnselectedFile":        o.BTRemoveUnselectedFile,
		"BTDetachSeedOnly":              o.BTDetachSeedOnly,
		"BTForceEncryption":             o.BTForceEncryption,
		"BTRequireCrypto":               o.BTRequireCrypto,
		"ShowFiles":                     o.ShowFiles,
		"EnableRPC":                     o.EnableRPC,
		"RPCListenAll":                  o.RPCListenAll,
		"RPCAllowOriginAll":             o.RPCAllowOriginAll,
		"RPCSecure":                     o.RPCSecure,
		"Pause":                         o.Pause,
		"PauseMetadata":                 o.PauseMetadata,
		"NoConf":                        o.NoConf,
		"AllowOverwrite":                o.AllowOverwrite,
		"AllowPieceLengthChange":        o.AllowPieceLengthChange,
		"ConditionalGet":                o.ConditionalGet,
		"ContentDispositionDefaultUTF8": o.ContentDispositionDefaultUTF8,
		"EnableMmap":                    o.EnableMmap,
		"ForceSave":                     o.ForceSave,
		"RemoveControlFile":             o.RemoveControlFile,
		"HashCheckOnly":                 o.HashCheckOnly,
		"DisableIPv6":                   o.DisableIPv6,
		"DeferredInput":                 o.DeferredInput,
		"EnableDHT6":                    o.EnableDHT6,
		"EnableAsyncDNS6":               o.EnableAsyncDNS6,
	}
	for name, v := range checkFalse {
		if v {
			t.Errorf("%s = true, want false", name)
		}
	}
}

func TestDefaultStringEmpties(t *testing.T) {
	o := Default()

	emptyStrings := map[string]string{
		"InputFile":            o.InputFile,
		"Log":                  o.Log,
		"Out":                  o.Out,
		"HTTPUser":             o.HTTPUser,
		"HTTPPasswd":           o.HTTPPasswd,
		"Referer":              o.Referer,
		"LoadCookies":          o.LoadCookies,
		"SaveCookies":          o.SaveCookies,
		"CACertificate":        o.CACertificate,
		"Certificate":          o.Certificate,
		"PrivateKey":           o.PrivateKey,
		"HTTPProxy":            o.HTTPProxy,
		"HTTPProxyUser":        o.HTTPProxyUser,
		"HTTPProxyPasswd":      o.HTTPProxyPasswd,
		"HTTPSProxy":           o.HTTPSProxy,
		"HTTPSProxyUser":       o.HTTPSProxyUser,
		"HTTPSProxyPasswd":     o.HTTPSProxyPasswd,
		"FTPProxy":             o.FTPProxy,
		"FTPProxyUser":         o.FTPProxyUser,
		"FTPProxyPasswd":       o.FTPProxyPasswd,
		"SSHHostKeyMD":         o.SSHHostKeyMD,
		"ServerStatOf":         o.ServerStatOf,
		"ServerStatIf":         o.ServerStatIf,
		"AllProxy":             o.AllProxy,
		"AllProxyUser":         o.AllProxyUser,
		"AllProxyPasswd":       o.AllProxyPasswd,
		"NoProxy":              o.NoProxy,
		"Checksum":             o.Checksum,
		"BTExternalIP":         o.BTExternalIP,
		"BTLPDInterface":       o.BTLPDInterface,
		"BTPrioritizePiece":    o.BTPrioritizePiece,
		"TorrentFile":          o.TorrentFile,
		"SelectFile":           o.SelectFile,
		"MetalinkBaseURI":      o.MetalinkBaseURI,
		"MetalinkFile":         o.MetalinkFile,
		"MetalinkLanguage":     o.MetalinkLanguage,
		"MetalinkLocation":     o.MetalinkLocation,
		"MetalinkOS":           o.MetalinkOS,
		"MetalinkVersion":      o.MetalinkVersion,
		"RPCSecret":            o.RPCSecret,
		"RPCUser":              o.RPCUser,
		"RPCPasswd":            o.RPCPasswd,
		"RPCCertificate":       o.RPCCertificate,
		"RPCPrivateKey":        o.RPCPrivateKey,
		"ConfPath":             o.ConfPath,
		"SaveSession":          o.SaveSession,
		"GID":                  o.GID,
		"Interface":            o.Interface,
		"MultipleInterface":    o.MultipleInterface,
		"AsyncDNSServer":       o.AsyncDNSServer,
		"EventPoll":            o.EventPoll,
		"OnDownloadStart":      o.OnDownloadStart,
		"OnDownloadPause":      o.OnDownloadPause,
		"OnDownloadStop":       o.OnDownloadStop,
		"OnDownloadComplete":   o.OnDownloadComplete,
		"OnDownloadError":      o.OnDownloadError,
		"OnBTDownloadComplete": o.OnBTDownloadComplete,
		"DHTListenAddr":        o.DHTListenAddr,
	}
	for name, v := range emptyStrings {
		if v != "" {
			t.Errorf("%s = %q, want empty string", name, v)
		}
	}
}

func TestValidateDefaultsPass(t *testing.T) {
	o := Default()
	if err := Validate(o); err != nil {
		t.Errorf("Validate(Default()) returned error: %v", err)
	}
}

func TestValidateNil(t *testing.T) {
	err := Validate(nil)
	if err == nil {
		t.Fatal("Validate(nil) returned nil, want error")
	}
	cfgErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error is %T, want *config.Error", err)
	}
	if cfgErr.Code != ErrInvalidOption {
		t.Errorf("error code = %d, want %d", cfgErr.Code, ErrInvalidOption)
	}
}

func TestValidateSplitZero(t *testing.T) {
	o := Default()
	o.Split = 0
	err := Validate(o)
	if err == nil {
		t.Fatal("Validate with Split=0 returned nil, want error")
	}
	cfgErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error is %T, want *config.Error", err)
	}
	if cfgErr.Code != ErrInvalidOption {
		t.Errorf("error code = %d, want %d", cfgErr.Code, ErrInvalidOption)
	}
}

func TestValidateSplitNegative(t *testing.T) {
	o := Default()
	o.Split = -1
	err := Validate(o)
	if err == nil {
		t.Fatal("Validate with Split=-1 returned nil, want error")
	}
}

func TestValidateMaxConnectionPerServerZero(t *testing.T) {
	o := Default()
	o.MaxConnectionPerServer = 0
	err := Validate(o)
	if err == nil {
		t.Fatal("Validate with MaxConnectionPerServer=0 returned nil, want error")
	}
}

func TestValidateMaxConnectionPerServerNegative(t *testing.T) {
	o := Default()
	o.MaxConnectionPerServer = -1
	err := Validate(o)
	if err == nil {
		t.Fatal("Validate with MaxConnectionPerServer=-1 returned nil, want error")
	}
}

func TestValidateMaxConcurrentDownloadsZero(t *testing.T) {
	o := Default()
	o.MaxConcurrentDownloads = 0
	err := Validate(o)
	if err == nil {
		t.Fatal("Validate with MaxConcurrentDownloads=0 returned nil, want error")
	}
}

func TestValidateRPCListenPortTooLow(t *testing.T) {
	o := Default()
	o.RPCListenPort = 1023
	err := Validate(o)
	if err == nil {
		t.Fatal("Validate with RPCListenPort=1023 returned nil, want error")
	}
}

func TestValidateRPCListenPortTooHigh(t *testing.T) {
	o := Default()
	o.RPCListenPort = 65536
	err := Validate(o)
	if err == nil {
		t.Fatal("Validate with RPCListenPort=65536 returned nil, want error")
	}
}

func TestValidateRPCListenPortZeroIsOK(t *testing.T) {
	o := Default()
	o.RPCListenPort = 0
	if err := Validate(o); err != nil {
		t.Errorf("Validate with RPCListenPort=0 returned error: %v", err)
	}
}

func TestValidateRPCListenPortValid(t *testing.T) {
	o := Default()
	o.RPCListenPort = 8080
	if err := Validate(o); err != nil {
		t.Errorf("Validate with RPCListenPort=8080 returned error: %v", err)
	}
}

func TestValidateLogLevelInvalid(t *testing.T) {
	o := Default()
	o.LogLevel = "critical"
	err := Validate(o)
	if err == nil {
		t.Fatal("Validate with LogLevel=critical returned nil, want error")
	}
}

func TestValidateLogLevelValid(t *testing.T) {
	for _, lvl := range []string{"debug", "info", "notice", "warn", "error"} {
		o := Default()
		o.LogLevel = lvl
		if err := Validate(o); err != nil {
			t.Errorf("Validate with LogLevel=%q returned error: %v", lvl, err)
		}
	}
}

func TestValidateConsoleLogLevelInvalid(t *testing.T) {
	o := Default()
	o.ConsoleLogLevel = "trace"
	err := Validate(o)
	if err == nil {
		t.Fatal("Validate with ConsoleLogLevel=trace returned nil, want error")
	}
}

func TestValidateConsoleLogLevelValid(t *testing.T) {
	for _, lvl := range []string{"debug", "info", "notice", "warn", "error"} {
		o := Default()
		o.ConsoleLogLevel = lvl
		if err := Validate(o); err != nil {
			t.Errorf("Validate with ConsoleLogLevel=%q returned error: %v", lvl, err)
		}
	}
}

func TestValidateFileAllocationInvalid(t *testing.T) {
	o := Default()
	o.FileAllocation = "malloc"
	err := Validate(o)
	if err == nil {
		t.Fatal("Validate with FileAllocation=malloc returned nil, want error")
	}
}

func TestValidateFileAllocationValid(t *testing.T) {
	for _, fa := range []string{"none", "prealloc", "falloc", "trunc"} {
		o := Default()
		o.FileAllocation = fa
		if err := Validate(o); err != nil {
			t.Errorf("Validate with FileAllocation=%q returned error: %v", fa, err)
		}
	}
}

func TestValidateMinSplitSizeValid(t *testing.T) {
	valid := []string{"1M", "20M", "1024M", "1K", "512k", "1024", "0", "20m", "10M"}
	for _, s := range valid {
		o := Default()
		o.MinSplitSize = s
		if err := Validate(o); err != nil {
			t.Errorf("Validate with MinSplitSize=%q returned error: %v", s, err)
		}
	}
}

func TestValidateMinSplitSizeInvalid(t *testing.T) {
	invalid := []string{"abc", "20G", "1.5M", "-1", "M", "K"}
	for _, s := range invalid {
		o := Default()
		o.MinSplitSize = s
		if err := Validate(o); err == nil {
			t.Errorf("Validate with MinSplitSize=%q returned nil, want error", s)
		}
	}
}

func TestValidateNegativeIntegerFields(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Options)
	}{
		{"MaxTries", func(o *Options) { o.MaxTries = -1 }},
		{"MaxFileNotFound", func(o *Options) { o.MaxFileNotFound = -1 }},
		{"MaxResumeFailureTries", func(o *Options) { o.MaxResumeFailureTries = -1 }},
		{"BTMaxPeers", func(o *Options) { o.BTMaxPeers = -1 }},
		{"BTMaxOpenFiles", func(o *Options) { o.BTMaxOpenFiles = -1 }},
		{"MaxDownloadResult", func(o *Options) { o.MaxDownloadResult = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := Default()
			tt.setup(o)
			if err := Validate(o); err == nil {
				t.Errorf("Validate with %s=-1 returned nil, want error", tt.name)
			}
		})
	}
}

func TestValidateEmptyStringEnumPasses(t *testing.T) {
	o := Default()
	o.LogLevel = ""
	o.ConsoleLogLevel = ""
	o.FileAllocation = ""
	if err := Validate(o); err != nil {
		t.Errorf("Validate with empty enum strings returned error: %v", err)
	}
}

func TestErrorType(t *testing.T) {
	e := &Error{Code: ErrInvalidOption, Msg: "test message"}
	if e.Error() != "test message" {
		t.Errorf("Error() = %q, want %q", e.Error(), "test message")
	}
}

func TestValidateOptimizeConcurrentDownloadsValid(t *testing.T) {
	valid := []string{"", "true", "false", "1.0:10.0", "0.5:5", "2:8", "1.5:20.0"}
	for _, s := range valid {
		o := Default()
		o.OptimizeConcurrentDownloads = s
		if err := Validate(o); err != nil {
			t.Errorf("Validate with optimize-concurrent-downloads=%q returned error: %v", s, err)
		}
	}
}

func TestValidateOptimizeConcurrentDownloadsInvalid(t *testing.T) {
	invalid := []string{"yes", "no", "A:B", ":10", "1:", "abc:def", "1x:2"}
	for _, s := range invalid {
		o := Default()
		o.OptimizeConcurrentDownloads = s
		if err := Validate(o); err == nil {
			t.Errorf("Validate with optimize-concurrent-downloads=%q returned nil, want error", s)
		}
	}
}

func TestValidateSummaryInterval(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{value: "0", valid: true},
		{value: "60", valid: true},
		{value: "-1", valid: false},
		{value: "nope", valid: false},
	}
	for _, tt := range tests {
		o := Default()
		o.SummaryInterval = tt.value
		err := Validate(o)
		if tt.valid && err != nil {
			t.Fatalf("Validate summary-interval=%q: %v", tt.value, err)
		}
		if !tt.valid && err == nil {
			t.Fatalf("Validate summary-interval=%q returned nil, want error", tt.value)
		}
	}
}

func TestValidateStartupIdleTime(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{value: "1", valid: true},
		{value: "10", valid: true},
		{value: "60", valid: true},
		{value: "0", valid: false},
		{value: "61", valid: false},
		{value: "nope", valid: false},
	}
	for _, tt := range tests {
		o := Default()
		o.StartupIdleTime = tt.value
		err := Validate(o)
		if tt.valid && err != nil {
			t.Fatalf("Validate startup-idle-time=%q: %v", tt.value, err)
		}
		if !tt.valid && err == nil {
			t.Fatalf("Validate startup-idle-time=%q returned nil, want error", tt.value)
		}
	}
}

func TestValidateEventPoll(t *testing.T) {
	for _, value := range validEventPollValues() {
		o := Default()
		o.EventPoll = value
		if err := Validate(o); err != nil {
			t.Fatalf("Validate event-poll=%q: %v", value, err)
		}
	}

	o := Default()
	o.EventPoll = "bogus"
	if err := Validate(o); err == nil {
		t.Fatal("Validate event-poll=bogus returned nil, want error")
	}
}
