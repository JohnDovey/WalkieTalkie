package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
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

// SyncRemoteBase pulls devices+voice from remoteURL and pushes into local Base,
// then mirrors the local Base's devices+voice into the remote (so two same-LAN
// Bases both show each other under Remote Users with one MeshBridge).
// Returns the pulled device list for MeshSniff inventory.
// Skips when remote is the same Base as LocalBaseURL (by mesh id).
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

	localDevices, localID, localName, lerr := FetchRemoteDevices(c.http(), c.BaseURL)
	if lerr == nil && localID != "" && remoteBaseID == localID {
		return nil, remoteBaseID, remoteBaseName, fmt.Errorf("skip self (%s)", remoteBaseID)
	}
	if lerr == nil && sameBaseURL(c.BaseURL, remoteURL) {
		return nil, remoteBaseID, remoteBaseName, fmt.Errorf("skip self (%s)", remoteURL)
	}

	if err := c.PushDevices(remoteBaseID, remoteBaseName, devices); err != nil {
		return devices, remoteBaseID, remoteBaseName, err
	}
	if err := c.PushVoice(remoteURL); err != nil {
		return devices, remoteBaseID, remoteBaseName, err
	}

	// Reverse: push local registry into the remote Base's bridge ingest.
	if lerr == nil {
		if localName == "" {
			localName = "Local Base"
		}
		if localID == "" {
			localID = c.BaseURL
		}
		if err := PushDevicesTo(c.http(), remoteURL, localID, localName, localDevices); err != nil {
			log.Printf("meshbridge reverse devices → %s: %v", remoteURL, err)
		} else if err := PushVoiceTo(c.http(), remoteURL, c.BaseURL); err != nil {
			log.Printf("meshbridge reverse voice → %s: %v", remoteURL, err)
		}
	}
	return devices, remoteBaseID, remoteBaseName, nil
}

// PushDevicesTo posts devices into a remote Base's bridge ingest API.
func PushDevicesTo(hc *http.Client, baseURL, remoteBaseID, remoteBaseName string, devices []registry.Device) error {
	body, _ := json.Marshal(map[string]any{
		"remoteBaseId":   remoteBaseID,
		"remoteBaseName": remoteBaseName,
		"devices":        devices,
	})
	resp, err := hc.Post(strings.TrimRight(baseURL, "/")+"/api/bridge/remote-devices", "application/json", bytes.NewReader(body))
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

// PushVoiceTo pulls sync lists from sourceURL and posts them to destBaseURL's bridge ingest.
func PushVoiceTo(hc *http.Client, destBaseURL, sourceURL string) error {
	chRaw, err := fetchJSON(hc, sourceURL+"/api/sync/channels")
	if err != nil {
		return err
	}
	notesRaw, err := fetchJSON(hc, sourceURL+"/api/sync/voice-notes")
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
			continue
		}
		data, aerr := fetchBytes(hc, sourceURL+"/api/voice-notes/"+n.ID+"/audio")
		if aerr == nil && len(data) > 0 {
			audio[n.ID] = data
		}
	}
	body, _ := json.Marshal(map[string]any{
		"remoteBaseUrl": sourceURL,
		"channels":      json.RawMessage(chRaw),
		"notes":         json.RawMessage(notesRaw),
		"audio":         audio,
	})
	resp, err := hc.Post(strings.TrimRight(destBaseURL, "/")+"/api/bridge/voice-sync", "application/json", bytes.NewReader(body))
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

// sameBaseURL reports whether two Base URLs likely refer to the same HTTP origin
// (host:port), treating loopback and blank hosts as local.
func sameBaseURL(a, b string) bool {
	ua, errA := url.Parse(strings.TrimSpace(a))
	ub, errB := url.Parse(strings.TrimSpace(b))
	if errA != nil || errB != nil {
		return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
	}
	portA, portB := ua.Port(), ub.Port()
	if portA == "" {
		portA = defaultHTTPPort(ua.Scheme)
	}
	if portB == "" {
		portB = defaultHTTPPort(ub.Scheme)
	}
	if portA != portB {
		return false
	}
	ha, hb := strings.ToLower(ua.Hostname()), strings.ToLower(ub.Hostname())
	if ha == hb {
		return true
	}
	// 127.0.0.1 / localhost are the local machine; match only if the other is also loopback.
	return isLoopbackHost(ha) && isLoopbackHost(hb)
}

func defaultHTTPPort(scheme string) string {
	if strings.EqualFold(scheme, "https") {
		return "443"
	}
	return "80"
}

func isLoopbackHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
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
