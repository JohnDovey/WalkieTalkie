// Package api implements the server's REST surface: device registry reads,
// device-originated writes (announce/GPS/name/peer-report), and settings.
// See docs/2026-07-13-implementation-plan.md ("Registry + web UI").
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/config"
	"github.com/JohnDovey/WalkieTalkie/core/proto"
	"github.com/JohnDovey/WalkieTalkie/core/registry"
)

// Talker is the narrow interface the Talk control (see
// docs/2026-07-13-implementation-plan.md, "Web UI: Talk control") needs —
// satisfied by *media.PTTSession, kept as an interface here so api doesn't
// need to import core/media just for these method names.
type Talker interface {
	StartTalking()
	StartTalkingTo(peerID string)
	StartTalkingToPeers(peerIDs []string)
	StopTalking()
	DirectConnected(peerID string) bool
	RelayConnected(peerID string) bool
	LiveTalkAvailable(peerID string) bool
}

// DeviceListEnricher optionally wraps GET /api/devices with extra fields
// (e.g. pendingVoiceNotes). Implemented by server/voicenote.Handlers.
type DeviceListEnricher interface {
	EnrichDevices(devices []*registry.Device) (any, error)
}

// Handlers wires the registry store (and, for settings changes, a restart
// hook) into HTTP handlers.
type Handlers struct {
	Store  *registry.Store
	Talker Talker // the server's own PTT session; nil if audio is unavailable

	// SelfID, SelfName, Platform, and Version describe this Base Station
	// itself, for the About endpoint.
	SelfID   string
	SelfName string
	Platform string
	Version  string

	// EnrichDevices, when set, replaces the raw device list JSON with an
	// enriched payload (voice-note waiting counts, etc.).
	EnrichDevices DeviceListEnricher

	// OnDeviceSeen is called after a successful announce upsert with whether
	// the device was newly created.
	OnDeviceSeen func(created bool)

	// OnSettingsChanged is called after new settings are persisted, so
	// main.go can restart the HTTP listener on a new port if it changed.
	OnSettingsChanged func(config.Settings)

	// OnLocationUpdated is called after a successful GPS POST so the Base
	// Station can recompute its mean location estimate.
	OnLocationUpdated func(deviceID string)

	// ChannelTalkPeers, when set, returns LiveTalk peers for a private channel
	// (other focused participants, else the channel peer), excluding SelfID.
	ChannelTalkPeers func(channelID string) []string
}

// AboutInfo is the response shape for GET /api/about.
type AboutInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Version  string `json:"version"`
}

// Register attaches every route to mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/devices", h.listDevices)
	mux.HandleFunc("GET /api/devices/{id}", h.getDevice)
	mux.HandleFunc("GET /api/devices/{id}/gps-history", h.gpsHistory)
	mux.HandleFunc("GET /api/time", h.serverTime)
	mux.HandleFunc("POST /api/devices/announce", h.announce)
	mux.HandleFunc("POST /api/devices/{id}/location", h.updateLocation)
	mux.HandleFunc("PUT /api/devices/{id}/name", h.updateName)
	mux.HandleFunc("POST /api/devices/peer-reports", h.peerReport)
	mux.HandleFunc("GET /api/settings", h.getSettings)
	mux.HandleFunc("PUT /api/settings", h.putSettings)
	mux.HandleFunc("GET /api/about", h.about)
	mux.HandleFunc("POST /api/talk/start", h.talkStart)
	mux.HandleFunc("POST /api/talk/stop", h.talkStop)
	mux.HandleFunc("GET /api/talk/peer", h.talkPeer)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func badRequest(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func serverError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (h *Handlers) listDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := h.Store.List()
	if err != nil {
		serverError(w, err)
		return
	}
	if h.EnrichDevices != nil {
		enriched, err := h.EnrichDevices.EnrichDevices(devices)
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, enriched)
		return
	}
	writeJSON(w, devices)
}

func (h *Handlers) getDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, ok, err := h.Store.Get(id)
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, d)
}

func (h *Handlers) gpsHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	pts, err := h.Store.ListGPSHistory(id, limit)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, pts)
}

func (h *Handlers) serverTime(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	writeJSON(w, map[string]any{
		"unixMs":  now.UnixMilli(),
		"rfc3339": now.Format(time.RFC3339Nano),
	})
}

func (h *Handlers) announce(w http.ResponseWriter, r *http.Request) {
	var payload proto.AnnouncePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		badRequest(w, err)
		return
	}
	created, err := h.Store.UpsertFromDirectContact(
		payload.ID, payload.Name, payload.Platform, payload.AppVersion, payload.Capabilities, "direct", time.Now(),
	)
	if err != nil {
		serverError(w, err)
		return
	}
	if h.OnDeviceSeen != nil {
		h.OnDeviceSeen(created)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) updateLocation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var payload proto.GPSUpdatePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		badRequest(w, err)
		return
	}
	if err := h.Store.SetLocation(id, payload.GeoPoint); err != nil {
		serverError(w, err)
		return
	}
	if h.OnLocationUpdated != nil {
		h.OnLocationUpdated(id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) updateName(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var payload proto.NameUpdatePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		badRequest(w, err)
		return
	}
	if err := h.Store.SetName(id, payload.Name, time.Now()); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) peerReport(w http.ResponseWriter, r *http.Request) {
	var payload proto.PeerReportPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		badRequest(w, err)
		return
	}
	if err := h.Store.UpsertFromReport(payload.Reporter, payload.Peer); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) talkStart(w http.ResponseWriter, r *http.Request) {
	if h.Talker == nil {
		http.Error(w, "audio unavailable on this Base Station", http.StatusServiceUnavailable)
		return
	}
	channelID := r.URL.Query().Get("channel")
	if channelID != "" && h.ChannelTalkPeers != nil {
		peers := h.ChannelTalkPeers(channelID)
		if len(peers) > 0 {
			h.Talker.StartTalkingToPeers(peers)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	to := r.URL.Query().Get("to")
	if to == "" {
		var body struct {
			To      string `json:"to"`
			Channel string `json:"channel"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		to = body.To
		if channelID == "" {
			channelID = body.Channel
		}
		if channelID != "" && h.ChannelTalkPeers != nil {
			peers := h.ChannelTalkPeers(channelID)
			if len(peers) > 0 {
				h.Talker.StartTalkingToPeers(peers)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
	}
	if to != "" {
		h.Talker.StartTalkingTo(to)
	} else {
		h.Talker.StartTalking()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) talkStop(w http.ResponseWriter, r *http.Request) {
	if h.Talker == nil {
		http.Error(w, "audio unavailable on this Base Station", http.StatusServiceUnavailable)
		return
	}
	h.Talker.StopTalking()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) talkPeer(w http.ResponseWriter, r *http.Request) {
	if h.Talker == nil {
		http.Error(w, "audio unavailable on this Base Station", http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id query required", http.StatusBadRequest)
		return
	}
	direct := h.Talker.DirectConnected(id)
	relay := h.Talker.RelayConnected(id)
	writeJSON(w, map[string]bool{
		"direct": direct,
		"relay":  relay,
		"live":   h.Talker.LiveTalkAvailable(id),
	})
}

func (h *Handlers) about(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, AboutInfo{ID: h.SelfID, Name: h.SelfName, Platform: h.Platform, Version: h.Version})
}

func (h *Handlers) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Store.GetSettings()
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, settings)
}

func (h *Handlers) putSettings(w http.ResponseWriter, r *http.Request) {
	var settings config.Settings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		badRequest(w, err)
		return
	}
	if err := h.Store.SetSettings(settings); err != nil {
		serverError(w, err)
		return
	}
	if h.OnSettingsChanged != nil {
		h.OnSettingsChanged(settings)
	}
	w.WriteHeader(http.StatusNoContent)
}
