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

type SignalingServer struct {
	mu       sync.Mutex
	sessions map[string]*signalSession
}

type signalSession struct {
	peers map[string]*signalMailbox
}

type signalMailbox struct {
	queue  []SignalEvent
	waitCh chan struct{}
}

func NewSignalingServer() *SignalingServer {
	return &SignalingServer{
		sessions: map[string]*signalSession{},
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
	if req.SessionID == "" || req.PeerID == "" {
		http.Error(w, "session_id and peer_id are required", http.StatusBadRequest)
		return
	}

	s.ensureMailbox(req.SessionID, req.PeerID)
	w.WriteHeader(http.StatusAccepted)
}

func (s *SignalingServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req AuthMessage
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || req.FromPeerID == "" || req.ToPeerID == "" {
		http.Error(w, "session_id, from_peer_id, to_peer_id are required", http.StatusBadRequest)
		return
	}

	s.enqueue(req.SessionID, req.ToPeerID, SignalEvent{
		Type:       SignalEventAuth,
		FromPeerID: req.FromPeerID,
		Params:     &req.Params,
	})
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
		http.Error(w, "session_id, from_peer_id, to_peer_id are required", http.StatusBadRequest)
		return
	}

	s.enqueue(req.SessionID, req.ToPeerID, SignalEvent{
		Type:       SignalEventCandidate,
		FromPeerID: req.FromPeerID,
		Candidate:  &req.Candidate,
	})
	w.WriteHeader(http.StatusAccepted)
}

func (s *SignalingServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	peerID := r.URL.Query().Get("peer_id")
	if sessionID == "" || peerID == "" {
		http.Error(w, "session_id and peer_id are required", http.StatusBadRequest)
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

	events, err := s.poll(ctx, sessionID, peerID)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(PollResponse{Events: events})
}

func (s *SignalingServer) poll(ctx context.Context, sessionID, peerID string) ([]SignalEvent, error) {
	for {
		s.mu.Lock()
		box := s.ensureMailboxLocked(sessionID, peerID)
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

func (s *SignalingServer) enqueue(sessionID, peerID string, ev SignalEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	box := s.ensureMailboxLocked(sessionID, peerID)
	box.queue = append(box.queue, ev)
	close(box.waitCh)
	box.waitCh = make(chan struct{})
}

func (s *SignalingServer) ensureMailbox(sessionID, peerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMailboxLocked(sessionID, peerID)
}

func (s *SignalingServer) ensureMailboxLocked(sessionID, peerID string) *signalMailbox {
	session := s.sessions[sessionID]
	if session == nil {
		session = &signalSession{peers: map[string]*signalMailbox{}}
		s.sessions[sessionID] = session
	}

	box := session.peers[peerID]
	if box == nil {
		box = &signalMailbox{waitCh: make(chan struct{})}
		session.peers[peerID] = box
	}

	return box
}
