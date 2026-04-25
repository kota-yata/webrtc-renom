package renom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type SignalingClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewSignalingClient(baseURL string) *SignalingClient {
	return &SignalingClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 35 * time.Second,
		},
	}
}

func (c *SignalingClient) Register(ctx context.Context, req RegisterRequest) error {
	return c.post(ctx, "/register", req, nil)
}

func (c *SignalingClient) SendAuth(ctx context.Context, req AuthMessage) error {
	return c.post(ctx, "/auth", req, nil)
}

func (c *SignalingClient) SendCandidate(ctx context.Context, req CandidateMessage) error {
	return c.post(ctx, "/candidate", req, nil)
}

func (c *SignalingClient) PollEvents(ctx context.Context, sessionID, peerID string, timeout time.Duration) ([]SignalEvent, error) {
	q := url.Values{}
	q.Set("session_id", sessionID)
	q.Set("peer_id", peerID)
	q.Set("timeout_ms", strconv.FormatInt(timeout.Milliseconds(), 10))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/events?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status from /events: %s", resp.Status)
	}

	var out PollResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	return out.Events, nil
}

func (c *SignalingClient) post(ctx context.Context, path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status from %s: %s", path, resp.Status)
	}

	if out == nil {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
