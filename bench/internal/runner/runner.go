package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/smartass08/aria2go/bench/internal/dashboard"
	"github.com/smartass08/aria2go/bench/internal/metrics"
	"github.com/smartass08/aria2go/bench/internal/nullfs"
	"github.com/smartass08/aria2go/bench/internal/report"
	"github.com/smartass08/aria2go/bench/internal/seeder"
	"github.com/smartass08/aria2go/bench/internal/server"
	"github.com/smartass08/aria2go/bench/internal/types"
)

type Config struct {
	Aria2cPath     string
	Aria2goPath    string
	OutputDir      string
	TmpDir         string
	RAMDiskSizeMB  int
	SampleInterval time.Duration
	Dashboard      *dashboard.Server
	HTTPOnly       bool
	Duration       time.Duration
	StopTimeout    time.Duration
	Concurrency    int
}

const (
	defaultStopTimeout     = 3 * time.Second
	defaultKillWaitTimeout = 1 * time.Second
)

type Runner struct {
	cfg Config
}

func New(cfg Config) (*Runner, error) {
	if cfg.SampleInterval == 0 {
		cfg.SampleInterval = 100 * time.Millisecond
	}
	if cfg.RAMDiskSizeMB == 0 {
		cfg.RAMDiskSizeMB = 4096
	}
	if cfg.Duration == 0 {
		cfg.Duration = 10 * time.Second
	}
	if cfg.StopTimeout <= 0 {
		cfg.StopTimeout = defaultStopTimeout
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 5
	}
	return &Runner{cfg: cfg}, nil
}

func (r *Runner) Run(ctx context.Context) (*types.Report, error) {
	mnt, err := nullfs.New(filepath.Join(r.cfg.TmpDir, "aria2-bench-void"), r.cfg.RAMDiskSizeMB)
	if err != nil {
		return nil, fmt.Errorf("nullfs: %w", err)
	}
	defer mnt.Unmount()

	httpSrv, err := server.NewInfinite("")
	if err != nil {
		return nil, err
	}
	if err := httpSrv.Start(); err != nil {
		return nil, err
	}
	defer httpSrv.Close()

	var seederSrv *seeder.Seeder
	if !r.cfg.HTTPOnly {
		seederSrv, err = seeder.New(seeder.Config{ListenAddr: "127.0.0.1:0"})
		if err != nil {
			return nil, fmt.Errorf("seeder: %w", err)
		}
		defer seederSrv.Close()
		if err := seederSrv.Start(seeder.Config{
			NumTorrents: 10,
			FileSizeMB:  64,
			PieceLen:    256 * 1024,
		}); err != nil {
			return nil, fmt.Errorf("seeder start: %w", err)
		}
	}

	rep := &types.Report{
		Host:       hostname(),
		Aria2cVer:  r.getVersion(r.cfg.Aria2cPath),
		Aria2goVer: r.getVersion(r.cfg.Aria2goPath),
	}

	r.publishMeta(rep)

	for _, bin := range []types.Binary{types.BinaryAria2c, types.BinaryAria2go} {
		binPath := r.cfg.Aria2cPath
		if bin == types.BinaryAria2go {
			binPath = r.cfg.Aria2goPath
		}

		if res, err := r.runHTTPSingle(ctx, bin, binPath, mnt.Path, httpSrv); err == nil {
			rep.Scenarios = append(rep.Scenarios, res)
		}

		if res, err := r.runHTTPConcurrent(ctx, bin, binPath, mnt.Path, httpSrv, r.cfg.Concurrency); err == nil {
			rep.Scenarios = append(rep.Scenarios, res)
		}

		if seederSrv != nil {
			if res, err := r.runTorrentSingle(ctx, bin, binPath, mnt.Path, seederSrv); err == nil {
				rep.Scenarios = append(rep.Scenarios, res)
			}
		}

		if seederSrv != nil {
			if res, err := r.runTorrentBurst(ctx, bin, binPath, mnt.Path, seederSrv, r.cfg.Concurrency); err == nil {
				rep.Scenarios = append(rep.Scenarios, res)
			}
		}

		time.Sleep(2 * time.Second)
	}

	return rep, nil
}

func commonArgs(outDir string, extra ...string) []string {
	args := []string{
		"--no-conf",
		"--file-allocation=none",
		"--auto-file-renaming=false",
		"--allow-overwrite=true",
		"--max-tries=0",
		"--timeout=300",
		"--connect-timeout=10",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--console-log-level=info",
		"--enable-dht=false",
		"--enable-dht6=false",
		"--enable-rpc=false",
		"--no-netrc",
		"--check-certificate=false",
		"--disable-ipv6",
		"--min-split-size=1M",
		"--dir=" + outDir,
	}
	return append(args, extra...)
}

func writeInputFile(dir, baseURL string, n int) (string, error) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%s?id=%d\n\tout=dl_%05d.bin\n", baseURL, i, i)
	}
	path := filepath.Join(dir, "bench.in")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func runWithMetrics(ctx context.Context, r *Runner, bin types.Binary, binPath string, args []string, httpSrv *server.InfiniteServer) (types.ScenarioResult, error) {
	res := types.ScenarioResult{
		Binary:    bin,
		StartedAt: time.Now(),
	}

	cmd := exec.Command(binPath, args...)
	configureDownloadCommand(cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	bytesStart := nullfs.TotalBytesWritten.Load()
	if err := cmd.Start(); err != nil {
		return res, err
	}

	sampler := metrics.NewSampler(cmd.Process.Pid, bin, r.cfg.SampleInterval)
	sampler.Start(ctx)

	timer := time.NewTimer(r.cfg.Duration)
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}

	forced, err := stopDownloadProcess(cmd, r.cfg.StopTimeout)
	if forced {
		res.Errors = append(res.Errors, fmt.Sprintf("download process force-killed after %s", r.cfg.StopTimeout))
	}
	if err != nil && forced {
		res.Errors = append(res.Errors, err.Error())
	}

	bytesEnd := nullfs.TotalBytesWritten.Load()
	sampler.Stop()

	res.Duration = time.Since(res.StartedAt)
	res.Samples = sampler.Samples()
	res.Summary = report.ComputeSummary(res.Samples)
	res.Summary.ThroughputMbps = float64(bytesEnd-bytesStart) / res.Duration.Seconds() / 1024 / 1024 * 8
	res.Summary.TotalBytes = bytesEnd - bytesStart
	return res, nil
}

func stopDownloadProcess(cmd *exec.Cmd, stopTimeout time.Duration) (bool, error) {
	if stopTimeout <= 0 {
		stopTimeout = defaultStopTimeout
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	_ = signalDownloadProcess(cmd, os.Interrupt)

	timer := time.NewTimer(stopTimeout)
	defer timer.Stop()

	select {
	case err := <-waitCh:
		return false, err
	case <-timer.C:
		if err := killDownloadProcess(cmd); err != nil {
			select {
			case waitErr := <-waitCh:
				return false, waitErr
			default:
				return true, err
			}
		}
	}

	select {
	case err := <-waitCh:
		return true, err
	case <-time.After(defaultKillWaitTimeout):
		return true, fmt.Errorf("process did not exit within %s after SIGKILL", defaultKillWaitTimeout)
	}
}

func (r *Runner) runHTTPSingle(ctx context.Context, bin types.Binary, binPath, outDir string, httpSrv *server.InfiniteServer) (types.ScenarioResult, error) {
	inDir, err := os.MkdirTemp(r.cfg.TmpDir, "bench-in-")
	if err != nil {
		return types.ScenarioResult{}, err
	}
	defer os.RemoveAll(inDir)

	args := commonArgs(outDir, "-s1", "-x1", "--max-concurrent-downloads=1")
	inPath, err := writeInputFile(inDir, httpSrv.URL(), 1)
	if err != nil {
		return types.ScenarioResult{}, err
	}
	args = append(args, "--input-file="+inPath)

	res, err := runWithMetrics(ctx, r, bin, binPath, args, httpSrv)
	res.Kind = types.ScenarioHTTPSingle
	res.Protocol = types.ProtocolHTTP
	return res, err
}

func (r *Runner) runHTTPConcurrent(ctx context.Context, bin types.Binary, binPath, outDir string, httpSrv *server.InfiniteServer, concurrency int) (types.ScenarioResult, error) {
	inDir, err := os.MkdirTemp(r.cfg.TmpDir, "bench-in-")
	if err != nil {
		return types.ScenarioResult{}, err
	}
	defer os.RemoveAll(inDir)

	args := commonArgs(outDir, "-s1", "-x1",
		fmt.Sprintf("--max-concurrent-downloads=%d", concurrency))
	inPath, err := writeInputFile(inDir, httpSrv.URL(), concurrency)
	if err != nil {
		return types.ScenarioResult{}, err
	}
	args = append(args, "--input-file="+inPath)

	res, err := runWithMetrics(ctx, r, bin, binPath, args, httpSrv)
	res.Kind = types.ScenarioHTTPConcurrent
	res.Protocol = types.ProtocolHTTP
	return res, err
}

func (r *Runner) runTorrentSingle(ctx context.Context, bin types.Binary, binPath, outDir string, s *seeder.Seeder) (types.ScenarioResult, error) {
	magnets := s.MagnetURIs()
	if len(magnets) == 0 {
		return types.ScenarioResult{}, fmt.Errorf("no magnets available")
	}
	args := commonArgs(outDir,
		"--bt-tracker="+s.TrackerURL(),
		"--bt-tracker-connect-timeout=5",
		"--bt-tracker-timeout=5",
		"--max-concurrent-downloads=1",
		magnets[0],
	)
	httpSrv := serverProxyForSeeder(s)
	res, err := runWithMetrics(ctx, r, bin, binPath, args, httpSrv)
	res.Kind = types.ScenarioTorrentSingle
	res.Protocol = types.ProtocolTorrent
	return res, err
}

func (r *Runner) runTorrentBurst(ctx context.Context, bin types.Binary, binPath, outDir string, s *seeder.Seeder, concurrency int) (types.ScenarioResult, error) {
	magnets := s.MagnetURIs()
	if len(magnets) < concurrency {
		concurrency = len(magnets)
	}
	args := commonArgs(outDir,
		"--bt-tracker="+s.TrackerURL(),
		"--bt-tracker-connect-timeout=5",
		"--bt-tracker-timeout=5",
		fmt.Sprintf("--max-concurrent-downloads=%d", concurrency),
	)
	args = append(args, magnets[:concurrency]...)
	httpSrv := serverProxyForSeeder(s)
	res, err := runWithMetrics(ctx, r, bin, binPath, args, httpSrv)
	res.Kind = types.ScenarioTorrentBurst
	res.Protocol = types.ProtocolTorrent
	return res, err
}

// serverProxyForSeeder returns a stub that reports 0 bytes (no HTTP throughput for torrents).
func serverProxyForSeeder(s *seeder.Seeder) *server.InfiniteServer {
	return nil
}

func (r *Runner) publishMeta(rep *types.Report) {
	if r.cfg.Dashboard == nil {
		return
	}
	r.cfg.Dashboard.Publish(dashboard.Event{
		Type: "meta",
		Meta: map[string]string{
			"host":            rep.Host,
			"aria2c_version":  rep.Aria2cVer,
			"aria2go_version": rep.Aria2goVer,
		},
	})
}

func (r *Runner) getVersion(binPath string) string {
	out, err := exec.Command(binPath, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return string(out)
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}
