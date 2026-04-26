package renom

import (
	"fmt"
	"net"
	"runtime"
	"strings"
)

func interfaceNameForIP(ip string) (string, error) {
	target := net.ParseIP(ip)
	if target == nil {
		return "", fmt.Errorf("invalid IP address %q", ip)
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, ifi := range ifaces {
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ipnet *net.IPNet
			switch v := addr.(type) {
			case *net.IPNet:
				ipnet = v
			case *net.IPAddr:
				ipnet = &net.IPNet{IP: v.IP, Mask: net.CIDRMask(8*len(v.IP), 8*len(v.IP))}
			default:
				continue
			}

			if ipnet.IP.Equal(target) {
				return ifi.Name, nil
			}
		}
	}

	return "", fmt.Errorf("no interface matched IP %s", ip)
}

func SplitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}

	return out
}

type InterfaceAddress struct {
	Name           string
	IP             net.IP
	IsWiFi         bool
	IsEthernet     bool
	IsCellularLike bool
}

func ListActiveInterfaceAddrs() ([]InterfaceAddress, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	out := make([]InterfaceAddress, 0, len(ifaces))
	for _, iface := range ifaces {
		if (iface.Flags&net.FlagUp) == 0 || (iface.Flags&net.FlagLoopback) != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
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

			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}

			out = append(out, InterfaceAddress{
				Name:           iface.Name,
				IP:             ip4,
				IsWiFi:         isWiFiInterface(iface.Name),
				IsEthernet:     isEthernetInterface(iface.Name),
				IsCellularLike: isCellularLikeInterface(iface.Name),
			})
		}
	}

	return out, nil
}

func isWiFiInterface(ifaceName string) bool {
	if strings.HasPrefix(ifaceName, "wlan") || strings.HasPrefix(ifaceName, "wl") {
		return true
	}

	return runtime.GOOS == "darwin" && strings.HasPrefix(ifaceName, "en")
}

func isEthernetInterface(ifaceName string) bool {
	return strings.HasPrefix(ifaceName, "eth") || strings.HasPrefix(ifaceName, "en")
}

func isCellularLikeInterface(ifaceName string) bool {
	lower := strings.ToLower(ifaceName)
	for _, prefix := range []string{"rmnet", "ccmni", "wwan", "pdp_ip", "cell", "usb"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	return false
}
