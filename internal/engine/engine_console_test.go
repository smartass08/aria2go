package engine

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/console"
	"github.com/smartass08/aria2go/internal/core"
)

func TestEngineConsoleOptionsWiresConfig(t *testing.T) {
	opts := &config.Options{
		Quiet:                  true,
		SummaryInterval:        "60",
		Stderr:                 true,
		ShowConsoleReadout:     false,
		TruncateConsoleReadout: false,
	}

	got := engineConsoleOptions(opts)
	if !got.Quiet {
		t.Error("Quiet = false, want true")
	}
	if got.SummaryInterval != 60*time.Second {
		t.Errorf("SummaryInterval = %s, want 1m0s", got.SummaryInterval)
	}
	if got.Interactive {
		t.Error("Interactive = true, want false when quiet")
	}
	if !got.Stderr {
		t.Error("Stderr = false, want true")
	}
	if got.ShowReadout {
		t.Error("ShowReadout = true, want false")
	}
	if got.Truncate {
		t.Error("Truncate = true, want false")
	}
}

func TestEngineConsoleOptionsSummaryDisabledWhenIntervalEmpty(t *testing.T) {
	opts := config.Default()
	opts.SummaryInterval = ""
	got := engineConsoleOptions(opts)
	if got.SummaryInterval != 0 {
		t.Errorf("SummaryInterval = %s, want 0", got.SummaryInterval)
	}
	if !got.Interactive {
		t.Error("Interactive = false, want true when not quiet")
	}
	if !got.ShowReadout {
		t.Error("ShowReadout = false, want true")
	}
	if !got.Truncate {
		t.Error("Truncate = false, want true")
	}
}

func TestShowDownloadResultsPrintsDefault(t *testing.T) {
	var buf bytes.Buffer
	e := &Engine{
		cfg:         &config.Options{DownloadResult: "default"},
		console:     console.NewWithWriter(&buf, console.Options{}),
		stoppedRing: newStoppedRing(10),
	}
	e.stoppedRing.push(&downloadResult{
		gid:                   core.GID(0x1234567890abcdef),
		state:                 core.StatusComplete,
		errCode:               core.ExitSuccess,
		filePath:              "/tmp/result.bin",
		totalLength:           1024,
		completedLength:       1024,
		sessionDownloadLength: 1024,
		sessionTime:           time.Second,
	})

	e.showDownloadResults()

	out := buf.String()
	if !strings.Contains(out, "Download Results:") {
		t.Fatalf("Download Results header missing:\n%s", out)
	}
	if !strings.Contains(out, "123456|OK") {
		t.Errorf("result row missing GID/status:\n%s", out)
	}
	if !strings.Contains(out, "/tmp/result.bin") {
		t.Errorf("result row missing path:\n%s", out)
	}
}

func TestShowDownloadResultsHideSuppressesOutput(t *testing.T) {
	var buf bytes.Buffer
	e := &Engine{
		cfg:         &config.Options{DownloadResult: "hide"},
		console:     console.NewWithWriter(&buf, console.Options{}),
		stoppedRing: newStoppedRing(10),
	}
	e.stoppedRing.push(&downloadResult{
		gid:     core.GID(1),
		state:   core.StatusComplete,
		errCode: core.ExitSuccess,
	})

	e.showDownloadResults()

	if got := buf.String(); got != "" {
		t.Errorf("DownloadResult=hide should suppress output, got %q", got)
	}
}

func TestShowDownloadResultsQuietSuppressesOutput(t *testing.T) {
	var buf bytes.Buffer
	e := &Engine{
		cfg:         &config.Options{DownloadResult: "default", Quiet: true},
		console:     console.NewWithWriter(&buf, console.Options{}),
		stoppedRing: newStoppedRing(10),
	}
	e.stoppedRing.push(&downloadResult{
		gid:     core.GID(1),
		state:   core.StatusComplete,
		errCode: core.ExitSuccess,
	})

	e.showDownloadResults()

	if got := buf.String(); got != "" {
		t.Errorf("quiet console should suppress output, got %q", got)
	}
}
