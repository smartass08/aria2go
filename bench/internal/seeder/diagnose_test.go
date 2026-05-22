package seeder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestBitTorrentDiagnostics(t *testing.T) {
	// 1. Start tracker/seeder
	cfg := Config{
		NumTorrents: 1,
		FileSizeMB:  1,
		PieceLen:    256 * 1024,
		ListenAddr:  "127.0.0.1:0",
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create seeder: %v", err)
	}
	defer s.Close()

	if err := s.Start(cfg); err != nil {
		t.Fatalf("Failed to start seeder: %v", err)
	}

	trackerURL := s.TrackerURL()
	magnet := s.MagnetURIs()[0]

	fmt.Printf("\n=== DIAGNOSTICS START ===\n")
	fmt.Printf("Tracker URL: %s\n", trackerURL)
	fmt.Printf("Magnet Link: %s\n", magnet)

	// Create temporary directory for download
	tmpDir, err := os.MkdirTemp("", "aria2-diagnose-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Try to find aria2c binary in path
	aria2cPath := "aria2c"

	fmt.Printf("Using aria2c binary: %s\n", aria2cPath)

	args := []string{
		"--no-conf",
		"--file-allocation=none",
		"--auto-file-renaming=false",
		"--allow-overwrite=true",
		"--max-tries=0",
		"--timeout=5",
		"--connect-timeout=5",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--console-log-level=debug", // Debug logging to capture all details
		"--enable-dht=false",
		"--enable-dht6=false",
		"--enable-rpc=false",
		"--no-netrc",
		"--check-certificate=false",
		"--disable-ipv6",
		"--dir=" + tmpDir,
		"--bt-tracker=" + trackerURL,
		magnet,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, aria2cPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start the command
	fmt.Printf("Spawning aria2c to download the torrent...\n")
	startTime := time.Now()

	err = cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start aria2c: %v", err)
	}

	// Sleep 5 seconds
	time.Sleep(5 * time.Second)

	// Terminate aria2c
	fmt.Printf("Terminating aria2c...\n")
	if cmd.Process != nil {
		cmd.Process.Signal(os.Interrupt)
	}

	// Wait for process to exit
	cmd.Wait()
	fmt.Printf("aria2c finished in %v\n", time.Since(startTime))
	fmt.Printf("=== DIAGNOSTICS END ===\n")
}
