//go:build !unix

package runner

import (
	"os"
	"os/exec"
)

func configureDownloadCommand(cmd *exec.Cmd) {}

func signalDownloadProcess(cmd *exec.Cmd, signal os.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(signal)
}

func killDownloadProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
