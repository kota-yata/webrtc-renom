//go:build linux || android

package renom

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/vishvananda/netlink"
)

type signalEvent struct {
	IfName    string
	RemovedIP net.IP
}

type signalMonitor struct {
	iface      *net.Interface
	updateChan chan netlink.AddrUpdate
	stopChan   chan struct{}
}

func newSignalMonitor(ifName string) (*signalMonitor, error) {
	iface, err := wifiInterface(ifName)
	if err != nil {
		return nil, err
	}

	m := &signalMonitor{
		iface:      iface,
		updateChan: make(chan netlink.AddrUpdate),
		stopChan:   make(chan struct{}),
	}
	if err := netlink.AddrSubscribe(m.updateChan, m.stopChan); err != nil {
		return nil, fmt.Errorf("subscribe to netlink address updates: %w", err)
	}

	log.Printf("watching WiFi address removal if=%s index=%d", iface.Name, iface.Index)
	return m, nil
}

func (m *signalMonitor) Close() error {
	if m == nil || m.stopChan == nil {
		return nil
	}

	select {
	case <-m.stopChan:
	default:
		close(m.stopChan)
	}
	return nil
}

func (m *signalMonitor) Watch(ctx context.Context) <-chan signalEvent {
	out := make(chan signalEvent, 1)

	go func() {
		defer close(out)

		for {
			select {
			case update, ok := <-m.updateChan:
				if !ok {
					return
				}
				if update.NewAddr || update.LinkIndex != m.iface.Index {
					continue
				}

				ip := update.LinkAddress.IP.To4()
				if ip == nil {
					continue
				}

				log.Printf("WiFi address removed if=%s ip=%s", m.iface.Name, ip)
				select {
				case out <- signalEvent{IfName: m.iface.Name, RemovedIP: ip}:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}

func wifiInterface(ifName string) (*net.Interface, error) {
	if ifName != "" {
		iface, err := net.InterfaceByName(ifName)
		if err != nil {
			return nil, fmt.Errorf("get WiFi interface %q: %w", ifName, err)
		}
		if (iface.Flags & net.FlagLoopback) != 0 {
			return nil, fmt.Errorf("interface %q is loopback", ifName)
		}
		if !isWiFiInterface(iface.Name) {
			return nil, fmt.Errorf("interface %q does not look like WiFi", ifName)
		}
		return iface, nil
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}

	for _, iface := range ifaces {
		if (iface.Flags&net.FlagUp) == 0 || (iface.Flags&net.FlagLoopback) != 0 || !isWiFiInterface(iface.Name) {
			continue
		}
		if hasIPv4Address(iface) {
			return &iface, nil
		}
	}

	return nil, fmt.Errorf("no active WiFi interface with an IPv4 address found")
}

func hasIPv4Address(iface net.Interface) bool {
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}

	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		default:
			continue
		}
		if ip.To4() != nil {
			return true
		}
	}

	return false
}
