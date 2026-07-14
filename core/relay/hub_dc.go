package relay

import (
	"encoding/json"

	"github.com/pion/webrtc/v4"
)

const voiceNoteDCLabel = "voicenote"

type noteRoute struct {
	toID string
}

// attachVoiceNoteDC wires Hub forwarding for the SFU voicenote DataChannel.
func (h *Hub) attachVoiceNoteDC(fromID string, p *participant, dc *webrtc.DataChannel) {
	p.dc = dc
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		h.forwardVoiceDC(fromID, msg)
	})
}

func (h *Hub) forwardVoiceDC(fromID string, msg webrtc.DataChannelMessage) {
	if msg.IsString {
		var m struct {
			T    string `json:"t"`
			ToID string `json:"toId"`
		}
		if json.Unmarshal(msg.Data, &m) == nil {
			h.mu.Lock()
			if h.noteRoutes == nil {
				h.noteRoutes = make(map[string]string)
			}
			switch m.T {
			case "start":
				if m.ToID != "" {
					h.noteRoutes[fromID] = m.ToID
				} else {
					delete(h.noteRoutes, fromID)
				}
			case "end":
				delete(h.noteRoutes, fromID)
			}
			h.mu.Unlock()
		}
	}

	h.mu.Lock()
	toID := h.noteRoutes[fromID]
	targets := make([]*participant, 0, 4)
	if toID != "" {
		if p, ok := h.participants[toID]; ok && p.dc != nil && toID != fromID {
			targets = append(targets, p)
		}
	} else {
		fromRoom := h.rooms[fromID]
		for id, p := range h.participants {
			if id == fromID || p.dc == nil {
				continue
			}
			if h.rooms[id] != fromRoom {
				continue
			}
			targets = append(targets, p)
		}
	}
	h.mu.Unlock()

	for _, p := range targets {
		if msg.IsString {
			_ = p.dc.SendText(string(msg.Data))
		} else {
			_ = p.dc.Send(msg.Data)
		}
	}
}
