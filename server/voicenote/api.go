package voicenote

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
)

// Handlers exposes voice-note and private-channel REST endpoints.
type Handlers struct {
	Voice *Store
	Reg   *registry.Store
	// SelfID is this Base Station's device id (used as default "from" for
	// web-UI uploads when the form omits from).
	SelfID string
}

// DeviceDTO is a registry device plus pending voice-note count for the UI.
type DeviceDTO struct {
	*registry.Device
	PendingVoiceNotes int `json:"pendingVoiceNotes"`
}

// Register attaches voice-note and channel routes to mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/voice-notes", h.upload)
	mux.HandleFunc("GET /api/voice-notes", h.listNotes)
	mux.HandleFunc("GET /api/voice-notes/{id}/audio", h.audio)
	mux.HandleFunc("POST /api/voice-notes/{id}/ack", h.ack)
	mux.HandleFunc("DELETE /api/voice-notes/{id}", h.deleteNote)

	mux.HandleFunc("POST /api/channels/invite", h.invite)
	mux.HandleFunc("POST /api/channels/{id}/accept", h.accept)
	mux.HandleFunc("GET /api/channels", h.listChannels)
	mux.HandleFunc("POST /api/channels/{id}/close", h.closeChannel)
	mux.HandleFunc("POST /api/channels/{id}/focus", h.focus)
	mux.HandleFunc("POST /api/channels/{id}/blur", h.blur)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// EnrichDevices returns devices with pendingVoiceNotes filled in.
func (h *Handlers) EnrichDevices(devices []*registry.Device) (any, error) {
	counts, err := h.Voice.PendingCounts()
	if err != nil {
		return nil, err
	}
	out := make([]DeviceDTO, 0, len(devices))
	for _, d := range devices {
		out = append(out, DeviceDTO{Device: d, PendingVoiceNotes: counts[d.ID]})
	}
	return out, nil
}

func (h *Handlers) upload(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	var fromID, toID, channelID string
	var opus []byte

	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fromID = r.FormValue("from")
		toID = r.FormValue("to")
		channelID = r.FormValue("channelId")
		file, _, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, "audio file required", http.StatusBadRequest)
			return
		}
		defer file.Close()
		var readErr error
		opus, readErr = io.ReadAll(file)
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusBadRequest)
			return
		}
	} else {
		fromID = r.URL.Query().Get("from")
		toID = r.URL.Query().Get("to")
		channelID = r.URL.Query().Get("channelId")
		var err error
		opus, err = io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if fromID == "" {
		fromID = r.Header.Get("X-Device-Id")
	}
	if fromID == "" {
		fromID = h.SelfID
	}
	if toID == "" && channelID == "" {
		http.Error(w, "to or channelId is required", http.StatusBadRequest)
		return
	}

	if channelID != "" {
		ch, ok, err := h.Voice.GetChannel(channelID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok || ch.Status == ChannelClosed {
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
		if ch.ParticipantA != fromID && ch.ParticipantB != fromID {
			http.Error(w, "not a participant", http.StatusForbidden)
			return
		}
		toID = ch.PeerOf(fromID)
	}

	note, err := h.Voice.SaveNote(fromID, toID, channelID, opus)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, note)
}

func (h *Handlers) listNotes(w http.ResponseWriter, r *http.Request) {
	forID := r.URL.Query().Get("for")
	if forID == "" {
		http.Error(w, "for is required", http.StatusBadRequest)
		return
	}
	with := r.URL.Query().Get("with")
	channelID := r.URL.Query().Get("channelId")

	var notes []*Note
	var err error
	if with != "" || channelID != "" {
		peer := with
		if channelID != "" {
			ch, ok, gerr := h.Voice.GetChannel(channelID)
			if gerr != nil {
				http.Error(w, gerr.Error(), http.StatusInternalServerError)
				return
			}
			if !ok {
				http.Error(w, "channel not found", http.StatusNotFound)
				return
			}
			peer = ch.PeerOf(forID)
		}
		notes, err = h.Voice.ThreadBetween(forID, peer, channelID)
	} else {
		notes, err = h.Voice.ListFor(forID, "", "")
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if notes == nil {
		notes = []*Note{}
	}
	writeJSON(w, notes)
}

func (h *Handlers) audio(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	data, note, err := h.Voice.ReadAudio(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	ct := "audio/opus"
	if len(data) >= 4 && data[0] == 0x1a && data[1] == 0x45 && data[2] == 0xdf && data[3] == 0xa3 {
		ct = "audio/webm"
	} else if len(data) >= 4 && string(data[0:4]) == "OggS" {
		ct = "audio/ogg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("X-Voice-Note-From", note.FromID)
	w.Write(data)
}

func (h *Handlers) ack(w http.ResponseWriter, r *http.Request) {
	if err := h.Voice.Ack(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) deleteNote(w http.ResponseWriter, r *http.Request) {
	if err := h.Voice.Delete(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type inviteBody struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (h *Handlers) invite(w http.ResponseWriter, r *http.Request) {
	var body inviteBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.From == "" {
		body.From = h.SelfID
	}
	peer, ok, err := h.Reg.Get(body.To)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok || peer.Status != registry.StatusConnected {
		http.Error(w, "peer must be connected to invite", http.StatusConflict)
		return
	}
	ch, err := h.Voice.Invite(body.From, body.To)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, ch)
}

type deviceBody struct {
	DeviceID string `json:"deviceId"`
}

func (h *Handlers) accept(w http.ResponseWriter, r *http.Request) {
	var body deviceBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.DeviceID == "" {
		body.DeviceID = h.SelfID
	}
	ch, err := h.Voice.Accept(r.PathValue("id"), body.DeviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, ch)
}

func (h *Handlers) listChannels(w http.ResponseWriter, r *http.Request) {
	forID := r.URL.Query().Get("for")
	if forID == "" {
		forID = h.SelfID
	}
	views, err := h.Voice.ListChannels(forID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if views == nil {
		views = []ChannelView{}
	}
	// Attach peer display names from registry.
	type channelDTO struct {
		ChannelView
		PeerName string `json:"peerName"`
	}
	out := make([]channelDTO, 0, len(views))
	for _, v := range views {
		name := v.PeerID
		if d, ok, _ := h.Reg.Get(v.PeerID); ok && d != nil {
			name = d.Name
		}
		out = append(out, channelDTO{ChannelView: v, PeerName: name})
	}
	writeJSON(w, out)
}

func (h *Handlers) closeChannel(w http.ResponseWriter, r *http.Request) {
	var body deviceBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.DeviceID == "" {
		body.DeviceID = h.SelfID
	}
	if err := h.Voice.CloseChannel(r.PathValue("id"), body.DeviceID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) focus(w http.ResponseWriter, r *http.Request) {
	var body deviceBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.DeviceID == "" {
		body.DeviceID = h.SelfID
	}
	if err := h.Voice.SetFocus(r.PathValue("id"), body.DeviceID, true); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) blur(w http.ResponseWriter, r *http.Request) {
	var body deviceBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.DeviceID == "" {
		body.DeviceID = h.SelfID
	}
	if err := h.Voice.SetFocus(r.PathValue("id"), body.DeviceID, false); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunPurge periodically deletes notes past the 21-day retention.
func RunPurge(stop <-chan struct{}, voice *Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if n, err := voice.PurgeExpired(time.Now()); err == nil && n > 0 {
				fmt.Printf("voicenote: purged %d expired note(s)\n", n)
			}
		}
	}
}
