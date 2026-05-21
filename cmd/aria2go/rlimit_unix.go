//go:build !windows

package main

import (
	"log/slog"
	"strconv"
	"syscall"

	"github.com/smartass08/aria2go/internal/config"
)

func setRLimitNOFILE(opts *config.Options, logger *slog.Logger) {
	if opts.RlimitNofile == "" || opts.RlimitNofile == "0" {
		return
	}
	n, err := strconv.Atoi(opts.RlimitNofile)
	if err != nil {
		logger.Warn("Failed to parse rlimit-nofile", "value", opts.RlimitNofile, "error", err)
		return
	}
	if n <= 0 {
		return
	}
	var rlim syscall.Rlimit
	if e := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim); e != nil {
		logger.Warn("Failed to get rlimit NOFILE", "error", e)
		return
	}
	if rlim.Cur >= uint64(n) {
		return
	}
	rlim.Cur = uint64(n)
	if rlim.Cur > rlim.Max {
		rlim.Cur = rlim.Max
	}
	if e := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rlim); e != nil {
		logger.Warn("Failed to set rlimit NOFILE", "from", rlim.Cur, "to", n, "error", e)
		return
	}
	logger.Info("Set rlimit NOFILE", "value", n)
}
