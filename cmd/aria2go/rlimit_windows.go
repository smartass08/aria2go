//go:build windows

package main

import (
	"log/slog"

	"github.com/smartass08/aria2go/internal/config"
)

func setRLimitNOFILE(opts *config.Options, logger *slog.Logger) {
	if opts.RlimitNofile != "" && opts.RlimitNofile != "0" {
		logger.Debug("rlimit-nofile is not supported on Windows", "value", opts.RlimitNofile)
	}
}
