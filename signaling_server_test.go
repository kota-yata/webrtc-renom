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
	sessionID := server.registerPeer("peer-b", "peer-a")

	params := webrtc.ICEParameters{
		UsernameFragment: "ufrag-a",
		Password:         "pwd-a",
	}
	server.mu.Lock()
	server.enqueue("peer-b", SignalEvent{
		Type:       SignalEventAuth,
		FromPeerID: "peer-a",
		Params:     &params,
	})
	server.mu.Unlock()

	events, err := server.poll(context.Background(), "peer-b", sessionID)
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
	otherEvents, err := server.poll(ctx, "peer-a", "missing-session")
	if !errors.Is(err, errInvalidSignalingSession) {
		t.Fatalf("expected peer-a poll timeout, got events=%v err=%v", otherEvents, err)
	}
}

func TestSignalingServerRegisterClearsQueuedEvents(t *testing.T) {
	server := NewSignalingServer()

	params := webrtc.ICEParameters{
		UsernameFragment: "stale-ufrag",
		Password:         "stale-pwd",
	}
	server.registerPeer("peer-b", "peer-a")
	server.mu.Lock()
	server.enqueue("peer-b", SignalEvent{
		Type:       SignalEventAuth,
		FromPeerID: "peer-a",
		Params:     &params,
	})
	server.mu.Unlock()

	sessionID := server.registerPeer("peer-b", "peer-a")

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	events, err := server.poll(ctx, "peer-b", sessionID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected poll timeout after register cleared queue, got events=%v err=%v", events, err)
	}
}

func TestSignalingServerPeerRegisteredRequiresMutualRegister(t *testing.T) {
	server := NewSignalingServer()

	sessionA := server.registerPeer("peer-a", "peer-b")

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	events, err := server.poll(ctx, "peer-a", sessionA)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected peer-a poll timeout before peer-b register, got events=%v err=%v", events, err)
	}

	sessionB := server.registerPeer("peer-b", "peer-a")

	eventsA, err := server.poll(context.Background(), "peer-a", sessionA)
	if err != nil {
		t.Fatalf("poll peer-a: %v", err)
	}
	if len(eventsA) != 1 || eventsA[0].Type != SignalEventPeerRegistered || eventsA[0].FromPeerID != "peer-b" {
		t.Fatalf("unexpected peer-a events: %+v", eventsA)
	}

	eventsB, err := server.poll(context.Background(), "peer-b", sessionB)
	if err != nil {
		t.Fatalf("poll peer-b: %v", err)
	}
	if len(eventsB) != 1 || eventsB[0].Type != SignalEventPeerRegistered || eventsB[0].FromPeerID != "peer-a" {
		t.Fatalf("unexpected peer-b events: %+v", eventsB)
	}
}

func TestSignalingServerRejectsStaleSessionPoll(t *testing.T) {
	server := NewSignalingServer()

	staleSession := server.registerPeer("peer-b", "peer-a")
	sessionA := server.registerPeer("peer-a", "peer-b")
	currentSession := server.registerPeer("peer-b", "peer-a")

	events, err := server.poll(context.Background(), "peer-b", staleSession)
	if !errors.Is(err, errInvalidSignalingSession) {
		t.Fatalf("expected stale session error, got events=%v err=%v", events, err)
	}

	eventsB, err := server.poll(context.Background(), "peer-b", currentSession)
	if err != nil {
		t.Fatalf("poll current peer-b session: %v", err)
	}
	if len(eventsB) != 1 || eventsB[0].Type != SignalEventPeerRegistered || eventsB[0].FromPeerID != "peer-a" {
		t.Fatalf("unexpected current peer-b events: %+v", eventsB)
	}

	eventsA, err := server.poll(context.Background(), "peer-a", sessionA)
	if err != nil {
		t.Fatalf("poll peer-a: %v", err)
	}
	if len(eventsA) == 0 || eventsA[0].Type != SignalEventPeerRegistered || eventsA[0].FromPeerID != "peer-b" {
		t.Fatalf("unexpected peer-a events: %+v", eventsA)
	}
}
