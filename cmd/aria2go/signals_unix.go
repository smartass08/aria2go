//go:build !windows

package main

import (
	"os"
	"syscall"
)

func ignoredSignals() []os.Signal {
	return []os.Signal{syscall.SIGPIPE, syscall.SIGCHLD}
}

func shutdownSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}
}

func isInterruptSignal(sig os.Signal) bool {
	return sig == syscall.SIGINT
}
