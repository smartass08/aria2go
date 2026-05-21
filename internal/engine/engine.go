package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/console"
	"github.com/smartass08/aria2go/internal/cookies"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/dht"
	"github.com/smartass08/aria2go/internal/disk"
	"github.com/smartass08/aria2go/internal/hash"
	"github.com/smartass08/aria2go/internal/hookrunner"
	"github.com/smartass08/aria2go/internal/lpd"
	"github.com/smartass08/aria2go/internal/magnet"
	"github.com/smartass08/aria2go/internal/netrc"
	"github.com/smartass08/aria2go/internal/netx"
	"github.com/smartass08/aria2go/internal/portmap"
	btprogress "github.com/smartass08/aria2go/internal/protocol/bittorrent/progress"
	ftpproto "github.com/smartass08/aria2go/internal/protocol/ftp"
	httpproto "github.com/smartass08/aria2go/internal/protocol/http"
	"github.com/smartass08/aria2go/internal/protocol/metalink"
	sftpproto "github.com/smartass08/aria2go/internal/protocol/sftp"
	rpc_transport "github.com/smartass08/aria2go/internal/rpc/transport"
	"github.com/smartass08/aria2go/internal/sessionfile"
	"github.com/smartass08/aria2go/internal/tlsx"
)

// ShutdownDelay is the delay before shutdown actually executes, matching
// aria2's TimedHaltCommand 3-second delay to give clients time to receive
// the RPC response.
const ShutdownDelay = 3 * time.Second

// numGIDShards is the number of shards for the GID → requestGroup map.
// Must be a power of two for fast modulo.
const numGIDShards = 64

// requestGroup represents a single download in the engine's lifecycle.
// It mirrors aria2's RequestGroup class state machine but uses Go contexts
// for cancellation instead of the command/event-poll pattern.
type requestGroup struct {
	gid          core.GID
	opts         *config.Options
	uris         []string
	torrent      []byte
	metalinkData []byte
	state        core.Status

	created       time.Time
	errCode       core.ErrorCode
	errMsg        string
	pauseReq      bool
	restartReq    bool
	forceHaltReq  bool
	haltRequested bool

	belongsTo  core.GID
	following  core.GID
	followedBy []core.GID

	seeder bool

	pendingOpts *config.Options

	filePath          string
	inMemory          bool
	controlPath       string
	controlLoaded     bool
	controlInfo       *btprogress.Info
	controlPieceBytes []int64
	adaptor           disk.Adaptor
	controlMu         sync.Mutex
	probed            bool
	probedSize        int64
	acceptsRanges     bool
	cdFilename        string
	lastModified      time.Time

	completedLength int64
	totalLength     int64
	numConnections  int
	numSeeders      int
	fileName        string
	bytesDownloaded int64
	bytesUploaded   int64
	lastSpeedSample time.Time

	downloadLimit *Throttle

	filePathFromURI bool

	ctx    context.Context
	cancel context.CancelFunc

	downloadSpeed int64
	uploadSpeed   int64
}

// gidShard holds a portion of the GID → requestGroup map with its own mutex.
type gidShard struct {
	mu     sync.Mutex
	groups map[core.GID]*requestGroup
}

// gidShardMap provides sharded access to requestGroup lookups by GID.
type gidShardMap struct {
	shards [numGIDShards]gidShard
}

func newGIDShardMap(capacity int) *gidShardMap {
	m := &gidShardMap{}
	perShard := capacity / numGIDShards
	if perShard < 1 {
		perShard = 1
	}
	for i := range m.shards {
		m.shards[i].groups = make(map[core.GID]*requestGroup, perShard)
	}
	return m
}

func (m *gidShardMap) shard(gid core.GID) *gidShard {
	return &m.shards[uint64(gid)&(numGIDShards-1)]
}

func (m *gidShardMap) get(gid core.GID) (*requestGroup, bool) {
	s := m.shard(gid)
	s.mu.Lock()
	rg, ok := s.groups[gid]
	s.mu.Unlock()
	return rg, ok
}

func (m *gidShardMap) set(gid core.GID, rg *requestGroup) {
	s := m.shard(gid)
	s.mu.Lock()
	s.groups[gid] = rg
	s.mu.Unlock()
}

func (m *gidShardMap) delete(gid core.GID) {
	s := m.shard(gid)
	s.mu.Lock()
	delete(s.groups, gid)
	s.mu.Unlock()
}

func (m *gidShardMap) getLocked(gid core.GID) (*requestGroup, bool) {
	s := m.shard(gid)
	s.mu.Lock()
	rg, ok := s.groups[gid]
	if !ok {
		s.mu.Unlock()
	}
	return rg, ok
}

func (m *gidShardMap) unlock(gid core.GID) {
	m.shard(gid).mu.Unlock()
}

// Subscriber receives engine lifecycle events. Implementations must not block.
type Subscriber interface {
	OnEvent(ev core.Event)
}

// drPool reuses downloadResult allocations to reduce GC pressure.
var drPool = sync.Pool{
	New: func() any {
		return &downloadResult{}
	},
}

// Engine is the central download orchestrator, matching aria2's DownloadEngine
// architecture. It manages the lifecycle of all downloads (RequestGroups),
// assigns GIDs via random generation (matching aria2's GroupId::create),
// and dispatches lifecycle events via a pub/sub event bus.
//
// In aria2's C++ implementation the engine uses an event-poll + command
// pattern. In Go the same semantics are achieved with goroutines + context
// cancellation: each active download runs in a goroutine that watches its
// context for cancellation, and the engine's queue management goroutine
// moves downloads between waiting/active/stopped queues.
type Engine struct {
	cfg *config.Options
	log *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	groups *gidShardMap

	queuesMu  sync.Mutex
	active    []core.GID
	waiting   []core.GID
	queueWake chan struct{}

	usedGIDsMu sync.Mutex
	usedGIDs   map[core.GID]struct{}

	stoppedRing  *stoppedRing
	stoppedTotal atomic.Int64

	removedErrors  atomic.Int64
	removedLastErr atomic.Int64

	bus *EventBus

	subMu      sync.RWMutex
	subs       map[any]Subscriber
	dispatcher Subscriber

	sessionID string
	created   time.Time

	running      atomic.Bool
	shuttingDown atomic.Bool
	haltReq      atomic.Bool
	keepRunning  bool

	httpDriver  *httpproto.Driver
	netDialer   *netx.Dialer
	cookieJar   *cookies.Jar
	authFactory *AuthConfigFactory

	rpcServer   *rpc_transport.Server
	RPCBackend  RPCBackend
	dhtServer   *dht.Server
	portMapper  *portmap.Mapper
	console     *console.Console
	lpdListener *lpd.Listener
	btSession   *BtSession

	rateGlobal   *Throttle
	rateGlobalUp *Throttle

	saveInterval        time.Duration
	saveSessionInterval time.Duration
	lastSessionHash     [20]byte
	hasSessionHash      bool

	downloadSpeed atomic.Int64
	uploadSpeed   atomic.Int64

	newDispatcher func(e *Engine, secret string) RPCBackend
}

// AddSpec describes a download to be added to the engine.
type AddSpec struct {
	URIs        []string
	Options     *config.Options
	Torrent     []byte
	Metalink    []byte
	Position    int
	PositionSet bool
	BelongsTo   core.GID
}

// SubscribeResult holds the result of subscribing to engine events.
type SubscribeResult struct {
	C           <-chan core.Event
	Unsubscribe func()
}

// Status is the public status snapshot of a single download, matching
// aria2's aria2.tellStatus RPC response shape.
type Status struct {
	GID                    core.GID       `json:"gid"`
	Status                 core.Status    `json:"status"`
	TotalLength            int64          `json:"totalLength"`
	CompletedLength        int64          `json:"completedLength"`
	UploadLength           int64          `json:"uploadLength"`
	DownloadSpeed          int64          `json:"downloadSpeed"`
	UploadSpeed            int64          `json:"uploadSpeed"`
	InfoHash               string         `json:"infoHash,omitempty"`
	NumSeeders             int64          `json:"numSeeders,string"`
	Connections            int            `json:"connections"`
	ErrorCode              core.ErrorCode `json:"errorCode,string"`
	ErrorMessage           string         `json:"errorMessage,omitempty"`
	FollowedBy             []core.GID     `json:"followedBy,omitempty"`
	BelongsTo              core.GID       `json:"belongsTo,omitempty"`
	Following              core.GID       `json:"following,omitempty"`
	Dir                    string         `json:"dir"`
	Files                  []FileStatus   `json:"files"`
	Seeder                 bool           `json:"seeder,string"`
	Bittorrent             map[string]any `json:"bittorrent,omitempty"`
	VerifiedLength         int64          `json:"verifiedLength"`
	VerifyIntegrityPending bool           `json:"verifyIntegrityPending,string"`
	Bitfield               string         `json:"bitfield,omitempty"`
	PieceLength            int64          `json:"pieceLength"`
	NumPieces              int64          `json:"numPieces"`
}

// FileStatus describes a file within a multi-file download.
type FileStatus struct {
	Index           int         `json:"index,string"`
	Path            string      `json:"path"`
	Length          int64       `json:"length,string"`
	CompletedLength int64       `json:"completedLength,string"`
	Selected        bool        `json:"selected,string"`
	URIs            []URIStatus `json:"uris"`
}

// URIStatus describes the status of a single URI for a file.
type URIStatus struct {
	URI    string `json:"uri"`
	Status string `json:"status"`
}

// downloadResult holds the final state of a completed/error/removed download,
// mirroring aria2's DownloadResult / downloadResults_ queue.
type downloadResult struct {
	gid        core.GID
	state      core.Status
	errCode    core.ErrorCode
	errMsg     string
	belongsTo  core.GID
	following  core.GID
	followedBy []core.GID
	opts       *config.Options

	filePath              string
	totalLength           int64
	completedLength       int64
	sessionDownloadLength int64
	sessionTime           time.Duration
}

// New creates a new Engine with the given configuration and logger.
// The engine does not start any goroutines until Run is called.
func New(cfg *config.Options, log *slog.Logger) (*Engine, error) {
	if cfg == nil {
		return nil, fmt.Errorf("engine: config is nil")
	}
	if log == nil {
		return nil, fmt.Errorf("engine: logger is nil")
	}

	maxDownloads := cfg.MaxConcurrentDownloads
	if maxDownloads <= 0 {
		maxDownloads = 5
	}
	maxResults := cfg.MaxDownloadResult
	if maxResults <= 0 {
		maxResults = 1000
	}

	dialer, err := netx.NewDialer(engineDialerConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("engine: dialer: %w", err)
	}

	httpTLS, err := httpClientTLSConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("engine: http tls: %w", err)
	}

	cookieJar := newHTTPCookieJar(cfg, log)
	acceptEncoding := ""
	if cfg.HTTPAcceptGzip {
		acceptEncoding = "deflate, gzip"
	}

	httpDriver := httpproto.NewDriver(httpproto.Opts{
		Dialer:            dialer,
		TLS:               httpTLS,
		Jar:               httpCookieJar(cookieJar),
		Timeout:           time.Duration(parseInt(cfg.Timeout)) * time.Second,
		UserAgent:         cfg.UserAgent,
		Header:            cfg.Header,
		CheckCertificate:  &cfg.CheckCertificate,
		MaxRedirs:         20,
		AcceptEncoding:    acceptEncoding,
		Referer:           cfg.Referer,
		HTTPUser:          cfg.HTTPUser,
		HTTPPasswd:        cfg.HTTPPasswd,
		HTTPAuthChallenge: cfg.HTTPAuthChallenge,
		DisableKeepAlive:  !cfg.EnableHTTPKeepAlive,
		NoCache:           &cfg.HTTPNoCache,
		EnableWantDigest:  boolPtr(!cfg.NoWantDigestHeader),
		UseHead:           cfg.UseHead,
		DryRun:            cfg.DryRun,
	})
	authFactory := NewAuthConfigFactory()
	if !cfg.NoNetrc {
		entries, defaultEntry := loadNetrcForConfig(cfg, log)
		authFactory.SetNetrc(entries, defaultEntry)
	}

	e := &Engine{
		cfg:          cfg,
		log:          log,
		groups:       newGIDShardMap(maxDownloads * 2),
		active:       make([]core.GID, 0, maxDownloads),
		waiting:      make([]core.GID, 0, maxDownloads),
		queueWake:    make(chan struct{}, 1),
		usedGIDs:     make(map[core.GID]struct{}),
		stoppedRing:  newStoppedRing(maxResults),
		bus:          NewEventBus(),
		sessionID:    newSessionID(),
		created:      time.Now(),
		keepRunning:  cfg.EnableRPC,
		httpDriver:   httpDriver,
		netDialer:    dialer,
		cookieJar:    cookieJar,
		authFactory:  authFactory,
		rateGlobal:   NewThrottle(parseSize(cfg.MaxOverallDownloadLimit)),
		rateGlobalUp: NewThrottle(parseSize(cfg.MaxOverallUploadLimit)),
		btSession:    NewBtSession(cfg),
	}

	if cfg.BTEnableLPD {
		lpdL, lpdErr := lpd.NewListener()
		if lpdErr != nil {
			log.Warn("lpd init failed, continuing without LPD", "error", lpdErr)
		} else {
			e.lpdListener = lpdL
		}
	}

	if cfg.AutoSaveInterval != "" {
		e.saveInterval = time.Duration(parseInt(cfg.AutoSaveInterval)) * time.Second
	}
	if e.saveInterval <= 0 {
		e.saveInterval = 0
	}

	if interval := cfg.SaveSessionInterval; interval != "" {
		e.saveSessionInterval = time.Duration(parseInt(interval)) * time.Second
	}
	if e.saveSessionInterval <= 0 {
		e.saveSessionInterval = 0
	}

	dhtPort := parseDHTPort(cfg.DHTListenPort)
	listenPort := parseListenPort(cfg.ListenPort)

	if cfg.EnableDHT {
		dhtSrv, err := dht.NewServer(engineDHTConfig(dhtPort, cfg))
		if err != nil {
			return nil, fmt.Errorf("engine: dht server: %w", err)
		}
		e.dhtServer = dhtSrv
	}

	pm, err := portmap.New(portmap.Config{
		InternalPort: listenPort,
		ExternalPort: listenPort,
		Protocols:    []string{"tcp"},
	})
	if err != nil {
		log.Warn("portmap init failed, continuing without port mapping", "error", err)
	} else {
		e.portMapper = pm
	}

	e.console = console.New(engineConsoleOptions(cfg))

	e.removedLastErr.Store(int64(core.ExitSuccess))
	return e, nil
}

func engineConsoleOptions(cfg *config.Options) console.Options {
	return console.Options{
		Quiet:       cfg.Quiet,
		Summary:     cfg.SummaryInterval != "",
		Interactive: !cfg.Quiet,
		Stderr:      cfg.Stderr,
	}
}

func engineDialerConfig(cfg *config.Options) netx.DialerConfig {
	return netx.DialerConfig{
		Timeout:              time.Duration(parseInt(cfg.ConnectTimeout)) * time.Second,
		KeepAlive:            30 * time.Second,
		Interface:            cfg.Interface,
		PreferIPv4:           !cfg.DisableIPv6,
		ProxyURL:             resolveProxyURI("http", cfg),
		SocketRecvBufferSize: parseInt(cfg.SocketRecvBufferSize),
		DSCP:                 parseInt(cfg.DSCP),
		Interfaces:           cfg.MultipleInterface,
		NoProxy:              cfg.NoProxy,
	}
}

func httpClientTLSConfig(cfg *config.Options) (*tls.Config, error) {
	minVersion, err := tlsx.TLSVersion(cfg.MinTLSVersion)
	if err != nil {
		return nil, err
	}
	var caCerts [][]byte
	if cfg.CACertificate != "" {
		ca, err := os.ReadFile(cfg.CACertificate)
		if err != nil {
			return nil, fmt.Errorf("read ca-certificate: %w", err)
		}
		caCerts = append(caCerts, ca)
	}
	var clientCert []byte
	if cfg.Certificate != "" {
		clientCert, err = os.ReadFile(cfg.Certificate)
		if err != nil {
			return nil, fmt.Errorf("read certificate: %w", err)
		}
	}
	var clientKey []byte
	if cfg.PrivateKey != "" {
		clientKey, err = os.ReadFile(cfg.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("read private-key: %w", err)
		}
	}
	return tlsx.ClientConfig(tlsx.ClientOpts{
		CACerts:    caCerts,
		ClientCert: clientCert,
		ClientKey:  clientKey,
		MinVersion: minVersion,
	})
}

func newHTTPCookieJar(cfg *config.Options, log *slog.Logger) *cookies.Jar {
	if cfg.LoadCookies == "" && cfg.SaveCookies == "" {
		return nil
	}
	jar := cookies.New()
	if cfg.LoadCookies == "" {
		return jar
	}
	f, err := os.Open(cfg.LoadCookies)
	if err != nil {
		log.Error("failed to load cookies", "path", cfg.LoadCookies, "error", err)
		return jar
	}
	defer f.Close()
	if err := jar.LoadNetscape(f); err != nil {
		log.Error("failed to parse cookies", "path", cfg.LoadCookies, "error", err)
		return jar
	}
	log.Info("loaded cookies", "path", cfg.LoadCookies)
	return jar
}

func httpCookieJar(jar *cookies.Jar) http.CookieJar {
	if jar == nil {
		return nil
	}
	return jar
}

func loadNetrcForConfig(cfg *config.Options, log *slog.Logger) ([]netrc.Entry, *netrc.DefaultEntry) {
	path := expandHomePath(cfg.NetrcPath)
	if path == "" {
		return nil, nil
	}
	st, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn("netrc stat failed", "path", path, "error", err)
		}
		return nil, nil
	}
	if !st.Mode().IsRegular() {
		return nil, nil
	}
	if st.Mode().Perm()&0o077 != 0 {
		log.Warn("netrc ignored due to permissions", "path", path)
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		log.Warn("netrc open failed", "path", path, "error", err)
		return nil, nil
	}
	defer f.Close()
	entries, def, err := netrc.Parse(f)
	if err != nil {
		log.Warn("netrc parse failed", "path", path, "error", err)
		return nil, nil
	}
	return entries, def
}

func expandHomePath(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	switch {
	case path == "$(HOME)":
		return home
	case strings.HasPrefix(path, "$(HOME)/"), strings.HasPrefix(path, "$(HOME)\\"):
		return filepath.Join(home, path[len("$(HOME)/"):])
	default:
		return path
	}
}

func engineDHTConfig(port int, cfg *config.Options) dht.Config {
	return dht.Config{
		Addr:      fmt.Sprintf(":%d", port),
		PersistTo: cfg.DHTFilePath,
	}
}

// SetDispatcherFactory sets the factory function used to create an RPC dispatcher
// backend when EnableRPC is true. Must be called before Run().
// The factory receives the engine and RPC secret, and returns an RPCBackend
// which bridges to the transport layer. This indirection avoids an import cycle
// between engine and rpc/dispatcher.
func (e *Engine) SetDispatcherFactory(fn func(e *Engine, secret string) RPCBackend) {
	e.newDispatcher = fn
}

// newSessionID generates a random 20-byte session identifier (matching
// aria2's DownloadEngine constructor which calls generateRandomKey for 20 bytes).
func newSessionID() string {
	var b [20]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("engine: failed to generate session ID: %v", err))
	}
	return fmt.Sprintf("%x", b[:])
}

// nextGID generates a random uint64 GID, retrying if zero or already used.
// Matches aria2's GroupId::create() behavior exactly — GIDs are random,
// not monotonic. Used GIDs are tracked in e.usedGIDs (cleared via ClearGIDs
// for tests, or retained for session-scoped uniqueness).
func (e *Engine) nextGID() core.GID {
	e.usedGIDsMu.Lock()
	defer e.usedGIDsMu.Unlock()
	return e.nextGIDLocked()
}

func (e *Engine) nextGIDLocked() core.GID {
	var buf [8]byte
	for {
		if _, err := rand.Read(buf[:]); err != nil {
			panic(fmt.Sprintf("engine: failed to generate GID: %v", err))
		}
		n := core.GID(binary.BigEndian.Uint64(buf[:]))
		if n == 0 {
			continue
		}
		if _, used := e.usedGIDs[n]; !used {
			e.usedGIDs[n] = struct{}{}
			return n
		}
	}
}

// ClearGIDs resets the used GID set. Only for testing.
func (e *Engine) ClearGIDs() {
	e.usedGIDsMu.Lock()
	defer e.usedGIDsMu.Unlock()
	e.usedGIDs = make(map[core.GID]struct{})
}

// SessionID returns the engine's session identifier, used by the RPC layer
// for session-scoped state.
func (e *Engine) SessionID() string {
	return e.sessionID
}

// Created returns the time the engine was created.
func (e *Engine) Created() time.Time {
	return e.created
}

// emit sends a lifecycle event to all registered subscribers via the EventBus.
func (e *Engine) emit(kind core.EventKind, gid core.GID) {
	e.bus.Emit(core.Event{
		Kind: kind,
		GID:  gid,
		Time: time.Now(),
	})
}

// emitBatch sends multiple lifecycle events to all registered subscribers
// in a single notification pass via the EventBus.
func (e *Engine) emitBatch(evs []core.Event) {
	if len(evs) == 0 {
		return
	}
	for i := range evs {
		e.bus.Emit(evs[i])
	}
}

// Run starts the engine's main goroutines and blocks until the context is
// cancelled or all downloads complete. The engine exits cleanly when ctx
// is done; call Shutdown for graceful shutdown.
//
// When keepRunning is true (set via PREF_ENABLE_RPC), the engine continues
// running even when there are no active downloads, matching aria2's
// keepRunning_ flag.
func (e *Engine) Run(ctx context.Context) error {
	e.ctx, e.cancel = context.WithCancel(ctx)
	e.running.Store(true)
	defer e.running.Store(false)

	e.log.Info("engine started", "session", e.sessionID)

	if e.cfg.InputFile != "" {
		if err := e.loadInputFile(e.cfg.InputFile); err != nil {
			e.log.Warn("failed to load input file", "path", e.cfg.InputFile, "error", err)
		}
	}

	// 1. Load session if path exists. If input-file and save-session point to
	// the same path, load it once through the input-file path.
	if e.cfg.SaveSession != "" && e.cfg.SaveSession != e.cfg.InputFile {
		if err := e.LoadSession(e.cfg.SaveSession); err != nil {
			e.log.Warn("failed to load session", "error", err)
		}
	}

	// Initialize RPC transport if dispatcher factory is set.
	if e.newDispatcher != nil {
		backend := e.newDispatcher(e, e.cfg.RPCSecret)
		tsCfg, err := e.rpcTransportConfig(backend)
		if err != nil {
			return err
		}
		ts, err := rpc_transport.New(tsCfg)
		if err != nil {
			return fmt.Errorf("engine: rpc transport: %w", err)
		}
		e.rpcServer = ts
	}

	// 2. Start queue manager (scheduler)
	e.wg.Add(1)
	go e.queueManager()

	// 3. Start ticker (periodic stats, console, auto-save)
	e.wg.Add(1)
	go e.ticker()

	// 3. Start RPC if enabled
	if e.rpcServer != nil {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			if err := e.rpcServer.Run(e.ctx); err != nil {
				e.log.Error("rpc transport error", "error", err)
			}
		}()
	}

	// 4. Start DHT if enabled
	if e.dhtServer != nil {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			if err := e.dhtServer.Run(e.ctx); err != nil && err != context.Canceled {
				e.log.Error("dht server error", "error", err)
			}
		}()
	}

	// 5. Start portmap (only when DHT/BT features are enabled)
	if e.cfg.EnableDHT && e.portMapper != nil {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			if err := e.portMapper.Run(e.ctx); err != nil && err != context.Canceled {
				e.log.Error("portmap error", "error", err)
			}
		}()
	}

	// 6. Start LPD if enabled
	if e.lpdListener != nil {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			if err := e.lpdListener.Run(e.ctx); err != nil && err != context.Canceled {
				e.log.Error("lpd listener error", "error", err)
			}
		}()
	}

	<-e.ctx.Done()

	e.log.Info("engine stopping")
	e.wg.Wait()
	e.log.Info("engine stopped")

	e.showDownloadResults()
	e.saveHTTPCookies()

	// Save session on exit (only if path configured)
	if e.cfg.SaveSession != "" {
		if err := e.SaveSession(); err != nil {
			e.log.Error("failed to save session on exit", "error", err)
		}
	}

	return nil
}

func (e *Engine) rpcTransportConfig(backend RPCBackend) (rpc_transport.Config, error) {
	rpcPort := e.cfg.RPCListenPort
	if rpcPort <= 0 {
		rpcPort = 6800
	}
	rpcListen := fmt.Sprintf(":%d", rpcPort)
	var origins []string
	if e.cfg.RPCAllowOriginAll {
		origins = []string{"*"}
	}
	tsCfg := rpc_transport.Config{
		Listen:         rpcListen,
		ListenAll:      e.cfg.RPCListenAll,
		AllowedOrigins: origins,
		Dispatcher:     &rpcAdapter{b: backend},
		Secret:         e.cfg.RPCSecret,
		RPCUser:        e.cfg.RPCUser,
		RPCPasswd:      e.cfg.RPCPasswd,
		MaxRequestSize: parseSize(e.cfg.RPCMaxRequestSize),
	}
	if e.cfg.RPCSecure {
		tlsCfg, err := loadRPCServerTLSConfig(e.cfg.RPCCertificate, e.cfg.RPCPrivateKey)
		if err != nil {
			return rpc_transport.Config{}, fmt.Errorf("engine: rpc tls: %w", err)
		}
		tsCfg.TLS = tlsCfg
	}
	return tsCfg, nil
}

func loadRPCServerTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	return tlsx.ServerConfig(tlsx.ServerOpts{
		CertFile: certFile,
		KeyFile:  keyFile,
	})
}

func (e *Engine) saveHTTPCookies() {
	if e.cookieJar == nil || e.cfg.SaveCookies == "" {
		return
	}
	f, err := os.Create(e.cfg.SaveCookies)
	if err != nil {
		e.log.Error("failed to save cookies", "path", e.cfg.SaveCookies, "error", err)
		return
	}
	defer f.Close()
	if err := e.cookieJar.SaveNetscape(f); err != nil {
		e.log.Error("failed to write cookies", "path", e.cfg.SaveCookies, "error", err)
		return
	}
	e.log.Info("saved cookies", "path", e.cfg.SaveCookies)
}

// Shutdown initiates an orderly shutdown. If force is true, the shutdown
// is immediate (matching aria2's requestForceHalt). Otherwise, active
// downloads are requested to halt gracefully (requestHalt).
func (e *Engine) Shutdown(force bool) error {
	if e.shuttingDown.Swap(true) {
		return fmt.Errorf("engine: already shutting down")
	}

	e.log.Info("shutdown requested", "force", force)

	e.queuesMu.Lock()
	for _, gid := range e.active {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		if rg.cancel != nil {
			if force {
				rg.forceHaltReq = true
			} else {
				rg.haltRequested = true
			}
		}
		e.groups.unlock(gid)
	}
	e.queuesMu.Unlock()

	e.haltReq.Store(true)
	e.cancel()
	return nil
}

// ShutdownDelayed schedules a shutdown after ShutdownDelay, matching
// aria2's TimedHaltCommand pattern. This gives the RPC client time to
// receive the response before the server exits.
func (e *Engine) ShutdownDelayed(force bool) {
	e.log.Info("scheduling delayed shutdown", "force", force, "delay", ShutdownDelay)
	time.AfterFunc(ShutdownDelay, func() {
		_ = e.Shutdown(force)
	})
}

// Add creates a new download from the given specification and assigns either
// the supplied valid GID or a random GID (matching aria2's GroupId::create).
// The download is placed in the waiting queue. If there are available active
// slots (respecting max-concurrent-downloads), the queue manager will promote
// it to active.
//
// Returns the assigned GID on success.
func (e *Engine) Add(spec AddSpec) (core.GID, error) {
	if len(spec.URIs) == 0 && len(spec.Torrent) == 0 && len(spec.Metalink) == 0 {
		return 0, fmt.Errorf("engine: Add requires URIs, Torrent, or Metalink")
	}

	if e.shuttingDown.Load() {
		return 0, fmt.Errorf("engine: cannot add download during shutdown")
	}

	opts := spec.Options
	if opts == nil {
		opts = &config.Options{}
	}
	opts = config.Merge(e.cfg, opts)

	gid, err := e.gidForOptions(opts)
	if err != nil {
		return 0, err
	}
	filePath := ""
	filePathFromURI := false
	if opts.Out != "" {
		filePath = filepath.Join(opts.Dir, opts.Out)
	} else if len(spec.URIs) > 0 {
		filePath = defaultOutputPathFromURI(spec.URIs[0])
		filePathFromURI = true
	}
	rg := &requestGroup{
		gid:             gid,
		opts:            opts,
		uris:            spec.URIs,
		torrent:         spec.Torrent,
		metalinkData:    spec.Metalink,
		state:           core.StatusWaiting,
		created:         time.Now(),
		pauseReq:        opts.Pause && e.keepRunning,
		belongsTo:       spec.BelongsTo,
		filePath:        filePath,
		filePathFromURI: filePathFromURI,
	}

	if dl := parseSize(opts.MaxDownloadLimit); dl > 0 {
		rg.downloadLimit = NewThrottle(dl)
	}

	e.groups.set(gid, rg)

	e.queuesMu.Lock()
	pos := len(e.waiting)
	if spec.PositionSet {
		pos = spec.Position
		if pos < 0 || pos > len(e.waiting) {
			pos = len(e.waiting)
		}
	}
	e.waiting = append(e.waiting, 0)
	copy(e.waiting[pos+1:], e.waiting[pos:])
	e.waiting[pos] = gid
	e.queuesMu.Unlock()

	// Wake queue manager so it can immediately try to promote this download.
	select {
	case e.queueWake <- struct{}{}:
	default:
	}

	e.log.Info("download added", "gid", gid, "uris", len(spec.URIs), "position", pos)
	return gid, nil
}

func (e *Engine) gidForOptions(opts *config.Options) (core.GID, error) {
	if opts.GID == "" {
		return e.nextGID(), nil
	}
	gid, err := core.ParseGID(opts.GID)
	if err != nil || gid == 0 {
		if err == nil {
			err = fmt.Errorf("zero GID")
		}
		return 0, fmt.Errorf("engine: invalid GID %q: %w", opts.GID, err)
	}
	if _, exists := e.groups.get(gid); exists {
		return 0, fmt.Errorf("engine: duplicate GID %s", gid.Hex())
	}
	e.usedGIDsMu.Lock()
	defer e.usedGIDsMu.Unlock()
	if _, used := e.usedGIDs[gid]; used {
		return 0, fmt.Errorf("engine: duplicate GID %s", gid.Hex())
	}
	e.usedGIDs[gid] = struct{}{}
	return gid, nil
}

// Pause pauses an active or waiting download. If the download is active,
// its context is cancelled, the download worker stops, and the group is
// moved to the waiting queue with pause-requested flag (matching aria2's
// behavior of moving paused groups to reservedGroups_ with pauseRequested_).
//
// If force is true, the pause is immediate (force-halt semantics).
func (e *Engine) Pause(gid core.GID, force bool) error {
	rg, ok := e.groups.getLocked(gid)
	if !ok {
		return fmt.Errorf("engine: download GID#%s not found", gid)
	}
	defer e.groups.unlock(gid)

	if rg.pauseReq {
		return fmt.Errorf("engine: download GID#%s is already paused", gid)
	}

	e.queuesMu.Lock()
	defer e.queuesMu.Unlock()

	switch rg.state {
	case core.StatusActive:
		rg.pauseReq = true
		if force {
			rg.forceHaltReq = true
		}
		if rg.cancel != nil {
			rg.cancel()
		}
		e.moveFromActiveLocked(gid)
		e.waiting = append([]core.GID{gid}, e.waiting...)
		rg.state = core.StatusWaiting
		e.emit(core.EvPause, gid)
		e.log.Info("download paused", "gid", gid, "force", force)

	case core.StatusWaiting:
		rg.pauseReq = true
		// aria2 keeps the group in the reserved queue with pauseRequested;
		// state remains STATUS_WAITING + pauseRequested.
		e.emit(core.EvPause, gid)
		e.log.Info("download paused", "gid", gid)

	case core.StatusComplete, core.StatusError, core.StatusRemoved:
		return fmt.Errorf("engine: cannot pause download GID#%s in state %s", gid, rg.state)
	}

	return nil
}

// Resume restarts a paused download, clearing the pause flag and leaving it
// in the waiting queue. The queue manager will promote it to active when a
// slot becomes available (matching aria2's resume -> waiting -> active flow).
func (e *Engine) Resume(gid core.GID) error {
	rg, ok := e.groups.getLocked(gid)
	if !ok {
		return fmt.Errorf("engine: download GID#%s not found", gid)
	}
	defer e.groups.unlock(gid)

	if !rg.pauseReq {
		return fmt.Errorf("engine: download GID#%s is not paused (state=%s)", gid, rg.state)
	}

	rg.pauseReq = false
	rg.restartReq = true
	// Download is already in waiting queue (aria2 places paused groups
	// in reservedGroups_ which is the waiting queue). State stays Waiting.
	e.log.Info("download resumed", "gid", gid)
	return nil
}

// Remove removes a download. Active downloads are force-halted first.
// The download's final state is moved to the stopped/results queue.
//
// If force is true, the removal is immediate even if the download is
// mid-transfer.
func (e *Engine) Remove(gid core.GID, force bool) error {
	rg, ok := e.groups.getLocked(gid)
	if !ok {
		return fmt.Errorf("engine: download GID#%s not found", gid)
	}

	if rg.state == core.StatusActive {
		rg.forceHaltReq = true
		if force {
			rg.pauseReq = true
		}
		if rg.cancel != nil {
			rg.cancel()
		}
		e.queuesMu.Lock()
		e.moveFromActiveLocked(gid)
		e.queuesMu.Unlock()
	} else if rg.state == core.StatusWaiting {
		e.queuesMu.Lock()
		e.moveFromWaitingLocked(gid)
		e.queuesMu.Unlock()
	}

	e.addStoppedLocked(rg, core.StatusRemoved, core.ExitRemoved, "removed by user")
	e.groups.unlock(gid)
	e.groups.delete(gid)
	e.log.Info("download removed", "gid", gid, "force", force)
	return nil
}

// PurgeDownloadResult clears the stopped/results queue and returns the
// number of entries purged. This matches aria2's purgeDownloadResult.
func (e *Engine) PurgeDownloadResult() int {
	e.stoppedRing.mu.Lock()
	n := e.stoppedRing.size
	e.stoppedRing.mu.Unlock()
	e.stoppedRing.purge()
	return n
}

// RemoveDownloadResult removes a completed/error/removed result from the
// stopped-results queue.
func (e *Engine) RemoveDownloadResult(gid core.GID) error {
	if _, ok := e.groups.get(gid); ok {
		return fmt.Errorf("engine: download GID#%s is still active or waiting", gid)
	}
	if !e.stoppedRing.removeByGID(gid) {
		return fmt.Errorf("engine: download result GID#%s not found", gid)
	}
	return nil
}

// NumStopped returns the total number of downloads that have ever been stopped
// (mirrors aria2's RequestGroupMan::getNumStoppedTotal).
func (e *Engine) NumStopped() int64 {
	return e.stoppedTotal.Load()
}

// RemovedErrorResult returns the count and last error code of error results
// evicted from the stopped queue due to max-download-result limits.
func (e *Engine) RemovedErrorResult() (count int, lastErr core.ErrorCode) {
	return int(e.removedErrors.Load()), core.ErrorCode(e.removedLastErr.Load())
}

// GetDownloadStat returns aggregate statistics for completed/error/in-progress/waiting
// downloads, matching aria2's RequestGroupMan::getDownloadStat.
func (e *Engine) GetDownloadStat() (completed, errors, inProgress, waiting int) {
	e.queuesMu.Lock()
	inProgress = len(e.active)
	waiting = len(e.waiting)
	e.queuesMu.Unlock()

	e.stoppedRing.mu.Lock()
	defer e.stoppedRing.mu.Unlock()
	capacity := len(e.stoppedRing.buf)
	for i := 0; i < e.stoppedRing.size; i++ {
		pos := (e.stoppedRing.head + i) % capacity
		dr := e.stoppedRing.buf[pos]
		if dr.belongsTo != 0 {
			continue
		}
		switch dr.errCode {
		case core.ExitSuccess:
			completed++
		case core.ExitRemoved:
			// removed — don't count
		default:
			errors++
		}
	}
	errors += int(e.removedErrors.Load())
	return
}

// ExitCode returns the CLI session result code after Run has drained.
func (e *Engine) ExitCode() core.ErrorCode {
	_, errorsCount, inProgress, waiting := e.GetDownloadStat()
	lastErr := core.ErrorCode(e.removedLastErr.Load())

	e.stoppedRing.mu.Lock()
	capacity := len(e.stoppedRing.buf)
	for i := 0; i < e.stoppedRing.size; i++ {
		pos := (e.stoppedRing.head + i) % capacity
		dr := e.stoppedRing.buf[pos]
		if dr.belongsTo != 0 {
			continue
		}
		switch dr.errCode {
		case core.ExitSuccess:
		case core.ExitRemoved:
		case core.ExitInProgress:
			inProgress++
		default:
			lastErr = dr.errCode
		}
	}
	e.stoppedRing.mu.Unlock()

	if errorsCount == 0 && inProgress == 0 && waiting == 0 {
		return core.ExitSuccess
	}
	if lastErr == core.ExitSuccess && inProgress > 0 {
		return core.ExitInProgress
	}
	if lastErr != core.ExitSuccess {
		return lastErr
	}
	return core.ExitUnknownError
}

// TellStatus returns the current status of a single download.
func (e *Engine) TellStatus(gid core.GID) (*Status, error) {
	rg, ok := e.groups.get(gid)
	if ok {
		return e.makeStatus(rg), nil
	}

	dr, found := e.stoppedRing.getByGID(gid)
	if !found {
		return nil, fmt.Errorf("engine: download GID#%s not found", gid)
	}
	return &Status{
		GID:          gid,
		Status:       dr.state,
		ErrorCode:    dr.errCode,
		ErrorMessage: dr.errMsg,
		BelongsTo:    dr.belongsTo,
		Following:    dr.following,
		FollowedBy:   dr.followedBy,
	}, nil
}

// TellActive returns status snapshots of all active downloads.
func (e *Engine) TellActive() []Status {
	e.queuesMu.Lock()
	activeCopy := make([]core.GID, len(e.active))
	copy(activeCopy, e.active)
	e.queuesMu.Unlock()

	result := make([]Status, 0, len(activeCopy))
	for _, gid := range activeCopy {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		result = append(result, *e.makeStatus(rg))
		e.groups.unlock(gid)
	}
	return result
}

// TellWaiting returns status snapshots of waiting/paused downloads,
// starting at offset, up to num entries.
func (e *Engine) TellWaiting(offset, num int) []Status {
	e.queuesMu.Lock()
	waitingCopy := make([]core.GID, len(e.waiting))
	copy(waitingCopy, e.waiting)
	e.queuesMu.Unlock()

	if offset >= len(waitingCopy) {
		return nil
	}
	end := offset + num
	if end > len(waitingCopy) {
		end = len(waitingCopy)
	}

	result := make([]Status, 0, end-offset)
	for _, gid := range waitingCopy[offset:end] {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		result = append(result, *e.makeStatus(rg))
		e.groups.unlock(gid)
	}
	return result
}

// TellStopped returns status snapshots of completed/error/removed downloads,
// starting at offset, up to num entries.
func (e *Engine) TellStopped(offset, num int) []Status {
	return e.stoppedRing.snapshotStatuses(offset, num)
}

// ChangeOption applies per-download option changes. For active downloads,
// the options are stored as pending and take effect on restart (pause+resume),
// matching aria2's pendingOption_ pattern (RequestGroupMan.cc:440-443).
// For waiting downloads, the options are applied immediately since the
// download has not yet started.
func (e *Engine) ChangeOption(gid core.GID, opts *config.Options) error {
	if opts == nil {
		return fmt.Errorf("engine: options is nil")
	}

	rg, ok := e.groups.getLocked(gid)
	if !ok {
		if e.stoppedRing.mergeOptions(gid, opts) {
			return nil
		}
		return fmt.Errorf("engine: download GID#%s not found", gid)
	}
	defer e.groups.unlock(gid)

	if rg.state == core.StatusActive {
		if rg.pendingOpts == nil {
			rg.pendingOpts = &config.Options{}
		}
		rg.pendingOpts = config.Merge(rg.pendingOpts, opts)
		rg.restartReq = true
		rg.pauseReq = true
		if rg.cancel != nil {
			rg.cancel()
		}
	} else {
		rg.opts = config.Merge(rg.opts, opts)
	}
	return nil
}

// GetOption returns a copy of the current per-download options for gid.
func (e *Engine) GetOption(gid core.GID) (*config.Options, error) {
	rg, ok := e.groups.getLocked(gid)
	if !ok {
		if opts, found := e.stoppedRing.optionsByGID(gid); found {
			return opts, nil
		}
		return nil, fmt.Errorf("engine: download GID#%s not found", gid)
	}
	defer e.groups.unlock(gid)

	if rg.opts == nil {
		return &config.Options{}, nil
	}
	cp := *rg.opts
	return &cp, nil
}

// ChangePosition moves a download within the waiting queue. how must be
// one of "POS_SET", "POS_CUR", or "POS_END". Returns the new absolute
// position (0-indexed) matching aria2's ChangePositionRpcMethod.
func (e *Engine) ChangePosition(gid core.GID, pos int, how string) (int64, error) {
	rg, ok := e.groups.getLocked(gid)
	if !ok {
		return 0, fmt.Errorf("engine: download GID#%s not found", gid)
	}
	defer e.groups.unlock(gid)

	if rg.state != core.StatusWaiting {
		return 0, fmt.Errorf("engine: download GID#%s is not in waiting state", gid)
	}

	e.queuesMu.Lock()
	defer e.queuesMu.Unlock()

	n := len(e.waiting)
	curIdx := -1
	for i, g := range e.waiting {
		if g == gid {
			curIdx = i
			break
		}
	}
	if curIdx < 0 {
		return 0, fmt.Errorf("engine: download GID#%s not found in waiting queue", gid)
	}

	var dest int
	switch how {
	case "POS_SET":
		dest = pos
	case "POS_CUR":
		dest = curIdx + pos
	case "POS_END":
		dest = n + pos // pos is typically negative for POS_END
	default:
		return 0, fmt.Errorf("engine: invalid how parameter: %q", how)
	}
	if dest < 0 {
		dest = 0
	}
	if dest >= n {
		dest = n - 1
	}

	// Remove from current position.
	e.waiting = append(e.waiting[:curIdx], e.waiting[curIdx+1:]...)
	if dest > curIdx {
		dest--
	}
	// Insert at new position.
	e.waiting = append(e.waiting, 0)
	copy(e.waiting[dest+1:], e.waiting[dest:])
	e.waiting[dest] = gid

	return int64(dest), nil
}

// ChangeURI mutates the URI list for a single-file download and returns the
// number of removed and added URIs.
func (e *Engine) ChangeURI(gid core.GID, fileIndex int, delURIs, addURIs []string, pos int) (int64, int64, error) {
	return e.ChangeURIWithPosition(gid, fileIndex, delURIs, addURIs, pos, true)
}

// ChangeURIWithPosition mutates the URI list for a single-file download. When
// positionSet is false, new URIs are appended; when true, pos is clamped to the
// current URI list length, matching aria2.changeUri's optional position.
func (e *Engine) ChangeURIWithPosition(gid core.GID, fileIndex int, delURIs, addURIs []string, pos int, positionSet bool) (int64, int64, error) {
	if fileIndex < 1 {
		return 0, 0, fmt.Errorf("engine: fileIndex must be >= 1")
	}
	if fileIndex != 1 {
		return 0, 0, fmt.Errorf("engine: fileIndex is out of range")
	}
	if positionSet && pos < 0 {
		return 0, 0, fmt.Errorf("engine: position must be >= 0")
	}
	rg, ok := e.groups.getLocked(gid)
	if !ok {
		return 0, 0, fmt.Errorf("engine: download GID#%s not found", gid)
	}
	defer e.groups.unlock(gid)

	removed := 0
	for _, target := range delURIs {
		for i, uri := range rg.uris {
			if uri == target {
				rg.uris = append(rg.uris[:i], rg.uris[i+1:]...)
				removed++
				break
			}
		}
	}

	if !positionSet || pos > len(rg.uris) {
		pos = len(rg.uris)
	}
	added := 0
	for _, rawURI := range addURIs {
		uri, valid := normalizeChangeURI(rawURI)
		if !valid {
			continue
		}
		rg.uris = append(rg.uris, "")
		copy(rg.uris[pos+1:], rg.uris[pos:])
		rg.uris[pos] = uri
		pos++
		added++
	}
	if rg.filePath == "" && len(rg.uris) > 0 {
		rg.filePath = filepath.Base(rg.uris[0])
	}
	return int64(removed), int64(added), nil
}

func normalizeChangeURI(rawURI string) (string, bool) {
	uri := strings.ReplaceAll(rawURI, " ", "%20")
	parsed, err := url.Parse(uri)
	if err != nil || !parsed.IsAbs() || parsed.Scheme == "" {
		return "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "ftp", "sftp":
		if parsed.Host == "" {
			return "", false
		}
	}
	return uri, true
}

// ChangeGlobalOption updates the global engine options. These apply to
// all new downloads created after the change.
func (e *Engine) ChangeGlobalOption(opts *config.Options) error {
	if opts == nil {
		return fmt.Errorf("engine: options is nil")
	}

	e.queuesMu.Lock()
	e.cfg = config.Merge(e.cfg, opts)
	e.queuesMu.Unlock()
	return nil
}

// GetGlobalOption returns a copy of the current global options.
func (e *Engine) GetGlobalOption() *config.Options {
	e.queuesMu.Lock()
	defer e.queuesMu.Unlock()
	cp := *e.cfg
	return &cp
}

// SaveSession serializes the current engine state (all active and waiting
// downloads) to the save-session file path specified in configuration.
// The output is aria2-compatible session file format using the sessionfile
// package. If no save session path is configured, this is a no-op.
func (e *Engine) SaveSession() error {
	path := e.cfg.SaveSession
	if path == "" {
		return fmt.Errorf("engine: Filename is not given.")
	}

	e.queuesMu.Lock()
	waitingCopy := make([]core.GID, len(e.waiting))
	copy(waitingCopy, e.waiting)
	activeCopy := make([]core.GID, len(e.active))
	copy(activeCopy, e.active)
	e.queuesMu.Unlock()

	var entries []sessionfile.Entry
	for _, gid := range waitingCopy {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		entries = append(entries, e.toSessionEntry(rg))
		e.groups.unlock(gid)
	}
	for _, gid := range activeCopy {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		entries = append(entries, e.toSessionEntry(rg))
		e.groups.unlock(gid)
	}

	if len(entries) == 0 {
		return nil
	}

	hash, err := sessionfile.SerializedHash(entries)
	if err != nil {
		return err
	}
	e.queuesMu.Lock()
	if e.hasSessionHash && e.lastSessionHash == hash {
		e.queuesMu.Unlock()
		return nil
	}
	e.lastSessionHash = hash
	e.hasSessionHash = true
	e.queuesMu.Unlock()

	return sessionfile.AtomicSave(path, entries, false)
}

func (e *Engine) toSessionEntry(rg *requestGroup) sessionfile.Entry {
	entry := sessionfile.Entry{
		URIs: rg.uris,
		GID:  rg.gid,
		Options: map[string]string{
			"gid": rg.gid.Hex(),
		},
	}
	if rg.pauseReq {
		entry.Options["pause"] = "true"
		entry.Status = core.StatusPaused
	} else {
		entry.Status = core.StatusWaiting
	}
	if rg.opts != nil && rg.opts.Dir != "" {
		entry.Options["dir"] = rg.opts.Dir
	}
	if rg.opts != nil && rg.opts.Out != "" {
		entry.Options["out"] = rg.opts.Out
	}
	return entry
}

// LoadSession restores downloads from an aria2 session file at the given path.
// Each entry in the session file is added to the engine as a waiting download.
// Entries with the pause flag set are added in paused state.
func (e *Engine) LoadSession(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("engine: load session: %w", err)
	}
	defer f.Close()

	entries, err := sessionfile.Read(f)
	if err != nil {
		return fmt.Errorf("engine: load session: %w", err)
	}

	for _, entry := range entries {
		if len(entry.URIs) == 0 {
			continue
		}
		opts, optErr := optionsFromSessionEntry(entry)
		if optErr != nil {
			e.log.Warn("session load: failed to parse entry options", "gid", entry.GID, "error", optErr)
			continue
		}
		spec := AddSpec{
			URIs:    entry.URIs,
			Options: opts,
		}
		gid, err := e.Add(spec)
		if err != nil {
			e.log.Warn("session load: failed to add entry", "gid", entry.GID, "error", err)
			continue
		}
		e.log.Debug("session load: restored download", "gid", gid)
	}

	e.log.Info("session loaded", "entries", len(entries))
	return nil
}

// loadInputFile reads aria2 input/session entries and adds them to the engine.
// Option lines following each URI entry are parsed with the normal config parser.
func (e *Engine) loadInputFile(path string) error {
	f, err := openInputFile(path)
	if err != nil {
		return fmt.Errorf("engine: input file: %w", err)
	}
	defer f.Close()

	entries, err := sessionfile.Read(f)
	if err != nil {
		return fmt.Errorf("engine: input file read: %w", err)
	}

	loaded := 0
	for _, entry := range entries {
		if len(entry.URIs) == 0 {
			continue
		}
		opts, optErr := optionsFromSessionEntry(entry)
		if optErr != nil {
			return fmt.Errorf("engine: input file options: %w", optErr)
		}
		added, err := e.addURIEntry(entry.URIs, opts)
		if err != nil {
			return fmt.Errorf("engine: input file add: %w", err)
		}
		loaded += added
	}

	e.log.Info("input file loaded", "path", path, "entries", loaded)
	return nil
}

func openInputFile(path string) (io.ReadCloser, error) {
	if path == "-" {
		return io.NopCloser(os.Stdin), nil
	}
	return os.Open(path)
}

func (e *Engine) addURIEntry(uris []string, opts *config.Options) (int, error) {
	effective := config.Merge(e.cfg, opts)
	if effective.ParameterizedURI {
		expanded, err := config.ExpandParameterizedURIs(uris)
		if err != nil {
			return 0, err
		}
		uris = expanded
	}
	if len(uris) == 0 {
		return 0, nil
	}
	if effective.ForceSequential {
		for _, uri := range uris {
			if _, err := e.Add(AddSpec{URIs: []string{uri}, Options: opts}); err != nil {
				return 0, err
			}
		}
		return len(uris), nil
	}
	if _, err := e.Add(AddSpec{URIs: uris, Options: opts}); err != nil {
		return 0, err
	}
	return 1, nil
}

func optionsFromSessionEntry(entry sessionfile.Entry) (*config.Options, error) {
	opts := &config.Options{}
	if len(entry.Options) > 0 {
		var b strings.Builder
		for key, val := range entry.Options {
			for _, line := range strings.Split(val, "\n") {
				b.WriteString(key)
				b.WriteByte('=')
				b.WriteString(line)
				b.WriteByte('\n')
			}
		}
		if err := config.ParseConf(strings.NewReader(b.String()), opts); err != nil {
			return nil, err
		}
	}
	if entry.Status == core.StatusPaused {
		opts.Pause = true
	}
	return opts, nil
}

// SubscribeChannel registers a channel to receive engine lifecycle events.
// Returns a result containing the read-only channel and an unsubscribe function.
//
// Callers should use a buffered channel (e.g. make(chan core.Event, 64))
// to avoid missing events during bursts. If the channel is full, events
// are dropped — matching aria2's Notifier behavior where missed events
// are not retried.
func (e *Engine) Subscribe(ch chan core.Event) SubscribeResult {
	unsub := e.bus.Subscribe(ch)
	return SubscribeResult{
		C:           ch,
		Unsubscribe: unsub,
	}
}

// SubscribeChannel registers a channel to receive engine lifecycle events.
// Identical to Subscribe; provided for API compatibility with tests that
// prefer the explicit channel-oriented name.
func (e *Engine) SubscribeChannel(ch chan core.Event) SubscribeResult {
	return e.Subscribe(ch)
}

// SubscribeLegacy registers a Subscriber-compatible interface for engines
// that use the older synchronous pattern. Returns an unsubscribe function.
func (e *Engine) SubscribeLegacy(s Subscriber) (unsubscribe func()) {
	ch := make(chan core.Event, 64)
	unsub := e.bus.Subscribe(ch)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			s.OnEvent(ev)
		}
	}()
	return func() {
		unsub()
		close(ch)
		<-done
	}
}

// ticker performs periodic background tasks: auto-save and stat aggregation.
// It runs every 1 second, matching aria2's DEFAULT_REFRESH_INTERVAL.
func (e *Engine) ticker() {
	defer e.wg.Done()

	t := time.NewTicker(1 * time.Second)
	defer t.Stop()

	var lastControlSave time.Time
	var lastSessionSave time.Time

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-t.C:
			e.refreshStats()

			if e.console != nil && !e.cfg.Quiet && e.cfg.ShowConsoleReadout {
				e.console.Render(e.collectDownloadStats())
			}

			if e.saveInterval > 0 && time.Since(lastControlSave) >= e.saveInterval {
				e.saveControlFiles()
				lastControlSave = time.Now()
			}
			if e.saveSessionInterval > 0 && time.Since(lastSessionSave) >= e.saveSessionInterval {
				if err := e.SaveSession(); err != nil && e.cfg.SaveSession != "" {
					e.log.Warn("failed to save session", "error", err)
				}
				lastSessionSave = time.Now()
			}
		}
	}
}

func (e *Engine) saveControlFiles() {
	e.queuesMu.Lock()
	gids := make([]core.GID, 0, len(e.active)+len(e.waiting))
	gids = append(gids, e.active...)
	gids = append(gids, e.waiting...)
	e.queuesMu.Unlock()

	for _, gid := range gids {
		rg, ok := e.groups.get(gid)
		if !ok {
			continue
		}
		e.saveControlFile(rg)
	}
}

func (e *Engine) controlFileAllowsResume(path string, opts *config.Options, expectedTotal, expectedPieceLen int64, infoHash []byte) bool {
	if path == "" {
		return false
	}
	controlPath := path + btprogress.Suffix
	if opts != nil && opts.RemoveControlFile {
		if err := os.Remove(controlPath); err != nil && !os.IsNotExist(err) {
			e.log.Warn("failed to remove requested control file", "path", controlPath, "error", err)
		}
		return false
	}
	if _, statErr := os.Stat(controlPath); statErr != nil {
		return false
	}
	info, err := btprogress.Load(path)
	if err != nil {
		if !os.IsNotExist(err) {
			e.removeInvalidControlFile(controlPath, "load failed", err)
		}
		return false
	}
	if !controlInfoMatchesRequest(info, expectedTotal, expectedPieceLen, infoHash) {
		e.removeInvalidControlFile(controlPath, "metadata mismatch", nil)
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return true
	} else if os.IsNotExist(err) {
		if rmErr := os.Remove(controlPath); rmErr != nil && !os.IsNotExist(rmErr) {
			e.log.Warn("failed to remove defunct control file", "path", controlPath, "error", rmErr)
		}
		return false
	} else {
		e.log.Warn("failed to stat payload for control file", "path", path, "error", err)
		return false
	}
}

func controlInfoMatchesRequest(info *btprogress.Info, expectedTotal, expectedPieceLen int64, infoHash []byte) bool {
	if info == nil || info.PieceLength <= 0 || info.TotalLength < 0 {
		return false
	}
	if expectedTotal >= 0 && info.TotalLength != expectedTotal {
		return false
	}
	if expectedPieceLen > 0 && info.PieceLength != expectedPieceLen {
		return false
	}
	if len(infoHash) == 0 {
		if len(info.InfoHash) != 0 {
			return false
		}
	} else if !bytes.Equal(info.InfoHash, infoHash) {
		return false
	}
	expectedBitfieldLen := controlBitfieldLen(controlNumPieces(info.TotalLength, info.PieceLength))
	return len(info.Bitfield) == expectedBitfieldLen
}

func (e *Engine) removeInvalidControlFile(controlPath, reason string, cause error) {
	args := []any{"path", controlPath, "reason", reason}
	if cause != nil {
		args = append(args, "error", cause)
	}
	e.log.Warn("removing invalid control file", args...)
	if err := os.Remove(controlPath); err != nil && !os.IsNotExist(err) {
		e.log.Warn("failed to remove invalid control file", "path", controlPath, "error", err)
	}
}

func (e *Engine) initControlInfo(rg *requestGroup, path string, total int64, pieceLen int64, infoHash []byte) {
	if total < 0 {
		total = 0
	}
	if pieceLen <= 0 {
		pieceLen = controlPieceLength(rg.opts)
	}
	info := newControlInfo(total, pieceLen, infoHash)
	loaded := false
	controlPath := path + btprogress.Suffix
	if _, statErr := os.Stat(controlPath); statErr == nil {
		if loadedInfo, err := btprogress.Load(path); err == nil {
			if normalized, ok := normalizeControlInfo(loadedInfo, total, pieceLen, infoHash, rg.opts); ok {
				info = normalized
				loaded = true
			} else {
				e.log.Warn("ignoring incompatible control file", "gid", rg.gid, "path", controlPath)
				e.removeInvalidControlFile(controlPath, "metadata mismatch", nil)
			}
		} else {
			e.log.Warn("failed to load control file", "gid", rg.gid, "path", controlPath, "error", err)
			e.removeInvalidControlFile(controlPath, "load failed", err)
		}
	}

	rg.controlMu.Lock()
	rg.controlPath = path
	rg.controlInfo = info
	rg.controlLoaded = loaded
	rg.controlPieceBytes = seedControlPieceBytes(info)
	rg.controlMu.Unlock()

	e.saveControlFile(rg)
}

func newControlInfo(total, pieceLen int64, infoHash []byte) *btprogress.Info {
	pieces := controlNumPieces(total, pieceLen)
	return &btprogress.Info{
		InfoHash:    append([]byte(nil), infoHash...),
		PieceLength: pieceLen,
		TotalLength: total,
		Bitfield:    make([]byte, controlBitfieldLen(pieces)),
	}
}

func normalizeControlInfo(info *btprogress.Info, total, pieceLen int64, infoHash []byte, opts *config.Options) (*btprogress.Info, bool) {
	if info == nil || info.TotalLength != total || info.PieceLength <= 0 {
		return nil, false
	}
	if len(infoHash) == 0 {
		if len(info.InfoHash) != 0 {
			return nil, false
		}
	} else if !bytes.Equal(info.InfoHash, infoHash) {
		return nil, false
	}

	if info.PieceLength != pieceLen {
		if opts == nil || !opts.AllowPieceLengthChange {
			return nil, false
		}
		return &btprogress.Info{
			InfoHash:    append([]byte(nil), infoHash...),
			PieceLength: pieceLen,
			TotalLength: total,
			Bitfield:    convertControlBitfield(info.Bitfield, total, info.PieceLength, pieceLen),
		}, true
	}

	expectedLen := controlBitfieldLen(controlNumPieces(total, pieceLen))
	if len(info.Bitfield) != expectedLen {
		return nil, false
	}
	return cloneControlInfo(info), true
}

func cloneControlInfo(info *btprogress.Info) *btprogress.Info {
	if info == nil {
		return nil
	}
	clone := &btprogress.Info{
		InfoHash:     append([]byte(nil), info.InfoHash...),
		PieceLength:  info.PieceLength,
		TotalLength:  info.TotalLength,
		UploadLength: info.UploadLength,
		Bitfield:     append([]byte(nil), info.Bitfield...),
	}
	if len(info.InFlight) > 0 {
		clone.InFlight = make([]btprogress.InFlightPiece, len(info.InFlight))
		for i, piece := range info.InFlight {
			clone.InFlight[i] = btprogress.InFlightPiece{
				Index:    piece.Index,
				Length:   piece.Length,
				Bitfield: append([]byte(nil), piece.Bitfield...),
			}
		}
	}
	return clone
}

func seedControlPieceBytes(info *btprogress.Info) []int64 {
	pieces := controlNumPieces(info.TotalLength, info.PieceLength)
	pieceBytes := make([]int64, pieces)
	for i := 0; i < pieces; i++ {
		if controlBit(info.Bitfield, i) {
			pieceBytes[i] = controlPieceSize(info.TotalLength, info.PieceLength, i)
		}
	}
	return pieceBytes
}

func (e *Engine) saveControlFile(rg *requestGroup) {
	rg.controlMu.Lock()
	path := rg.controlPath
	info := cloneControlInfo(rg.controlInfo)
	adaptor := rg.adaptor
	rg.controlMu.Unlock()
	if path == "" || info == nil {
		return
	}
	if adaptor != nil {
		if err := adaptor.Sync(); err != nil && !errors.Is(err, disk.ErrFileClosed) {
			e.log.Warn("failed to sync payload before saving control file", "gid", rg.gid, "path", path, "error", err)
		}
	}
	if err := btprogress.Save(path, info); err != nil {
		e.log.Warn("failed to save control file", "gid", rg.gid, "path", path+btprogress.Suffix, "error", err)
	}
}

func (e *Engine) setControlAdaptor(rg *requestGroup, adaptor disk.Adaptor) {
	rg.controlMu.Lock()
	rg.adaptor = adaptor
	rg.controlMu.Unlock()
}

func (e *Engine) syncControlAdaptor(rg *requestGroup) {
	rg.controlMu.Lock()
	adaptor := rg.adaptor
	rg.controlMu.Unlock()
	if adaptor == nil {
		return
	}
	if err := adaptor.Sync(); err != nil && !errors.Is(err, disk.ErrFileClosed) {
		e.log.Warn("failed to sync payload", "gid", rg.gid, "error", err)
	}
}

func (e *Engine) removeControlFile(rg *requestGroup) {
	rg.controlMu.Lock()
	path := rg.controlPath
	rg.controlMu.Unlock()
	if path == "" {
		path = rg.filePath
	}
	if path == "" {
		return
	}
	if err := os.Remove(path + btprogress.Suffix); err != nil && !os.IsNotExist(err) {
		e.log.Warn("failed to remove control file", "gid", rg.gid, "path", path+btprogress.Suffix, "error", err)
	}
}

func (e *Engine) finishControlFile(rg *requestGroup) {
	locked, ok := e.groups.getLocked(rg.gid)
	if !ok {
		return
	}
	errCode := locked.errCode
	e.groups.unlock(rg.gid)

	if errCode == core.ExitSuccess {
		if rg.opts != nil && rg.opts.ForceSave {
			e.saveControlFile(rg)
		} else {
			e.removeControlFile(rg)
		}
		return
	}
	e.saveControlFile(rg)
}

func (e *Engine) applyControlBitfield(rg *requestGroup, adaptor disk.Adaptor) {
	rg.controlMu.Lock()
	info := cloneControlInfo(rg.controlInfo)
	rg.controlMu.Unlock()
	if info == nil {
		return
	}
	pieces := controlNumPieces(info.TotalLength, info.PieceLength)
	adaptor.SetPieceCount(pieces)
	for i := 0; i < pieces; i++ {
		if controlBit(info.Bitfield, i) {
			adaptor.MarkPiece(i, true)
		}
	}
}

func (e *Engine) markControlWritten(rg *requestGroup, start, length int64) {
	if length <= 0 {
		return
	}
	rg.controlMu.Lock()
	defer rg.controlMu.Unlock()
	info := rg.controlInfo
	if info == nil || info.PieceLength <= 0 || info.TotalLength <= 0 {
		return
	}
	end := start + length
	if end > info.TotalLength {
		end = info.TotalLength
	}
	if start < 0 || start >= end {
		return
	}
	pieces := controlNumPieces(info.TotalLength, info.PieceLength)
	if len(rg.controlPieceBytes) != pieces {
		rg.controlPieceBytes = seedControlPieceBytes(info)
	}
	first := int(start / info.PieceLength)
	last := int((end - 1) / info.PieceLength)
	for i := first; i <= last && i < pieces; i++ {
		if controlBit(info.Bitfield, i) {
			continue
		}
		pieceStart := int64(i) * info.PieceLength
		pieceEnd := pieceStart + controlPieceSize(info.TotalLength, info.PieceLength, i)
		overlapStart := max64(start, pieceStart)
		overlapEnd := min64(end, pieceEnd)
		if overlapEnd <= overlapStart {
			continue
		}
		rg.controlPieceBytes[i] += overlapEnd - overlapStart
		if rg.controlPieceBytes[i] >= pieceEnd-pieceStart {
			rg.controlPieceBytes[i] = pieceEnd - pieceStart
			setControlBit(info.Bitfield, i, true)
		}
	}
}

func (e *Engine) markControlPiece(rg *requestGroup, index int, ok bool) {
	rg.controlMu.Lock()
	defer rg.controlMu.Unlock()
	info := rg.controlInfo
	if info == nil {
		return
	}
	pieces := controlNumPieces(info.TotalLength, info.PieceLength)
	if index < 0 || index >= pieces {
		return
	}
	setControlBit(info.Bitfield, index, ok)
	if len(rg.controlPieceBytes) != pieces {
		rg.controlPieceBytes = seedControlPieceBytes(info)
	}
	if ok {
		rg.controlPieceBytes[index] = controlPieceSize(info.TotalLength, info.PieceLength, index)
	} else {
		rg.controlPieceBytes[index] = 0
	}
}

func (e *Engine) controlResumeOffset(rg *requestGroup, path string) int64 {
	rg.controlMu.Lock()
	loaded := rg.controlLoaded
	info := cloneControlInfo(rg.controlInfo)
	rg.controlMu.Unlock()
	if !loaded || info == nil {
		return 0
	}
	offset := controlContiguousLength(info)
	if st, err := os.Stat(path); err == nil && !st.IsDir() && st.Size() < offset {
		offset = st.Size()
	}
	return offset
}

func (e *Engine) controlCompletedLength(rg *requestGroup) int64 {
	rg.controlMu.Lock()
	info := cloneControlInfo(rg.controlInfo)
	rg.controlMu.Unlock()
	if info == nil {
		return 0
	}
	return controlCompletedLength(info)
}

func (e *Engine) controlLoaded(rg *requestGroup) bool {
	rg.controlMu.Lock()
	defer rg.controlMu.Unlock()
	return rg.controlLoaded
}

func (e *Engine) controlSegmentMan(rg *requestGroup) *SegmentMan {
	rg.controlMu.Lock()
	loaded := rg.controlLoaded
	info := cloneControlInfo(rg.controlInfo)
	rg.controlMu.Unlock()
	if !loaded || info == nil {
		return nil
	}
	return newControlSegmentMan(info.TotalLength, info.PieceLength, info.Bitfield)
}

func controlPieceLength(opts *config.Options) int64 {
	if opts != nil && opts.PieceLength != "" {
		if n := parseSize(opts.PieceLength); n > 0 {
			return n
		}
	}
	return 1 << 20
}

func controlNumPieces(total, pieceLen int64) int {
	if total <= 0 || pieceLen <= 0 {
		return 0
	}
	return int((total + pieceLen - 1) / pieceLen)
}

func controlBitfieldLen(pieces int) int {
	if pieces <= 0 {
		return 0
	}
	return (pieces + 7) / 8
}

func controlPieceSize(total, pieceLen int64, index int) int64 {
	start := int64(index) * pieceLen
	if start >= total {
		return 0
	}
	end := start + pieceLen
	if end > total {
		end = total
	}
	return end - start
}

func controlBit(bitfield []byte, index int) bool {
	if index < 0 || index/8 >= len(bitfield) {
		return false
	}
	return bitfield[index/8]&(1<<uint(7-index%8)) != 0
}

func setControlBit(bitfield []byte, index int, ok bool) {
	if index < 0 || index/8 >= len(bitfield) {
		return
	}
	mask := byte(1 << uint(7-index%8))
	if ok {
		bitfield[index/8] |= mask
		return
	}
	bitfield[index/8] &^= mask
}

func controlContiguousLength(info *btprogress.Info) int64 {
	pieces := controlNumPieces(info.TotalLength, info.PieceLength)
	var completed int64
	for i := 0; i < pieces; i++ {
		if !controlBit(info.Bitfield, i) {
			break
		}
		completed += controlPieceSize(info.TotalLength, info.PieceLength, i)
	}
	return completed
}

func controlCompletedLength(info *btprogress.Info) int64 {
	pieces := controlNumPieces(info.TotalLength, info.PieceLength)
	var completed int64
	for i := 0; i < pieces; i++ {
		if controlBit(info.Bitfield, i) {
			completed += controlPieceSize(info.TotalLength, info.PieceLength, i)
		}
	}
	return completed
}

func convertControlBitfield(src []byte, total, srcPieceLen, dstPieceLen int64) []byte {
	dstPieces := controlNumPieces(total, dstPieceLen)
	dst := make([]byte, controlBitfieldLen(dstPieces))
	for i := 0; i < dstPieces; i++ {
		start := int64(i) * dstPieceLen
		end := start + controlPieceSize(total, dstPieceLen, i)
		complete := true
		for pos := start; pos < end; {
			srcIdx := int(pos / srcPieceLen)
			if !controlBit(src, srcIdx) {
				complete = false
				break
			}
			next := int64(srcIdx+1) * srcPieceLen
			if next > end {
				next = end
			}
			pos = next
		}
		if complete {
			setControlBit(dst, i, true)
		}
	}
	return dst
}

func newControlSegmentMan(total, pieceLen int64, bitfield []byte) *SegmentMan {
	pieces := controlNumPieces(total, pieceLen)
	segments := make([]*Segment, 0)
	for i := 0; i < pieces; {
		if controlBit(bitfield, i) {
			i++
			continue
		}
		startPiece := i
		for i < pieces && !controlBit(bitfield, i) {
			i++
		}
		start := int64(startPiece) * pieceLen
		endPiece := i - 1
		end := int64(endPiece)*pieceLen + controlPieceSize(total, pieceLen, endPiece)
		segments = append(segments, &Segment{Index: len(segments), Start: start, End: end})
	}
	return &SegmentMan{segments: segments, totalSize: total}
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// collectDownloadStats gathers download stat snapshots from all active
// downloads for console rendering.
func (e *Engine) collectDownloadStats() []console.DownloadStat {
	active := e.TellActive()
	snapshots := make([]console.DownloadStat, 0, len(active))
	for _, s := range active {
		fn := ""
		if len(s.Files) > 0 {
			fn = filepath.Base(s.Files[0].Path)
		}
		snapshots = append(snapshots, console.DownloadStat{
			GID:           s.GID.Hex(),
			Status:        s.Status.String(),
			TotalSize:     s.TotalLength,
			CompletedSize: s.CompletedLength,
			Speed:         s.DownloadSpeed,
			UploadSpeed:   s.UploadSpeed,
			Connections:   s.Connections,
			ErrorCode:     int(s.ErrorCode),
			NumSeeders:    int(s.NumSeeders),
			Filename:      fn,
			Seeder:        s.Seeder,
		})
	}
	return snapshots
}

func (e *Engine) showDownloadResults() {
	if e.console == nil || e.cfg == nil || e.cfg.Quiet || e.cfg.DownloadResult == "hide" {
		return
	}
	e.console.PrintDownloadResults(e.collectDownloadResults(), e.cfg.DownloadResult == "full")
}

func (e *Engine) collectDownloadResults() []console.ResultStat {
	e.stoppedRing.mu.Lock()
	defer e.stoppedRing.mu.Unlock()

	results := make([]console.ResultStat, 0, e.stoppedRing.size)
	capacity := len(e.stoppedRing.buf)
	for i := 0; i < e.stoppedRing.size; i++ {
		pos := (e.stoppedRing.head + i) % capacity
		dr := e.stoppedRing.buf[pos]
		if dr == nil || dr.belongsTo != 0 {
			continue
		}
		results = append(results, console.ResultStat{
			GID:          dr.gid.Hex(),
			Status:       downloadResultStatus(dr),
			AverageSpeed: downloadResultAverageSpeed(dr),
			Path:         dr.filePath,
			Percent:      downloadResultPercent(dr),
		})
	}
	return results
}

func downloadResultStatus(dr *downloadResult) string {
	if dr.errCode == core.ExitSuccess && dr.state == core.StatusComplete {
		return "OK"
	}
	if dr.errCode == core.ExitInProgress || (dr.state == core.StatusError && dr.errMsg == "shutdown") {
		return "INPR"
	}
	if dr.state == core.StatusRemoved {
		return "RM"
	}
	return "ERR"
}

func downloadResultAverageSpeed(dr *downloadResult) int64 {
	if dr.sessionTime <= 0 {
		return -1
	}
	return int64(float64(dr.sessionDownloadLength) / dr.sessionTime.Seconds())
}

func downloadResultPercent(dr *downloadResult) int {
	if dr.totalLength <= 0 {
		return -1
	}
	percent := int(100 * dr.completedLength / dr.totalLength)
	if percent > 100 {
		return 100
	}
	if percent < 0 {
		return 0
	}
	return percent
}

// emaAlpha is the EMA smoothing factor for speed calculation.
// aria2's SpeedCalc uses window-based calculation with equivalent smoothing;
// 0.25 matches aria2's default behavior for stable speed reporting.
const emaAlpha = 0.25

// refreshStats updates the download/upload speed stats using an exponential
// moving average. It mirrors aria2's RequestGroupMan::calculateStat() which
// aggregates per-request-group TransferStat, and StatCalc which applies EMA.
//
// For each active download, the instantaneous speed is computed as
// bytesDownloaded / elapsed, then smoothed with EMA:
//
//	speed = emaAlpha * current + (1 - emaAlpha) * previous
func (e *Engine) refreshStats() {
	e.queuesMu.Lock()
	activeCopy := make([]core.GID, len(e.active))
	copy(activeCopy, e.active)
	e.queuesMu.Unlock()

	var totalDL, totalUL int64

	for _, gid := range activeCopy {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}

		now := time.Now()
		elapsed := now.Sub(rg.lastSpeedSample)

		if elapsed > 0 {
			// Download Speed EMA
			dlInstant := int64(0)
			if rg.bytesDownloaded > 0 {
				dlInstant = int64(float64(rg.bytesDownloaded) / elapsed.Seconds())
				rg.completedLength += rg.bytesDownloaded
				rg.bytesDownloaded = 0
			}
			prevDL := rg.downloadSpeed
			smoothedDL := int64(emaAlpha*float64(dlInstant) + (1-emaAlpha)*float64(prevDL))
			if dlInstant == 0 && prevDL == 0 {
				smoothedDL = 0
			}
			rg.downloadSpeed = smoothedDL
			totalDL += smoothedDL

			// Upload Speed EMA
			ulInstant := int64(0)
			if rg.bytesUploaded > 0 {
				ulInstant = int64(float64(rg.bytesUploaded) / elapsed.Seconds())
				rg.bytesUploaded = 0
			}
			prevUL := rg.uploadSpeed
			smoothedUL := int64(emaAlpha*float64(ulInstant) + (1-emaAlpha)*float64(prevUL))
			if ulInstant == 0 && prevUL == 0 {
				smoothedUL = 0
			}
			rg.uploadSpeed = smoothedUL
			totalUL += smoothedUL
		}

		rg.lastSpeedSample = now
		e.groups.unlock(gid)
	}

	e.downloadSpeed.Store(totalDL)
	e.uploadSpeed.Store(totalUL)
}

// queueManager is the main scheduling goroutine. It periodically:
// 1. Processes stopped active downloads (removeStoppedGroup equivalent)
// 2. Promotes waiting downloads to active (fillRequestGroupFromReserver)
// 3. On shutdown, performs onEndOfRun cleanup
//
// The tick interval matches aria2's DEFAULT_REFRESH_INTERVAL = 1 second.
func (e *Engine) queueManager() {
	defer e.wg.Done()

	e.log.Debug("queue manager started")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			e.log.Debug("queue manager draining")
			e.onEndOfRun()
			e.log.Debug("queue manager stopped")
			return
		case <-e.queueWake:
			e.removeStoppedGroup()
			e.fillRequestGroupFromReserver()
			e.cancelIfIdle()
		case <-ticker.C:
			e.removeStoppedGroup()
			e.fillRequestGroupFromReserver()
			e.cancelIfIdle()
		}
	}
}

func (e *Engine) cancelIfIdle() {
	e.queuesMu.Lock()
	isEmpty := len(e.active) == 0 && len(e.waiting) == 0
	e.queuesMu.Unlock()
	if isEmpty && !e.keepRunning && e.cancel != nil {
		e.cancel()
	}
}

// removeStoppedGroup processes active downloads that have been cancelled
// (context done). For paused downloads, it applies pendingOption and moves
// the stopped results queue. Matches aria2's ProcessStoppedRequestGroup +
// removeStoppedGroup flow (RequestGroupMan.cc:290-483).
func (e *Engine) removeStoppedGroup() {
	e.queuesMu.Lock()
	activeCopy := make([]core.GID, len(e.active))
	copy(activeCopy, e.active)
	e.queuesMu.Unlock()

	var stoppedGIDs []core.GID
	for _, gid := range activeCopy {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		// Check if the download's context has been cancelled (stopped).
		isDone := false
		if rg.ctx != nil {
			select {
			case <-rg.ctx.Done():
				isDone = true
			default:
			}
		}
		if isDone {
			stoppedGIDs = append(stoppedGIDs, gid)
		}
		e.groups.unlock(gid)
	}

	if len(stoppedGIDs) == 0 {
		return
	}

	e.queuesMu.Lock()
	for _, gid := range stoppedGIDs {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		e.moveFromActiveLocked(gid)

		if rg.pauseReq {
			if !rg.restartReq {
				e.log.Info("download paused", "gid", gid)
				e.runHookByName(rg, 1, "on-download-pause")
			}
			rg.state = core.StatusWaiting
			e.waiting = append([]core.GID{gid}, e.waiting...)

			if rg.pendingOpts != nil {
				rg.opts = config.Merge(rg.opts, rg.pendingOpts)
				rg.pendingOpts = nil
			}

			if rg.restartReq {
				rg.pauseReq = false
			}

			rg.restartReq = false
			rg.forceHaltReq = false
		} else if rg.haltRequested {
			errCode := core.ExitRemoved
			if rg.errCode != 0 {
				errCode = rg.errCode
			}
			e.addStoppedLocked(rg, core.StatusError, errCode, rg.errMsg)
			e.groups.unlock(gid)
			e.groups.delete(gid)
			continue
		} else {
			// Completed or error — add to stopped results.
			errCode := core.ExitSuccess
			if rg.errCode != 0 {
				errCode = rg.errCode
			}
			finalState := core.StatusComplete
			if errCode != core.ExitSuccess {
				finalState = core.StatusError
			}
			e.addStoppedLocked(rg, finalState, errCode, rg.errMsg)
			e.groups.unlock(gid)
			e.groups.delete(gid)
			continue
		}
		e.groups.unlock(gid)
	}
	e.queuesMu.Unlock()
}

// fillRequestGroupFromReserver promotes waiting downloads to active up to
// maxConcurrentDownloads, skipping paused and dependency-unresolved groups,
// and launches a download goroutine for each newly promoted group.
// Matches aria2's RequestGroupMan::fillRequestGroupFromReserver
// (RequestGroupMan.cc:515-593).
func (e *Engine) fillRequestGroupFromReserver() {
	e.queuesMu.Lock()

	max := e.cfg.MaxConcurrentDownloads
	if max <= 0 {
		max = 1
	}

	if len(e.active) >= max {
		e.queuesMu.Unlock()
		return
	}

	num := max - len(e.active)
	promoted := 0
	var pending []core.GID
	var promotedGIDs []core.GID

	// Pre-allocate event slice for batching.
	batchSize := num
	if batchSize < 1 {
		batchSize = 1
	}
	startEvents := make([]core.Event, 0, batchSize)

	for promoted < num && len(e.waiting) > 0 {
		gid := e.waiting[0]
		e.waiting = e.waiting[1:]

		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}

		if rg.pauseReq {
			pending = append(pending, gid)
			e.groups.unlock(gid)
			continue
		}

		rg.state = core.StatusActive
		e.active = append(e.active, gid)
		promoted++
		promotedGIDs = append(promotedGIDs, gid)
		startEvents = append(startEvents, core.Event{
			Kind: core.EvStart,
			GID:  gid,
			Time: time.Now(),
		})
		e.log.Info("download started", "gid", gid)
		e.groups.unlock(gid)
	}

	if len(pending) > 0 {
		e.waiting = append(pending, e.waiting...)
	}

	e.emitBatch(startEvents)
	e.queuesMu.Unlock()

	if !e.running.Load() {
		return
	}

	for _, gid := range promotedGIDs {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		e.runHookByName(rg, 1, "on-download-start")
		rg.ctx, rg.cancel = context.WithCancel(e.ctx)
		e.wg.Add(1)
		go e.runDownload(rg)
		e.groups.unlock(gid)
	}
}

// onEndOfRun performs cleanup when the engine stops, matching aria2's
// DownloadEngine::onEndOfRun (DownloadEngine.cc:237-242):
//
//	requestGroupMan_->removeStoppedGroup(this);
//	requestGroupMan_->closeFile();
//	requestGroupMan_->save();
func (e *Engine) onEndOfRun() {
	// 1. Process stopped groups (removeStoppedGroup).
	e.queuesMu.Lock()
	activeCopy := make([]core.GID, len(e.active))
	copy(activeCopy, e.active)
	waitingCopy := make([]core.GID, len(e.waiting))
	copy(waitingCopy, e.waiting)
	e.queuesMu.Unlock()

	for _, gid := range activeCopy {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		if rg.state == core.StatusActive {
			e.queuesMu.Lock()
			e.moveFromActiveLocked(gid)
			e.queuesMu.Unlock()
			// Graceful shutdown: mark as IN_PROGRESS so they can be
			// resumed on restart (matches aria2's SHUTDOWN_SIGNAL halt reason).
			errCode := core.ExitRemoved
			if rg.errCode != 0 && rg.errCode != core.ExitSuccess {
				errCode = rg.errCode
			}
			e.addStoppedLocked(rg, core.StatusError, errCode, "shutdown")
			e.groups.unlock(gid)
			e.groups.delete(gid)
			continue
		}
		e.groups.unlock(gid)
	}

	for _, gid := range waitingCopy {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		e.queuesMu.Lock()
		e.moveFromWaitingLocked(gid)
		e.queuesMu.Unlock()
		// If paused, mark for resume; otherwise just removed.
		if rg.pauseReq {
			e.addStoppedLocked(rg, core.StatusError, core.ExitRemoved, "shutdown")
		} else {
			e.addStoppedLocked(rg, core.StatusRemoved, core.ExitRemoved, "shutdown")
		}
		e.groups.unlock(gid)
		e.groups.delete(gid)
	}

	e.log.Debug("engine files closed and state saved for shutdown")
}

// moveFromActiveLocked removes gid from the active queue. Caller must hold e.queuesMu.
func (e *Engine) moveFromActiveLocked(gid core.GID) {
	for i, g := range e.active {
		if g == gid {
			e.active = append(e.active[:i], e.active[i+1:]...)
			return
		}
	}
}

// moveFromWaitingLocked removes gid from the waiting queue. Caller must hold e.queuesMu.
func (e *Engine) moveFromWaitingLocked(gid core.GID) {
	for i, g := range e.waiting {
		if g == gid {
			e.waiting = append(e.waiting[:i], e.waiting[i+1:]...)
			return
		}
	}
}

// addStoppedLocked adds a download to the results/stopped queue.
// Caller must hold the shard lock for rg.gid.
func (e *Engine) addStoppedLocked(rg *requestGroup, state core.Status, errCode core.ErrorCode, errMsg string) {
	rg.state = state
	rg.errCode = errCode
	rg.errMsg = errMsg

	dr := drPool.Get().(*downloadResult)
	dr.gid = rg.gid
	dr.state = state
	dr.errCode = errCode
	dr.errMsg = errMsg
	dr.belongsTo = rg.belongsTo
	dr.following = rg.following
	dr.followedBy = append(dr.followedBy[:0], rg.followedBy...)
	dr.opts = config.Merge(rg.opts)
	dr.filePath = resultFilePath(rg)
	dr.totalLength = rg.totalLength
	dr.completedLength = rg.completedLength
	dr.sessionDownloadLength = rg.completedLength
	dr.sessionTime = 0
	if !rg.created.IsZero() {
		dr.sessionTime = time.Since(rg.created)
	}

	e.stoppedRing.push(dr)
	e.stoppedTotal.Add(1)

	// Handle eviction-based error tracking.
	e.stoppedRing.mu.Lock()
	_, errors, lastErr := e.stoppedRing.evictionInfo()
	e.stoppedRing.mu.Unlock()
	e.removedErrors.Store(int64(errors))
	e.removedLastErr.Store(int64(lastErr))

	// Handle keep-unfinished logic (keep evicted unfinished in ring if configured).
	e.stoppedRing.mu.Lock()
	maxResults := e.cfg.MaxDownloadResult
	if maxResults <= 0 {
		maxResults = 1000
	}
	for e.stoppedRing.size > maxResults {
		evicted := e.stoppedRing.buf[e.stoppedRing.head]
		delete(e.stoppedRing.index, evicted.gid)
		e.stoppedRing.head = (e.stoppedRing.head + 1) % len(e.stoppedRing.buf)
		e.stoppedRing.size--
		e.stoppedRing.evictedTotal++
		if evicted.belongsTo == 0 && evicted.errCode != core.ExitSuccess {
			e.stoppedRing.evictedErrors++
			e.stoppedRing.evictedLastErr = evicted.errCode
			e.removedErrors.Store(int64(e.stoppedRing.evictedErrors))
			e.removedLastErr.Store(int64(evicted.errCode))
			if e.cfg.KeepUnfinishedDownloadResult {
				if evicted.state != core.StatusRemoved || e.cfg.ForceSave {
					// Re-insert the evicted entry.
					pos := (e.stoppedRing.head + e.stoppedRing.size) % len(e.stoppedRing.buf)
					e.stoppedRing.buf[pos] = evicted
					e.stoppedRing.index[evicted.gid] = pos
					e.stoppedRing.size++
				}
			}
		}
	}
	e.stoppedRing.mu.Unlock()

	if errCode == core.ExitSuccess {
		if rg.opts != nil && rg.opts.ForceSave {
			e.saveControlFile(rg)
		} else {
			e.removeControlFile(rg)
		}
	} else {
		e.saveControlFile(rg)
	}

	// Emit appropriate event.
	switch state {
	case core.StatusComplete:
		e.emit(core.EvComplete, rg.gid)
	case core.StatusError:
		e.emit(core.EvError, rg.gid)
	case core.StatusRemoved:
		e.emit(core.EvStop, rg.gid)
	}

	e.runStopHook(rg, errCode)
}

// runDownload executes the actual data transfer for a download, matching
// aria2's HttpDownloadCommand / FtpDownloadCommand lifecycle (DownloadCommand
// reads from socket → writes to DiskAdaptor → segment completion → finish).
//
// The download goroutine watches rg.ctx for cancellation (triggered by Pause,
// Remove, or Shutdown). On dry-run, it immediately transitions to complete
// without I/O. On error, it sets the error code and cancels the context so
// removeStoppedGroup picks it up.
func (e *Engine) runDownload(rg *requestGroup) {
	defer e.wg.Done()

	ctx := rg.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	defer e.finishControlFile(rg)

	if e.cfg.DryRun || rg.opts.DryRun {
		e.log.Info("dry-run mode, skipping download", "gid", rg.gid)
		if _, ok := e.groups.getLocked(rg.gid); ok {
			rg.errCode = core.ExitSuccess
			e.groups.unlock(rg.gid)
		}
		rg.cancel()
		return
	}

	// BitTorrent downloads are driven by torrent metadata, not URIs.
	if len(rg.torrent) > 0 {
		if err := e.runBTDownload(ctx, rg, rg.torrent); err != nil {
			e.log.Error("BT download failed", "gid", rg.gid, "error", err)
		}
		rg.cancel()
		return
	}
	if len(rg.metalinkData) > 0 {
		e.runMetalinkDownload(ctx, rg, rg.metalinkData)
		rg.cancel()
		return
	}

	if len(rg.uris) == 0 {
		e.log.Error("download has no URIs", "gid", rg.gid)
		rg.errCode = core.ExitResourceNotFound
		rg.errMsg = "no URIs specified"
		rg.cancel()
		return
	}

	uri := rg.uris[0]
	if strings.HasPrefix(uri, "magnet:?") {
		e.runMagnetDownload(ctx, rg, uri)
		rg.cancel()
		return
	}
	u, err := url.Parse(uri)
	if err != nil {
		e.log.Error("cannot parse URI", "gid", rg.gid, "uri", uri, "error", err)
		rg.errCode = core.ExitResourceNotFound
		rg.errMsg = fmt.Sprintf("invalid URI: %s", uri)
		rg.cancel()
		return
	}

	outPath := rg.filePath
	outPathExplicit := rg.opts.Out != "" || (rg.filePath != "" && !rg.filePathFromURI)
	if outPath == "" {
		outPath = filepath.Base(u.Path)
		if outPath == "" || outPath == "." || outPath == "/" {
			outPath = "index.html"
		}
		if rg.opts.Dir != "" {
			outPath = filepath.Join(rg.opts.Dir, outPath)
		}
	} else {
		if rg.opts.Out == "" && rg.opts.Dir != "" && !filepath.IsAbs(outPath) {
			outPath = filepath.Join(rg.opts.Dir, outPath)
		}
	}

	proto := strings.ToLower(u.Scheme)
	expectedControlTotal := int64(-1)
	expectedControlPieceLen := controlPieceLength(rg.opts)
	if outPathExplicit {
		resolvedPath, ok := e.resolveFileCollision(rg, outPath, expectedControlTotal, expectedControlPieceLen)
		if !ok {
			rg.cancel()
			return
		}
		outPath = resolvedPath
	}
	if proto == "http" || proto == "https" {
		var stopHTTP bool
		outPath, expectedControlTotal, stopHTTP = e.prepareHTTPMetadata(ctx, rg, uri, outPath, outPathExplicit)
		if stopHTTP {
			rg.cancel()
			return
		}
	}

	if !outPathExplicit {
		resolvedPath, ok := e.resolveFileCollision(rg, outPath, expectedControlTotal, expectedControlPieceLen)
		if !ok {
			rg.cancel()
			return
		}
		outPath = resolvedPath
	}

	switch proto {
	case "http", "https":
		e.runHTTPDownload(ctx, rg, uri, outPath)
	case "ftp":
		e.runFTPDownload(ctx, rg, uri, u, outPath)
	case "sftp":
		e.runSFTPDownload(ctx, rg, uri, u, outPath)
	default:
		e.log.Error("unsupported protocol", "gid", rg.gid, "protocol", proto)
		rg.errCode = core.ExitResourceNotFound
		rg.errMsg = fmt.Sprintf("unsupported protocol: %s", proto)
		rg.cancel()
		return
	}

	rg.cancel()
}

func (e *Engine) resolveFileCollision(rg *requestGroup, outPath string, expectedControlTotal, expectedControlPieceLen int64) (string, bool) {
	// Resolve filename collision and auto-renaming under queuesMu lock to prevent race conditions.
	e.queuesMu.Lock()
	defer e.queuesMu.Unlock()

	originalPath := outPath
	suffix := 1
	for {
		collision := false
		if e.isSameFileBeingDownloadedLocked(outPath, rg.gid) {
			collision = true
		} else if e.controlFileAllowsResume(outPath, rg.opts, expectedControlTotal, expectedControlPieceLen, nil) {
			collision = false
		} else {
			// If file exists on disk and we are not continuing (continuing means rg.opts.Continue is true)
			if _, err := os.Stat(outPath); err == nil && !rg.opts.Continue && !rg.opts.AllowOverwrite {
				collision = true
			}
		}

		if collision {
			if !rg.opts.AutoFileRenaming {
				e.log.Error("file collision detected but auto-file-renaming is disabled", "gid", rg.gid, "path", outPath)
				if _, ok := e.groups.getLocked(rg.gid); ok {
					rg.errCode = core.ExitFileAlreadyExists
					rg.errMsg = fmt.Sprintf("file already exists: %s", outPath)
					e.groups.unlock(rg.gid)
				}
				return outPath, false
			}
			outPath = tryAutoFileRenamingWithSuffix(originalPath, suffix)
			suffix++
		} else {
			break
		}
	}

	// Update the request group's filePath and fileName under lock
	if _, ok := e.groups.getLocked(rg.gid); ok {
		rg.filePath = outPath
		rg.fileName = filepath.Base(outPath)
		rg.filePathFromURI = false
		e.groups.unlock(rg.gid)
	}
	return outPath, true
}

func (e *Engine) runMagnetDownload(ctx context.Context, rg *requestGroup, uri string) {
	m, err := magnet.Parse(uri)
	if err != nil {
		e.log.Error("magnet parse failed", "gid", rg.gid, "error", err)
		rg.errCode = protocolErrorCode(err)
		rg.errMsg = err.Error()
		return
	}
	if m.InfoHashV1 == nil {
		rg.errCode = core.ExitMagnetParseError
		rg.errMsg = "magnet link missing BitTorrent v1 info hash"
		return
	}
	if !rg.opts.BTLoadSavedMetadata {
		rg.errCode = core.ExitResourceNotFound
		rg.errMsg = "magnet link metadata download not supported (local .torrent not found)"
		return
	}
	dir := rg.opts.Dir
	if dir == "" {
		dir = "."
	}
	torrentPath := filepath.Join(dir, fmt.Sprintf("%x.torrent", m.InfoHashV1[:]))
	torrentData, err := os.ReadFile(torrentPath)
	if err != nil {
		if os.IsNotExist(err) {
			rg.errCode = core.ExitResourceNotFound
			rg.errMsg = "magnet link metadata download not supported (local .torrent not found)"
			return
		}
		rg.errCode = core.ExitFileIOError
		rg.errMsg = err.Error()
		return
	}
	if err := e.runBTDownload(ctx, rg, torrentData); err != nil {
		e.log.Error("magnet local torrent download failed", "gid", rg.gid, "path", torrentPath, "error", err)
	}
}

func (e *Engine) prepareHTTPMetadata(ctx context.Context, rg *requestGroup, uri, outPath string, outPathExplicit bool) (string, int64, bool) {
	driver, err := e.httpDriverForURI(rg, uri)
	if err != nil {
		rg.errCode = protocolErrorCode(err)
		rg.errMsg = err.Error()
		return outPath, -1, false
	}
	info, err := e.probeHTTPInfoWithRetry(ctx, rg, driver, uri, outPath)
	if errors.Is(err, httpproto.ErrNotModified) {
		e.completeHTTPNotModified(rg, outPath)
		return outPath, -1, true
	}
	if err != nil {
		e.log.Error("HTTP probe failed", "gid", rg.gid, "uri", uri, "error", err)
		rg.errCode = protocolErrorCode(err)
		rg.errMsg = err.Error()
		return outPath, -1, true
	}

	rg.probed = true
	rg.probedSize = info.Size
	rg.acceptsRanges = info.AcceptsRanges
	rg.cdFilename = info.ContentDispositionFilename
	rg.lastModified = info.LastModified

	if !outPathExplicit && info.ContentDispositionFilename != "" {
		outPath = httpContentDispositionPath(rg.opts, info.ContentDispositionFilename)
	}
	return outPath, info.Size, false
}

func (e *Engine) probeHTTPInfoWithRetry(ctx context.Context, rg *requestGroup, driver *httpproto.Driver, uri, outPath string) (httpproto.ResourceInfo, error) {
	opts := e.httpRequestOptions(rg, outPath)
	maxTries := rg.opts.MaxTries
	retryWait := time.Duration(parseInt(rg.opts.RetryWait)) * time.Second
	maxFileNotFound := rg.opts.MaxFileNotFound
	fileNotFound := 0
	failures := 0

	for {
		info, err := driver.ProbeInfoWithOptions(ctx, uri, opts)
		if err == nil || errors.Is(err, httpproto.ErrNotModified) {
			return info, err
		}

		code := core.CodeFrom(err)
		shouldRetry := false
		switch code {
		case core.ExitResourceNotFound:
			if maxFileNotFound == 0 {
				return info, err
			}
			fileNotFound++
			if fileNotFound >= maxFileNotFound {
				return info, fmt.Errorf("http: max-file-not-found reached: %w",
					core.NewError(core.ExitMaxFileNotFound, "max file not found"))
			}
			shouldRetry = true
		case core.ExitHTTPServiceUnavailable:
			shouldRetry = retryWait > 0
		}
		if !shouldRetry {
			return info, err
		}

		failures++
		if maxTries > 0 && failures >= maxTries {
			return info, err
		}
		if retryWait > 0 {
			timer := time.NewTimer(retryWait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return info, ctx.Err()
			}
		}
	}
}

func (e *Engine) httpDriverForURI(rg *requestGroup, rawURI string) (*httpproto.Driver, error) {
	opts := e.cfg
	if rg != nil && rg.opts != nil {
		opts = rg.opts
	}
	proto := "http"
	if u, err := url.Parse(rawURI); err == nil && u.Scheme != "" {
		proto = strings.ToLower(u.Scheme)
	}
	dialer, err := e.dialerForProtocol(proto, opts)
	if err != nil {
		return nil, err
	}
	httpTLS, err := httpClientTLSConfig(opts)
	if err != nil {
		return nil, err
	}
	httpUser := opts.HTTPUser
	httpPasswd := opts.HTTPPasswd
	if e.authFactory != nil {
		if ac := e.authFactory.CreateAuthConfig(rawURI, opts); ac != nil {
			httpUser = ac.User()
			httpPasswd = ac.Password()
		}
	}
	acceptEncoding := ""
	if opts.HTTPAcceptGzip {
		acceptEncoding = "deflate, gzip"
	}
	jar := e.cookieJar
	if jar == nil {
		jar = newHTTPCookieJar(opts, e.log)
	}
	return httpproto.NewDriver(httpproto.Opts{
		Dialer:            dialer,
		TLS:               httpTLS,
		Jar:               httpCookieJar(jar),
		Timeout:           time.Duration(parseInt(opts.Timeout)) * time.Second,
		UserAgent:         opts.UserAgent,
		Header:            opts.Header,
		CheckCertificate:  &opts.CheckCertificate,
		MaxRedirs:         20,
		AcceptEncoding:    acceptEncoding,
		Referer:           opts.Referer,
		HTTPUser:          httpUser,
		HTTPPasswd:        httpPasswd,
		HTTPAuthChallenge: opts.HTTPAuthChallenge,
		DisableKeepAlive:  !opts.EnableHTTPKeepAlive,
		NoCache:           &opts.HTTPNoCache,
		EnableWantDigest:  boolPtr(!opts.NoWantDigestHeader),
		UseHead:           opts.UseHead,
		DryRun:            opts.DryRun,
	}), nil
}

func (e *Engine) httpRequestOptions(rg *requestGroup, outPath string) httpproto.RequestOptions {
	if rg == nil || rg.opts == nil || !rg.opts.ConditionalGet || outPath == "" {
		return httpproto.RequestOptions{}
	}
	if _, err := os.Stat(outPath + btprogress.Suffix); err == nil {
		return httpproto.RequestOptions{}
	}
	st, err := os.Stat(outPath)
	if err != nil || st.IsDir() {
		return httpproto.RequestOptions{}
	}
	return httpproto.RequestOptions{IfModifiedSince: st.ModTime().UTC().Format(http.TimeFormat)}
}

func (e *Engine) completeHTTPNotModified(rg *requestGroup, outPath string) {
	st, err := os.Stat(outPath)
	if err != nil {
		rg.errCode = core.ExitFileIOError
		rg.errMsg = err.Error()
		return
	}
	rg.filePath = outPath
	rg.fileName = filepath.Base(outPath)
	rg.filePathFromURI = false
	rg.totalLength = st.Size()
	rg.completedLength = st.Size()
	rg.errCode = core.ExitSuccess
	e.log.Info("HTTP resource not modified", "gid", rg.gid, "path", outPath)
}

func httpContentDispositionPath(opts *config.Options, filename string) string {
	if opts != nil && opts.Dir != "" {
		return filepath.Join(opts.Dir, filename)
	}
	return filename
}

// runHTTPDownload performs an HTTP/HTTPS download.
func (e *Engine) runHTTPDownload(ctx context.Context, rg *requestGroup, uri, outPath string) {
	driver, err := e.httpDriverForURI(rg, uri)
	if err != nil {
		rg.errCode = protocolErrorCode(err)
		rg.errMsg = err.Error()
		return
	}
	size := rg.probedSize
	acceptsRanges := rg.acceptsRanges
	cdFilename := rg.cdFilename
	lastModified := rg.lastModified
	requestOpts := e.httpRequestOptions(rg, outPath)
	if !rg.probed {
		info, err := e.probeHTTPInfoWithRetry(ctx, rg, driver, uri, outPath)
		if errors.Is(err, httpproto.ErrNotModified) {
			e.completeHTTPNotModified(rg, outPath)
			return
		}
		if err != nil {
			e.log.Error("HTTP probe failed", "gid", rg.gid, "uri", uri, "error", err)
			rg.errCode = protocolErrorCode(err)
			rg.errMsg = err.Error()
			return
		}
		size = info.Size
		acceptsRanges = info.AcceptsRanges
		cdFilename = info.ContentDispositionFilename
		lastModified = info.LastModified
	}
	if cdFilename != "" {
		e.log.Info("Content-Disposition filename", "gid", rg.gid, "filename", cdFilename)
	}

	rg.totalLength = size
	rg.lastSpeedSample = time.Now()
	e.initControlInfo(rg, outPath, size, 0, nil)
	existingSize := int64(0)
	if controlOffset := e.controlResumeOffset(rg, outPath); controlOffset > 0 && acceptsRanges {
		existingSize = controlOffset
		if size > 0 && existingSize >= size {
			rg.completedLength = size
			rg.errCode = core.ExitSuccess
			e.applyHTTPRemoteTime(rg, outPath, lastModified)
			e.log.Info("download already complete", "gid", rg.gid, "size", size)
			return
		}
	} else if rg.opts.Continue && acceptsRanges {
		if st, statErr := os.Stat(outPath); statErr == nil && !st.IsDir() {
			existingSize = st.Size()
			if size > 0 && existingSize >= size {
				rg.completedLength = size
				rg.errCode = core.ExitSuccess
				e.applyHTTPRemoteTime(rg, outPath, lastModified)
				e.log.Info("download already complete", "gid", rg.gid, "size", size)
				return
			}
		} else if statErr != nil && !os.IsNotExist(statErr) {
			rg.errCode = core.ExitFileIOError
			rg.errMsg = statErr.Error()
			return
		}
	}
	if rg.opts.AllowOverwrite && !rg.opts.Continue && !e.controlLoaded(rg) {
		if err := os.Truncate(outPath, 0); err != nil && !os.IsNotExist(err) {
			rg.errCode = core.ExitFileIOError
			rg.errMsg = err.Error()
			return
		}
	}

	var alloc disk.Allocator = disk.AllocatorNone{}
	if rg.opts.FileAllocation == "trunc" {
		alloc = disk.AllocatorTrunc{}
	}

	adaptor, err := disk.NewSingleFile(outPath, size, alloc)
	if err != nil {
		e.log.Error("cannot create output file", "gid", rg.gid, "path", outPath, "error", err)
		rg.errCode = core.ExitFileCreateError
		rg.errMsg = err.Error()
		return
	}
	e.setControlAdaptor(rg, adaptor)
	defer adaptor.Close()
	defer e.syncControlAdaptor(rg)

	split := rg.opts.Split
	if split < 1 {
		split = 1
	}
	if acceptsRanges && size > 0 && split > 1 {
		minSplitSize := parseSize(rg.opts.MinSplitSize)
		baseCompleted := int64(0)
		segMan := e.controlSegmentMan(rg)
		if segMan != nil {
			baseCompleted = e.controlCompletedLength(rg)
		} else {
			segMan = NewSegmentManWithSplit(size, split, minSplitSize)
		}
		if existingSize > 0 && !e.controlLoaded(rg) {
			segMan = newResumeSegmentMan(size, split, minSplitSize, existingSize)
		}
		if segMan.Done() {
			rg.completedLength = size
			rg.errCode = core.ExitSuccess
			e.applyHTTPRemoteTime(rg, outPath, lastModified)
			e.log.Info("download already complete", "gid", rg.gid, "size", size)
			return
		}
		rg.numConnections = split

		segmentCtx, segmentCancel := context.WithCancel(ctx)
		defer segmentCancel()

		var segMu sync.Mutex
		firstErr := error(nil)

		var wg sync.WaitGroup
		for i := 0; i < split; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					seg := segMan.Next()
					if seg == nil {
						if segMan.Done() {
							return
						}
						select {
						case <-segmentCtx.Done():
							return
						case <-time.After(200 * time.Millisecond):
						}
						continue
					}

					segSize := int64(0)
					if seg.End != -1 {
						segSize = seg.End - seg.Start
					}

					body, err := driver.Download(segmentCtx, uri, seg.Start, segSize)
					if err != nil {
						e.log.Error("HTTP segment download failed", "gid", rg.gid, "start", seg.Start, "error", err)
						segMu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						segMu.Unlock()
						segmentCancel()
						return
					}

					written := e.downloadToAdaptor(segmentCtx, rg, adaptor, body, seg.Start)
					body.Close()

					if written < 0 {
						segMu.Lock()
						if firstErr == nil {
							firstErr = fmt.Errorf("%s: %s", rg.errCode, rg.errMsg)
						}
						segMu.Unlock()
						segmentCancel()
						return
					}

					segMan.MarkDone(seg.Index, written)
				}
			}()
		}
		wg.Wait()

		if firstErr != nil {
			if errors.Is(firstErr, httpproto.ErrRangeIgnored) {
				if rg.opts.AlwaysResume {
					rg.errCode = core.ExitFileAlreadyExists
					rg.errMsg = firstErr.Error()
					return
				}
				e.log.Warn("HTTP server ignored resume range; restarting from scratch", "gid", rg.gid, "path", outPath)
				if closeErr := adaptor.Close(); closeErr != nil {
					rg.errCode = core.ExitFileIOError
					rg.errMsg = closeErr.Error()
					return
				}
				if truncErr := os.Truncate(outPath, 0); truncErr != nil {
					rg.errCode = core.ExitFileIOError
					rg.errMsg = truncErr.Error()
					return
				}
				oldContinue, oldSplit := rg.opts.Continue, rg.opts.Split
				rg.opts.Continue = false
				rg.opts.Split = 1
				e.runHTTPDownload(ctx, rg, uri, outPath)
				rg.opts.Continue, rg.opts.Split = oldContinue, oldSplit
				return
			}
			rg.errCode = protocolErrorCode(firstErr)
			rg.errMsg = firstErr.Error()
			return
		}

		if err := adaptor.Sync(); err != nil {
			e.log.Error("file sync failed", "gid", rg.gid, "error", err)
			rg.errCode = core.ExitFileIOError
			rg.errMsg = err.Error()
			return
		}

		rg.completedLength = baseCompleted + segMan.Written()
		if size > 0 && rg.completedLength > size {
			rg.completedLength = size
		}
		rg.errCode = core.ExitSuccess
		rg.numConnections = len(segMan.segments)
		e.applyHTTPRemoteTime(rg, outPath, lastModified)
		e.log.Info("download complete", "gid", rg.gid, "size", rg.completedLength)
		return
	}

	// Single-connection path (split <= 1 or unknown size).
	rg.numConnections = 1

	offset := existingSize
	requestSize := int64(0)
	if size > 0 && offset > 0 {
		requestSize = size - offset
	}
	body, err := driver.DownloadWithOptions(ctx, uri, offset, requestSize, requestOpts)
	if errors.Is(err, httpproto.ErrRangeIgnored) && offset > 0 {
		if rg.opts.AlwaysResume {
			rg.errCode = core.ExitFileAlreadyExists
			rg.errMsg = err.Error()
			return
		}
		e.log.Warn("HTTP server ignored resume range; restarting from scratch", "gid", rg.gid, "path", outPath)
		if closeErr := adaptor.Close(); closeErr != nil {
			rg.errCode = core.ExitFileIOError
			rg.errMsg = closeErr.Error()
			return
		}
		if truncErr := os.Truncate(outPath, 0); truncErr != nil {
			rg.errCode = core.ExitFileIOError
			rg.errMsg = truncErr.Error()
			return
		}
		adaptor, err = disk.NewSingleFile(outPath, size, alloc)
		if err != nil {
			e.log.Error("cannot recreate output file", "gid", rg.gid, "path", outPath, "error", err)
			rg.errCode = core.ExitFileCreateError
			rg.errMsg = err.Error()
			return
		}
		e.setControlAdaptor(rg, adaptor)
		defer adaptor.Close()
		defer e.syncControlAdaptor(rg)
		offset = 0
		requestSize = 0
		body, err = driver.DownloadWithOptions(ctx, uri, offset, requestSize, requestOpts)
	}
	if errors.Is(err, httpproto.ErrNotModified) {
		e.completeHTTPNotModified(rg, outPath)
		return
	}
	if err != nil {
		e.log.Error("HTTP download failed", "gid", rg.gid, "uri", uri, "error", err)
		rg.errCode = protocolErrorCode(err)
		rg.errMsg = err.Error()
		return
	}
	defer body.Close()

	written := e.downloadToAdaptor(ctx, rg, adaptor, body, offset)
	if written < 0 {
		return
	}

	if err := adaptor.Sync(); err != nil {
		e.log.Error("file sync failed", "gid", rg.gid, "error", err)
		rg.errCode = core.ExitFileIOError
		rg.errMsg = err.Error()
		return
	}

	rg.completedLength = offset + written
	rg.errCode = core.ExitSuccess
	e.applyHTTPRemoteTime(rg, outPath, lastModified)
	e.log.Info("download complete", "gid", rg.gid, "size", rg.completedLength)
}

func (e *Engine) applyHTTPRemoteTime(rg *requestGroup, outPath string, lastModified time.Time) {
	if rg == nil || rg.opts == nil || !rg.opts.RemoteTime || outPath == "" || lastModified.IsZero() {
		return
	}
	if err := os.Chtimes(outPath, time.Now(), lastModified); err != nil {
		e.log.Warn("failed to apply Last-Modified time", "gid", rg.gid, "path", outPath, "error", err)
	}
}

func newResumeSegmentMan(totalSize int64, split int, minSplitSize, completed int64) *SegmentMan {
	if split <= 1 || completed <= 0 || totalSize <= 0 {
		return NewSegmentManWithSplit(totalSize, split, minSplitSize)
	}
	if completed > totalSize {
		completed = totalSize
	}
	segments := []*Segment{{
		Index:   0,
		Start:   0,
		End:     completed,
		Written: completed,
		Done:    true,
	}}
	remaining := totalSize - completed
	if remaining <= 0 {
		return &SegmentMan{segments: segments, totalSize: totalSize, minSplitSize: minSplitSize}
	}
	remainingSegments := split - 1
	if int64(remainingSegments) > remaining {
		remainingSegments = int(remaining)
	}
	chunkSize := remaining / int64(remainingSegments)
	start := completed
	for i := 0; i < remainingSegments; i++ {
		end := start + chunkSize
		if i == remainingSegments-1 {
			end = totalSize
		}
		segments = append(segments, &Segment{Index: len(segments), Start: start, End: end})
		start = end
	}
	return &SegmentMan{segments: segments, totalSize: totalSize, minSplitSize: minSplitSize}
}

// downloadToAdaptor reads from body and writes to adaptor at the given startOffset.
// Returns the number of bytes written, or -1 on error (caller already set rg.errCode).
func (e *Engine) downloadToAdaptor(ctx context.Context, rg *requestGroup, adaptor disk.Adaptor, body io.Reader, startOffset int64) int64 {
	buf := make([]byte, 64*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			rg.errCode = core.ExitRemoved
			rg.errMsg = "download cancelled"
			return -1
		default:
		}
		n, readErr := body.Read(buf)
		if n > 0 {
			if err := e.rateGlobal.Wait(ctx, n); err != nil {
				rg.errCode = core.ExitRemoved
				rg.errMsg = "download cancelled"
				return -1
			}
			if rg.downloadLimit != nil {
				if err := rg.downloadLimit.Wait(ctx, n); err != nil {
					rg.errCode = core.ExitRemoved
					rg.errMsg = "download cancelled"
					return -1
				}
			}
			writeOffset := startOffset + written
			wn, writeErr := adaptor.WriteAt(buf[:n], writeOffset)
			if writeErr != nil {
				e.log.Error("disk write failed", "gid", rg.gid, "error", writeErr)
				rg.errCode = core.ExitFileIOError
				rg.errMsg = writeErr.Error()
				return -1
			}
			written += int64(wn)
			e.markControlWritten(rg, writeOffset, int64(wn))
			atomic.AddInt64(&rg.bytesDownloaded, int64(wn))
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			if errors.Is(readErr, context.Canceled) {
				rg.errCode = core.ExitRemoved
				rg.errMsg = "download cancelled"
				return -1
			}
			e.log.Error("read failed", "gid", rg.gid, "error", readErr)
			rg.errCode = protocolErrorCode(readErr)
			rg.errMsg = readErr.Error()
			return -1
		}
	}
	return written
}

// runFTPDownload performs an FTP download.
func (e *Engine) runFTPDownload(ctx context.Context, rg *requestGroup, uri string, u *url.URL, outPath string) {
	host := u.Host
	if u.Port() == "" {
		host = netJoinHostPort(u.Host, "21")
	}

	ftpUser, ftpPass := ftpCredentials(u, rg.opts, e.cfg)

	dialer, err := e.dialerForProtocol("ftp", rg.opts)
	if err != nil {
		e.log.Error("FTP proxy setup failed", "gid", rg.gid, "error", err)
		rg.errCode = core.ExitFTPProtocolError
		rg.errMsg = err.Error()
		return
	}
	conn, err := ftpproto.Dial(ctx, dialer, host, ftpproto.Opt{
		User:    ftpUser,
		Pass:    ftpPass,
		Passive: rg.opts.FTPPasv,
	})
	if err != nil {
		e.log.Error("FTP dial failed", "gid", rg.gid, "host", host, "error", err)
		rg.errCode = core.ExitFTPProtocolError
		rg.errMsg = err.Error()
		return
	}
	defer conn.Close()

	size, err := conn.Size(ctx, u.Path)
	if err != nil {
		e.log.Error("FTP SIZE failed", "gid", rg.gid, "path", u.Path, "error", err)
		rg.errCode = core.ExitFTPProtocolError
		rg.errMsg = err.Error()
		return
	}

	rg.totalLength = size
	rg.lastSpeedSample = time.Now()
	e.initControlInfo(rg, outPath, size, 0, nil)
	offset := e.controlResumeOffset(rg, outPath)
	if offset == 0 && rg.opts.Continue {
		if st, statErr := os.Stat(outPath); statErr == nil && !st.IsDir() {
			offset = st.Size()
			if size > 0 && offset >= size {
				rg.completedLength = size
				rg.errCode = core.ExitSuccess
				return
			}
		} else if statErr != nil && !os.IsNotExist(statErr) {
			rg.errCode = core.ExitFileIOError
			rg.errMsg = statErr.Error()
			return
		}
	}

	var alloc disk.Allocator = disk.AllocatorNone{}
	if rg.opts.FileAllocation == "trunc" {
		alloc = disk.AllocatorTrunc{}
	}

	adaptor, err := disk.NewSingleFile(outPath, size, alloc)
	if err != nil {
		e.log.Error("cannot create output file", "gid", rg.gid, "path", outPath, "error", err)
		rg.errCode = core.ExitFileCreateError
		rg.errMsg = err.Error()
		return
	}
	e.setControlAdaptor(rg, adaptor)
	defer adaptor.Close()
	defer e.syncControlAdaptor(rg)

	// Single-connection path.
	rg.numConnections = 1

	body, err := conn.Retrieve(ctx, u.Path, offset)
	if err != nil {
		e.log.Error("FTP RETR failed", "gid", rg.gid, "path", u.Path, "error", err)
		rg.errCode = core.ExitFTPProtocolError
		rg.errMsg = err.Error()
		return
	}
	bodyClosed := false
	defer func() {
		if !bodyClosed {
			_ = body.Close()
		}
	}()

	written := e.downloadToAdaptor(ctx, rg, adaptor, body, offset)
	if written < 0 {
		return
	}
	bodyClosed = true
	if err := body.Close(); err != nil {
		e.log.Error("FTP body close failed", "gid", rg.gid, "path", u.Path, "error", err)
		rg.errCode = core.ExitFTPProtocolError
		rg.errMsg = err.Error()
		return
	}

	if err := adaptor.Sync(); err != nil {
		e.log.Error("file sync failed", "gid", rg.gid, "error", err)
		rg.errCode = core.ExitFileIOError
		rg.errMsg = err.Error()
		return
	}

	rg.completedLength = offset + written
	rg.errCode = core.ExitSuccess
	e.log.Info("download complete", "gid", rg.gid, "size", rg.completedLength)
}

// runSFTPDownload performs an SFTP download.
func (e *Engine) runSFTPDownload(ctx context.Context, rg *requestGroup, uri string, u *url.URL, outPath string) {
	host := u.Hostname()
	port := 22
	if p := u.Port(); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			port = v
		}
	}

	user := u.User.Username()
	pass, _ := u.User.Password()
	if user == "" {
		user = rg.opts.FTPUser
		if user == "" {
			user = e.cfg.FTPUser
		}
	}
	if pass == "" {
		pass = rg.opts.FTPPasswd
		if pass == "" {
			pass = e.cfg.FTPPasswd
		}
	}

	dialer, err := e.dialerForProtocol("sftp", rg.opts)
	if err != nil {
		e.log.Error("SFTP proxy setup failed", "gid", rg.gid, "error", err)
		rg.errCode = core.ExitFTPProtocolError
		rg.errMsg = err.Error()
		return
	}
	sess, err := sftpproto.Open(ctx, dialer, sftpproto.Opts{
		Host:      host,
		Port:      port,
		User:      user,
		Auth:      sftpproto.AuthMethods{Password: pass},
		HostKeyMD: rg.opts.SSHHostKeyMD,
	})
	if err != nil {
		e.log.Error("SFTP open failed", "gid", rg.gid, "host", host, "error", err)
		if sftpproto.IsHostKeyDigestError(err) {
			rg.errCode = core.ExitUnknownError
		} else {
			rg.errCode = core.ExitFTPProtocolError
		}
		rg.errMsg = err.Error()
		return
	}
	defer sess.Close()

	info, err := sess.Stat(ctx, u.Path)
	if err != nil {
		e.log.Error("SFTP stat failed", "gid", rg.gid, "path", u.Path, "error", err)
		rg.errCode = core.ExitFTPProtocolError
		rg.errMsg = err.Error()
		return
	}

	size := info.Size
	rg.totalLength = size
	rg.lastSpeedSample = time.Now()
	e.initControlInfo(rg, outPath, size, 0, nil)
	offset := e.controlResumeOffset(rg, outPath)
	if offset == 0 && rg.opts.Continue {
		if st, statErr := os.Stat(outPath); statErr == nil && !st.IsDir() {
			offset = st.Size()
			if size > 0 && offset >= size {
				rg.completedLength = size
				rg.errCode = core.ExitSuccess
				return
			}
		} else if statErr != nil && !os.IsNotExist(statErr) {
			rg.errCode = core.ExitFileIOError
			rg.errMsg = statErr.Error()
			return
		}
	}

	var alloc disk.Allocator = disk.AllocatorNone{}
	if rg.opts.FileAllocation == "trunc" {
		alloc = disk.AllocatorTrunc{}
	}

	adaptor, err := disk.NewSingleFile(outPath, size, alloc)
	if err != nil {
		e.log.Error("cannot create output file", "gid", rg.gid, "path", outPath, "error", err)
		rg.errCode = core.ExitFileCreateError
		rg.errMsg = err.Error()
		return
	}
	e.setControlAdaptor(rg, adaptor)
	defer adaptor.Close()
	defer e.syncControlAdaptor(rg)

	reader, err := sess.OpenFile(ctx, u.Path, offset)
	if err != nil {
		e.log.Error("SFTP open file failed", "gid", rg.gid, "path", u.Path, "error", err)
		rg.errCode = core.ExitFTPProtocolError
		rg.errMsg = err.Error()
		return
	}
	readerClosed := false
	defer func() {
		if !readerClosed {
			_ = reader.Close()
		}
	}()

	rg.numConnections = 1
	written := e.downloadToAdaptor(ctx, rg, adaptor, reader, offset)
	if written < 0 {
		return
	}
	readerClosed = true
	if err := reader.Close(); err != nil {
		e.log.Error("SFTP reader close failed", "gid", rg.gid, "path", u.Path, "error", err)
		rg.errCode = core.ExitFTPProtocolError
		rg.errMsg = err.Error()
		return
	}

	if err := adaptor.Sync(); err != nil {
		e.log.Error("file sync failed", "gid", rg.gid, "error", err)
		rg.errCode = core.ExitFileIOError
		rg.errMsg = err.Error()
		return
	}

	rg.completedLength = offset + written
	rg.errCode = core.ExitSuccess
	e.log.Info("download complete", "gid", rg.gid, "size", rg.completedLength)
}

// runMetalinkDownload parses metalink data and downloads files by iterating
// over file entries and trying each URL in priority order.
func (e *Engine) runMetalinkDownload(ctx context.Context, rg *requestGroup, metalinkData []byte) {
	e.log.Info("starting metalink download", "gid", rg.gid)

	entries, err := metalinkDownloadEntries(metalinkData, rg.opts)
	if err != nil {
		e.log.Error("cannot parse metalink", "gid", rg.gid, "error", err)
		rg.errCode = core.ExitResourceNotFound
		rg.errMsg = fmt.Sprintf("invalid metalink: %v", err)
		return
	}
	if len(entries) == 0 && len(rg.uris) > 0 {
		for _, uri := range rg.uris {
			entries = append(entries, metalinkDownloadEntry{URI: uri})
		}
	}
	if len(entries) == 0 {
		e.log.Error("metalink download has no fallback URIs", "gid", rg.gid)
		rg.errCode = core.ExitResourceNotFound
		rg.errMsg = "no URIs in metalink"
		return
	}

	entry := entries[0]
	if rg.filePath == "" && entry.Name != "" {
		rg.filePath = filepath.Join(rg.opts.Dir, entry.Name)
	}
	if entry.SizeKnown {
		rg.totalLength = entry.Size
	}
	uri := entry.URI
	u, errParse := url.Parse(uri)
	if errParse != nil {
		e.log.Error("cannot parse URI", "gid", rg.gid, "uri", uri, "error", errParse)
		rg.errCode = core.ExitResourceNotFound
		rg.errMsg = fmt.Sprintf("invalid URI: %s", uri)
		return
	}

	proto := strings.ToLower(u.Scheme)
	switch proto {
	case "http", "https":
		e.runHTTPDownload(ctx, rg, uri, rg.filePath)
	case "ftp":
		e.runFTPDownload(ctx, rg, uri, u, rg.filePath)
	case "sftp":
		e.runSFTPDownload(ctx, rg, uri, u, rg.filePath)
	default:
		e.log.Error("unsupported protocol in metalink", "gid", rg.gid, "protocol", proto)
		rg.errCode = core.ExitResourceNotFound
		rg.errMsg = fmt.Sprintf("unsupported protocol: %s", proto)
	}
}

type metalinkDownloadEntry struct {
	URI           string
	Name          string
	Size          int64
	SizeKnown     bool
	Hashes        map[hash.Kind][]byte
	Pieces        [][]byte
	PieceHashKind hash.Kind
	PieceLength   int64
	Version       string
	Languages     []string
	OSes          []string
}

func metalinkDownloadEntries(data []byte, opts *config.Options) ([]metalinkDownloadEntry, error) {
	doc, err := metalink.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	preferred := ""
	if opts != nil {
		preferred = opts.MetalinkPreferredProtocol
	}
	var entries []metalinkDownloadEntry
	for _, f := range doc.Files {
		urls := append([]metalink.URLEntry(nil), f.URLs...)
		sort.SliceStable(urls, func(i, j int) bool {
			pi := metalinkProtocolRank(urls[i].Type, preferred)
			pj := metalinkProtocolRank(urls[j].Type, preferred)
			if pi != pj {
				return pi < pj
			}
			return urls[i].Priority < urls[j].Priority
		})
		for _, u := range urls {
			if u.URL == "" {
				continue
			}
			entries = append(entries, metalinkDownloadEntry{
				URI:           u.URL,
				Name:          f.Name,
				Size:          f.Size,
				SizeKnown:     f.SizeKnown,
				Hashes:        cloneMetalinkHashes(f.Hashes),
				Pieces:        cloneByteSlices(f.Pieces),
				PieceHashKind: f.PieceHashKind,
				PieceLength:   f.PieceLength,
				Version:       f.Version,
				Languages:     append([]string(nil), f.Languages...),
				OSes:          append([]string(nil), f.OSes...),
			})
		}
	}
	return entries, nil
}

func cloneMetalinkHashes(src map[hash.Kind][]byte) map[hash.Kind][]byte {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[hash.Kind][]byte, len(src))
	for k, v := range src {
		dst[k] = append([]byte(nil), v...)
	}
	return dst
}

func cloneByteSlices(src [][]byte) [][]byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([][]byte, len(src))
	for i := range src {
		dst[i] = append([]byte(nil), src[i]...)
	}
	return dst
}

func metalinkProtocolRank(proto, preferred string) int {
	if preferred == "" || preferred == "none" {
		return 1
	}
	if strings.EqualFold(proto, preferred) {
		return 0
	}
	return 1
}

func ftpCredentials(u *url.URL, opts, global *config.Options) (string, string) {
	user := u.User.Username()
	pass, _ := u.User.Password()
	if user == "" && opts != nil {
		user = opts.FTPUser
	}
	if user == "" && global != nil {
		user = global.FTPUser
	}
	if pass == "" && opts != nil {
		pass = opts.FTPPasswd
	}
	if pass == "" && global != nil {
		pass = global.FTPPasswd
	}
	return user, pass
}

func (e *Engine) dialerForProtocol(proto string, opts *config.Options) (*netx.Dialer, error) {
	if opts == nil {
		opts = e.cfg
	}
	proxyURI := resolveProxyURI(proto, opts)
	if proxyURI == "" {
		return e.netDialer, nil
	}
	cfg := engineDialerConfig(opts)
	cfg.ProxyURL = proxyURI
	return netx.NewDialer(cfg)
}

// downloadFTPSegment performs a ranged FTP download segment. It opens a new
// FTP connection, retrieves the given byte range, and writes to the adaptor.
func (e *Engine) downloadFTPSegment(ctx context.Context, host, path, user, pass string, pasv bool, rg *requestGroup, adaptor disk.Adaptor, seg *Segment) (int64, error) {
	dialer, err := e.dialerForProtocol("ftp", rg.opts)
	if err != nil {
		return -1, err
	}
	conn, err := ftpproto.Dial(ctx, dialer, host, ftpproto.Opt{
		User:    user,
		Pass:    pass,
		Passive: pasv,
	})
	if err != nil {
		return -1, err
	}
	defer conn.Close()

	body, err := conn.Retrieve(ctx, path, seg.Start)
	if err != nil {
		return -1, err
	}
	bodyToClose := body
	bodyClosed := false
	defer func() {
		if !bodyClosed {
			_ = bodyToClose.Close()
		}
	}()

	if seg.End != -1 {
		body = io.NopCloser(io.LimitReader(body, seg.End-seg.Start))
	}

	written := e.downloadToAdaptor(ctx, rg, adaptor, body, seg.Start)
	if written < 0 {
		return -1, fmt.Errorf("%s: %s", rg.errCode, rg.errMsg)
	}
	if !bodyClosed {
		bodyClosed = true
		if err := bodyToClose.Close(); err != nil {
			return -1, err
		}
	}
	return written, nil
}

// boolPtr returns a pointer to b.
func boolPtr(b bool) *bool { return &b }

// parseInt parses a string to int, returning 0 on error.
func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// parseDHTPort parses the DHT listen port string to an int, defaulting to 6881.
func parseDHTPort(s string) int {
	if s == "" {
		return 6881
	}
	parts := strings.SplitN(s, "-", 2)
	n, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	if n <= 0 {
		return 6881
	}
	return n
}

// parseListenPort parses the listen port range string to the first port.
func parseListenPort(s string) int {
	if s == "" {
		return 6881
	}
	parts := strings.SplitN(s, "-", 2)
	n, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	if n <= 0 {
		return 6881
	}
	return n
}

// runHookExec fires a hook command with the standard aria2 arguments:
// command, GID (hex), numFiles (decimal), firstFilename.
func (e *Engine) runHookExec(rg *requestGroup, numFiles int, command string) {
	if command == "" {
		return
	}
	e.log.Debug("executing hook", "command", command, "gid", rg.gid)
	if err := hookrunner.Run(e.ctx, command, rg.gid.Hex(), strconv.Itoa(numFiles), rg.getFirstFilePath()); err != nil {
		e.log.Error("hook execution failed", "command", command, "error", err)
	}
}

// runHookByName looks up the hook command from engine config by option name
// and fires it. numFiles defaults to 1; BT downloads should pass the actual count.
func (e *Engine) runHookByName(rg *requestGroup, numFiles int, hookName string) {
	var command string
	switch hookName {
	case "on-download-start":
		command = e.cfg.OnDownloadStart
	case "on-download-pause":
		command = e.cfg.OnDownloadPause
	case "on-download-stop":
		command = e.cfg.OnDownloadStop
	case "on-download-complete":
		command = e.cfg.OnDownloadComplete
	case "on-download-error":
		command = e.cfg.OnDownloadError
	case "on-bt-download-complete":
		command = e.cfg.OnBTDownloadComplete
	}
	e.runHookExec(rg, numFiles, command)
}

// runStopHook fires the appropriate stop-completion hook for a download that
// transitioned to the stopped/results queue. Priority matches aria2's
// executeStopHook: if result is FINISHED and on-download-complete is set,
// use complete; else if error (not IN_PROGRESS/REMOVED) and on-download-error
// is set, use error; otherwise fall back to on-download-stop.
func (e *Engine) runStopHook(rg *requestGroup, errCode core.ErrorCode) {
	hookName := ""
	if e.cfg.OnDownloadStop != "" {
		hookName = "on-download-stop"
	}
	if errCode == core.ExitSuccess && e.cfg.OnDownloadComplete != "" {
		hookName = "on-download-complete"
	} else if errCode != core.ExitSuccess && errCode != core.ExitRemoved && e.cfg.OnDownloadError != "" {
		hookName = "on-download-error"
	}
	e.runHookByName(rg, 1, hookName)
}

// protocolErrorCode maps protocol-level errors to aria2 error codes.
func protocolErrorCode(err error) core.ErrorCode {
	if err == nil {
		return core.ExitUnknownError
	}
	if errors.Is(err, context.Canceled) {
		return core.ExitRemoved
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return core.ExitTimeout
	}
	return core.CodeFrom(err)
}

// netJoinHostPort is a local copy of net.JoinHostPort to avoid import cycle.
func netJoinHostPort(host, port string) string {
	if strings.IndexByte(host, ':') >= 0 {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

func defaultOutputPathFromURI(rawURI string) string {
	u, err := url.Parse(rawURI)
	if err == nil && u.Path != "" {
		base := filepath.Base(u.Path)
		if base != "" && base != "." && base != "/" {
			return base
		}
	}
	base := filepath.Base(rawURI)
	if base == "" || base == "." || base == "/" {
		return "index.html"
	}
	return base
}

// getFirstFilePath returns the first file path for this download.
// For in-memory downloads, returns "[MEMORY]<basename>" (matching aria2's
// RequestGroup::getFirstFilePath which prepends "[MEMORY]" with basename).
// For regular downloads, returns the full file path.
func (rg *requestGroup) getFirstFilePath() string {
	if rg.inMemory {
		return "[MEMORY]" + filepath.Base(rg.filePath)
	}
	return rg.filePath
}

func resultFilePath(rg *requestGroup) string {
	if rg == nil {
		return ""
	}
	path := rg.getFirstFilePath()
	if path != "" {
		return path
	}
	if len(rg.uris) > 0 {
		return rg.uris[0]
	}
	return ""
}

// tryAutoFileRenaming generates the auto-renamed path for a file that would
// conflict with an existing file. Mirrors aria2's RequestGroup::tryAutoFileRenaming:
// it extracts the extension from the path (respecting dotfile and path boundary
// rules), then appends a ".1" suffix before the extension.
//
// The extension extraction logic:
//   - foo.txt      → name=foo,      ext=.txt  → foo.1.txt
//   - .dotfile     → name=.dotfile,  ext=      → .dotfile.1
//   - dir/.hidden  → name=dir/.hidden, ext=    → dir/.hidden.1
//   - foo.tar.gz   → name=foo.tar,  ext=.gz   → foo.tar.1.gz
//
// In aria2, the actual suffix number (1..9999) is incremented until a non-conflicting
// path is found. This function always returns suffix 1; callers must loop to find
// a non-conflicting path in their filesystem context.
func tryAutoFileRenaming(fileName string) string {
	if fileName == "" {
		return fileName
	}
	fn := fileName
	ext := ""
	idx := strings.LastIndexByte(fn, '.')
	slash := strings.LastIndexByte(fn, '/')
	if bsl := strings.LastIndexByte(fn, '\\'); bsl > slash {
		slash = bsl
	}

	if idx != -1 &&
		idx != 0 &&
		(slash == -1 || slash < idx-1) {
		ext = fn[idx:]
		fn = fn[:idx]
	}
	var b strings.Builder
	b.Grow(len(fn) + len(ext) + 4)
	b.WriteString(fn)
	b.WriteString(".1")
	b.WriteString(ext)
	return b.String()
}

// tryAutoFileRenamingWithSuffix generates the auto-renamed path for a file with a specific suffix counter.
func tryAutoFileRenamingWithSuffix(fileName string, suffix int) string {
	if fileName == "" {
		return fileName
	}
	fn := fileName
	ext := ""
	idx := strings.LastIndexByte(fn, '.')
	slash := strings.LastIndexByte(fn, '/')
	if bsl := strings.LastIndexByte(fn, '\\'); bsl > slash {
		slash = bsl
	}

	if idx != -1 &&
		idx != 0 &&
		(slash == -1 || slash < idx-1) {
		ext = fn[idx:]
		fn = fn[:idx]
	}
	var b strings.Builder
	b.Grow(len(fn) + len(ext) + 12)
	b.WriteString(fn)
	b.WriteString(fmt.Sprintf(".%d", suffix))
	b.WriteString(ext)
	return b.String()
}

// isSameFileBeingDownloadedLocked checks whether any active or waiting download
// is already targeting the given path. Caller must hold e.queuesMu lock.
func (e *Engine) isSameFileBeingDownloadedLocked(path string, excludeGID core.GID) bool {
	path = normalizedFileIdentity(path)
	for _, gid := range e.active {
		if gid == excludeGID {
			continue
		}
		rg, ok := e.groups.get(gid)
		if !ok {
			continue
		}
		if rg.filePath != "" && normalizedFileIdentity(rg.filePath) == path {
			return true
		}
	}
	for _, gid := range e.waiting {
		if gid == excludeGID {
			continue
		}
		rg, ok := e.groups.get(gid)
		if !ok {
			continue
		}
		if rg.filePath != "" && normalizedFileIdentity(rg.filePath) == path {
			return true
		}
	}
	return false
}

func normalizedFileIdentity(path string) string {
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}

// isSameFileBeingDownloaded checks whether any active or waiting download
// is already targeting the given path. Mirrors aria2's RequestGroupMan::
// isSameFileBeingDownloaded which collects all FileEntry paths from all
// request groups (excluding the one being checked) and checks for overlap.
func (e *Engine) isSameFileBeingDownloaded(path string, excludeGID core.GID) bool {
	e.queuesMu.Lock()
	defer e.queuesMu.Unlock()
	return e.isSameFileBeingDownloadedLocked(path, excludeGID)
}

// makeProxyURI parses and reconstructs a proxy URI, merging in optional
// username and password. If the proxy URI is empty or fails to parse,
// returns an empty string. Mirrors aria2's makeProxyUri in AbstractCommand.cc.
func makeProxyURI(proxyURI, user, passwd string) string {
	if proxyURI == "" {
		return ""
	}
	schemeEnd := strings.Index(proxyURI, "://")
	if schemeEnd < 0 || schemeEnd == 0 {
		return ""
	}
	hostStart := schemeEnd + 3
	if hostStart >= len(proxyURI) {
		return ""
	}
	if user == "" {
		return proxyURI
	}
	var b strings.Builder
	b.Grow(len(proxyURI) + len(user) + len(passwd) + 2)
	b.WriteString(proxyURI[:hostStart])
	b.WriteString(user)
	if passwd != "" {
		b.WriteByte(':')
		b.WriteString(passwd)
	}
	b.WriteByte('@')
	b.WriteString(proxyURI[hostStart:])
	return b.String()
}

// resolveProxyURI returns the proxy URI for the given protocol and options.
// It first checks the protocol-specific proxy option (e.g. http-proxy for "http").
// If that is empty or unparseable, falls back to the all-proxy option.
// Returns an empty string if no proxy is configured.
//
// Mirrors aria2's getProxyUri + getProxyOptionFor in AbstractCommand.cc,
// which dispatches on protocol ("http", "https", "ftp"/"sftp") and falls back
// to PREF_ALL_PROXY / PREF_ALL_PROXY_USER / PREF_ALL_PROXY_PASSWD.
func resolveProxyURI(proto string, opts *config.Options) string {
	switch proto {
	case "http":
		if uri := makeProxyURI(opts.HTTPProxy, opts.HTTPProxyUser, opts.HTTPProxyPasswd); uri != "" {
			return uri
		}
		return makeProxyURI(opts.AllProxy, opts.AllProxyUser, opts.AllProxyPasswd)
	case "https":
		if uri := makeProxyURI(opts.HTTPSProxy, opts.HTTPSProxyUser, opts.HTTPSProxyPasswd); uri != "" {
			return uri
		}
		return makeProxyURI(opts.AllProxy, opts.AllProxyUser, opts.AllProxyPasswd)
	case "ftp", "sftp":
		if uri := makeProxyURI(opts.FTPProxy, opts.FTPProxyUser, opts.FTPProxyPasswd); uri != "" {
			return uri
		}
		return makeProxyURI(opts.AllProxy, opts.AllProxyUser, opts.AllProxyPasswd)
	default:
		return ""
	}
}

// GlobalStat holds aggregate engine statistics matching aria2's getGlobalStat
// RPC response (downloadSpeed, uploadSpeed, numActive, numWaiting, numStopped,
// numStoppedTotal).
type GlobalStat struct {
	DownloadSpeed   int64
	UploadSpeed     int64
	NumActive       int
	NumWaiting      int
	NumStopped      int
	NumStoppedTotal int64
}

// GetGlobalStat returns aggregate statistics about the engine's current state,
// matching aria2's GetGlobalStatRpcMethod which calls RequestGroupMan::calculateStat
// plus getReservedGroups().size(), getDownloadResults().size(), and getNumStoppedTotal().
func (e *Engine) GetGlobalStat() GlobalStat {
	e.queuesMu.Lock()
	activeCopy := make([]core.GID, len(e.active))
	copy(activeCopy, e.active)
	waitingCopy := make([]core.GID, len(e.waiting))
	copy(waitingCopy, e.waiting)
	e.queuesMu.Unlock()

	return GlobalStat{
		DownloadSpeed:   e.downloadSpeed.Load(),
		UploadSpeed:     e.uploadSpeed.Load(),
		NumActive:       len(activeCopy),
		NumWaiting:      len(waitingCopy),
		NumStopped:      e.stoppedRing.len(),
		NumStoppedTotal: e.stoppedTotal.Load(),
	}
}

// makeStatus builds a Status snapshot from a requestGroup. Caller must hold the
// shard lock for rg.gid.
func (e *Engine) makeStatus(rg *requestGroup) *Status {
	dir := ""
	if rg.opts != nil {
		dir = rg.opts.Dir
	}
	st := rg.state
	if rg.pauseReq && st == core.StatusWaiting {
		st = core.StatusPaused
	}
	files := e.buildFileStatus(rg)

	return &Status{
		GID:             rg.gid,
		Status:          st,
		Seeder:          rg.seeder,
		ErrorCode:       rg.errCode,
		ErrorMessage:    rg.errMsg,
		Dir:             dir,
		TotalLength:     rg.totalLength,
		CompletedLength: rg.completedLength,
		DownloadSpeed:   rg.downloadSpeed,
		UploadSpeed:     rg.uploadSpeed,
		Connections:     rg.numConnections,
		Files:           files,
		BelongsTo:       rg.belongsTo,
		Following:       rg.following,
		FollowedBy:      rg.followedBy,
	}
}

func (e *Engine) buildFileStatus(rg *requestGroup) []FileStatus {
	if len(rg.uris) == 0 {
		return nil
	}
	path := rg.filePath
	if path == "" {
		path = rg.uris[0]
	}
	uris := make([]URIStatus, len(rg.uris))
	for i, u := range rg.uris {
		uriState := "waiting"
		if rg.state == core.StatusActive && i == 0 {
			uriState = "used"
		}
		uris[i] = URIStatus{URI: u, Status: uriState}
	}
	return []FileStatus{{
		Index:           1,
		Path:            path,
		Length:          rg.totalLength,
		CompletedLength: rg.completedLength,
		Selected:        true,
		URIs:            uris,
	}}
}

// statusToRPC converts a Status snapshot to the aria2 JSON-RPC map format,
// optionally filtering to only the requested keys. If keys is nil or empty
// all fields are included. Mirrors the C++ gatherProgressCommon +
// gatherStoppedDownload in RpcMethodImpl.cc.
func (e *Engine) statusToRPC(s *Status, keys []string) map[string]any {
	m := map[string]any{
		"gid":             s.GID.Hex(),
		"status":          s.Status.String(),
		"totalLength":     fmt.Sprintf("%d", s.TotalLength),
		"completedLength": fmt.Sprintf("%d", s.CompletedLength),
		"uploadLength":    fmt.Sprintf("%d", s.UploadLength),
		"downloadSpeed":   fmt.Sprintf("%d", s.DownloadSpeed),
		"uploadSpeed":     fmt.Sprintf("%d", s.UploadSpeed),
		"connections":     fmt.Sprintf("%d", s.Connections),
		"dir":             s.Dir,
		"pieceLength":     fmt.Sprintf("%d", s.PieceLength),
		"numPieces":       fmt.Sprintf("%d", s.NumPieces),
	}
	if s.VerifiedLength > 0 {
		m["verifiedLength"] = fmt.Sprintf("%d", s.VerifiedLength)
	}
	if s.VerifyIntegrityPending {
		m["verifyIntegrityPending"] = "true"
	}

	switch s.Status {
	case core.StatusComplete, core.StatusError, core.StatusRemoved:
		m["errorCode"] = fmt.Sprintf("%d", s.ErrorCode)
		m["errorMessage"] = s.ErrorMessage
	}
	if s.InfoHash != "" {
		m["infoHash"] = s.InfoHash
	}
	if s.NumSeeders > 0 || s.InfoHash != "" {
		m["numSeeders"] = fmt.Sprintf("%d", s.NumSeeders)
		if s.InfoHash != "" && s.TotalLength > 0 && s.CompletedLength >= s.TotalLength {
			m["seeder"] = "true"
		} else {
			m["seeder"] = "false"
		}
	}
	if s.Bitfield != "" {
		m["bitfield"] = s.Bitfield
	}
	if len(s.FollowedBy) > 0 {
		followed := make([]string, len(s.FollowedBy))
		for i, g := range s.FollowedBy {
			followed[i] = g.Hex()
		}
		m["followedBy"] = followed
	}
	if s.Following != 0 {
		m["following"] = s.Following.Hex()
	}
	if s.BelongsTo != 0 {
		m["belongsTo"] = s.BelongsTo.Hex()
	}
	if s.Bittorrent != nil {
		m["bittorrent"] = s.Bittorrent
	}
	if s.VerifyIntegrityPending {
		m["verifyIntegrityPending"] = "true"
	}

	files := make([]map[string]any, len(s.Files))
	for i, f := range s.Files {
		fm := map[string]any{
			"index":           fmt.Sprintf("%d", f.Index),
			"path":            f.Path,
			"length":          fmt.Sprintf("%d", f.Length),
			"completedLength": fmt.Sprintf("%d", f.CompletedLength),
			"selected":        boolStrVal(f.Selected),
		}
		uris := make([]map[string]any, len(f.URIs))
		for j, u := range f.URIs {
			uris[j] = map[string]any{
				"uri":    u.URI,
				"status": u.Status,
			}
		}
		fm["uris"] = uris
		files[i] = fm
	}
	m["files"] = files

	if len(keys) > 0 {
		filtered := make(map[string]any, len(keys))
		for _, k := range keys {
			if v, ok := m[k]; ok {
				filtered[k] = v
			}
		}
		return filtered
	}

	return m
}

func boolStrVal(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// TellStatusKeys returns the current status of a single download as an aria2
// RPC-compatible map. If keys is non-empty, only the requested keys are included
// (matching aria2's tellStatus keys parameter behavior).
func (e *Engine) TellStatusKeys(gid core.GID, keys []string) (map[string]any, error) {
	rg, ok := e.groups.get(gid)
	if ok {
		return e.statusToRPC(e.makeStatus(rg), keys), nil
	}

	dr, found := e.stoppedRing.getByGID(gid)
	if !found {
		return nil, fmt.Errorf("engine: download GID#%s not found", gid)
	}
	s := &Status{
		GID:          gid,
		Status:       dr.state,
		ErrorCode:    dr.errCode,
		ErrorMessage: dr.errMsg,
		BelongsTo:    dr.belongsTo,
		Following:    dr.following,
		FollowedBy:   dr.followedBy,
	}
	return e.statusToRPC(s, keys), nil
}

// TellActiveKeys returns status maps of all active downloads, filtered to the
// requested keys.
func (e *Engine) TellActiveKeys(keys []string) []map[string]any {
	e.queuesMu.Lock()
	activeCopy := make([]core.GID, len(e.active))
	copy(activeCopy, e.active)
	e.queuesMu.Unlock()

	result := make([]map[string]any, 0, len(activeCopy))
	for _, gid := range activeCopy {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		result = append(result, e.statusToRPC(e.makeStatus(rg), keys))
		e.groups.unlock(gid)
	}
	return result
}

// TellWaitingKeys returns status maps of waiting/paused downloads, starting at
// offset, up to num entries, filtered to the requested keys.
func (e *Engine) TellWaitingKeys(offset, num int, keys []string) []map[string]any {
	e.queuesMu.Lock()
	waitingCopy := make([]core.GID, len(e.waiting))
	copy(waitingCopy, e.waiting)
	e.queuesMu.Unlock()

	if offset >= len(waitingCopy) {
		return nil
	}
	end := offset + num
	if end > len(waitingCopy) {
		end = len(waitingCopy)
	}

	result := make([]map[string]any, 0, end-offset)
	for _, gid := range waitingCopy[offset:end] {
		rg, ok := e.groups.getLocked(gid)
		if !ok {
			continue
		}
		result = append(result, e.statusToRPC(e.makeStatus(rg), keys))
		e.groups.unlock(gid)
	}
	return result
}

// TellStoppedKeys returns status maps of completed/error/removed downloads,
// starting at offset, up to num entries, filtered to the requested keys.
func (e *Engine) TellStoppedKeys(offset, num int, keys []string) []map[string]any {
	statuses := e.stoppedRing.snapshotStatuses(offset, num)
	result := make([]map[string]any, len(statuses))
	for i := range statuses {
		result[i] = e.statusToRPC(&statuses[i], keys)
	}
	return result
}
