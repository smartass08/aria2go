//go:build unix

package runner

import (
	"os"
	"os/exec"
	"syscall"
)

func configureDownloadCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = downloadSysProcAttr()
}

func signalDownloadProcess(cmd *exec.Cmd, signal os.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	sig, ok := signal.(syscall.Signal)
	if !ok {
		return cmd.Process.Signal(signal)
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return cmd.Process.Signal(signal)
	}
	return syscall.Kill(-pgid, sig)
}

func killDownloadProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return cmd.Process.Kill()
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}
