package media

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pion/webrtc/v4"
)

const (
	voiceNoteDCLabel   = "voicenote"
	voiceNoteMaxBytes  = 8 << 20 // 8 MiB, matches Base Station upload cap
	voiceNoteChunkSize = 16 << 10
	voiceNoteDCWait    = 5 * time.Second
)

// VoiceNoteMeta is the envelope for a P2P voice-note / channel clip transfer.
type VoiceNoteMeta struct {
	ID        string    `json:"id"`
	FromID    string    `json:"fromId"`
	ToID      string    `json:"toId"`
	ChannelID string    `json:"channelId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Size      int64     `json:"size"`
}

type voiceDCMsg struct {
	T         string    `json:"t"` // "start" | "end"
	ID        string    `json:"id"`
	FromID    string    `json:"fromId,omitempty"`
	ToID      string    `json:"toId,omitempty"`
	ChannelID string    `json:"channelId,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	Size      int64     `json:"size,omitempty"`
}

type voiceRecvBuf struct {
	meta VoiceNoteMeta
	buf  []byte
}

func (mm *MeshManager) attachVoiceDC(peerID string, dc *webrtc.DataChannel) {
	bind := func() {
		mm.mu.Lock()
		defer mm.mu.Unlock()
		if p, ok := mm.peers[peerID]; ok {
			p.dc = dc
			if p.dcReady != nil {
				select {
				case <-p.dcReady:
				default:
					close(p.dcReady)
				}
			}
			return
		}
		if mm.pendingDC == nil {
			mm.pendingDC = make(map[string]*webrtc.DataChannel)
		}
		mm.pendingDC[peerID] = dc
	}
	if dc.ReadyState() == webrtc.DataChannelStateOpen {
		bind()
	}
	dc.OnOpen(bind)
	dc.OnClose(func() {
		mm.mu.Lock()
		if p, ok := mm.peers[peerID]; ok && p.dc == dc {
			p.dc = nil
		}
		delete(mm.pendingDC, peerID)
		delete(mm.voiceRecv, peerID)
		mm.mu.Unlock()
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		mm.handleVoiceDCMessage(peerID, msg)
	})
}

func (mm *MeshManager) handleVoiceDCMessage(peerID string, msg webrtc.DataChannelMessage) {
	if msg.IsString {
		var m voiceDCMsg
		if err := json.Unmarshal(msg.Data, &m); err != nil {
			return
		}
		switch m.T {
		case "start":
			if m.Size <= 0 || m.Size > voiceNoteMaxBytes || m.ID == "" {
				return
			}
			mm.mu.Lock()
			if mm.voiceRecv == nil {
				mm.voiceRecv = make(map[string]*voiceRecvBuf)
			}
			mm.voiceRecv[peerID] = &voiceRecvBuf{
				meta: VoiceNoteMeta{
					ID: m.ID, FromID: m.FromID, ToID: m.ToID, ChannelID: m.ChannelID,
					CreatedAt: m.CreatedAt, Size: m.Size,
				},
				buf: make([]byte, 0, int(m.Size)),
			}
			mm.mu.Unlock()
		case "end":
			mm.mu.Lock()
			rb := mm.voiceRecv[peerID]
			delete(mm.voiceRecv, peerID)
			handler := mm.OnVoiceNoteReceived
			mm.mu.Unlock()
			if rb == nil || handler == nil {
				return
			}
			if int64(len(rb.buf)) != rb.meta.Size {
				return
			}
			audio := append([]byte(nil), rb.buf...)
			meta := rb.meta
			go handler(meta, audio)
		}
		return
	}

	mm.mu.Lock()
	rb := mm.voiceRecv[peerID]
	if rb == nil {
		mm.mu.Unlock()
		return
	}
	if int64(len(rb.buf))+int64(len(msg.Data)) > rb.meta.Size+1024 {
		delete(mm.voiceRecv, peerID)
		mm.mu.Unlock()
		return
	}
	rb.buf = append(rb.buf, msg.Data...)
	mm.mu.Unlock()
}

// SendVoiceNoteP2P delivers a voice note over the direct PeerConnection DataChannel.
// Returns an error if the peer is not DirectConnected, the DC is not open in time,
// or the transfer fails — callers should fall back to Base Station upload.
func (mm *MeshManager) SendVoiceNoteP2P(meta VoiceNoteMeta, audio []byte) error {
	if meta.ID == "" || meta.ToID == "" {
		return fmt.Errorf("media: voice note P2P requires id and toId")
	}
	if len(audio) == 0 {
		return fmt.Errorf("media: empty audio")
	}
	if len(audio) > voiceNoteMaxBytes {
		return fmt.Errorf("media: voice note too large (%d > %d)", len(audio), voiceNoteMaxBytes)
	}
	meta.Size = int64(len(audio))
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now()
	}
	if meta.FromID == "" {
		meta.FromID = mm.selfID
	}

	mm.mu.Lock()
	p, ok := mm.peers[meta.ToID]
	mm.mu.Unlock()
	if !ok || p == nil {
		return fmt.Errorf("media: no direct peer %s for P2P voice note", meta.ToID)
	}

	dc, err := mm.waitVoiceDC(p, voiceNoteDCWait)
	if err != nil {
		return err
	}

	start, err := json.Marshal(voiceDCMsg{
		T: "start", ID: meta.ID, FromID: meta.FromID, ToID: meta.ToID,
		ChannelID: meta.ChannelID, CreatedAt: meta.CreatedAt, Size: meta.Size,
	})
	if err != nil {
		return err
	}
	if err := dc.SendText(string(start)); err != nil {
		return fmt.Errorf("media: voice note start: %w", err)
	}
	for off := 0; off < len(audio); off += voiceNoteChunkSize {
		end := off + voiceNoteChunkSize
		if end > len(audio) {
			end = len(audio)
		}
		if err := dc.Send(audio[off:end]); err != nil {
			return fmt.Errorf("media: voice note chunk: %w", err)
		}
	}
	endMsg, err := json.Marshal(voiceDCMsg{T: "end", ID: meta.ID})
	if err != nil {
		return err
	}
	if err := dc.SendText(string(endMsg)); err != nil {
		return fmt.Errorf("media: voice note end: %w", err)
	}
	return nil
}

func (mm *MeshManager) waitVoiceDC(p *peerConn, timeout time.Duration) (*webrtc.DataChannel, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mm.mu.Lock()
		dc := p.dc
		ready := p.dcReady
		mm.mu.Unlock()
		if dc != nil && dc.ReadyState() == webrtc.DataChannelStateOpen {
			return dc, nil
		}
		if ready != nil {
			select {
			case <-ready:
			case <-time.After(50 * time.Millisecond):
			}
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
	mm.mu.Lock()
	dc := p.dc
	mm.mu.Unlock()
	if dc != nil && dc.ReadyState() == webrtc.DataChannelStateOpen {
		return dc, nil
	}
	return nil, fmt.Errorf("media: voice note DataChannel not open")
}
