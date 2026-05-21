//go:build darwin || freebsd || openbsd

package console

import "syscall"

const terminalGetAttrRequest = uintptr(syscall.TIOCGETA)
