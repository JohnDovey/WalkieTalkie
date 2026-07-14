// Package voicenote is the shared HTTP client for Base Station
// store-and-forward voice notes and private channels. See
// docs/2026-07-13-voice-message-and-private-channels.md.
package voicenote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// Note mirrors the server metadata JSON.
type Note struct {
	ID        string    `json:"id"`
	FromID    string    `json:"fromId"`
	ToID      string    `json:"toId"`
	ChannelID string    `json:"channelId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Size      int64     `json:"size"`
	Status    string    `json:"status"`
}

// ChannelPeer is one other member shown in N-party channel UIs.
type ChannelPeer struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Channel mirrors the server channel list entry.
type Channel struct {
	ID             string        `json:"id"`
	ParticipantA   string        `json:"participantA"`
	ParticipantB   string        `json:"participantB"`
	Participants   []string      `json:"participants,omitempty"`
	PendingInvites []string      `json:"pendingInvites,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	Status         string        `json:"status"`
	PeerID         string        `json:"peerId"`
	PeerName       string        `json:"peerName"`
	Peers          []ChannelPeer `json:"peers,omitempty"`
	UnreadFor      int           `json:"unreadFor"`
	Focused        []string      `json:"focused,omitempty"`
	FocusedBy      string        `json:"focusedBy,omitempty"`
}

// Client talks to one Base Station's REST API.
type Client struct {
	BaseURL    string
	DeviceID   string
	HTTPClient *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) requireBase() error {
	if c.BaseURL == "" {
		return fmt.Errorf("voicenote: no Base Station reachable (need a LAN Base Station for voice notes)")
	}
	return nil
}

// Upload sends an Opus/WebM clip to toID (or channelID). The Base Station
// assigns a new note ID.
func (c *Client) Upload(toID, channelID string, audio []byte, filename string) (*Note, error) {
	return c.UploadNote(Note{ToID: toID, ChannelID: channelID, FromID: c.DeviceID}, audio, filename)
}

// UploadNote uploads a clip, optionally preserving id/fromId/createdAt so a
// P2P-delivered note can be mirrored into the Base Station with the same ID.
func (c *Client) UploadNote(n Note, audio []byte, filename string) (*Note, error) {
	if err := c.requireBase(); err != nil {
		return nil, err
	}
	if filename == "" {
		filename = "note.opus"
	}
	fromID := n.FromID
	if fromID == "" {
		fromID = c.DeviceID
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("from", fromID)
	if n.ID != "" {
		_ = w.WriteField("id", n.ID)
	}
	if n.ToID != "" {
		_ = w.WriteField("to", n.ToID)
	}
	if n.ChannelID != "" {
		_ = w.WriteField("channelId", n.ChannelID)
	}
	if !n.CreatedAt.IsZero() {
		_ = w.WriteField("createdAt", n.CreatedAt.UTC().Format(time.RFC3339Nano))
	}
	part, err := w.CreateFormFile("audio", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/api/voice-notes", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Device-Id", c.DeviceID)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("voicenote: upload %s: %s", resp.Status, string(body))
	}
	var note Note
	if err := json.Unmarshal(body, &note); err != nil {
		return nil, err
	}
	return &note, nil
}

// Inbox lists notes for this device (optionally threaded with a peer or channel).
func (c *Client) Inbox(withPeerID, channelID string) ([]Note, error) {
	if err := c.requireBase(); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("for", c.DeviceID)
	if withPeerID != "" {
		q.Set("with", withPeerID)
	}
	if channelID != "" {
		q.Set("channelId", channelID)
	}
	resp, err := c.http().Get(c.BaseURL + "/api/voice-notes?" + q.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("voicenote: list %s: %s", resp.Status, string(body))
	}
	var notes []Note
	if err := json.Unmarshal(body, &notes); err != nil {
		return nil, err
	}
	return notes, nil
}

// DownloadAudio fetches the clip bytes.
func (c *Client) DownloadAudio(noteID string) ([]byte, error) {
	if err := c.requireBase(); err != nil {
		return nil, err
	}
	resp, err := c.http().Get(c.BaseURL + "/api/voice-notes/" + url.PathEscape(noteID) + "/audio")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("voicenote: audio %s: %s", resp.Status, string(body))
	}
	return body, nil
}

// Ack marks a note played.
func (c *Client) Ack(noteID string) error {
	return c.postEmpty("/api/voice-notes/" + url.PathEscape(noteID) + "/ack", nil)
}

// Delete soft-deletes a note.
func (c *Client) Delete(noteID string) error {
	if err := c.requireBase(); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodDelete, c.BaseURL+"/api/voice-notes/"+url.PathEscape(noteID), nil)
	if err != nil {
		return err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("voicenote: delete %s: %s", resp.Status, string(body))
	}
	return nil
}

// Invite opens a private channel invite to toID.
func (c *Client) Invite(toID string) (*Channel, error) {
	if err := c.requireBase(); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{"from": c.DeviceID, "to": toID})
	resp, err := c.http().Post(c.BaseURL+"/api/channels/invite", "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("voicenote: invite %s: %s", resp.Status, string(body))
	}
	var ch Channel
	if err := json.Unmarshal(body, &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

// InviteMore invites another peer onto an existing N-party channel (pending until accept).
func (c *Client) InviteMore(channelID, toID string) (*Channel, error) {
	if err := c.requireBase(); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{"from": c.DeviceID, "to": toID})
	resp, err := c.http().Post(
		c.BaseURL+"/api/channels/"+url.PathEscape(channelID)+"/invite",
		"application/json",
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("voicenote: invite-more %s: %s", resp.Status, string(body))
	}
	var ch Channel
	if err := json.Unmarshal(body, &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

// Accept accepts a pending channel.
func (c *Client) Accept(channelID string) error {
	payload, _ := json.Marshal(map[string]string{"deviceId": c.DeviceID})
	return c.postEmpty("/api/channels/"+url.PathEscape(channelID)+"/accept", payload)
}

// ListChannels returns this device's channels.
func (c *Client) ListChannels() ([]Channel, error) {
	if err := c.requireBase(); err != nil {
		return nil, err
	}
	resp, err := c.http().Get(c.BaseURL + "/api/channels?for=" + url.QueryEscape(c.DeviceID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("voicenote: channels %s: %s", resp.Status, string(body))
	}
	var ch []Channel
	if err := json.Unmarshal(body, &ch); err != nil {
		return nil, err
	}
	return ch, nil
}

// Close leaves a channel.
func (c *Client) Close(channelID string) error {
	payload, _ := json.Marshal(map[string]string{"deviceId": c.DeviceID})
	return c.postEmpty("/api/channels/"+url.PathEscape(channelID)+"/close", payload)
}

// Focus marks the channel as currently viewed.
func (c *Client) Focus(channelID string) error {
	payload, _ := json.Marshal(map[string]string{"deviceId": c.DeviceID})
	return c.postEmpty("/api/channels/"+url.PathEscape(channelID)+"/focus", payload)
}

// Blur clears channel focus.
func (c *Client) Blur(channelID string) error {
	payload, _ := json.Marshal(map[string]string{"deviceId": c.DeviceID})
	return c.postEmpty("/api/channels/"+url.PathEscape(channelID)+"/blur", payload)
}

func (c *Client) postEmpty(path string, payload []byte) error {
	if err := c.requireBase(); err != nil {
		return err
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	resp, err := c.http().Post(c.BaseURL+path, "application/json", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("voicenote: %s %s: %s", path, resp.Status, string(b))
	}
	return nil
}
