package renom

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"runtime"
	"time"

	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
)

const (
	nl80211FamilyName = "nl80211"
	nl80211Version    = 1

	nl80211CmdSetCQM    = 0x3f
	nl80211CmdNotifyCQM = 0x40

	nl80211AttrIfindex = 0x03
	nl80211AttrCQM     = 0x5e

	nl80211AttrCQMRSSIThreshold      = 0x01
	nl80211AttrCQMRSSIHysteresis     = 0x02
	nl80211AttrCQMRSSIThresholdEvent = 0x03
	nl80211AttrCQMRSSILevel          = 0x09

	nl80211CQMRSSIThresholdEventLow = 0x00
)

type cqmEvent struct {
	IfName    string
	IfIndex   int
	RSSI      int32
	Threshold int32
	IsLow     bool
	RawCmd    uint8
}

type nl80211Monitor struct {
	conn    *genetlink.Conn
	family  genetlink.Family
	ifName  string
	ifIndex int
}

func newNL80211Monitor(ifName string) (*nl80211Monitor, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("nl80211 CQM monitor requires linux")
	}

	ifi, err := net.InterfaceByName(ifName)
	if err != nil {
		return nil, fmt.Errorf("lookup interface %q: %w", ifName, err)
	}

	conn, err := genetlink.Dial(nil)
	if err != nil {
		return nil, fmt.Errorf("dial genetlink: %w", err)
	}

	family, err := conn.GetFamily(nl80211FamilyName)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("get nl80211 family: %w", err)
	}

	return &nl80211Monitor{
		conn:    conn,
		family:  family,
		ifName:  ifName,
		ifIndex: ifi.Index,
	}, nil
}

func (m *nl80211Monitor) Close() error {
	if m == nil || m.conn == nil {
		return nil
	}

	return m.conn.Close()
}

func (m *nl80211Monitor) ConfigureRSSIThreshold(threshold int32, hysteresis uint32) error {
	ae := netlink.NewAttributeEncoder()
	ae.Uint32(nl80211AttrIfindex, uint32(m.ifIndex))
	ae.Nested(nl80211AttrCQM, func(nae *netlink.AttributeEncoder) error {
		nae.Int32(nl80211AttrCQMRSSIThreshold, threshold)
		nae.Uint32(nl80211AttrCQMRSSIHysteresis, hysteresis)
		return nil
	})

	data, err := ae.Encode()
	if err != nil {
		return fmt.Errorf("encode CQM setup: %w", err)
	}

	req := genetlink.Message{
		Header: genetlink.Header{
			Command: nl80211CmdSetCQM,
			Version: nl80211Version,
		},
		Data: data,
	}

	_, err = m.conn.Execute(req, m.family.ID, netlink.Request|netlink.Acknowledge)
	if err != nil {
		return fmt.Errorf("set nl80211 CQM: %w", err)
	}

	return nil
}

func (m *nl80211Monitor) JoinGroups(groupNames ...string) error {
	for _, want := range groupNames {
		for _, group := range m.family.Groups {
			if group.Name != want {
				continue
			}

			if err := m.conn.JoinGroup(group.ID); err != nil {
				return fmt.Errorf("join nl80211 multicast group %q: %w", want, err)
			}
		}
	}

	return nil
}

func (m *nl80211Monitor) Watch(ctx context.Context, threshold int32) <-chan cqmEvent {
	out := make(chan cqmEvent, 8)

	go func() {
		defer close(out)

		go func() {
			<-ctx.Done()
			_ = m.Close()
		}()

		for {
			msgs, _, err := m.conn.Receive()
			if err != nil {
				if ctx.Err() != nil {
					return
				}

				log.Printf("nl80211 receive error: %v", err)
				time.Sleep(250 * time.Millisecond)
				continue
			}

			for _, msg := range msgs {
				if msg.Header.Command != nl80211CmdNotifyCQM {
					continue
				}

				ev, ok, err := m.parseCQMEvent(msg, threshold)
				if err != nil {
					log.Printf("failed to parse CQM event: %v", err)
					continue
				}
				if !ok {
					continue
				}

				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out
}

func (m *nl80211Monitor) parseCQMEvent(msg genetlink.Message, threshold int32) (cqmEvent, bool, error) {
	ad, err := netlink.NewAttributeDecoder(msg.Data)
	if err != nil {
		return cqmEvent{}, false, err
	}

	ev := cqmEvent{
		IfName:    m.ifName,
		IfIndex:   m.ifIndex,
		Threshold: threshold,
		RawCmd:    msg.Header.Command,
	}
	var (
		ifIndex int
		isLow   bool
		hasEv   bool
	)

	for ad.Next() {
		switch ad.Type() {
		case nl80211AttrIfindex:
			ifIndex = int(ad.Uint32())
		case nl80211AttrCQM:
			ad.Nested(func(nad *netlink.AttributeDecoder) error {
				for nad.Next() {
					switch nad.Type() {
					case nl80211AttrCQMRSSIThresholdEvent:
						hasEv = true
						isLow = nad.Uint32() == nl80211CQMRSSIThresholdEventLow
					case nl80211AttrCQMRSSILevel:
						ev.RSSI = nad.Int32()
					}
				}
				return nad.Err()
			})
		}
	}

	if err := ad.Err(); err != nil {
		return cqmEvent{}, false, err
	}
	if ifIndex != 0 && ifIndex != m.ifIndex {
		return cqmEvent{}, false, nil
	}
	if !hasEv {
		return cqmEvent{}, false, nil
	}

	ev.IsLow = isLow
	return ev, true, nil
}
