// Package relay wires core/relay's SFU Hub into the Base Station process
// (HTTP signaling on a dedicated port advertised via mDNS relay= TXT).
package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"

	corerelay "github.com/JohnDovey/WalkieTalkie/core/relay"
)

// Server hosts a Hub behind POST /offer.
type Server struct {
	Hub  *corerelay.Hub
	ln   net.Listener
	http *http.Server
}

type offerRequest struct {
	Sender string `json:"sender"`
	SDP    string `json:"sdp"`
}

type offerResponse struct {
	SDP string `json:"sdp"`
}

// New builds a Server around hub.
func New(hub *corerelay.Hub) *Server {
	return &Server{Hub: hub}
}

// Start listens on port (0 = ephemeral) and serves in the background.
func (s *Server) Start(port int) (int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return 0, fmt.Errorf("server/relay: listen: %w", err)
	}
	s.ln = ln
	mux := http.NewServeMux()
	mux.HandleFunc("POST /offer", s.handleOffer)
	s.http = &http.Server{Handler: mux}
	go func() {
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("server/relay: serve: %v", err)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// Shutdown stops the HTTP listener.
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
	if req.Sender == "" || req.SDP == "" {
		http.Error(w, "sender and sdp required", http.StatusBadRequest)
		return
	}
	answer, err := s.Hub.HandleOffer(req.Sender, req.SDP)
	if err != nil {
		log.Printf("server/relay: offer from %s: %v", req.Sender, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(offerResponse{SDP: answer})
}
