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
		// IPPROTO_IP = 0, IP_MULTICAST_LOOP = 11 on Windows winsock2.
		ctrlErr = syscall.SetsockoptInt(syscall.Handle(fd), 0, 11, 1)
	})
	if err != nil {
		return err
	}
	return ctrlErr
}
