//go:build windows

package console

import (
	"os"
	"os/signal"
)

func (c *Console) registerSignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt)
}
