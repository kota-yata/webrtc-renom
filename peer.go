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
	PeerID       string
	RemotePeerID string
	ServerURL    string
	TLSCACert    string
	ICEServers   []webrtc.ICEServer
	PollTimeout  time.Duration
}

type Peer struct {
	cfg      PeerConfig
	client   *SignalingClient
	endpoint *manualEndpoint
}

func NewPeer(cfg PeerConfig) (*Peer, error) {
	if cfg.PeerID == "" || cfg.RemotePeerID == "" || cfg.ServerURL == "" {
		return nil, fmt.Errorf("peer_id, remote_peer_id, and server_url are required")
	}
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = 25 * time.Second
	}
	endpoint, err := newManualEndpoint(endpointConfig{
		Controlling: cfg.Controlling,
		ICEServers:  cfg.ICEServers,
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
	defer func() {
		if err := p.endpoint.Close(); err != nil {
			log.Printf("close ICE endpoint failed: %v", err)
		}
	}()

	log.Printf("registering with signaling server peer=%s remote_peer=%s server=%s", p.cfg.PeerID, p.cfg.RemotePeerID, p.cfg.ServerURL)
	registerResp, err := p.client.Register(ctx, RegisterRequest{
		PeerID:       p.cfg.PeerID,
		RemotePeerID: p.cfg.RemotePeerID,
	})
	if err != nil {
		return fmt.Errorf("register with signaling server: %w", err)
	}
	log.Printf("registered with signaling server peer=%s remote_peer=%s session=%s", p.cfg.PeerID, p.cfg.RemotePeerID, registerResp.SessionID)

	peerRegisteredCh := make(chan struct{}, 1)
	remoteParamsCh := make(chan webrtc.ICEParameters, 1)
	go p.pollSignalEvents(ctx, registerResp.SessionID, peerRegisteredCh, remoteParamsCh)

	log.Printf("waiting for remote peer registration peer=%s remote_peer=%s", p.cfg.PeerID, p.cfg.RemotePeerID)
	select {
	case <-peerRegisteredCh:
		log.Printf("remote peer registered; starting ICE exchange peer=%s remote_peer=%s", p.cfg.PeerID, p.cfg.RemotePeerID)
	case <-ctx.Done():
		return ctx.Err()
	}

	p.endpoint.SetLocalCandidatePublisher(func(c webrtc.ICECandidate) error {
		return p.client.SendCandidate(ctx, CandidateMessage{
			SessionID:  registerResp.SessionID,
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
		SessionID:  registerResp.SessionID,
		FromPeerID: p.cfg.PeerID,
		ToPeerID:   p.cfg.RemotePeerID,
		Params:     localParams,
	}); err != nil {
		return fmt.Errorf("send ICE auth: %w", err)
	}

	monitor, err := newSignalMonitor()
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

	if err := p.endpoint.StartTransport(ctx, remoteParams); err != nil {
		return fmt.Errorf("ICE transport start failed: %w", err)
	}
	p.startAudioStreaming()

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

	log.Printf("peer is running; peer=%s controlling=%t", p.cfg.PeerID, p.cfg.Controlling)
	<-ctx.Done()
	return ctx.Err()
}

func (p *Peer) startAudioStreaming() {
	conn := p.endpoint.Conn()
	if conn == nil {
		log.Printf("audio streaming unavailable: ICE connection is nil")
		return
	}

	if p.cfg.Controlling {
		go func() {
			log.Printf("controlling peer waiting to receive 440hz.mp3 audio stream")
			audioReceiver := NewAudioReceiver(conn)
			if err := audioReceiver.ReceiveAudio(); err != nil {
				log.Printf("controlling peer audio receiving failed: %v", err)
			}
		}()
		return
	}

	go func() {
		log.Printf("controlled peer starting 440hz.mp3 audio stream")
		audioStreamer := NewAudioStreamer(conn)
		if err := audioStreamer.StreamAudio(); err != nil {
			log.Printf("controlled peer audio streaming failed: %v", err)
		}
	}()
}

func (p *Peer) pollSignalEvents(ctx context.Context, sessionID string, peerRegisteredCh chan<- struct{}, remoteParamsCh chan<- webrtc.ICEParameters) {
	log.Printf("starting signaling event poll peer=%s session=%s", p.cfg.PeerID, sessionID)
	for {
		events, err := p.client.PollEvents(ctx, p.cfg.PeerID, sessionID, p.cfg.PollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			log.Printf("poll signaling events failed: %v", err)
			time.Sleep(time.Second)
			continue
		}
		if len(events) == 0 {
			log.Printf("signaling event poll timeout peer=%s", p.cfg.PeerID)
			continue
		}

		for _, ev := range events {
			switch ev.Type {
			case SignalEventPeerRegistered:
				if ev.FromPeerID != p.cfg.RemotePeerID {
					log.Printf("ignore peer_registered from unexpected peer=%s", ev.FromPeerID)
					continue
				}
				select {
				case peerRegisteredCh <- struct{}{}:
				default:
				}
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
