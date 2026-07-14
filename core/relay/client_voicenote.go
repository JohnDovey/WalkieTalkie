package relay

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pion/webrtc/v4"
)

const (
	voiceNoteMaxBytes  = 8 << 20
	voiceNoteChunkSize = 16 << 10
	voiceNoteDCWait    = 5 * time.Second
)

// VoiceNoteMeta is the envelope for a note sent/received over the SFU DataChannel.
type VoiceNoteMeta struct {
	ID        string    `json:"id"`
	FromID    string    `json:"fromId"`
	ToID      string    `json:"toId"`
	ChannelID string    `json:"channelId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Size      int64     `json:"size"`
}

type voiceDCMsg struct {
	T         string    `json:"t"`
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

// Joined reports whether the SFU PeerConnection is up.
func (c *Client) Joined() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.joined
}

func (c *Client) attachVoiceDC(dc *webrtc.DataChannel) {
	c.mu.Lock()
	c.voiceDC = dc
	c.mu.Unlock()

	signalReady := func() {
		c.mu.Lock()
		ready := c.dcReady
		c.mu.Unlock()
		if ready == nil {
			return
		}
		select {
		case <-ready:
		default:
			close(ready)
		}
	}
	dc.OnOpen(func() { signalReady() })
	if dc.ReadyState() == webrtc.DataChannelStateOpen {
		signalReady()
	}
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		c.handleVoiceDCMessage(msg)
	})
}

func (c *Client) waitVoiceDC(timeout time.Duration) (*webrtc.DataChannel, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		dc := c.voiceDC
		ready := c.dcReady
		c.mu.Unlock()
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
	return nil, fmt.Errorf("relay: voicenote DataChannel not open")
}

func (c *Client) handleVoiceDCMessage(msg webrtc.DataChannelMessage) {
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
			c.mu.Lock()
			if c.voiceRecv == nil {
				c.voiceRecv = make(map[string]*voiceRecvBuf)
			}
			key := m.FromID
			if key == "" {
				key = m.ID
			}
			c.voiceRecv[key] = &voiceRecvBuf{
				meta: VoiceNoteMeta{
					ID: m.ID, FromID: m.FromID, ToID: m.ToID, ChannelID: m.ChannelID,
					CreatedAt: m.CreatedAt, Size: m.Size,
				},
				buf: make([]byte, 0, int(m.Size)),
			}
			c.mu.Unlock()
		case "end":
			c.mu.Lock()
			key := m.FromID
			if key == "" {
				key = m.ID
			}
			rb := c.voiceRecv[key]
			delete(c.voiceRecv, key)
			handler := c.OnVoiceNote
			c.mu.Unlock()
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
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, rb := range c.voiceRecv {
		if rb == nil {
			continue
		}
		if int64(len(rb.buf))+int64(len(msg.Data)) > rb.meta.Size+1024 {
			continue
		}
		rb.buf = append(rb.buf, msg.Data...)
		return
	}
}

// SendVoiceNote delivers a note over the SFU voicenote DataChannel. Hub routes
// by toId when set; empty toId with channelId set fans out within the sender's
// Hub room (N-party channel clips).
func (c *Client) SendVoiceNote(meta VoiceNoteMeta, audio []byte) error {
	if meta.ID == "" {
		return fmt.Errorf("relay: voice note requires id")
	}
	if meta.ToID == "" && meta.ChannelID == "" {
		return fmt.Errorf("relay: voice note requires toId or channelId")
	}
	if len(audio) == 0 || len(audio) > voiceNoteMaxBytes {
		return fmt.Errorf("relay: invalid audio size")
	}
	meta.Size = int64(len(audio))
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now()
	}
	if meta.FromID == "" {
		meta.FromID = c.SelfID
	}
	dc, err := c.waitVoiceDC(voiceNoteDCWait)
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
		return err
	}
	for i := 0; i < len(audio); i += voiceNoteChunkSize {
		end := i + voiceNoteChunkSize
		if end > len(audio) {
			end = len(audio)
		}
		if err := dc.Send(audio[i:end]); err != nil {
			return err
		}
	}
	endMsg, err := json.Marshal(voiceDCMsg{T: "end", ID: meta.ID, FromID: meta.FromID})
	if err != nil {
		return err
	}
	return dc.SendText(string(endMsg))
}
