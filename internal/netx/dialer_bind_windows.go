//go:build windows

package netx

import (
	"errors"
	"syscall"
)

// bindToDeviceSupported returns false on Windows.
func bindToDeviceSupported() bool {
	return false
}

// bindToDevice is a no-op on Windows.
func bindToDevice(c syscall.RawConn, ifName string) error {
	return errors.New("netx: interface binding not implemented on Windows")
}

// setSockOptInt wraps syscall.SetsockoptInt which takes Handle on Windows.
func setSockOptInt(fd uintptr, level, opt, value int) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), level, opt, value)
}

func getSockOptInt(fd uintptr, level, opt int) (int, error) {
	return syscall.GetsockoptInt(syscall.Handle(fd), level, opt)
}

// applyDSCP is a no-op on Windows: IP DSCP marking is not supported.
// Matches aria2 where applyIpDscp is only functional on Unix-like platforms.
func applyDSCP(fd int, dscp int) error {
	return nil
}
