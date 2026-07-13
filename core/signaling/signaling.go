// Package signaling implements the per-node offer/answer HTTP endpoint that
// lets two WalkieTalkie nodes set up a WebRTC session without a dedicated
// signaling server, per docs/2026-07-13-implementation-plan.md ("Audio
// layer"). Each node runs its own tiny HTTP server; the mDNS TXT record's
// "sig" field tells peers which port to reach it on. Since same-subnet ICE
// gathers usable host candidates directly (no STUN/TURN needed on a pure
// LAN), a single-round-trip, non-trickle SDP exchange is enough to start:
// the caller POSTs its offer and gets the answer back in the same response.
package signaling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// OnOffer is called with the sender's device ID and an incoming SDP offer
// from a peer wanting to start a session; it returns this node's SDP
// answer. Implemented by core/media, which needs the sender ID to attribute
// the resulting peer connection to the right Device.
type OnOffer func(senderID, offerSDP string) (answerSDP string, err error)

type offerRequest struct {
	Sender string `json:"sender"`
	SDP    string `json:"sdp"`
}

type offerResponse struct {
	SDP string `json:"sdp"`
}

// Server is one node's signaling listener.
type Server struct {
	onOffer OnOffer
	ln      net.Listener
	http    *http.Server
}

// NewServer creates a signaling server that calls onOffer for every incoming
// offer. Call Start to bind and begin serving.
func NewServer(onOffer OnOffer) *Server {
	return &Server{onOffer: onOffer}
}

// Start binds an OS-assigned free TCP port (unless port is non-zero) and
// begins serving in the background. The bound port is returned so the
// caller can publish it via the mDNS "sig" TXT field.
func (s *Server) Start(port int) (int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return 0, fmt.Errorf("signaling: listen: %w", err)
	}
	s.ln = ln

	mux := http.NewServeMux()
	mux.HandleFunc("POST /offer", s.handleOffer)
	s.http = &http.Server{Handler: mux}

	go s.http.Serve(ln)

	return ln.Addr().(*net.TCPAddr).Port, nil
}

// Shutdown stops the signaling server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

func (s *Server) handleOffer(w http.ResponseWriter, r *http.Request) {
	var req offerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	answer, err := s.onOffer(req.Sender, req.SDP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(offerResponse{SDP: answer})
}

// PostOffer sends offerSDP (from senderID) to a peer's signaling endpoint
// and returns its SDP answer.
func PostOffer(ctx context.Context, host string, port int, senderID, offerSDP string) (string, error) {
	body, err := json.Marshal(offerRequest{Sender: senderID, SDP: offerSDP})
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("http://%s:%d/offer", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("signaling: post offer to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("signaling: %s returned %s", url, resp.Status)
	}

	var res offerResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return res.SDP, nil
}
