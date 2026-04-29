package renom

import (
	"context"
	"net"
	"syscall"

	"github.com/pion/transport/v4"
	"github.com/pion/transport/v4/stdnet"
)

type deviceBoundNet struct {
	*stdnet.Net
	defaultIface string
}

func newDeviceBoundNet() (transport.Net, error) {
	ifaceName := ""
	addrs, err := ListActiveInterfaceAddrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if addr.IsCellularLike {
			ifaceName = addr.Name
			break
		}
	}
	if ifaceName == "" {
		return nil, nil
	}

	n, err := stdnet.NewNet()
	if err != nil {
		return nil, err
	}
	return &deviceBoundNet{Net: n, defaultIface: ifaceName}, nil
}

func (n *deviceBoundNet) ListenPacket(network, address string) (net.PacketConn, error) {
	lc := n.listenConfig(address)
	return lc.ListenPacket(context.Background(), network, address)
}

func (n *deviceBoundNet) ListenUDP(network string, laddr *net.UDPAddr) (transport.UDPConn, error) {
	lc := n.listenConfig(udpAddrString(laddr))
	conn, err := lc.ListenPacket(context.Background(), network, udpAddrString(laddr))
	if err != nil {
		return nil, err
	}
	return conn.(*net.UDPConn), nil
}

func (n *deviceBoundNet) Dial(network, address string) (net.Conn, error) {
	d := n.dialer(nil)
	return d.Dial(network, address)
}

func (n *deviceBoundNet) DialUDP(network string, laddr, raddr *net.UDPAddr) (transport.UDPConn, error) {
	address := ""
	if raddr != nil {
		address = raddr.String()
	}
	d := n.dialer(laddr)
	conn, err := d.Dial(network, address)
	if err != nil {
		return nil, err
	}
	return conn.(*net.UDPConn), nil
}

func (n *deviceBoundNet) DialTCP(network string, laddr, raddr *net.TCPAddr) (transport.TCPConn, error) {
	address := ""
	if raddr != nil {
		address = raddr.String()
	}
	d := n.dialerTCP(laddr)
	conn, err := d.Dial(network, address)
	if err != nil {
		return nil, err
	}
	return conn.(*net.TCPConn), nil
}

func (n *deviceBoundNet) ListenTCP(network string, laddr *net.TCPAddr) (transport.TCPListener, error) {
	lc := n.listenConfig(tcpAddrString(laddr))
	listener, err := lc.Listen(context.Background(), network, tcpAddrString(laddr))
	if err != nil {
		return nil, err
	}
	return deviceBoundTCPListener{TCPListener: listener.(*net.TCPListener)}, nil
}

func (n *deviceBoundNet) CreateDialer(d *net.Dialer) transport.Dialer {
	if d == nil {
		d = &net.Dialer{}
	}
	d.Control = n.controlForAddr(d.LocalAddr)
	return n.Net.CreateDialer(d)
}

func (n *deviceBoundNet) CreateListenConfig(lc *net.ListenConfig) transport.ListenConfig {
	if lc == nil {
		lc = &net.ListenConfig{}
	}
	lc.Control = n.controlForAddress("")
	return n.Net.CreateListenConfig(lc)
}

func (n *deviceBoundNet) listenConfig(address string) net.ListenConfig {
	return net.ListenConfig{Control: n.controlForAddress(address)}
}

func (n *deviceBoundNet) dialer(laddr *net.UDPAddr) net.Dialer {
	return net.Dialer{LocalAddr: laddr, Control: n.controlForAddr(laddr)}
}

func (n *deviceBoundNet) dialerTCP(laddr *net.TCPAddr) net.Dialer {
	return net.Dialer{LocalAddr: laddr, Control: n.controlForAddr(laddr)}
}

func (n *deviceBoundNet) controlForAddress(address string) func(string, string, syscall.RawConn) error {
	return func(network, _ string, c syscall.RawConn) error {
		ifName := n.ifaceForAddress(address)
		if ifName == "" {
			return nil
		}
		return bindSocketToDevice(network, address, ifName, c)
	}
}

func (n *deviceBoundNet) controlForAddr(addr net.Addr) func(string, string, syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		ifName := n.ifaceForNetAddr(addr)
		if ifName == "" {
			ifName = n.defaultIface
		}
		return bindSocketToDevice(network, address, ifName, c)
	}
}

func (n *deviceBoundNet) ifaceForAddress(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return n.defaultIface
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() {
		return n.defaultIface
	}
	ifName, err := interfaceNameForIP(ip.String())
	if err != nil {
		return n.defaultIface
	}
	return ifName
}

func (n *deviceBoundNet) ifaceForNetAddr(addr net.Addr) string {
	switch a := addr.(type) {
	case *net.UDPAddr:
		if a != nil && a.IP != nil && !a.IP.IsUnspecified() {
			ifName, err := interfaceNameForIP(a.IP.String())
			if err == nil {
				return ifName
			}
		}
	case *net.TCPAddr:
		if a != nil && a.IP != nil && !a.IP.IsUnspecified() {
			ifName, err := interfaceNameForIP(a.IP.String())
			if err == nil {
				return ifName
			}
		}
	}
	return ""
}

func udpAddrString(addr *net.UDPAddr) string {
	if addr == nil {
		return "0.0.0.0:0"
	}
	return addr.String()
}

func tcpAddrString(addr *net.TCPAddr) string {
	if addr == nil {
		return "0.0.0.0:0"
	}
	return addr.String()
}

type deviceBoundTCPListener struct {
	*net.TCPListener
}

func (l deviceBoundTCPListener) AcceptTCP() (transport.TCPConn, error) {
	return l.TCPListener.AcceptTCP()
}
