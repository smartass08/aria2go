package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/engine"
	"github.com/smartass08/aria2go/internal/log"
	"github.com/smartass08/aria2go/internal/rpc/dispatcher"
	"github.com/smartass08/aria2go/internal/torrent"
)

const (
	version     = "1.37.0"
	packageName = "aria2"
	bannerLine  = packageName + " " + version
)

var (
	goCompilerLine string
	platformLine   string
)

const versionText = `aria2 version 1.37.0
Copyright aria2go contributors

Clean-room Go reimplementation of aria2-compatible behavior.
License: Apache-2.0

** Configuration **
Enabled Features: Async DNS, BitTorrent, Firefox3 Cookie, GZip,
  HTTPS, Message Digest, Metalink, XML-RPC, SFTP
Hash Algorithms: sha-1, sha-224, sha-256, sha-384, sha-512,
  md5, adler32
Libraries: Go standard library
`

const basicHelpText = `Usage: aria2go [OPTIONS] [URI | MAGNET | TORRENT_FILE | METALINK_FILE]...

Options:

 Basic Options
  -d, --dir=DIR                The directory to store the downloaded file.
  -i, --input-file=FILE        Downloads URIs found in FILE.
  -l, --log=LOG                The file name of the log file.
  -j, --max-concurrent-downloads=N  Set maximum number of parallel downloads.
  -V, --check-integrity[=true|false]  Check file integrity by validating piece hashes.
  -c, --continue[=true|false]  Continue downloading a partially downloaded file.
  -D, --daemon[=true|false]    Run as daemon.
  -s, --split=N                Download a file using N connections.
  -x, --max-connection-per-server=N  The maximum number of connections to one server.
  -k, --min-split-size=SIZE    Minimum split size.
  -u, --max-upload-limit=SPEED Set max upload speed.
  -o, --out=FILE               The file name of the downloaded file.
  -q, --quiet[=true|false]     Make aria2 quiet (no console output).
  -Z, --force-sequential[=true|false]  Fetch URIs in command-line order.
  -U, --user-agent=USER_AGENT  Set user agent for HTTP(S) downloads.
  -p, --ftp-pasv[=true|false]  Use passive mode in FTP.
  -n, --no-netrc[=true|false]  Disables netrc support.
  -t, --timeout=SEC            Set timeout in seconds.
  -m, --max-tries=N            Set maximum number of tries.
  -R, --remote-time[=true|false]  Retrieve timestamp from remote file.
  -P, --parameterized-uri[=true|false]  Enable parameterized URI support.
  -T, --torrent-file=TORRENT_FILE  The path to the .torrent file.
  -S, --show-files[=true|false]  Print file listing of .torrent or .metalink file and exit.
  -O, --index-out=INDEX=PATH   Set file path for file at given index.
  -M, --metalink-file=METALINK_FILE  The path to the .metalink file.

 RPC Options
  --enable-rpc[=true|false]    Enable JSON-RPC/XML-RPC server.
  --rpc-listen-port=PORT       Specify a port number for JSON-RPC/XML-RPC server.
  --rpc-secret=TOKEN           Set RPC secret authorization token.

URI, MAGNET, TORRENT_FILE, METALINK_FILE:
 You can specify multiple HTTP(S)/FTP URIs.
 You can also specify arbitrary number of BitTorrent Magnet URIs, torrent/
 metalink files stored in a local drive.

Refer to man page for more information.
`

func init() {
	goCompilerLine = "Compiler: " + runtime.Version() + " (" + runtime.Compiler + ")"
	platformLine = "Platform: " + runtime.GOOS + "/" + runtime.GOARCH
}

// rpcDispatchAdapter wraps *dispatcher.Dispatcher and implements engine.RPCBackend.
type rpcDispatchAdapter struct {
	d *dispatcher.Dispatcher
}

func (a *rpcDispatchAdapter) Call(ctx context.Context, token, method string, params []any) (any, error) {
	return a.d.Call(ctx, token, method, params)
}

func (a *rpcDispatchAdapter) SubscribeNotifications(sink func(name string, params map[string]any)) func() {
	return a.d.SubscribeNotifications(dispatcher.NotifySink(sink))
}

func main() {
	// Pre-scan for --help and --version (ParseArgs silently skips them).
	for _, a := range os.Args[1:] {
		switch {
		case a == "--help" || a == "-h":
			showHelp(os.Stdout, "")
			os.Exit(0)
		case strings.HasPrefix(a, "--help="):
			showHelp(os.Stdout, a[len("--help="):])
			os.Exit(0)
		case a == "--version" || a == "-v":
			showVersion(os.Stdout)
			os.Exit(0)
		}
	}

	exitCode := run()
	os.Exit(int(exitCode))
}

func run() core.ErrorCode {
	// 1. Parse CLI args (ParseArgs expects full argv including program name).
	cliOpts, positionals, err := config.ParseArgs(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aria2go: %v\n", err)
		return core.ExitBadOption
	}

	// 2. Merge in aria2 C++ order: defaults → config file → env → CLI.
	//    CLI is the highest priority layer (parsed last).
	defaults := config.Default()

	// 2a. Load config file unless --no-conf.
	var cfOpts *config.Options
	if !cliOpts.NoConf {
		cfpath := cliOpts.ConfPath
		if cfpath == "" {
			cfpath = findDefaultConfigPath()
		}
		if cfpath != "" {
			var loadErr error
			cfOpts, loadErr = loadConfigFile(cfpath)
			if loadErr != nil {
				if os.IsNotExist(loadErr) && cliOpts.ConfPath != "" {
					fmt.Fprintf(os.Stderr, "Configuration file %s is not found.\n", cfpath)
					showHelp(os.Stderr, "#help")
					return core.ExitUnknownError
				}
				// Missing default config is not fatal.
			}
		} else if cliOpts.ConfPath != "" {
			fmt.Fprintf(os.Stderr, "Configuration file %s is not found.\n", cliOpts.ConfPath)
			showHelp(os.Stderr, "#help")
			return core.ExitUnknownError
		}
	}

	// 2b. Merge: defaults → config file → env → CLI.
	opts := config.Merge(defaults, cfOpts)
	overrideEnv(opts)
	opts = config.Merge(opts, cliOpts)

	// 3. Validate merged options.
	if vErr := config.Validate(opts); vErr != nil {
		if ce, ok := vErr.(*config.Error); ok {
			fmt.Fprintf(os.Stderr, "aria2go: %v\n", ce)
			return core.ErrorCode(ce.Code)
		}
		fmt.Fprintf(os.Stderr, "aria2go: %v\n", vErr)
		return core.ExitBadOption
	}

	// 4. Set up logging.
	logger, logFile, logErr := setupLogger(opts)
	if logErr != nil {
		fmt.Fprintf(os.Stderr, "aria2go: failed to initialize logging: %v\n", logErr)
		return core.ExitUnknownError
	}
	if logFile != nil {
		defer logFile.Close()
	}

	logger.Info(bannerLine)
	logger.Info(goCompilerLine)
	logger.Info(platformLine)
	logger.Info("Logging started.")

	// 5. Set rlimit NOFILE if configured.
	setRLimitNOFILE(opts, logger)

	// 6. Daemon mode: fork to background, parent exits (matching aria2 C++).
	if opts.Daemon {
		if daemonize() != 0 {
			logger.Error("daemon failed")
			return core.ExitUnknownError
		}
		return core.ExitSuccess
	}

	// 7. Redirect stdout to stderr if --stderr is set.
	if opts.Stderr {
		os.Stdout = os.Stderr
	}

	// 8. Handle show-files for torrent/metalink (prints file info and exits).
	//    aria2 C++ handles both explicit --torrent-file/--metalink-file and
	//    positional args that look like torrent/metalink files.
	if opts.ShowFiles {
		if opts.TorrentFile != "" || opts.MetalinkFile != "" {
			showFileContents(opts.TorrentFile, opts.MetalinkFile, logger)
			return core.ExitSuccess
		}
		showPositionalFiles(positionals, logger)
		return core.ExitSuccess
	}

	// 9. Create engine.
	eng, err := engine.New(opts, logger)
	if err != nil {
		logger.Error("Failed to create engine", "error", err)
		fmt.Fprintf(os.Stderr, "aria2go: %v\n", err)
		return core.ExitUnknownError
	}

	// 9a. Wire RPC dispatcher if enabled (must be done before Run()).
	if opts.EnableRPC {
		eng.SetDispatcherFactory(func(e *engine.Engine, secret string) engine.RPCBackend {
			return &rpcDispatchAdapter{d: dispatcher.New(e, dispatcher.Config{Secret: secret})}
		})
	}

	// 10. Dry-run: log but still add downloads (they appear in tellStatus).
	//    The engine propagates DryRun through config.Merge; individual
	//    download workers skip I/O when DryRun is true.
	if opts.DryRun {
		logger.Info("Dry-run mode enabled. No files will be downloaded.")
	}

	// 11. Add downloads from CLI args (URIs, torrents, metalinks).
	downloadCount := 0
	if shouldAddStandalonePositionals(opts) {
		for _, uri := range positionals {
			added, addErr := addDownloadSource(eng, opts, uri)
			if addErr != nil {
				logger.Error("Failed to add download source", "uri", uri, "error", addErr)
			} else {
				downloadCount += added
			}
		}
	}
	if opts.TorrentFile != "" {
		if addErr := addTorrentFile(eng, opts, opts.TorrentFile, positionals...); addErr != nil {
			logger.Error("Failed to add torrent file", "path", opts.TorrentFile, "error", addErr)
		} else {
			downloadCount++
		}
	}
	if opts.MetalinkFile != "" {
		if addErr := addMetalinkFile(eng, opts, opts.MetalinkFile); addErr != nil {
			logger.Error("Failed to add metalink file", "path", opts.MetalinkFile, "error", addErr)
		} else {
			downloadCount++
		}
	}

	// 12. Handle input-file.
	//    aria2 C++ Context.cc:273-279: if --deferred-input is true, create a
	//    UriListParser that reads URIs incrementally from the input file. The
	//    parser is attached to the engine and consumed as downloads progress.
	//    TODO: implement UriListParser in internal/uri-list-parser/ and wire
	//    it into engine.Run(). For now, Engine.Run eagerly loads entries.
	if opts.InputFile != "" {
		if opts.DeferredInput && opts.SaveSession != "" {
			logger.Warn("--deferred-input is disabled because --save-session is set")
			opts.DeferredInput = false
		}
	}

	// 13. If no downloads and not RPC mode, error out.
	//    aria2 C++ checks: no URIs AND no torrent-file AND no metalink-file
	//    AND no input-file AND no RPC.
	//    In dry-run mode with no downloads, aria2 exits successfully.
	if downloadCount == 0 &&
		!opts.EnableRPC &&
		opts.TorrentFile == "" &&
		opts.MetalinkFile == "" &&
		opts.InputFile == "" {
		if opts.DryRun {
			return core.ExitSuccess
		}
		io.WriteString(os.Stderr, "Specify at least one URL.\n")
		showHelp(os.Stderr, "")
		return core.ExitUnknownError
	}

	if downloadCount > 0 {
		logger.Info("Downloading " + strconv.Itoa(downloadCount) + " item(s)")
	}

	// 14. Remove one-shot CLI-only options from the template so they
	//      don't affect RPC-created or dynamically-added downloads.
	//      Mirrors Context.cc:295-302.
	opts.Out = ""
	opts.ForceSequential = false
	opts.IndexOut = nil
	opts.SelectFile = ""
	opts.Pause = false
	opts.Checksum = ""
	opts.GID = ""

	// 15. Signal handling: SIGINT first → graceful halt, second → force.
	//     SIGTERM/SIGHUP → force immediately where the platform exposes them.
	if ignored := ignoredSignals(); len(ignored) > 0 {
		signal.Ignore(ignored...)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, shutdownSignals()...)

	go func() {
		haltCount := 0
		for sig := range sigCh {
			haltCount++
			forceShutdown := true
			if isInterruptSignal(sig) {
				if haltCount < 2 {
					forceShutdown = false
				}
			}
			logger.Info("Signal received",
				"signal", sig.String(),
				"signal_count", haltCount,
				"force", forceShutdown,
			)
			_ = eng.Shutdown(forceShutdown)
		}
	}()

	// 16. Run engine (blocks until shutdown).
	runErr := eng.Run(ctx)
	if runErr != nil {
		logger.Error("Engine run error", "error", runErr)
	}

	// 17. Save session on exit.
	if opts.SaveSession != "" {
		logger.Info("Saving session", "path", opts.SaveSession)
		if saveErr := eng.SaveSession(); saveErr != nil {
			logger.Error("Failed to save session", "path", opts.SaveSession, "error", saveErr)
		}
	}

	// 18. Compute exit code.
	return eng.ExitCode()
}

func showVersion(w io.Writer) {
	io.WriteString(w, versionText)
	io.WriteString(w, goCompilerLine+"\n")
	io.WriteString(w, "System: "+runtime.GOOS+" "+runtime.GOARCH+"\n")
	io.WriteString(w, "\nReport bugs to https://github.com/smartass08/aria2go/issues\n")
	io.WriteString(w, "Visit https://github.com/smartass08/aria2go\n")
}

var helpTags = map[string]string{
	"#basic":        "Basic",
	"#advanced":     "Advanced",
	"#http":         "HTTP",
	"#https":        "HTTPS",
	"#ftp":          "FTP",
	"#metalink":     "Metalink",
	"#bittorrent":   "BitTorrent",
	"#cookie":       "Cookie",
	"#hook":         "Hook",
	"#file":         "File",
	"#rpc":          "RPC",
	"#checksum":     "Checksum",
	"#experimental": "Experimental",
	"#deprecated":   "Deprecated",
	"#help":         "Help",
}

var allHelpOptions = []struct {
	name, desc string
	tags       []string
}{
	// Basic — version and help come first to match aria2go
	{"--version", "Print the version number and exit.", []string{"#basic"}},
	{"--help[=TAG|KEYWORD]", "Print usage and exit. The help messages are classified with tags. A tag starts with \"#\". For example, type \"--help=#http\" to get the usage for the options tagged with \"#http\". If non-tag word is given, print the usage for the options whose name includes that word.", []string{"#basic", "#help"}},
	{"--dir=DIR", "The directory to store the downloaded file.", []string{"#basic", "#file"}},
	{"--out=FILE", "The file name of the downloaded file.", []string{"#basic", "#file"}},
	{"--log=LOG", "The file name of the log file.", []string{"#basic", "#file"}},
	{"--daemon[=true|false]", "Run as daemon.", []string{"#basic"}},
	{"--split=N", "Download a file using N connections.", []string{"#basic"}},
	{"--max-connection-per-server=N", "The maximum number of connections to one server.", []string{"#basic", "#http", "#ftp"}},
	{"--min-split-size=SIZE", "aria2 does not split less than 2*SIZE byte range.", []string{"#basic", "#http", "#ftp"}},
	{"--retry-wait=SEC", "Set the seconds to wait between retries.", []string{"#basic"}},
	{"--timeout=SEC", "Set timeout in seconds.", []string{"#basic", "#http", "#ftp"}},
	{"--max-tries=N", "Set maximum number of tries. 0 means unlimited.", []string{"#basic", "#http", "#ftp"}},
	{"--input-file=FILE", "Downloads URIs found in FILE.", []string{"#basic", "#file"}},
	{"--log-level=LEVEL", "Set log level to output to file.", []string{"#basic"}},
	{"--console-log-level=LEVEL", "Set console log level.", []string{"#basic"}},
	{"--max-concurrent-downloads=N", "Set maximum number of parallel downloads.", []string{"#basic"}},
	{"--check-integrity[=true|false]", "Check file integrity.", []string{"#basic", "#checksum", "#bittorrent", "#metalink"}},
	{"--continue[=true|false]", "Continue downloading a partially downloaded file.", []string{"#basic"}},
	{"--user-agent=USER_AGENT", "Set user agent for HTTP(S) downloads.", []string{"#basic", "#http"}},
	{"--no-netrc[=true|false]", "Disables netrc support.", []string{"#basic", "#ftp"}},
	{"--quiet[=true|false]", "Make aria2 quiet (no console output).", []string{"#basic"}},
	{"--show-console-readout[=true|false]", "Enable console readout.", []string{"#basic"}},
	{"--truncate-console-readout[=true|false]", "Truncate console readout.", []string{"#basic"}},
	{"--human-readable[=true|false]", "Print sizes in human readable format.", []string{"#basic"}},
	{"--summary-interval=SEC", "Set download progress summary interval.", []string{"#basic"}},
	{"--download-result=OPT", "Configure output of download results.", []string{"#basic"}},
	{"--optimize-concurrent-downloads[=true|false|A:B]", "Optimize number of concurrent downloads.", []string{"#basic"}},
	{"--force-sequential[=true|false]", "Fetch URIs in command-line order.", []string{"#basic"}},
	{"--stderr[=true|false]", "Redirect stdout to stderr.", []string{"#basic"}},
	// HTTP
	{"--http-user=USER", "Set HTTP user.", []string{"#http"}},
	{"--http-passwd=PASSWD", "Set HTTP password.", []string{"#http"}},
	{"--referer=REFERER", "Set Referer header.", []string{"#http"}},
	{"--enable-http-keep-alive[=true|false]", "Enable HTTP persistent connections.", []string{"#http"}},
	{"--enable-http-pipelining[=true|false]", "Enable HTTP pipelining.", []string{"#http"}},
	{"--http-accept-gzip[=true|false]", "Accept gzip content encoding.", []string{"#http"}},
	{"--http-auth-challenge[=true|false]", "Send HTTP authorization header when challenged.", []string{"#http"}},
	{"--http-no-cache[=true|false]", "Send Cache-Control and Pragma headers.", []string{"#http"}},
	{"--no-want-digest-header[=true|false]", "Disable Want-Digest header.", []string{"#http"}},
	{"--use-head[=true|false]", "Use HEAD method for the first request.", []string{"#http"}},
	{"--header=HEADER", "Append HEADER to HTTP request header.", []string{"#http"}},
	{"--ca-certificate=FILE", "Use the CA certificate in FILE.", []string{"#http", "#https", "#ftp"}},
	{"--certificate=FILE", "Use the client certificate in FILE.", []string{"#http", "#https"}},
	{"--check-certificate[=true|false]", "Verify peer.", []string{"#http", "#https"}},
	{"--private-key=FILE", "Use the private key in FILE.", []string{"#http", "#https"}},
	{"--http-proxy=PROXY", "Use this proxy for HTTP.", []string{"#http"}},
	{"--http-proxy-user=USER", "Set user for HTTP proxy.", []string{"#http"}},
	{"--http-proxy-passwd=PASSWD", "Set password for HTTP proxy.", []string{"#http"}},
	{"--https-proxy=PROXY", "Use this proxy for HTTPS.", []string{"#https"}},
	{"--https-proxy-user=USER", "Set user for HTTPS proxy.", []string{"#https"}},
	{"--https-proxy-passwd=PASSWD", "Set password for HTTPS proxy.", []string{"#https"}},
	// FTP/SFTP
	{"--ftp-user=USER", "Set FTP user.", []string{"#ftp"}},
	{"--ftp-passwd=PASSWD", "Set FTP password.", []string{"#ftp"}},
	{"--ftp-pasv[=true|false]", "Use passive mode in FTP.", []string{"#ftp"}},
	{"--ftp-type=TYPE", "Set FTP transfer type.", []string{"#ftp"}},
	{"--ftp-reuse-connection[=true|false]", "Reuse connection in FTP.", []string{"#ftp"}},
	{"--ftp-proxy=PROXY", "Use this proxy for FTP.", []string{"#ftp"}},
	{"--ftp-proxy-user=USER", "Set user for FTP proxy.", []string{"#ftp"}},
	{"--ftp-proxy-passwd=PASSWD", "Set password for FTP proxy.", []string{"#ftp"}},
	{"--ssh-host-key-md=HASH", "Set checksum for SSH host key.", []string{"#ftp"}},
	// HTTP/FTP/SFTP Shared
	{"--connect-timeout=SEC", "Set the connect timeout.", []string{"#http", "#ftp"}},
	{"--max-file-not-found=N", "Stop if max file-not-found error count reached.", []string{"#http", "#ftp"}},
	{"--lowest-speed-limit=SPEED", "Close connection if speed is lower than this limit.", []string{"#http", "#ftp"}},
	{"--max-overall-download-limit=SPEED", "Set max overall download speed.", []string{"#http", "#ftp", "#bittorrent"}},
	{"--max-download-limit=SPEED", "Set max download speed per download.", []string{"#http", "#ftp", "#bittorrent"}},
	{"--remote-time[=true|false]", "Retrieve remote timestamps.", []string{"#http", "#ftp"}},
	{"--reuse-uri[=true|false]", "Reuse already used URIs.", []string{"#http", "#ftp"}},
	{"--uri-selector=SELECTOR", "Specify URI selection algorithm.", []string{"#http", "#ftp"}},
	{"--stream-piece-selector=SELECTOR", "Specify piece selection algorithm.", []string{"#http", "#ftp"}},
	{"--server-stat-of=FILE", "Save server performance profile.", []string{"#http", "#ftp"}},
	{"--server-stat-if=FILE", "Load server performance profile.", []string{"#http", "#ftp"}},
	{"--server-stat-timeout=SEC", "Timeout for server performance profile.", []string{"#http", "#ftp"}},
	{"--proxy-method=METHOD", "Set proxy request method.", []string{"#http", "#ftp"}},
	{"--all-proxy=PROXY", "Use this proxy for all protocols.", []string{"#http", "#ftp"}},
	{"--all-proxy-user=USER", "Set user for all proxy.", []string{"#http", "#ftp"}},
	{"--all-proxy-passwd=PASSWD", "Set password for all proxy.", []string{"#http", "#ftp"}},
	{"--no-proxy=DOMAINS", "Comma separated domains to bypass proxy.", []string{"#http", "#ftp"}},
	{"--dry-run[=true|false]", "Enable dry run mode.", []string{"#http", "#ftp"}},
	{"--parameterized-uri[=true|false]", "Enable parameterized URI support.", []string{"#basic", "#http", "#ftp"}},
	// Checksum
	{"--checksum=TYPE=DIGEST", "Set checksum.", []string{"#checksum"}},
	{"--realtime-chunk-checksum[=true|false]", "Validate chunk checksum during transfer.", []string{"#checksum"}},
	// BitTorrent
	{"--bt-metadata-only[=true|false]", "Download metadata only.", []string{"#bittorrent"}},
	{"--bt-save-metadata[=true|false]", "Save metadata as .torrent file.", []string{"#bittorrent"}},
	{"--bt-load-saved-metadata[=true|false]", "Load saved metadata from .torrent file.", []string{"#bittorrent"}},
	{"--bt-enable-lpd[=true|false]", "Enable Local Peer Discovery.", []string{"#bittorrent"}},
	{"--bt-lpd-interface=INTERFACE", "Use given interface for LPD.", []string{"#bittorrent"}},
	{"--bt-tracker=URI", "Add BitTorrent tracker URI.", []string{"#bittorrent"}},
	{"--bt-exclude-tracker=URI", "Remove listed BitTorrent trackers.", []string{"#bittorrent"}},
	{"--bt-tracker-connect-timeout=SEC", "Set tracker connect timeout.", []string{"#bittorrent"}},
	{"--bt-tracker-timeout=SEC", "Set tracker timeout.", []string{"#bittorrent"}},
	{"--bt-tracker-interval=SEC", "Set tracker request interval.", []string{"#bittorrent"}},
	{"--bt-max-peers=NUM", "Set maximum number of peers per torrent.", []string{"#bittorrent"}},
	{"--bt-request-peer-speed-limit=SPEED", "Set request peer speed limit.", []string{"#bittorrent"}},
	{"--bt-stop-timeout=SEC", "Stop BT download after timeout.", []string{"#bittorrent"}},
	{"--bt-prioritize-piece=head=SIZE,tail=SIZE", "Prioritize piece download.", []string{"#bittorrent"}},
	{"--bt-hash-check-seed[=true|false]", "Hash check after seeding completes.", []string{"#bittorrent"}},
	{"--bt-seed-unverified[=true|false]", "Seed even if hash check reports corrupted pieces.", []string{"#bittorrent"}},
	{"--bt-remove-unselected-file[=true|false]", "Remove unselected files.", []string{"#bittorrent"}},
	{"--bt-max-open-files=NUM", "Set maximum number of open files.", []string{"#bittorrent"}},
	{"--bt-detach-seed-only[=true|false]", "Detach seed only.", []string{"#bittorrent"}},
	{"--bt-enable-hook-after-hash-check[=true|false]", "Enable hook after hash check.", []string{"#bittorrent"}},
	{"--bt-force-encryption[=true|false]", "Require BitTorrent message encryption.", []string{"#bittorrent"}},
	{"--bt-require-crypto[=true|false]", "Require BitTorrent crypto handshake.", []string{"#bittorrent"}},
	{"--bt-min-crypto-level=LEVEL", "Set minimum crypto level.", []string{"#bittorrent"}},
	{"--bt-external-ip=IPADDR", "Specify the external IP address.", []string{"#bittorrent"}},
	{"--peer-id-prefix=PREFIX", "Specify the prefix of peer ID.", []string{"#bittorrent"}},
	{"--peer-agent=AGENT", "Set the client reported as Peer Agent.", []string{"#bittorrent"}},
	{"--seed-ratio=RATIO", "Specify share ratio.", []string{"#bittorrent"}},
	{"--seed-time=MINUTES", "Specify minimum seed time.", []string{"#bittorrent"}},
	{"--listen-port=PORT", "Set TCP port number for BitTorrent downloads.", []string{"#bittorrent"}},
	{"--max-overall-upload-limit=SPEED", "Set max overall upload speed.", []string{"#basic", "#bittorrent"}},
	{"--max-upload-limit=SPEED", "Set max upload speed per torrent.", []string{"#basic", "#bittorrent"}},
	{"--torrent-file=TORRENT_FILE", "The path to the .torrent file.", []string{"#bittorrent"}},
	{"--follow-torrent=true|false|mem", "Download files in the torrent.", []string{"#bittorrent"}},
	{"--select-file=INDEX", "Select file indexes.", []string{"#bittorrent"}},
	{"--show-files[=true|false]", "Print file listing and exit.", []string{"#bittorrent", "#metalink"}},
	{"--index-out=INDEX=PATH", "Set file path for file at given index.", []string{"#bittorrent", "#metalink"}},
	{"--dscp=DSCP", "Set DSCP value.", []string{"#bittorrent"}},
	{"--enable-dht[=true|false]", "Enable IPv4 DHT.", []string{"#bittorrent"}},
	{"--enable-dht6[=true|false]", "Enable IPv6 DHT.", []string{"#bittorrent"}},
	{"--dht-listen-port=PORT", "Set UDP port number for DHT.", []string{"#bittorrent"}},
	{"--dht-listen-addr6=ADDR", "Set IPv6 DHT listening address.", []string{"#bittorrent"}},
	{"--dht-entry-point=HOST:PORT", "Add a DHT entry point.", []string{"#bittorrent"}},
	{"--dht-entry-point6=HOST:PORT", "Add a IPv6 DHT entry point.", []string{"#bittorrent"}},
	{"--dht-file-path=PATH", "Set DHT routing table file.", []string{"#bittorrent"}},
	{"--dht-file-path6=PATH", "Set IPv6 DHT routing table file.", []string{"#bittorrent"}},
	{"--dht-message-timeout=SEC", "Set timeout for DHT messages.", []string{"#bittorrent"}},
	{"--enable-peer-exchange[=true|false]", "Enable Peer Exchange extension.", []string{"#bittorrent"}},
	// Metalink
	{"--follow-metalink=true|false|mem", "Follow metalink.", []string{"#metalink"}},
	{"--metalink-base-uri=URI", "Specify base URI for metalink.", []string{"#metalink"}},
	{"--metalink-file=METALINK_FILE", "The path to the .metalink file.", []string{"#metalink"}},
	{"--metalink-language=LANGUAGE", "Specify language of the file.", []string{"#metalink"}},
	{"--metalink-location=LOCATION", "Specify location of the file.", []string{"#metalink"}},
	{"--metalink-os=OS", "Specify operating system of the file.", []string{"#metalink"}},
	{"--metalink-version=VERSION", "Specify version of the file.", []string{"#metalink"}},
	{"--metalink-preferred-protocol=PROTO", "Specify preferred protocol.", []string{"#metalink"}},
	{"--metalink-enable-unique-protocol[=true|false]", "Enable unique protocol.", []string{"#metalink"}},
	// Cookie
	{"--load-cookies=FILE", "Load cookies from FILE.", []string{"#cookie", "#http"}},
	{"--save-cookies=FILE", "Save cookies to FILE.", []string{"#cookie", "#http"}},
	// RPC
	{"--enable-rpc[=true|false]", "Enable JSON-RPC/XML-RPC server.", []string{"#rpc"}},
	{"--rpc-listen-port=PORT", "Specify a port number for RPC server.", []string{"#rpc"}},
	{"--rpc-listen-all[=true|false]", "Listen on all network interfaces.", []string{"#rpc"}},
	{"--rpc-allow-origin-all[=true|false]", "Add Access-Control-Allow-Origin header.", []string{"#rpc"}},
	{"--rpc-secret=TOKEN", "Set RPC secret authorization token.", []string{"#rpc"}},
	{"--rpc-user=USER", "Set RPC user.", []string{"#rpc"}},
	{"--rpc-passwd=PASSWD", "Set RPC password.", []string{"#rpc"}},
	{"--rpc-secure[=true|false]", "Enable RPC over SSL/TLS.", []string{"#rpc"}},
	{"--rpc-certificate=FILE", "Use the certificate in FILE for RPC.", []string{"#rpc"}},
	{"--rpc-private-key=FILE", "Set the private key for RPC.", []string{"#rpc"}},
	{"--rpc-max-request-size=SIZE", "Set max size of RPC request.", []string{"#rpc"}},
	{"--rpc-save-upload-metadata[=true|false]", "Save upload metadata.", []string{"#rpc"}},
	{"--pause[=true|false]", "Pause download after added.", []string{"#rpc"}},
	{"--pause-metadata[=true|false]", "Pause metadata downloads.", []string{"#rpc"}},
	// Advanced
	{"--conf-path=PATH", "Change the configuration file path.", []string{"#advanced"}},
	{"--no-conf[=true|false]", "Disable loading configuration file.", []string{"#advanced"}},
	{"--allow-overwrite[=true|false]", "Allow overwrite.", []string{"#advanced", "#file"}},
	{"--allow-piece-length-change[=true|false]", "Allow piece length change.", []string{"#advanced"}},
	{"--always-resume[=true|false]", "Always resume download.", []string{"#advanced", "#file"}},
	{"--max-resume-failure-tries=N", "Set max resume failure tries.", []string{"#advanced", "#file"}},
	{"--auto-file-renaming[=true|false]", "Rename file if it already exists.", []string{"#advanced", "#file"}},
	{"--conditional-get[=true|false]", "Enable conditional get.", []string{"#advanced", "#http"}},
	{"--content-disposition-default-utf8[=true|false]", "Handle Content-Disposition as UTF-8.", []string{"#advanced", "#http"}},
	{"--disk-cache=SIZE", "Enable disk cache.", []string{"#advanced"}},
	{"--file-allocation=METHOD", "Specify file allocation method.", []string{"#advanced", "#file"}},
	{"--no-file-allocation-limit=SIZE", "Disable file allocation for small files.", []string{"#advanced", "#file"}},
	{"--enable-mmap[=true|false]", "Map files into memory.", []string{"#advanced", "#file"}},
	{"--max-mmap-limit=SIZE", "Set max file size to enable mmap.", []string{"#advanced", "#file"}},
	{"--force-save[=true|false]", "Save download even if --continue is false.", []string{"#advanced", "#file"}},
	{"--save-not-found[=true|false]", "Save download even if file not found.", []string{"#advanced", "#file"}},
	{"--save-session=FILE", "Save error/unfinished downloads to FILE.", []string{"#advanced", "#file"}},
	{"--save-session-interval=SEC", "Save session at interval seconds.", []string{"#advanced", "#file"}},
	{"--auto-save-interval=SEC", "Save control file at interval seconds.", []string{"#advanced", "#file"}},
	{"--remove-control-file[=true|false]", "Delete control file after download.", []string{"#advanced", "#file"}},
	{"--hash-check-only[=true|false]", "Check file hash only.", []string{"#advanced", "#checksum"}},
	{"--gid=GID", "Set GID manually.", []string{"#advanced"}},
	{"--stop=SEC", "Stop application after SEC seconds.", []string{"#advanced"}},
	{"--stop-with-process=PID", "Stop application when PID is not running.", []string{"#advanced"}},
	{"--interface=IFACE", "Bind to this network interface.", []string{"#advanced"}},
	{"--multiple-interface=IFACES", "Bind to these network interfaces.", []string{"#advanced"}},
	{"--disable-ipv6[=true|false]", "Disable IPv6.", []string{"#advanced"}},
	{"--async-dns[=true|false]", "Enable asynchronous DNS.", []string{"#advanced"}},
	{"--async-dns-server=ADDR", "Use this DNS server for async DNS.", []string{"#advanced"}},
	{"--enable-async-dns6[=true|false]", "Enable IPv6 name resolution in asynchronous DNS resolver.", []string{"#advanced", "#deprecated"}},
	{"--min-tls-version=VERSION", "Specify minimum TLS version.", []string{"#advanced", "#http"}},
	{"--event-poll=POLL", "Specify the event polling method.", []string{"#advanced"}},
	{"--piece-length=LENGTH", "Set a piece length.", []string{"#advanced"}},
	{"--socket-recv-buffer-size=SIZE", "Set socket receive buffer size.", []string{"#advanced"}},
	{"--rlimit-nofile=NUM", "Set open file descriptor limit.", []string{"#advanced"}},
	{"--deferred-input[=true|false]", "Use deferred input.", []string{"#advanced"}},
	{"--max-download-result=NUM", "Set max download result entries.", []string{"#advanced"}},
	{"--keep-unfinished-download-result[=true|false]", "Keep unfinished download results.", []string{"#advanced"}},
	{"--enable-color[=true|false]", "Enable color output.", []string{"#advanced"}},
	{"--netrc-path=FILE", "Specify the path to the netrc file.", []string{"#advanced", "#ftp"}},
	// Hook
	{"--on-download-start=COMMAND", "Run COMMAND on download start.", []string{"#hook"}},
	{"--on-download-pause=COMMAND", "Run COMMAND on download pause.", []string{"#hook"}},
	{"--on-download-stop=COMMAND", "Run COMMAND on download stop.", []string{"#hook"}},
	{"--on-download-complete=COMMAND", "Run COMMAND on download complete.", []string{"#hook"}},
	{"--on-download-error=COMMAND", "Run COMMAND on download error.", []string{"#hook"}},
	{"--on-bt-download-complete=COMMAND", "Run COMMAND on BT download complete.", []string{"#hook", "#bittorrent"}},
}

func showHelp(w io.Writer, keyword string) {
	if keyword == "" || keyword == "#basic" {
		showBasicHelp(w)
		return
	}
	if keyword == "#all" {
		io.WriteString(w, "Printing all options.\n\n")
		showAllOptions(w)
		return
	}
	if keyword == "#help" {
		showAvailableTags(w)
		return
	}
	if strings.HasPrefix(keyword, "#") {
		if _, ok := helpTags[keyword]; !ok {
			fmt.Fprintf(w, "Unknown tag '%s'.\n", keyword)
			io.WriteString(w, "See 'aria2go -h#help' to know all available tags.\n")
			return
		}
		fmt.Fprintf(w, "Printing options tagged with '%s'.\n", keyword)
		io.WriteString(w, "See 'aria2go -h#help' to know all available tags.\n")
		showTaggedOptions(w, keyword)
		return
	}
	fmt.Fprintf(w, "Printing options whose name includes '%s'.\n", keyword)
	io.WriteString(w, "\n")
	showMatchingOptions(w, keyword)
}

func showAvailableTags(w io.Writer) {
	io.WriteString(w, availableTagsText)
}

const availableTagsText = `Available tags:

  #basic           Basic
  #advanced        Advanced
  #http            HTTP
  #https           HTTPS
  #ftp             FTP
  #metalink        Metalink
  #bittorrent      BitTorrent
  #cookie          Cookie
  #hook            Hook
  #file            File
  #rpc             RPC
  #checksum        Checksum
  #experimental    Experimental
  #deprecated      Deprecated
  #help            Help

`

func showAllOptions(w io.Writer) {
	io.WriteString(w, "Options:\n\n")
	for _, opt := range allHelpOptions {
		fmt.Fprintf(w, "  %-42s %s\n", opt.name, opt.desc)
	}
	io.WriteString(w, "\nRefer to man page for more information.\n")
}

func showTaggedOptions(w io.Writer, tag string) {
	io.WriteString(w, "Options:\n\n")
	for _, opt := range allHelpOptions {
		for _, t := range opt.tags {
			if t == tag {
				fmt.Fprintf(w, "  %-42s %s\n", opt.name, opt.desc)
				break
			}
		}
	}
	io.WriteString(w, "\nRefer to man page for more information.\n")
}

func showMatchingOptions(w io.Writer, keyword string) {
	io.WriteString(w, "Options:\n\n")
	kw := strings.ToLower(keyword)
	for _, opt := range allHelpOptions {
		if strings.Contains(strings.ToLower(opt.name), kw) {
			fmt.Fprintf(w, "  %-42s %s\n", opt.name, opt.desc)
		}
	}
	io.WriteString(w, "\nRefer to man page for more information.\n")
}

func showBasicHelp(w io.Writer) {
	io.WriteString(w, basicHelpText)
}

func findDefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	defaultPath := filepath.Join(home, ".aria2", "aria2.conf")
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath
	}
	return ""
}

func loadConfigFile(path string) (*config.Options, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	opts := &config.Options{}
	if err := config.ParseConf(f, opts); err != nil {
		return nil, err
	}
	return opts, nil
}

func overrideEnv(opts *config.Options) {
	for envName := range envOverrides {
		val, ok := os.LookupEnv(envName)
		if !ok || val == "" {
			continue
		}
		switch envName {
		case "http_proxy":
			opts.HTTPProxy = val
		case "https_proxy":
			opts.HTTPSProxy = val
		case "ftp_proxy":
			opts.FTPProxy = val
		case "all_proxy":
			opts.AllProxy = val
		case "no_proxy":
			opts.NoProxy = val
		}
	}
}

var envOverrides = map[string]string{
	"http_proxy":  "http-proxy",
	"https_proxy": "https-proxy",
	"ftp_proxy":   "ftp-proxy",
	"all_proxy":   "all-proxy",
	"no_proxy":    "no-proxy",
}

type downloadAdder interface {
	Add(engine.AddSpec) (core.GID, error)
}

func addDownloadSource(adder downloadAdder, opts *config.Options, source string) (int, error) {
	switch {
	case guessTorrentFile(source):
		return 1, addTorrentFile(adder, opts, source)
	case guessMetalinkFile(source):
		return 1, addMetalinkFile(adder, opts, source)
	default:
		uris := []string{source}
		if opts != nil && opts.ParameterizedURI {
			expanded, err := config.ExpandParameterizedURI(source)
			if err != nil {
				return 0, err
			}
			uris = expanded
		}
		if len(uris) == 0 {
			return 0, nil
		}
		if opts != nil && opts.ForceSequential {
			for _, uri := range uris {
				if _, err := adder.Add(engine.AddSpec{URIs: []string{uri}, Options: opts}); err != nil {
					return 0, err
				}
			}
			return len(uris), nil
		}
		_, err := adder.Add(engine.AddSpec{URIs: uris, Options: opts})
		return 1, err
	}
}

func shouldAddStandalonePositionals(opts *config.Options) bool {
	if opts == nil {
		return true
	}
	return opts.TorrentFile == "" && opts.MetalinkFile == "" && opts.InputFile == ""
}

func addTorrentFile(adder downloadAdder, opts *config.Options, path string, webSeedURIs ...string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read torrent file %s: %w", path, err)
	}
	uris := append([]string(nil), webSeedURIs...)
	if opts != nil && opts.ParameterizedURI {
		uris, err = config.ExpandParameterizedURIs(uris)
		if err != nil {
			return err
		}
	}
	_, err = adder.Add(engine.AddSpec{URIs: uris, Torrent: data, Options: opts})
	return err
}

func addMetalinkFile(adder downloadAdder, opts *config.Options, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read metalink file %s: %w", path, err)
	}
	_, err = adder.Add(engine.AddSpec{Metalink: data, Options: opts})
	return err
}

// dualHandler writes log records to two handlers independently.
// Each handler has its own level filter; a record is dispatched to both
// handlers that are enabled for its level.
type dualHandler struct {
	file    slog.Handler
	console slog.Handler
}

func (h *dualHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.file.Enabled(ctx, level) || h.console.Enabled(ctx, level)
}

func (h *dualHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.file.Enabled(ctx, r.Level) {
		if err := h.file.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	if h.console.Enabled(ctx, r.Level) {
		if err := h.console.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (h *dualHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &dualHandler{
		file:    h.file.WithAttrs(attrs),
		console: h.console.WithAttrs(attrs),
	}
}

func (h *dualHandler) WithGroup(name string) slog.Handler {
	return &dualHandler{
		file:    h.file.WithGroup(name),
		console: h.console.WithGroup(name),
	}
}

func setupLogger(opts *config.Options) (*slog.Logger, *os.File, error) {
	fileLevel, err := log.ParseLevel(opts.LogLevel)
	if err != nil {
		fileLevel = log.LevelDebug
	}
	consoleLevel, err := log.ParseLevel(opts.ConsoleLogLevel)
	if err != nil {
		consoleLevel = log.LevelNotice
	}

	var fileWriter io.Writer
	var outFile *os.File
	if opts.Log != "" {
		f, err := os.OpenFile(opts.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot open log file %q: %w", opts.Log, err)
		}
		fileWriter = f
		outFile = f
	} else {
		fileWriter = io.Discard
	}

	fileHandler := log.NewClassicHandler(fileWriter, &slog.HandlerOptions{
		Level: fileLevel,
	})

	var consoleHandler slog.Handler
	if opts.Quiet {
		consoleHandler = log.NewClassicHandler(io.Discard, &slog.HandlerOptions{
			Level: log.LevelError + 1,
		})
	} else {
		consoleHandler = log.NewClassicHandler(os.Stderr, &slog.HandlerOptions{
			Level: consoleLevel,
		})
	}

	handler := &dualHandler{file: fileHandler, console: consoleHandler}
	logger := slog.New(handler)
	log.SetLogger(logger)
	return logger, outFile, nil
}

func showFileContents(torrentFile, metalinkFile string, logger *slog.Logger) {
	if torrentFile != "" {
		fmt.Fprintf(os.Stdout, ">>> Printing the contents of file '%s'...\n", torrentFile)
		showTorrentFile(torrentFile)
	}
	if metalinkFile != "" {
		fmt.Fprintf(os.Stdout, ">>> Printing the contents of file '%s'...\n", metalinkFile)
		showMetalinkFile(metalinkFile)
	}
}

func showPositionalFiles(positionals []string, logger *slog.Logger) {
	for _, uri := range positionals {
		fmt.Fprintf(os.Stdout, ">>> Printing the contents of file '%s'...\n", uri)
		switch {
		case guessTorrentFile(uri):
			showTorrentFile(uri)
		case guessMetalinkFile(uri):
			showMetalinkFile(uri)
		default:
			io.WriteString(os.Stderr, "This file is neither Torrent nor Metalink file.\n\n")
		}
	}
}

func guessTorrentFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [1]byte
	n, err := f.Read(head[:])
	return err == nil && n == 1 && head[0] == 'd'
}

func guessMetalinkFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [5]byte
	n, err := f.Read(head[:])
	return err == nil && n == 5 && string(head[:]) == "<?xml"
}

func showTorrentFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aria2go: torrent file display not yet implemented: %v\n", err)
		return
	}
	meta, err := torrent.Load(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aria2go: cannot parse torrent file: %v\n", err)
		return
	}
	printTorrentFileInfo(os.Stdout, meta)
}

func showMetalinkFile(path string) {
	io.WriteString(os.Stderr, "aria2go: metalink file display not yet implemented\n")
}

func printTorrentFileInfo(w io.Writer, meta *torrent.MetaInfo) {
	io.WriteString(w, "*** BitTorrent File Information ***\n")
	if meta.Comment != "" {
		fmt.Fprintf(w, "Comment: %s\n", meta.Comment)
	}
	if meta.CreationDate > 0 {
		tm := time.Unix(meta.CreationDate, 0).UTC()
		fmt.Fprintf(w, "Creation Date: %s\n", tm.Format("Mon, 02 Jan 2006 15:04:05 GMT"))
	}
	mode := "single"
	if len(meta.Info.Files) > 0 {
		mode = "multi"
	}
	fmt.Fprintf(w, "Mode: %s\n", mode)
	io.WriteString(w, "Announce:\n")
	for _, announce := range torrentAnnounceLines(meta) {
		fmt.Fprintf(w, " %s\n", announce)
	}
	infoHash, err := meta.InfoHash()
	if err == nil {
		fmt.Fprintf(w, "Info Hash: %x\n", infoHash[:])
	}
	fmt.Fprintf(w, "Piece Length: %sB\n", torrentPieceSize(meta.Info.PieceLength))
	fmt.Fprintf(w, "The Number of Pieces: %d\n", meta.NumPieces())
	fmt.Fprintf(w, "Total Length: %s (%s)\n", torrentDisplaySize(meta.TotalSize()), formatCommaInt(meta.TotalSize()))
	fmt.Fprintf(w, "Name: %s\n", meta.Info.Name)
	if err == nil {
		fmt.Fprintf(w, "Magnet URI: %s\n", torrentMagnetURI(meta, infoHash))
	}
	io.WriteString(w, "Files:\n")
	io.WriteString(w, "idx|path/length\n")
	io.WriteString(w, "===+===========================================================================\n")
	for i, file := range torrentDisplayFiles(meta) {
		fmt.Fprintf(w, "%3d|%s\n", i+1, file.path)
		fmt.Fprintf(w, "   |%s (%s)\n", torrentDisplaySize(file.length), formatCommaInt(file.length))
		io.WriteString(w, "---+---------------------------------------------------------------------------\n")
	}
}

func torrentAnnounceLines(meta *torrent.MetaInfo) []string {
	if len(meta.AnnounceList) > 0 {
		var lines []string
		for _, tier := range meta.AnnounceList {
			lines = append(lines, tier...)
		}
		return lines
	}
	if meta.Announce != "" {
		return []string{meta.Announce}
	}
	return nil
}

type torrentDisplayFile struct {
	path   string
	length int64
}

func torrentDisplayFiles(meta *torrent.MetaInfo) []torrentDisplayFile {
	if len(meta.Info.Files) == 0 {
		return []torrentDisplayFile{{
			path:   "./" + filepath.ToSlash(meta.Info.Name),
			length: meta.Info.Length,
		}}
	}
	files := make([]torrentDisplayFile, 0, len(meta.Info.Files))
	for _, file := range meta.Info.Files {
		parts := append([]string{meta.Info.Name}, file.Path...)
		files = append(files, torrentDisplayFile{
			path:   "./" + filepath.ToSlash(filepath.Join(parts...)),
			length: file.Length,
		})
	}
	return files
}

func torrentMagnetURI(meta *torrent.MetaInfo, infoHash [20]byte) string {
	hexHash := strings.ToUpper(fmt.Sprintf("%x", infoHash[:]))
	var b strings.Builder
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(hexHash)
	if meta.Info.Name != "" {
		b.WriteString("&dn=")
		b.WriteString(url.QueryEscape(meta.Info.Name))
	}
	for _, announce := range torrentAnnounceLines(meta) {
		b.WriteString("&tr=")
		b.WriteString(url.QueryEscape(announce))
	}
	return b.String()
}

func torrentDisplaySize(n int64) string {
	return torrentAbbrevSize(n, false) + "B"
}

func torrentPieceSize(n int64) string {
	return torrentAbbrevSize(n, true)
}

func torrentAbbrevSize(n int64, omitExactDecimal bool) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10)
	}
	units := []string{"Ki", "Mi", "Gi", "Ti"}
	value := float64(n)
	unitSize := int64(1)
	for _, unit := range units {
		value /= 1024
		unitSize *= 1024
		if value < 1024 {
			if omitExactDecimal && n%unitSize == 0 {
				return fmt.Sprintf("%d%s", n/unitSize, unit)
			}
			value = math.Trunc(value*10) / 10
			return fmt.Sprintf("%.1f%s", value, unit)
		}
	}
	value = math.Trunc(value*10) / 10
	return fmt.Sprintf("%.1fPi", value)
}

func formatCommaInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	prefix := len(s) % 3
	if prefix == 0 {
		prefix = 3
	}
	b.WriteString(s[:prefix])
	for i := prefix; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func readInputFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var uris []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		uris = append(uris, line)
	}
	return uris, nil
}
