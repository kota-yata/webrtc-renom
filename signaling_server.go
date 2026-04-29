package renom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"
)

var errInvalidSignalingSession = errors.New("invalid signaling session")

type SignalingServer struct {
	mu          sync.Mutex
	nextSession uint64
	peers       map[string]*signalMailbox
}

type signalMailbox struct {
	sessionID    string
	remotePeerID string
	queue        []SignalEvent
	waitCh       chan struct{}
}

func NewSignalingServer() *SignalingServer {
	return &SignalingServer{
		peers: map[string]*signalMailbox{},
	}
}

func (s *SignalingServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/auth", s.handleAuth)
	mux.HandleFunc("/candidate", s.handleCandidate)
	mux.HandleFunc("/events", s.handleEvents)
	return mux
}

func (s *SignalingServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.PeerID == "" || req.RemotePeerID == "" {
		http.Error(w, "peer_id and remote_peer_id are required", http.StatusBadRequest)
		return
	}

	sessionID := s.registerPeer(req.PeerID, req.RemotePeerID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(RegisterResponse{SessionID: sessionID})
}

func (s *SignalingServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req AuthMessage
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || req.FromPeerID == "" || req.ToPeerID == "" {
		http.Error(w, "session_id, from_peer_id, and to_peer_id are required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	fromBox := s.peers[req.FromPeerID]
	if fromBox == nil || fromBox.sessionID != req.SessionID {
		s.mu.Unlock()
		http.Error(w, "invalid signaling session", http.StatusConflict)
		return
	}
	enqueued := s.enqueue(req.ToPeerID, SignalEvent{
		Type:       SignalEventAuth,
		FromPeerID: req.FromPeerID,
		Params:     &req.Params,
	})
	s.mu.Unlock()
	if !enqueued {
		http.Error(w, "to_peer_id is not registered", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *SignalingServer) handleCandidate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req CandidateMessage
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || req.FromPeerID == "" || req.ToPeerID == "" {
		http.Error(w, "session_id, from_peer_id, and to_peer_id are required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	fromBox := s.peers[req.FromPeerID]
	if fromBox == nil || fromBox.sessionID != req.SessionID {
		s.mu.Unlock()
		http.Error(w, "invalid signaling session", http.StatusConflict)
		return
	}
	enqueued := s.enqueue(req.ToPeerID, SignalEvent{
		Type:       SignalEventCandidate,
		FromPeerID: req.FromPeerID,
		Candidate:  &req.Candidate,
	})
	s.mu.Unlock()
	if !enqueued {
		http.Error(w, "to_peer_id is not registered", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *SignalingServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	peerID := r.URL.Query().Get("peer_id")
	sessionID := r.URL.Query().Get("session_id")
	if peerID == "" || sessionID == "" {
		http.Error(w, "peer_id and session_id are required", http.StatusBadRequest)
		return
	}

	timeout := 25 * time.Second
	if raw := r.URL.Query().Get("timeout_ms"); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			timeout = time.Duration(ms) * time.Millisecond
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	events, err := s.poll(ctx, peerID, sessionID)
	if errors.Is(err, errInvalidSignalingSession) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(PollResponse{Events: events})
}

func (s *SignalingServer) poll(ctx context.Context, peerID, sessionID string) ([]SignalEvent, error) {
	for {
		s.mu.Lock()
		box := s.peers[peerID]
		if box == nil || box.sessionID != sessionID {
			s.mu.Unlock()
			return nil, errInvalidSignalingSession
		}
		if len(box.queue) > 0 {
			events := append([]SignalEvent(nil), box.queue...)
			box.queue = nil
			s.mu.Unlock()
			return events, nil
		}

		waitCh := box.waitCh
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-waitCh:
		}
	}
}

// enqueue requires s.mu to be held.
func (s *SignalingServer) enqueue(peerID string, ev SignalEvent) bool {
	box := s.peers[peerID]
	if box == nil {
		return false
	}
	box.queue = append(box.queue, ev)
	close(box.waitCh)
	box.waitCh = make(chan struct{})
	return true
}

func (s *SignalingServer) registerPeer(peerID, remotePeerID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if box := s.peers[peerID]; box != nil {
		close(box.waitCh)
	}
	s.nextSession++
	sessionID := peerID + "-" + strconv.FormatUint(s.nextSession, 10)
	s.peers[peerID] = &signalMailbox{
		sessionID:    sessionID,
		remotePeerID: remotePeerID,
		waitCh:       make(chan struct{}),
	}

	remoteBox := s.peers[remotePeerID]
	if remoteBox == nil || remoteBox.remotePeerID != peerID {
		return sessionID
	}

	s.enqueue(peerID, SignalEvent{
		Type:       SignalEventPeerRegistered,
		FromPeerID: remotePeerID,
	})
	s.enqueue(remotePeerID, SignalEvent{
		Type:       SignalEventPeerRegistered,
		FromPeerID: peerID,
	})
	return sessionID
}
