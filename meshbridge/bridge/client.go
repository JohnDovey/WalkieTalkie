package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
)

// LocalClient talks to the local Base Station bridge ingest APIs.
type LocalClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (c *LocalClient) http() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// Heartbeat tells the Base Station MeshBridge is alive.
func (c *LocalClient) Heartbeat(bridges int, errMsg string) error {
	body, _ := json.Marshal(map[string]any{"bridges": bridges, "error": errMsg})
	resp, err := c.http().Post(c.BaseURL+"/api/bridge/heartbeat", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("heartbeat %s: %s", resp.Status, b)
	}
	return nil
}

// PushDevices posts remote devices to the local Base.
func (c *LocalClient) PushDevices(remoteBaseID, remoteBaseName string, devices []registry.Device) error {
	body, _ := json.Marshal(map[string]any{
		"remoteBaseId":   remoteBaseID,
		"remoteBaseName": remoteBaseName,
		"devices":        devices,
	})
	resp, err := c.http().Post(c.BaseURL+"/api/bridge/remote-devices", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push devices %s: %s", resp.Status, b)
	}
	return nil
}

// PushVoice pulls sync lists from remoteURL and posts them (plus small audio) to local.
func (c *LocalClient) PushVoice(remoteURL string) error {
	chRaw, err := fetchJSON(c.http(), remoteURL+"/api/sync/channels")
	if err != nil {
		return err
	}
	notesRaw, err := fetchJSON(c.http(), remoteURL+"/api/sync/voice-notes")
	if err != nil {
		return err
	}
	var notes []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Size   int64  `json:"size"`
	}
	_ = json.Unmarshal(notesRaw, &notes)
	audio := map[string][]byte{}
	for _, n := range notes {
		if n.Status == "deleted" || n.Size <= 0 || n.Size > 512<<10 {
			continue // skip huge clips in this lightweight pass; Base can re-pull later
		}
		data, aerr := fetchBytes(c.http(), remoteURL+"/api/voice-notes/"+n.ID+"/audio")
		if aerr == nil && len(data) > 0 {
			audio[n.ID] = data
		}
	}
	body, _ := json.Marshal(map[string]any{
		"remoteBaseUrl": remoteURL,
		"channels":      json.RawMessage(chRaw),
		"notes":         json.RawMessage(notesRaw),
		"audio":         audio,
	})
	resp, err := c.http().Post(c.BaseURL+"/api/bridge/voice-sync", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push voice %s: %s", resp.Status, b)
	}
	return nil
}

// SyncRemoteBase pulls devices+voice from remoteURL and pushes into local Base.
// Returns the pulled device list for MeshSniff inventory.
func (c *LocalClient) SyncRemoteBase(remoteURL, remoteBaseID, remoteBaseName string) ([]registry.Device, string, string, error) {
	devices, aboutID, aboutName, err := FetchRemoteDevices(c.http(), remoteURL)
	if err != nil {
		return nil, "", "", err
	}
	if remoteBaseID == "" {
		remoteBaseID = aboutID
	}
	if remoteBaseName == "" {
		remoteBaseName = aboutName
	}
	if remoteBaseID == "" {
		remoteBaseID = remoteURL
	}
	if err := c.PushDevices(remoteBaseID, remoteBaseName, devices); err != nil {
		return devices, remoteBaseID, remoteBaseName, err
	}
	return devices, remoteBaseID, remoteBaseName, c.PushVoice(remoteURL)
}

// FetchRemoteDevices loads GET /api/devices and GET /api/about from a Base.
func FetchRemoteDevices(hc *http.Client, remoteURL string) (devices []registry.Device, id, name string, err error) {
	raw, err := fetchJSON(hc, remoteURL+"/api/devices")
	if err != nil {
		return nil, "", "", err
	}
	if err := json.Unmarshal(raw, &devices); err != nil {
		return nil, "", "", err
	}
	aboutRaw, aerr := fetchJSON(hc, remoteURL+"/api/about")
	if aerr == nil {
		var about struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		_ = json.Unmarshal(aboutRaw, &about)
		id, name = about.ID, about.Name
	}
	return devices, id, name, nil
}

func fetchJSON(hc *http.Client, url string) ([]byte, error) {
	resp, err := hc.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", resp.Status, b)
	}
	return b, nil
}

func fetchBytes(hc *http.Client, url string) ([]byte, error) {
	return fetchJSON(hc, url)
}
