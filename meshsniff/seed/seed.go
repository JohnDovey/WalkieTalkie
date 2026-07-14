package seed

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/graph"
)

// BridgeInventory is MeshBridge GET /api/inventory.
type BridgeInventory struct {
	NodeID       string `json:"nodeId"`
	LocalBaseURL string `json:"localBaseURL"`
	Remotes      []struct {
		RemoteBaseID   string `json:"remoteBaseId"`
		RemoteBaseName string `json:"remoteBaseName"`
		Devices        []struct {
			ID         string   `json:"id"`
			Name       string   `json:"name"`
			Platform   string   `json:"platform"`
			AppVersion string   `json:"appVersion"`
			MACs       []string `json:"macs"`
			Lat        float64  `json:"lat"`
			Lon        float64  `json:"lon"`
		} `json:"devices"`
	} `json:"remotes"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// FetchBridgeInventory pulls MeshBridge inventory (may fail if not running).
func FetchBridgeInventory(baseURL string) (*BridgeInventory, error) {
	raw, err := getJSON(baseURL + "/api/inventory")
	if err != nil {
		return nil, err
	}
	var inv BridgeInventory
	if err := json.Unmarshal(raw, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// ApplyBridge seeds remoteHint / bridge nodes.
func ApplyBridge(g *graph.Store, inv *BridgeInventory) {
	if inv == nil {
		return
	}
	bridgeID := "bridge:" + inv.NodeID
	if inv.NodeID == "" {
		bridgeID = "bridge:local"
	}
	g.Upsert(graph.Node{
		ID:               bridgeID,
		Kind:             graph.KindBridge,
		Label:            "MeshBridge",
		MeshID:           inv.NodeID,
		DiscoveryMethods: []string{"meshbridge"},
		URLs:             map[string]string{"status": inv.LocalBaseURL},
	})
	for _, r := range inv.Remotes {
		baseNodeID := "remotebase:" + r.RemoteBaseID
		g.Upsert(graph.Node{
			ID:             baseNodeID,
			Kind:           graph.KindRemoteHint,
			Label:          r.RemoteBaseName,
			MeshID:         r.RemoteBaseID,
			Nickname:       r.RemoteBaseName,
			RemoteBaseID:   r.RemoteBaseID,
			RemoteBaseName: r.RemoteBaseName,
			DiscoveryMethods: []string{"meshbridge"},
		})
		g.Link(bridgeID, baseNodeID, "bridge", true)
		for _, d := range r.Devices {
			n := graph.Node{
				ID:               "remote:" + d.ID,
				Kind:             graph.KindRemoteHint,
				Label:            d.Name,
				Nickname:         d.Name,
				MeshID:           d.ID,
				Platform:         d.Platform,
				AppVersion:       d.AppVersion,
				MACs:             d.MACs,
				RemoteBaseID:     r.RemoteBaseID,
				RemoteBaseName:   r.RemoteBaseName,
				DiscoveryMethods: []string{"meshbridge"},
			}
			if d.Lat != 0 || d.Lon != 0 {
				n.GPS = &graph.GPS{Lat: d.Lat, Lon: d.Lon}
			}
			id := g.Upsert(n)
			g.Link(baseNodeID, id, "remote", true)
		}
	}
}

// ApplyBaseDevices seeds local Base Station registry (prefer ApplyWalkieTalkie).
func ApplyBaseDevices(g *graph.Store, devices []*registry.Device) {
	for _, d := range devices {
		if d == nil {
			continue
		}
		g.Upsert(deviceToWalkieNode(d, "walkietalkie"))
	}
}

// ApplyRemoteDevices seeds Base Station Remote Users (prefer ApplyWalkieTalkie).
func ApplyRemoteDevices(g *graph.Store, remotes []registry.RemoteDevice) {
	for _, rd := range remotes {
		d := rd.Device
		n := deviceToWalkieNode(&d, "walkietalkie-remote")
		n.Kind = graph.KindRemoteHint
		n.ID = "remote:" + d.ID
		n.RemoteBaseID = rd.RemoteBaseID
		n.RemoteBaseName = rd.RemoteBaseName
		n.DiscoveryMethods = []string{"walkietalkie", "remote-users"}
		g.Upsert(n)
	}
}

// FetchDevices GET /api/devices.
func FetchDevices(baseURL string) ([]*registry.Device, error) {
	raw, err := getJSON(baseURL + "/api/devices")
	if err != nil {
		return nil, err
	}
	var devices []*registry.Device
	if err := json.Unmarshal(raw, &devices); err != nil {
		// try DeviceDTO-shaped or value slices
		var vals []registry.Device
		if err2 := json.Unmarshal(raw, &vals); err2 != nil {
			return nil, err
		}
		for i := range vals {
			d := vals[i]
			devices = append(devices, &d)
		}
	}
	return devices, nil
}

type remoteWrap struct {
	registry.Device
	RemoteBaseID   string `json:"remoteBaseId"`
	RemoteBaseName string `json:"remoteBaseName"`
}

// FetchRemoteDevices GET /api/bridge/remote-devices.
func FetchRemoteDevices(baseURL string) ([]registry.RemoteDevice, error) {
	raw, err := getJSON(baseURL + "/api/bridge/remote-devices")
	if err != nil {
		return nil, err
	}
	var wrap []remoteWrap
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	var out []registry.RemoteDevice
	for _, w := range wrap {
		out = append(out, registry.RemoteDevice{
			Device:         w.Device,
			RemoteBaseID:   w.RemoteBaseID,
			RemoteBaseName: w.RemoteBaseName,
		})
	}
	return out, nil
}

func getJSON(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
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
