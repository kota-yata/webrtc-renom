package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	renom "ice-signal-renom"
)

func main() {
	var (
		isControlling  = flag.Bool("controlling", false, "run as the controlling side")
		serverURL      = flag.String("server-url", "http://127.0.0.1:8080", "signaling server base URL")
		sessionID      = flag.String("session-id", "demo", "signaling session identifier")
		peerID         = flag.String("peer-id", "peer-a", "local peer identifier")
		remotePeerID   = flag.String("remote-peer-id", "peer-b", "remote peer identifier")
		wifiIfName     = flag.String("wifi-iface", "", "WiFi interface to monitor with nl80211 CQM; empty means auto-detect heuristically")
		rssiThreshold  = flag.Int("rssi-threshold", -75, "RSSI threshold in dBm that triggers manual handover")
		rssiHysteresis = flag.Int("rssi-hysteresis", 4, "RSSI hysteresis used for nl80211 CQM")
		gatherIfaces   = flag.String("gather-ifaces", "", "optional comma separated list of interfaces to gather on")
		pollTimeout    = flag.Duration("poll-timeout", 25*time.Second, "long-poll timeout used against the signaling server")
	)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	peer, err := renom.NewPeer(renom.PeerConfig{
		Controlling:    *isControlling,
		SessionID:      *sessionID,
		PeerID:         *peerID,
		RemotePeerID:   *remotePeerID,
		ServerURL:      *serverURL,
		WifiIfName:     *wifiIfName,
		RSSIThreshold:  *rssiThreshold,
		RSSIHysteresis: *rssiHysteresis,
		GatherIfaces:   renom.SplitCSV(*gatherIfaces),
		PollTimeout:    *pollTimeout,
	})
	if err != nil {
		log.Fatalf("peer setup failed: %v", err)
	}

	if err := peer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("peer failed: %v", err)
	}
}
