//go:build windows

package console

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procGetStdHandle               = kernel32.NewProc("GetStdHandle")
)

const stdOutputHandle = ^uint32(10) // -11 = 0xFFFFFFF5

type winCoord struct {
	X int16
	Y int16
}

type winConsoleScreenBufferInfo struct {
	Size              winCoord
	CursorPosition    winCoord
	Attributes        uint16
	Window            winSmallRect
	MaximumWindowSize winCoord
}

type winSmallRect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

func isTerminal(fd uintptr) bool {
	var mode uint32
	err := syscall.GetConsoleMode(syscall.Handle(fd), &mode)
	return err == nil
}

func terminalWidth() int {
	handle, _, _ := procGetStdHandle.Call(uintptr(stdOutputHandle))
	if handle == 0 || handle == uintptr(syscall.InvalidHandle) {
		return 0
	}
	var info winConsoleScreenBufferInfo
	ret, _, _ := procGetConsoleScreenBufferInfo.Call(handle, uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return 0
	}
	cols := int(info.Size.X)
	if cols > 2 {
		return cols - 2
	}
	return cols
}
