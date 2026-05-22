// Command bench runs aria2c vs aria2go throughput/stress benchmarks.
//
// Usage:
//
//	bench -aria2c /path/to/aria2c -aria2go /path/to/aria2go [flags]
//
// Flags:
//
//	-aria2c           Path to aria2c binary (default: ./aria2c)
//	-aria2go          Path to aria2go binary (default: ./aria2go)
//	-output           Output directory for JSON reports (default: bench/results)
//	-tmpdir           Temp directory for RAM disk (default: /tmp)
//	-ramdisk-mb       RAM disk size in MB (default: 4096)
//	-sample-interval  Metrics sampling interval (default: 100ms)
//	-dashboard        Dashboard listen address (default: 127.0.0.1:7890)
//	-no-browser       Don't open dashboard in browser
//	-http-only        Run only HTTP scenarios (skip torrents)
//	-duration         Duration per scenario (default: 10s)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/smartass08/aria2go/bench/internal/dashboard"
	"github.com/smartass08/aria2go/bench/internal/report"
	"github.com/smartass08/aria2go/bench/internal/runner"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		aria2cPath     = flag.String("aria2c", "./aria2c", "path to aria2c binary")
		aria2goPath    = flag.String("aria2go", "./aria2go", "path to aria2go binary")
		outputDir      = flag.String("output", "bench/results", "output directory for JSON reports")
		tmpDir         = flag.String("tmpdir", os.TempDir(), "temp directory for RAM disk")
		ramDiskMB      = flag.Int("ramdisk-mb", 4096, "RAM disk size in MB")
		sampleInterval = flag.Duration("sample-interval", 100*time.Millisecond, "metrics sampling interval")
		dashboardAddr  = flag.String("dashboard", "127.0.0.1:7890", "dashboard listen address")
		noBrowser      = flag.Bool("no-browser", false, "don't open dashboard in browser")
		httpOnly       = flag.Bool("http-only", false, "run only HTTP scenarios")
		duration       = flag.Duration("duration", 10*time.Second, "duration per scenario")
		concurrency    = flag.Int("concurrency", 5, "number of concurrent downloads")
	)
	flag.Parse()

	aria2cResolved, err := findBinary(*aria2cPath, "aria2c")
	if err != nil {
		return fmt.Errorf("aria2c: %w", err)
	}
	aria2goResolved, err := findBinary(*aria2goPath, "aria2go")
	if err != nil {
		return fmt.Errorf("aria2go: %w", err)
	}

	fmt.Printf("aria2c:  %s\n", aria2cResolved)
	fmt.Printf("aria2go: %s\n", aria2goResolved)

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		return fmt.Errorf("mkdir output: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupt received, shutting down...")
		cancel()
	}()

	dash, err := dashboard.New(*dashboardAddr)
	if err != nil {
		return fmt.Errorf("dashboard: %w", err)
	}
	if err := dash.Start(); err != nil {
		return fmt.Errorf("dashboard start: %w", err)
	}
	defer dash.Close()

	fmt.Printf("Dashboard: http://%s\n", dash.Addr())
	if !*noBrowser {
		openBrowser("http://" + dash.Addr())
	}

	r, err := runner.New(runner.Config{
		Aria2cPath:     aria2cResolved,
		Aria2goPath:    aria2goResolved,
		OutputDir:      *outputDir,
		TmpDir:         *tmpDir,
		RAMDiskSizeMB:  *ramDiskMB,
		SampleInterval: *sampleInterval,
		Dashboard:      dash,
		HTTPOnly:       *httpOnly,
		Duration:       *duration,
		Concurrency:    *concurrency,
	})
	if err != nil {
		return fmt.Errorf("runner: %w", err)
	}

	fmt.Println("Starting benchmark...")
	rep, err := r.Run(ctx)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	reportPath := filepath.Join(*outputDir, fmt.Sprintf("bench-%s.json", timestamp))
	if err := report.WriteReport(reportPath, rep); err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	fmt.Printf("\nBenchmark complete. Report: %s\n", reportPath)
	fmt.Printf("Dashboard remains open at http://%s\n", dash.Addr())
	fmt.Println("Press Ctrl+C to exit.")

	<-ctx.Done()
	return nil
}

// findBinary resolves a binary path, trying multiple locations if the given path doesn't work.
// It verifies the binary actually runs by calling --version.
func findBinary(givenPath, name string) (string, error) {
	candidates := []string{givenPath}

	// If the given path is a simple name like "aria2c", LookPath might find it in PATH
	if !filepath.IsAbs(givenPath) && !filepath.IsLocal(givenPath) {
		if found, err := exec.LookPath(givenPath); err == nil {
			candidates = append(candidates, found)
		}
	}

	// Try common locations
	candidates = append(candidates,
		filepath.Join(os.Getenv("HOME"), ".nix-profile", "bin", name),
		filepath.Join(os.Getenv("HOME"), "aria2go", name),
		name, // bare name in current directory
	)

	for _, candidate := range candidates {
		// Check if file exists and is executable
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0111 == 0 {
			continue
		}

		// Try to run it
		cmd := exec.Command(candidate, "--version")
		if err := cmd.Run(); err != nil {
			continue
		}

		// Success
		absPath, _ := filepath.Abs(candidate)
		return absPath, nil
	}

	return "", fmt.Errorf("binary %q not found or not executable (tried: %v)", name, candidates)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		return
	}
	cmd.Start()
}
