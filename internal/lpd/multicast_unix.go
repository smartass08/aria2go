//go:build !windows

package lpd

import (
	"net"
	"syscall"
)

func enableMulticastLoopback(conn *net.UDPConn) error {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var ctrlErr error
	err = rawConn.Control(func(fd uintptr) {
		ctrlErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 1)
	})
	if err != nil {
		return err
	}
	return ctrlErr
}
