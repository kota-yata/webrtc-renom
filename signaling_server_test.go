package renom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestSignalingServerRoutesByPeerID(t *testing.T) {
	server := NewSignalingServer()

	params := webrtc.ICEParameters{
		UsernameFragment: "ufrag-a",
		Password:         "pwd-a",
	}
	server.enqueue("peer-b", SignalEvent{
		Type:       SignalEventAuth,
		FromPeerID: "peer-a",
		Params:     &params,
	})

	events, err := server.poll(context.Background(), "peer-b")
	if err != nil {
		t.Fatalf("poll peer-b: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].FromPeerID != "peer-a" || events[0].Params == nil || events[0].Params.UsernameFragment != "ufrag-a" {
		t.Fatalf("unexpected event: %+v", events[0])
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	otherEvents, err := server.poll(ctx, "peer-a")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected peer-a poll timeout, got events=%v err=%v", otherEvents, err)
	}
}

func TestSignalingServerRegisterClearsQueuedEvents(t *testing.T) {
	server := NewSignalingServer()

	params := webrtc.ICEParameters{
		UsernameFragment: "stale-ufrag",
		Password:         "stale-pwd",
	}
	server.enqueue("peer-b", SignalEvent{
		Type:       SignalEventAuth,
		FromPeerID: "peer-a",
		Params:     &params,
	})

	server.registerPeer("peer-b")

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	events, err := server.poll(ctx, "peer-b")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected poll timeout after register cleared queue, got events=%v err=%v", events, err)
	}
}
