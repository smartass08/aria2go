//go:build !windows

package netx

import (
	"errors"
	"syscall"
)

// bindToDeviceSupported returns true on all Unix platforms.  The actual
// syscall may fail if the OS does not implement SO_BINDTODEVICE, but
// the runtime guard (platform.Caps().InterfaceBind) prevents calling it
// on unsupported platforms.
func bindToDeviceSupported() bool {
	return true
}

// bindToDevice binds the socket to the named network interface using
// the SO_BINDTODEVICE socket option.
func bindToDevice(c syscall.RawConn, ifName string) error {
	var setErr error
	ctrlErr := c.Control(func(fd uintptr) {
		setErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, 0x19, ifName)
	})
	if ctrlErr != nil {
		return ctrlErr
	}
	return setErr
}

// errUnsupported is returned when a platform-specific feature is not
// available.
var errUnsupported = errors.New("netx: operation not supported on this platform")

// setSockOptInt wraps syscall.SetsockoptInt with platform-specific fd type.
func setSockOptInt(fd uintptr, level, opt, value int) error {
	return syscall.SetsockoptInt(int(fd), level, opt, value)
}

func getSockOptInt(fd uintptr, level, opt int) (int, error) {
	return syscall.GetsockoptInt(int(fd), level, opt)
}

// applyDSCP applies IP DSCP marking on the socket.
//
// On IPv4 sockets, it sets IP_TOS. On IPv6 sockets, it sets IPV6_TCLASS.
// The tos parameter is the full TOS/TCLASS byte (DSCP << 2), matching
// aria2 where SocketCore::setIpDscp does ipDscp_ = ipDscp << 2
// (SocketCore.h:147) and then sets that value directly.
//
// If the socket family cannot be determined, both options are tried.
func applyDSCP(fd int, tos int) error {
	family, err := socketFamily(fd)
	if err != nil {
		// Unknown family: try both IPv4 and IPv6. One will fail silently.
		_ = syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_TOS, tos)
		_ = syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_TCLASS, tos)
		return err
	}

	switch family {
	case syscall.AF_INET:
		return syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_TOS, tos)
	case syscall.AF_INET6:
		return syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_TCLASS, tos)
	}
	return nil
}

// socketFamily returns the address family of the socket.
func socketFamily(fd int) (int, error) {
	sa, err := syscall.Getsockname(fd)
	if err != nil {
		return 0, err
	}
	switch sa.(type) {
	case *syscall.SockaddrInet4:
		return syscall.AF_INET, nil
	case *syscall.SockaddrInet6:
		return syscall.AF_INET6, nil
	default:
		return 0, nil
	}
}
