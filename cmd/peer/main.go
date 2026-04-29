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
		isControlling = flag.Bool("controlling", false, "run as the controlling side")
		serverURL     = flag.String("server-url", "https://127.0.0.1:8080", "signaling server base URL")
		tlsCACert     = flag.String("tls-ca-cert", "", "TLS CA certificate file used to verify the signaling server")
		peerID        = flag.String("peer-id", "peer-a", "local peer identifier")
		remotePeerID  = flag.String("remote-peer-id", "peer-b", "remote peer identifier")
		iceServers    = flag.String("ice-servers", "", "optional comma separated list of STUN/TURN server URLs")
		iceUsername   = flag.String("ice-username", "", "username for TURN server URLs")
		iceCredential = flag.String("ice-credential", "", "credential for TURN server URLs")
		pollTimeout   = flag.Duration("poll-timeout", 25*time.Second, "long-poll timeout used against the signaling server")
	)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	peer, err := renom.NewPeer(renom.PeerConfig{
		Controlling:  *isControlling,
		PeerID:       *peerID,
		RemotePeerID: *remotePeerID,
		ServerURL:    *serverURL,
		TLSCACert:    *tlsCACert,
		ICEServers:   renom.ICEServersFromFlags(*iceServers, *iceUsername, *iceCredential),
		PollTimeout:  *pollTimeout,
	})
	if err != nil {
		log.Fatalf("peer setup failed: %v", err)
	}

	if err := peer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("peer failed: %v", err)
	}
}
