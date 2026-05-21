//go:build !windows

package console

import (
	"syscall"
	"unsafe"
)

// isTerminal checks whether the file descriptor fd is attached to a
// terminal, matching aria2's isatty() behavior via the platform termios
// ioctl request.
func isTerminal(fd uintptr) bool {
	var termios syscall.Termios
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, terminalGetAttrRequest, uintptr(unsafe.Pointer(&termios)))
	return err == 0
}

// terminalWidth returns the terminal column width using TIOCGWINSZ ioctl,
// matching aria2's ioctl(STDOUT_FILENO, TIOCGWINSZ, &size) call.
// Returns 0 if the width cannot be determined.
func terminalWidth() int {
	var ws struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, 1, uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if err != 0 {
		return 0
	}
	// aria2 uses ws.ws_col - 1 to avoid the last column triggering line wrap.
	if ws.Col > 1 {
		return int(ws.Col) - 1
	}
	return int(ws.Col)
}
