//go:build !windows

package main

import (
	"io"
	"os"
	"syscall"
)

func daemonize() int {
	// daemon(0, 0) forks and the parent exits, child continues.
	// On macOS, daemon() is deprecated but functional.
	pid, err := syscall.ForkExec(os.Args[0], os.Args, &syscall.ProcAttr{
		Env:   os.Environ(),
		Files: []uintptr{0, 1, 2},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	})
	if err != nil {
		io.WriteString(os.Stderr, "aria2go: daemon failed: "+err.Error()+"\n")
		return -1
	}
	if pid != 0 {
		os.Exit(0)
	}
	return 0
}
