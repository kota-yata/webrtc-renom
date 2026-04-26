//go:build !linux

package renom

import (
	"context"
	"fmt"
	"time"
)

type signalEvent struct {
	IfName    string
	RSSI      int
	Threshold int
}

type signalMonitor struct{}

func newSignalMonitor(string, int, time.Duration) (*signalMonitor, error) {
	return nil, fmt.Errorf("signal polling is only implemented on linux")
}

func (*signalMonitor) Close() error {
	return nil
}

func (*signalMonitor) Watch(context.Context) <-chan signalEvent {
	out := make(chan signalEvent)
	close(out)
	return out
}
