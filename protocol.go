package renom

import "github.com/pion/webrtc/v4"

type RegisterRequest struct {
	PeerID       string `json:"peer_id"`
	RemotePeerID string `json:"remote_peer_id"`
}

type RegisterResponse struct {
	SessionID string `json:"session_id"`
}

type AuthMessage struct {
	SessionID  string               `json:"session_id"`
	FromPeerID string               `json:"from_peer_id"`
	ToPeerID   string               `json:"to_peer_id"`
	Params     webrtc.ICEParameters `json:"params"`
}

type CandidateMessage struct {
	SessionID  string              `json:"session_id"`
	FromPeerID string              `json:"from_peer_id"`
	ToPeerID   string              `json:"to_peer_id"`
	Candidate  webrtc.ICECandidate `json:"candidate"`
}

type SignalEventType string

const (
	SignalEventPeerRegistered SignalEventType = "peer_registered"
	SignalEventAuth           SignalEventType = "auth"
	SignalEventCandidate      SignalEventType = "candidate"
)

type SignalEvent struct {
	Type       SignalEventType       `json:"type"`
	FromPeerID string                `json:"from_peer_id"`
	Params     *webrtc.ICEParameters `json:"params,omitempty"`
	Candidate  *webrtc.ICECandidate  `json:"candidate,omitempty"`
}

type PollResponse struct {
	Events []SignalEvent `json:"events"`
}
