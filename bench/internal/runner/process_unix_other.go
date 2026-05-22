//go:build unix && !linux

package runner

import "syscall"

func downloadSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
