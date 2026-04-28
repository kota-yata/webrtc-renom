//go:build !linux && !android

package renom

import (
	"context"
	"fmt"
	"net"
)

type signalEvent struct {
	IfName    string
	RemovedIP net.IP
}

type signalMonitor struct{}

func newSignalMonitor() (*signalMonitor, error) {
	return nil, fmt.Errorf("WiFi address monitoring is only implemented on linux")
}

func (*signalMonitor) Close() error {
	return nil
}

func (*signalMonitor) Watch(context.Context) <-chan signalEvent {
	out := make(chan signalEvent)
	close(out)
	return out
}
