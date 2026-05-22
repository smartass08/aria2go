//go:build !windows

package main

import (
	"io"
	"os"
	"syscall"
)

func daemonize() int {
	// Match aria2's daemon(0, 0) behavior where feasible: detach from the
	// controlling terminal, chdir to /, and redirect stdio to /dev/null.
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		io.WriteString(os.Stderr, "aria2go: daemon failed: "+err.Error()+"\n")
		return -1
	}
	defer devnull.Close()

	args := daemonChildArgs(os.Args)
	env := append(os.Environ(), daemonEnv+"=1")
	pid, err := syscall.ForkExec(args[0], args, &syscall.ProcAttr{
		Dir:   "/",
		Env:   env,
		Files: []uintptr{devnull.Fd(), devnull.Fd(), devnull.Fd()},
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
