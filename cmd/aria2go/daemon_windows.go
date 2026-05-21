//go:build windows

package main

import (
	"io"
	"os"
)

func daemonize() int {
	io.WriteString(os.Stderr, "aria2go: --daemon is not supported on Windows\n")
	return -1
}
