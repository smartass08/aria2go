// Package aria2c is the frozen public API for the aria2go download daemon.
// It is the only package consumers import; everything else is internal/.
//
// The Daemon type provides a concurrency-safe interface for adding URI,
// torrent, and metalink downloads, querying aggregate status, and
// controlling the daemon lifecycle.
package aria2c

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/engine"
)

// Daemon encapsulates the download engine and provides a frozen, concurrency-safe
// public API for managing downloads.
type Daemon struct {
	eng     *engine.Engine
	cfg     *config.Options
	rpcAddr string
}

// Config holds configuration for creating a Daemon.
type Config struct {
	// Options is the full aria2 option set. If nil, zero-value defaults are used.
	Options *config.Options
}

// New creates a new Daemon with the given configuration.
// Call Run to start the daemon; until then, downloads can be added to the
// waiting queue but will not begin.
func New(cfg Config) (*Daemon, error) {
	opts := cfg.Options
	if opts == nil {
		opts = &config.Options{}
	}

	log := slog.Default()
	eng, err := engine.New(opts, log)
	if err != nil {
		return nil, fmt.Errorf("aria2c: %w", err)
	}

	return &Daemon{
		eng:     eng,
		cfg:     opts,
		rpcAddr: computeRPCAddr(opts),
	}, nil
}

func computeRPCAddr(cfg *config.Options) string {
	if !cfg.EnableRPC {
		return ""
	}
	host := "127.0.0.1"
	if cfg.RPCListenAll {
		host = "0.0.0.0"
	}
	port := cfg.RPCListenPort
	if port == 0 {
		port = 6800
	}
	return fmt.Sprintf("http://%s:%d/jsonrpc", host, port)
}

// Run starts the daemon and blocks until ctx is cancelled or Shutdown is called.
// It must be running for downloads to progress.
func (d *Daemon) Run(ctx context.Context) error {
	return d.eng.Run(ctx)
}

// AddURI adds a download for the given URI and returns the assigned GID.
func (d *Daemon) AddURI(uri string, opts *config.Options) (core.GID, error) {
	return d.eng.Add(engine.AddSpec{
		URIs:    []string{uri},
		Options: opts,
	})
}

// AddTorrent adds a .torrent file download. data is the raw bencoded torrent.
func (d *Daemon) AddTorrent(data []byte, opts *config.Options) (core.GID, error) {
	return d.eng.Add(engine.AddSpec{
		Torrent: data,
		Options: opts,
	})
}

// AddMetalink adds a metalink download. data is the raw metalink XML.
// It returns one GID per selected metalink file, matching aria2's multi-GID
// metalink behavior.
func (d *Daemon) AddMetalink(data []byte, opts *config.Options) ([]core.GID, error) {
	gids, err := d.eng.AddMetalink(data, opts, 0, false)
	if err != nil {
		return nil, fmt.Errorf("aria2c: add metalink: %w", err)
	}
	return gids, nil
}

// RPCAddr returns the RPC listen address if RPC is enabled (EnableRPC=true),
// or an empty string otherwise. The address format is "http://<host>:<port>/jsonrpc".
// This value is pre-computed at construction time.
func (d *Daemon) RPCAddr() string {
	return d.rpcAddr
}

// Shutdown stops the daemon gracefully. If force is true, active downloads
// are halted immediately; otherwise they receive a pause request and the
// engine waits for clean teardown.
func (d *Daemon) Shutdown(force bool) error {
	return d.eng.Shutdown(force)
}

// DaemonStatus is an aggregate snapshot of daemon state.
type DaemonStatus struct {
	Active  int   // number of active downloads
	Waiting int   // number of waiting (including paused) downloads
	Stopped int   // number of stopped (complete/error/removed) downloads
	Speed   int64 // total download speed in bytes/sec across all active downloads
}

// Status returns the current aggregate daemon status. All counters are
// point-in-time snapshots taken under the engine's internal mutex.
func (d *Daemon) Status() DaemonStatus {
	active := d.eng.TellActive()

	var speed int64
	for _, s := range active {
		speed += s.DownloadSpeed
	}

	return DaemonStatus{
		Active:  len(active),
		Waiting: len(d.eng.TellWaiting(0, math.MaxInt)),
		Stopped: len(d.eng.TellStopped(0, math.MaxInt)),
		Speed:   speed,
	}
}
