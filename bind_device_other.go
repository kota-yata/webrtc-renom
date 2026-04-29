//go:build !linux

package renom

import "syscall"

func bindSocketToDevice(network, address, ifName string, c syscall.RawConn) error {
	return nil
}
