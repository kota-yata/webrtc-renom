package renom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SignalingServer struct {
	mu    sync.Mutex
	peers map[string]*signalMailbox
}

type signalMailbox struct {
	queue  []SignalEvent
	waitCh chan struct{}
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
	if req.PeerID == "" {
		http.Error(w, "peer_id is required", http.StatusBadRequest)
		return
	}

	s.registerPeer(req.PeerID)
	log.Printf("signal register peer=%s remote=%s", req.PeerID, r.RemoteAddr)
	w.WriteHeader(http.StatusAccepted)
}

func (s *SignalingServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req AuthMessage
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.FromPeerID == "" || req.ToPeerID == "" {
		http.Error(w, "from_peer_id and to_peer_id are required", http.StatusBadRequest)
		return
	}

	log.Printf("signal auth from=%s to=%s ufrag=%s remote=%s", req.FromPeerID, req.ToPeerID, req.Params.UsernameFragment, r.RemoteAddr)
	s.enqueue(req.ToPeerID, SignalEvent{
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
	if req.FromPeerID == "" || req.ToPeerID == "" {
		http.Error(w, "from_peer_id and to_peer_id are required", http.StatusBadRequest)
		return
	}

	log.Printf("signal candidate from=%s to=%s type=%s protocol=%s addr=%s port=%d related=%s:%d remote=%s",
		req.FromPeerID,
		req.ToPeerID,
		req.Candidate.Typ,
		req.Candidate.Protocol,
		req.Candidate.Address,
		req.Candidate.Port,
		req.Candidate.RelatedAddress,
		req.Candidate.RelatedPort,
		r.RemoteAddr,
	)
	s.enqueue(req.ToPeerID, SignalEvent{
		Type:       SignalEventCandidate,
		FromPeerID: req.FromPeerID,
		Candidate:  &req.Candidate,
	})
	w.WriteHeader(http.StatusAccepted)
}

func (s *SignalingServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	peerID := r.URL.Query().Get("peer_id")
	if peerID == "" {
		http.Error(w, "peer_id is required", http.StatusBadRequest)
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

	events, err := s.poll(ctx, peerID)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(events) > 0 {
		log.Printf("signal events peer=%s count=%d remote=%s summary=%s", peerID, len(events), r.RemoteAddr, signalEventSummary(events))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(PollResponse{Events: events})
}

func signalEventSummary(events []SignalEvent) string {
	summaries := make([]string, 0, len(events))
	for _, ev := range events {
		switch ev.Type {
		case SignalEventAuth:
			ufrag := ""
			if ev.Params != nil {
				ufrag = ev.Params.UsernameFragment
			}
			summaries = append(summaries, "auth from="+ev.FromPeerID+" ufrag="+ufrag)
		case SignalEventCandidate:
			if ev.Candidate == nil {
				summaries = append(summaries, "candidate from="+ev.FromPeerID)
				continue
			}
			summaries = append(summaries, fmt.Sprintf("candidate from=%s type=%s protocol=%s addr=%s:%d",
				ev.FromPeerID,
				ev.Candidate.Typ,
				ev.Candidate.Protocol,
				ev.Candidate.Address,
				ev.Candidate.Port,
			))
		default:
			summaries = append(summaries, string(ev.Type)+" from="+ev.FromPeerID)
		}
	}

	return strings.Join(summaries, ", ")
}

func (s *SignalingServer) poll(ctx context.Context, peerID string) ([]SignalEvent, error) {
	for {
		s.mu.Lock()
		box := s.ensureMailboxLocked(peerID)
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

func (s *SignalingServer) enqueue(peerID string, ev SignalEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	box := s.ensureMailboxLocked(peerID)
	box.queue = append(box.queue, ev)
	close(box.waitCh)
	box.waitCh = make(chan struct{})
}

func (s *SignalingServer) registerPeer(peerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if box := s.peers[peerID]; box != nil {
		close(box.waitCh)
	}
	s.peers[peerID] = &signalMailbox{waitCh: make(chan struct{})}
}

func (s *SignalingServer) ensureMailboxLocked(peerID string) *signalMailbox {
	box := s.peers[peerID]
	if box == nil {
		box = &signalMailbox{waitCh: make(chan struct{})}
		s.peers[peerID] = box
	}

	return box
}
