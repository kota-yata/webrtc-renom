//go:build linux

package renom

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/mdlayher/wifi"
)

type signalEvent struct {
	IfName    string
	RSSI      int
	Threshold int
}

type signalMonitor struct {
	client    *wifi.Client
	iface     *wifi.Interface
	threshold int
	interval  time.Duration
}

func newSignalMonitor(ifName string, threshold int, interval time.Duration) (*signalMonitor, error) {
	client, err := wifi.New()
	if err != nil {
		return nil, fmt.Errorf("create wifi client: %w", err)
	}

	iface, err := stationInterface(client, ifName)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	return &signalMonitor{
		client:    client,
		iface:     iface,
		threshold: threshold,
		interval:  interval,
	}, nil
}

func (m *signalMonitor) Close() error {
	if m == nil || m.client == nil {
		return nil
	}
	err := m.client.Close()
	m.client = nil
	return err
}

func (m *signalMonitor) Watch(ctx context.Context) <-chan signalEvent {
	out := make(chan signalEvent, 1)

	go func() {
		defer close(out)

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		degraded := false
		for {
			select {
			case <-ticker.C:
				rssi, err := m.rssi()
				if err != nil {
					log.Printf("signal poll failed: %v", err)
					continue
				}

				if rssi <= m.threshold {
					if degraded {
						continue
					}
					degraded = true
					select {
					case out <- signalEvent{IfName: m.iface.Name, RSSI: rssi, Threshold: m.threshold}:
					case <-ctx.Done():
						return
					}
					continue
				}

				degraded = false
			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}

func (m *signalMonitor) rssi() (int, error) {
	stations, err := m.client.StationInfo(m.iface)
	if err != nil {
		return 0, fmt.Errorf("get station info for %s: %w", m.iface.Name, err)
	}
	if len(stations) == 0 {
		return 0, fmt.Errorf("no station info for %s", m.iface.Name)
	}

	rssi := stations[0].SignalAverage
	if rssi == 0 {
		rssi = stations[0].Signal
	}
	return rssi, nil
}

func stationInterface(client *wifi.Client, ifName string) (*wifi.Interface, error) {
	ifaces, err := client.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list wifi interfaces: %w", err)
	}

	for _, iface := range ifaces {
		if ifName != "" && iface.Name != ifName {
			continue
		}
		if iface.Type != wifi.InterfaceTypeStation {
			continue
		}
		nif, err := net.InterfaceByIndex(iface.Index)
		if err != nil {
			continue
		}
		if (nif.Flags&net.FlagUp) == 0 || (nif.Flags&net.FlagLoopback) != 0 {
			continue
		}
		return iface, nil
	}

	if ifName != "" {
		return nil, fmt.Errorf("no active station-mode Wi-Fi interface %q found", ifName)
	}
	return nil, fmt.Errorf("no active station-mode Wi-Fi interface found")
}
