package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Default returns an Options with all aria2 1.37.0 defaults populated.
func Default() *Options {
	return &Options{
		// Basic
		Dir:                               ".",
		InputFile:                         "",
		Log:                               "",
		MaxConcurrentDownloads:            5,
		CheckIntegrity:                    false,
		Continue:                          false,
		LogLevel:                          "debug",
		ConsoleLogLevel:                   "notice",
		Daemon:                            false,
		Split:                             5,
		MaxConnectionPerServer:            1,
		MinSplitSize:                      "20M",
		MaxOverallDownloadLimit:           "0",
		MaxDownloadLimit:                  "0",
		MaxOverallUploadLimit:             "0",
		MaxUploadLimit:                    "0",
		Out:                               "",
		Quiet:                             false,
		ShowConsoleReadout:                true,
		TruncateConsoleReadout:            true,
		HumanReadable:                     true,
		SummaryInterval:                   "60",
		DownloadResult:                    "default",
		OptimizeConcurrentDownloads:       "false",
		OptimizeConcurrentDownloadsCoeffA: "",
		OptimizeConcurrentDownloadsCoeffB: "",
		ForceSequential:                   false,
		Stderr:                            false,

		// HTTP
		HTTPUser:             "",
		HTTPPasswd:           "",
		UserAgent:            "aria2/1.37.0",
		Referer:              "",
		EnableHTTPKeepAlive:  true,
		EnableHTTPPipelining: false,
		HTTPAcceptGzip:       false,
		HTTPAuthChallenge:    false,
		HTTPNoCache:          false,
		NoWantDigestHeader:   false,
		UseHead:              false,
		MaxHTTPPipelining:    "2",
		Header:               nil,
		LoadCookies:          "",
		SaveCookies:          "",
		CACertificate:        "",
		Certificate:          "",
		CheckCertificate:     true,
		PrivateKey:           "",
		HTTPProxy:            "",
		HTTPProxyUser:        "",
		HTTPProxyPasswd:      "",
		HTTPSProxy:           "",
		HTTPSProxyUser:       "",
		HTTPSProxyPasswd:     "",

		// FTP/SFTP
		FTPUser:            "",
		FTPPasswd:          "",
		FTPPasv:            true,
		FTPType:            "binary",
		FTPReuseConnection: true,
		FTPProxy:           "",
		FTPProxyUser:       "",
		FTPProxyPasswd:     "",
		SSHHostKeyMD:       "",
		NetrcPath:          "$(HOME)/.netrc",
		NoNetrc:            false,

		// HTTP/FTP/SFTP Shared
		ConnectTimeout:      "60",
		DNSTimeout:          "30",
		Timeout:             "60",
		MaxTries:            5,
		RetryWait:           "0",
		MaxFileNotFound:     0,
		LowestSpeedLimit:    "0",
		RemoteTime:          false,
		ReuseURI:            true,
		URISelector:         "feedback",
		StreamPieceSelector: "default",
		ServerStatOf:        "",
		ServerStatIf:        "",
		ServerStatTimeout:   "86400",
		ProxyMethod:         "get",
		AllProxy:            "",
		AllProxyUser:        "",
		AllProxyPasswd:      "",
		NoProxy:             "",
		DryRun:              false,
		ParameterizedURI:    false,

		// Checksum
		Checksum:              "",
		RealtimeChunkChecksum: true,

		// BitTorrent
		BTMetadataOnly:             false,
		BTSaveMetadata:             false,
		BTLoadSavedMetadata:        false,
		BTEnableLPD:                false,
		BTLPDInterface:             "",
		BTTracker:                  nil,
		BTExcludeTracker:           nil,
		BTTrackerConnectTimeout:    "60",
		BTTrackerTimeout:           "60",
		BTTrackerInterval:          "0",
		BTMaxPeers:                 55,
		BTRequestPeerSpeedLimit:    "50K",
		BTStopTimeout:              "0",
		BTTimeout:                  "180",
		BTRequestTimeout:           "60",
		BTKeepAliveInterval:        "120",
		PeerConnectionTimeout:      "20",
		BTPrioritizePiece:          "",
		BTHashCheckSeed:            true,
		BTSeedUnverified:           false,
		BTRemoveUnselectedFile:     false,
		BTMaxOpenFiles:             100,
		BTDetachSeedOnly:           false,
		BTEnableHookAfterHashCheck: true,
		BTForceEncryption:          false,
		BTRequireCrypto:            false,
		BTMinCryptoLevel:           "plain",
		BTExternalIP:               "",
		PeerIDPrefix:               "A2-1-37-0-",
		PeerAgent:                  "aria2/1.37.0",
		SeedRatio:                  "1.0",
		SeedTime:                   "",
		ListenPort:                 "6881-6999",
		TorrentFile:                "",
		FollowTorrent:              "true",
		SelectFile:                 "",
		ShowFiles:                  false,
		IndexOut:                   nil,
		DSCP:                       "0",
		EnableDHT:                  true,
		EnableDHT6:                 false,
		DHTListenPort:              "6881-6999",
		DHTListenAddr:              "",
		DHTListenAddr6:             "",
		DHTEntryPoint:              nil,
		DHTEntryPoint6:             nil,
		DHTFilePath:                "",
		DHTFilePath6:               "",
		DHTMessageTimeout:          "10",
		EnablePeerExchange:         true,

		// Metalink
		FollowMetalink:               "true",
		MetalinkBaseURI:              "",
		MetalinkFile:                 "",
		MetalinkLanguage:             "",
		MetalinkLocation:             "",
		MetalinkOS:                   "",
		MetalinkVersion:              "",
		MetalinkPreferredProtocol:    "none",
		MetalinkEnableUniqueProtocol: true,

		// RPC
		EnableRPC:             false,
		RPCListenPort:         6800,
		RPCListenAll:          false,
		RPCAllowOriginAll:     false,
		RPCSecret:             "",
		RPCUser:               "",
		RPCPasswd:             "",
		RPCSecure:             false,
		RPCCertificate:        "",
		RPCPrivateKey:         "",
		RPCMaxRequestSize:     "2M",
		RPCSaveUploadMetadata: true,
		Pause:                 false,
		PauseMetadata:         false,

		// Advanced
		ConfPath:                      "",
		NoConf:                        false,
		AllowOverwrite:                false,
		AllowPieceLengthChange:        false,
		AlwaysResume:                  true,
		MaxResumeFailureTries:         0,
		AutoFileRenaming:              true,
		ConditionalGet:                false,
		SelectLeastUsedHost:           true,
		ContentDispositionDefaultUTF8: false,
		DiskCache:                     "16M",
		FileAllocation:                "prealloc",
		NoFileAllocationLimit:         "5M",
		EnableMmap:                    false,
		MaxMmapLimit:                  "9223372036854775807",
		ForceSave:                     false,
		SaveNotFound:                  true,
		SaveSession:                   "",
		SaveSessionInterval:           "0",
		AutoSaveInterval:              "60",
		StartupIdleTime:               "10",
		RemoveControlFile:             false,
		HashCheckOnly:                 false,
		GID:                           "",
		Stop:                          "0",
		StopWithProcess:               0,
		Interface:                     "",
		MultipleInterface:             "",
		DisableIPv6:                   false,
		AsyncDNS:                      true,
		EnableAsyncDNS6:               false,
		AsyncDNSServer:                "",
		MinTLSVersion:                 "TLSv1.2",
		EventPoll:                     "",
		PieceLength:                   "1M",
		SocketRecvBufferSize:          "0",
		RlimitNofile:                  "1024",
		DeferredInput:                 false,
		MaxDownloadResult:             1000,
		KeepUnfinishedDownloadResult:  true,
		EnableColor:                   true,
		OnDownloadStart:               "",
		OnDownloadPause:               "",
		OnDownloadStop:                "",
		OnDownloadComplete:            "",
		OnDownloadError:               "",
		OnBTDownloadComplete:          "",
	}
}

// validateEnum checks that val is one of the allowed values. Case-sensitive.
func validateEnum(val string, allowed []string) bool {
	for _, a := range allowed {
		if val == a {
			return true
		}
	}
	return false
}

// validateSize checks that val is a valid aria2 size string:
// a non-negative integer optionally suffixed with K/k or M/m.
func validateSize(val string) bool {
	if val == "" {
		return true
	}
	if val == "0" {
		return true
	}
	last := val[len(val)-1]
	if last == 'K' || last == 'k' || last == 'M' || last == 'm' {
		_, err := strconv.ParseUint(val[:len(val)-1], 10, 64)
		return err == nil
	}
	_, err := strconv.ParseUint(val, 10, 64)
	return err == nil
}

var validLogLevels = []string{"debug", "info", "notice", "warn", "error"}
var validFileAllocations = []string{"none", "prealloc", "falloc", "trunc"}

// Validate checks the Options for constraint violations.
// Returns nil if all values are valid.
func Validate(o *Options) error {
	if o == nil {
		return &Error{Code: ErrInvalidOption, Msg: "Options is nil"}
	}
	if o.Split < 1 {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("split must be >= 1, got %d", o.Split)}
	}
	if o.MaxConnectionPerServer < 1 {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("max-connection-per-server must be >= 1, got %d", o.MaxConnectionPerServer)}
	}
	if o.MaxConcurrentDownloads < 1 {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("max-concurrent-downloads must be >= 1, got %d", o.MaxConcurrentDownloads)}
	}
	if o.RPCListenPort != 0 && (o.RPCListenPort < 1024 || o.RPCListenPort > 65535) {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("rpc-listen-port must be 1024..65535, got %d", o.RPCListenPort)}
	}
	if o.ConsoleLogLevel != "" && !validateEnum(o.ConsoleLogLevel, validLogLevels) {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("console-log-level must be one of %s, got %q", strings.Join(validLogLevels, ","), o.ConsoleLogLevel)}
	}
	if o.LogLevel != "" && !validateEnum(o.LogLevel, validLogLevels) {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("log-level must be one of %s, got %q", strings.Join(validLogLevels, ","), o.LogLevel)}
	}
	if o.FileAllocation != "" && !validateEnum(o.FileAllocation, validFileAllocations) {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("file-allocation must be one of %s, got %q", strings.Join(validFileAllocations, ","), o.FileAllocation)}
	}
	if !validateSize(o.MinSplitSize) {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("min-split-size is not a valid size: %q", o.MinSplitSize)}
	}
	if o.MaxTries < 0 {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("max-tries must be >= 0, got %d", o.MaxTries)}
	}
	if o.MaxFileNotFound < 0 {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("max-file-not-found must be >= 0, got %d", o.MaxFileNotFound)}
	}
	if o.MaxResumeFailureTries < 0 {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("max-resume-failure-tries must be >= 0, got %d", o.MaxResumeFailureTries)}
	}
	if o.BTMaxPeers < 0 {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("bt-max-peers must be >= 0, got %d", o.BTMaxPeers)}
	}
	if o.BTMaxOpenFiles < 1 {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("bt-max-open-files must be >= 1, got %d", o.BTMaxOpenFiles)}
	}
	if o.MaxDownloadResult < 0 {
		return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("max-download-result must be >= 0, got %d", o.MaxDownloadResult)}
	}
	if o.OptimizeConcurrentDownloads != "" &&
		o.OptimizeConcurrentDownloads != "true" &&
		o.OptimizeConcurrentDownloads != "false" {
		if !isOptimizeABFormat(o.OptimizeConcurrentDownloads) {
			return &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf(
				"optimize-concurrent-downloads must be 'true', 'false', or 'A:B' where A and B are numeric, got %q",
				o.OptimizeConcurrentDownloads)}
		}
	}
	return nil
}

func isOptimizeABFormat(s string) bool {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	if _, err := strconv.ParseFloat(parts[0], 64); err != nil {
		return false
	}
	if _, err := strconv.ParseFloat(parts[1], 64); err != nil {
		return false
	}
	return true
}

// Error is the config package error type.
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// config error codes aligned with aria2 exit codes.
const (
	ErrInvalidOption = 28
)
