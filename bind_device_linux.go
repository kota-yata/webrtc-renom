//go:build linux

package renom

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func bindSocketToDevice(network, address, ifName string, c syscall.RawConn) error {
	var bindErr error
	if err := c.Control(func(fd uintptr) {
		bindErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifName)
	}); err != nil {
		return err
	}
	return bindErr
}
