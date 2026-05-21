//go:build linux

package console

import "syscall"

const terminalGetAttrRequest = uintptr(syscall.TCGETS)
