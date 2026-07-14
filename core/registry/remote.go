package registry

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"go.etcd.io/bbolt"
)

// RemoteDevice is a device sighted via MeshBridge from another Base Station.
// These never join the local WebRTC mesh (no ConnectAny).
type RemoteDevice struct {
	Device
	RemoteBaseID   string    `json:"remoteBaseId"`
	RemoteBaseName string    `json:"remoteBaseName,omitempty"`
	BridgedAt      time.Time `json:"bridgedAt"`
}

// RemoteDevicesBucket is the bbolt bucket for MeshBridge remote devices.
func RemoteDevicesBucket() []byte { return remoteDevicesBucket }

// MergeRemoteDevices upserts bridged devices for one remote Base.
// Prefer newer LastSeen when the same device ID is already stored from that Base.
func (s *Store) MergeRemoteDevices(remoteBaseID, remoteBaseName string, devices []Device) (int, error) {
	if remoteBaseID == "" {
		return 0, fmt.Errorf("registry: remoteBaseId required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	updated := 0
	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(remoteDevicesBucket)
		if b == nil {
			return fmt.Errorf("registry: remote_devices bucket missing")
		}
		for _, incoming := range devices {
			if incoming.ID == "" {
				continue
			}
			key := []byte(remoteBaseID + "/" + incoming.ID)
			var existing RemoteDevice
			if raw := b.Get(key); raw != nil {
				_ = json.Unmarshal(raw, &existing)
				if !incoming.LastSeen.After(existing.LastSeen) {
					continue
				}
			}
			rd := RemoteDevice{
				Device:         incoming,
				RemoteBaseID:   remoteBaseID,
				RemoteBaseName: remoteBaseName,
				BridgedAt:      now,
			}
			raw, err := json.Marshal(rd)
			if err != nil {
				return err
			}
			if err := b.Put(key, raw); err != nil {
				return err
			}
			updated++
		}
		return nil
	})
	return updated, err
}

// ListRemoteDevices returns all MeshBridge-ingested remote devices, newest first.
func (s *Store) ListRemoteDevices() ([]RemoteDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []RemoteDevice
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(remoteDevicesBucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			var d RemoteDevice
			if err := json.Unmarshal(v, &d); err != nil {
				return err
			}
			out = append(out, d)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out, nil
}

// ClearRemoteBase removes all remote devices for one origin Base.
func (s *Store) ClearRemoteBase(remoteBaseID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := []byte(remoteBaseID + "/")
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(remoteDevicesBucket)
		if b == nil {
			return nil
		}
		var keys [][]byte
		c := b.Cursor()
		for k, _ := c.Seek(prefix); k != nil && len(k) >= len(prefix) && string(k[:len(prefix)]) == string(prefix); k, _ = c.Next() {
			keys = append(keys, append([]byte(nil), k...))
		}
		for _, k := range keys {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}
