// Package relay provides a minimal WebRTC SFU for mesh force-relay /
// ICE-fail fallback (Phase 3 desktop hardening). Peers open one
// PeerConnection to a Hub; Opus frames are forwarded to every other
// participant on a single outbound track each (star SFU, no mid-call
// renegotiation).
package relay

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const opusFrameDuration = 20 * time.Millisecond

// Hub is an in-process SFU. The Base Station hosts one; clients dial it over
// HTTP signaling (see server/relay).
type Hub struct {
	mu           sync.Mutex
	api          *webrtc.API
	participants map[string]*participant
	// routes maps sender ID → exclusive recipient ID for private Talk.
	// When set, writeToOthers forwards only to that recipient (not the full mesh).
	routes map[string]string
	// rooms maps participant (or local publisher) ID → room name.
	// Empty room is the group mesh. Non-empty rooms only fan out within the room.
	rooms map[string]string

	// OnRemoteFrame is called for every Opus payload from a remote
	// participant (Base Station local playback / accounting). Callers should
	// check RouteOf before playing when private unicast must stay off the
	// Base Station speaker.
	OnRemoteFrame func(fromID string, payload []byte)

	// OnParticipantJoined is called after a peer successfully joins the Hub.
	OnParticipantJoined func(id string)
}

type participant struct {
	id       string
	pc       *webrtc.PeerConnection
	outbound *webrtc.TrackLocalStaticSample // audio from everyone else → this peer
}

// NewHub builds an empty SFU hub.
func NewHub() (*Hub, error) {
	m := webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("relay: register codecs: %w", err)
	}
	return &Hub{
		api:          webrtc.NewAPI(webrtc.WithMediaEngine(&m)),
		participants: make(map[string]*participant),
		routes:       make(map[string]string),
		rooms:        make(map[string]string),
	}, nil
}

// ParticipantCount returns how many remote peers are currently on the hub.
func (h *Hub) ParticipantCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.participants)
}

// Has reports whether id is currently joined.
func (h *Hub) Has(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.participants[id]
	return ok
}

// HandleOffer answers an SDP offer from senderID and joins them to the SFU.
func (h *Hub) HandleOffer(senderID, offerSDP string) (answerSDP string, err error) {
	h.mu.Lock()
	if old, ok := h.participants[senderID]; ok {
		_ = old.pc.Close()
		delete(h.participants, senderID)
	}
	h.mu.Unlock()

	pc, err := h.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return "", fmt.Errorf("relay: new pc: %w", err)
	}

	out, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"wt-sfu",
	)
	if err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("relay: new outbound track: %w", err)
	}
	if _, err := pc.AddTrack(out); err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("relay: add outbound track: %w", err)
	}

	p := &participant{id: senderID, pc: pc, outbound: out}

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go h.forwardFrom(senderID, remote)
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed ||
			state == webrtc.PeerConnectionStateDisconnected {
			h.Remove(senderID)
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}); err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("relay: set remote: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("relay: create answer: %w", err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("relay: set local: %w", err)
	}
	<-gatherComplete

	h.mu.Lock()
	h.participants[senderID] = p
	h.mu.Unlock()

	if h.OnParticipantJoined != nil {
		h.OnParticipantJoined(senderID)
	}

	return pc.LocalDescription().SDP, nil
}

func (h *Hub) forwardFrom(fromID string, remote *webrtc.TrackRemote) {
	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			if err != io.EOF {
				return
			}
			return
		}
		payload := append([]byte(nil), pkt.Payload...)
		if h.OnRemoteFrame != nil {
			h.OnRemoteFrame(fromID, payload)
		}
		h.writeToOthers(fromID, payload)
	}
}

func (h *Hub) writeToOthers(fromID string, frame []byte) {
	sample := media.Sample{Data: frame, Duration: opusFrameDuration}
	h.mu.Lock()
	defer h.mu.Unlock()
	if toID, ok := h.routes[fromID]; ok {
		if p, ok := h.participants[toID]; ok && toID != fromID {
			_ = p.outbound.WriteSample(sample)
		}
		return
	}
	fromRoom := h.rooms[fromID]
	for id, p := range h.participants {
		if id == fromID {
			continue
		}
		if h.rooms[id] != fromRoom {
			continue
		}
		_ = p.outbound.WriteSample(sample)
	}
}

// SetRoute restricts fromID's Opus to a single recipient until ClearRoute.
// Used for private-channel live Talk over the SFU.
func (h *Hub) SetRoute(fromID, toID string) {
	if fromID == "" || toID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.routes == nil {
		h.routes = make(map[string]string)
	}
	h.routes[fromID] = toID
}

// ClearRoute removes a private unicast restriction for fromID (resume fan-out).
func (h *Hub) ClearRoute(fromID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.routes, fromID)
}

// RouteOf returns the private unicast target for fromID, if any.
func (h *Hub) RouteOf(fromID string) (toID string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	toID, ok = h.routes[fromID]
	return toID, ok
}

// SetRoom places id in a named Hub room. Empty roomID joins the group mesh.
// Frame fan-out stays within the same room (unless SetRoute is active).
func (h *Hub) SetRoom(id, roomID string) {
	if id == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms == nil {
		h.rooms = make(map[string]string)
	}
	if roomID == "" {
		delete(h.rooms, id)
		return
	}
	h.rooms[id] = roomID
}

// ClearRoom returns id to the group mesh room.
func (h *Hub) ClearRoom(id string) {
	h.SetRoom(id, "")
}

// RoomOf returns the room for id (empty = group mesh).
func (h *Hub) RoomOf(id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rooms[id]
}

// InjectLocal pushes one Opus frame from a local publisher (Base Station mic)
// to SFU participants in the same room (or only the SetRoute target for fromID).
func (h *Hub) InjectLocal(fromID string, frame []byte) {
	h.writeToOthers(fromID, frame)
}

// InjectTo pushes one Opus frame from a local publisher to a single Hub
// participant (Base Station private Talk to an SFU-only peer).
func (h *Hub) InjectTo(fromID, toID string, frame []byte) {
	if toID == "" || toID == fromID {
		return
	}
	sample := media.Sample{Data: frame, Duration: opusFrameDuration}
	h.mu.Lock()
	defer h.mu.Unlock()
	if p, ok := h.participants[toID]; ok {
		_ = p.outbound.WriteSample(sample)
	}
}

// Remove drops a participant from the hub.
func (h *Hub) Remove(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p, ok := h.participants[id]; ok {
		_ = p.pc.Close()
		delete(h.participants, id)
	}
	delete(h.routes, id)
	delete(h.rooms, id)
	for from, to := range h.routes {
		if to == id {
			delete(h.routes, from)
		}
	}
}

// Close tears down every participant.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, p := range h.participants {
		_ = p.pc.Close()
		delete(h.participants, id)
	}
	h.routes = make(map[string]string)
	h.rooms = make(map[string]string)
}
