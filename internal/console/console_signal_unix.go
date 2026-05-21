//go:build !windows

package console

import (
	"os"
	"os/signal"
	"syscall"
)

func (c *Console) registerSignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
}
