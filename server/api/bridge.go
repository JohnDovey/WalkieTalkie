// Package api implements MeshBridge ingest on the Base Station.
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
)

// BridgeVoiceIngester imports channels/notes pulled by MeshBridge.
type BridgeVoiceIngester interface {
	IngestBridgeVoice(remoteBaseURL string, channelsJSON, notesJSON []byte, audio map[string][]byte) (channels, notes int, err error)
}

// BridgeHandlers exposes /api/bridge/* for MeshBridge.
type BridgeHandlers struct {
	Store *registry.Store
	Voice BridgeVoiceIngester

	mu        sync.Mutex
	lastSeen  time.Time
	lastError string
	bridges   int
}

// Register attaches bridge routes.
func (h *BridgeHandlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/bridge/remote-devices", h.postRemoteDevices)
	mux.HandleFunc("GET /api/bridge/remote-devices", h.listRemoteDevices)
	mux.HandleFunc("POST /api/bridge/voice-sync", h.postVoiceSync)
	mux.HandleFunc("GET /api/bridge/status", h.status)
	mux.HandleFunc("POST /api/bridge/heartbeat", h.heartbeat)
}

type remoteDevicesBody struct {
	RemoteBaseID   string            `json:"remoteBaseId"`
	RemoteBaseName string            `json:"remoteBaseName"`
	Devices        []registry.Device `json:"devices"`
}

func (h *BridgeHandlers) postRemoteDevices(w http.ResponseWriter, r *http.Request) {
	var body remoteDevicesBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n, err := h.Store.MergeRemoteDevices(body.RemoteBaseID, body.RemoteBaseName, body.Devices)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.touch("", 0)
	writeJSON(w, map[string]any{"updated": n})
}

func (h *BridgeHandlers) listRemoteDevices(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListRemoteDevices()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []registry.RemoteDevice{}
	}
	writeJSON(w, list)
}

type voiceSyncBody struct {
	RemoteBaseURL string            `json:"remoteBaseUrl"`
	ChannelsJSON  json.RawMessage   `json:"channels"`
	NotesJSON     json.RawMessage   `json:"notes"`
	Audio         map[string][]byte `json:"audio,omitempty"` // noteID → bytes (optional; prefer streaming later)
}

func (h *BridgeHandlers) postVoiceSync(w http.ResponseWriter, r *http.Request) {
	if h.Voice == nil {
		http.Error(w, "voice ingest not configured", http.StatusServiceUnavailable)
		return
	}
	var body voiceSyncBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	chN, noteN, err := h.Voice.IngestBridgeVoice(body.RemoteBaseURL, body.ChannelsJSON, body.NotesJSON, body.Audio)
	if err != nil {
		h.touch(err.Error(), 0)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.touch("", 0)
	writeJSON(w, map[string]any{"channels": chN, "notes": noteN})
}

func (h *BridgeHandlers) status(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	writeJSON(w, map[string]any{
		"attached":  !h.lastSeen.IsZero() && time.Since(h.lastSeen) < 2*time.Minute,
		"lastSeen":  h.lastSeen,
		"lastError": h.lastError,
		"bridges":   h.bridges,
	})
}

func (h *BridgeHandlers) heartbeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Bridges int    `json:"bridges"`
		Error   string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	h.touch(body.Error, body.Bridges)
	w.WriteHeader(http.StatusNoContent)
}

func (h *BridgeHandlers) touch(errMsg string, bridges int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastSeen = time.Now()
	h.lastError = errMsg
	if bridges > 0 {
		h.bridges = bridges
	}
}
