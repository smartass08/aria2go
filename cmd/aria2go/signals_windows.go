//go:build windows

package main

import (
	"os"
	"syscall"
)

func ignoredSignals() []os.Signal {
	return nil
}

func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func isInterruptSignal(sig os.Signal) bool {
	return sig == os.Interrupt
}
