package renom

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

type PeerConfig struct {
	Controlling  bool
	SessionID    string
	PeerID       string
	RemotePeerID string
	ServerURL    string
	TLSCACert    string
	WifiIfName   string
	GatherIfaces []string
	ICEServers   []webrtc.ICEServer
	PollTimeout  time.Duration
}

type Peer struct {
	cfg      PeerConfig
	client   *SignalingClient
	endpoint *manualEndpoint
}

func NewPeer(cfg PeerConfig) (*Peer, error) {
	if cfg.SessionID == "" || cfg.PeerID == "" || cfg.RemotePeerID == "" || cfg.ServerURL == "" {
		return nil, fmt.Errorf("session_id, peer_id, remote_peer_id, and server_url are required")
	}
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = 25 * time.Second
	}
	endpoint, err := newManualEndpoint(endpointConfig{
		Controlling:      cfg.Controlling,
		GatherInterfaces: cfg.GatherIfaces,
		ICEServers:       cfg.ICEServers,
	})
	if err != nil {
		return nil, err
	}

	if addrs, err := ListActiveInterfaceAddrs(); err == nil {
		summaries := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			flags := make([]string, 0, 3)
			if addr.IsWiFi {
				flags = append(flags, "wifi")
			}
			if addr.IsEthernet {
				flags = append(flags, "ethernet")
			}
			if addr.IsCellularLike {
				flags = append(flags, "cellular_like")
			}
			if len(flags) == 0 {
				flags = append(flags, "other")
			}

			summaries = append(summaries, fmt.Sprintf("%s=%s(%s)", addr.Name, addr.IP, strings.Join(flags, "+")))
		}
		log.Printf("discovered active interfaces: %s", strings.Join(summaries, ", "))
	}

	client, err := NewSignalingClientWithCACert(cfg.ServerURL, cfg.TLSCACert)
	if err != nil {
		return nil, err
	}

	return &Peer{
		cfg:      cfg,
		client:   client,
		endpoint: endpoint,
	}, nil
}

func (p *Peer) Run(ctx context.Context) error {
	if err := p.client.Register(ctx, RegisterRequest{
		SessionID: p.cfg.SessionID,
		PeerID:    p.cfg.PeerID,
	}); err != nil {
		return fmt.Errorf("register with signaling server: %w", err)
	}

	remoteParamsCh := make(chan webrtc.ICEParameters, 1)
	go p.pollSignalEvents(ctx, remoteParamsCh)

	p.endpoint.SetLocalCandidatePublisher(func(c webrtc.ICECandidate) error {
		return p.client.SendCandidate(ctx, CandidateMessage{
			SessionID:  p.cfg.SessionID,
			FromPeerID: p.cfg.PeerID,
			ToPeerID:   p.cfg.RemotePeerID,
			Candidate:  c,
		})
	})

	if err := p.endpoint.StartGathering(); err != nil {
		return fmt.Errorf("ICE gather failed: %w", err)
	}

	localParams, err := p.endpoint.LocalParameters()
	if err != nil {
		return fmt.Errorf("get local ICE params: %w", err)
	}

	if err := p.client.SendAuth(ctx, AuthMessage{
		SessionID:  p.cfg.SessionID,
		FromPeerID: p.cfg.PeerID,
		ToPeerID:   p.cfg.RemotePeerID,
		Params:     localParams,
	}); err != nil {
		return fmt.Errorf("send ICE auth: %w", err)
	}

	monitor, err := newSignalMonitor(p.cfg.WifiIfName)
	if err != nil {
		log.Printf("WiFi address monitoring unavailable, continuing without monitor: %v", err)
	} else {
		defer monitor.Close()
	}

	var remoteParams webrtc.ICEParameters
	select {
	case remoteParams = <-remoteParamsCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := p.endpoint.StartTransport(remoteParams); err != nil {
		return fmt.Errorf("ICE transport start failed: %w", err)
	}

	if monitor != nil {
		events := monitor.Watch(ctx)
		go func() {
			for ev := range events {
				log.Printf("WiFi disabled or disconnected if=%s removed_ip=%s", ev.IfName, ev.RemovedIP)
				if err := p.endpoint.ForceHandoverToCellular(); err != nil {
					log.Printf("manual handover failed: %v", err)
				}
			}
		}()
	}

	log.Printf("peer is running; session=%s peer=%s controlling=%t", p.cfg.SessionID, p.cfg.PeerID, p.cfg.Controlling)
	<-ctx.Done()
	return ctx.Err()
}

func (p *Peer) pollSignalEvents(ctx context.Context, remoteParamsCh chan<- webrtc.ICEParameters) {
	for {
		events, err := p.client.PollEvents(ctx, p.cfg.SessionID, p.cfg.PeerID, p.cfg.PollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			log.Printf("poll signaling events failed: %v", err)
			time.Sleep(time.Second)
			continue
		}

		for _, ev := range events {
			switch ev.Type {
			case SignalEventAuth:
				if ev.Params == nil {
					continue
				}
				select {
				case remoteParamsCh <- *ev.Params:
				default:
				}
			case SignalEventCandidate:
				if ev.Candidate == nil {
					continue
				}
				if err := p.endpoint.AddRemoteCandidate(*ev.Candidate); err != nil {
					log.Printf("add remote candidate from signaling server failed: %v", err)
				}
			}
		}
	}
}
