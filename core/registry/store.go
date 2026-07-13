package registry

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/config"
	"github.com/JohnDovey/WalkieTalkie/core/proto"
	"go.etcd.io/bbolt"
)

var (
	devicesBucket = []byte("devices")
	configBucket  = []byte("config")
	settingsKey   = []byte("settings")
)

// Store is a bbolt-backed registry of every device this node has seen,
// directly or via a peer report. All methods are safe for concurrent use.
type Store struct {
	db *bbolt.DB
	mu sync.Mutex
}

// Open creates/opens a bbolt database file at path and ensures the devices
// bucket exists. Callers are responsible for choosing an OS-appropriate
// runtime data path (see docs/2026-07-13-implementation-plan.md, "Registry +
// web UI" — never hardcode a dev-machine path here).
func Open(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("registry: open %s: %w", path, err)
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(devicesBucket); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(configBucket)
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("registry: init bucket: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) get(tx *bbolt.Tx, id string) (*Device, bool) {
	raw := tx.Bucket(devicesBucket).Get([]byte(id))
	if raw == nil {
		return nil, false
	}
	var d Device
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, false
	}
	return &d, true
}

func (s *Store) put(tx *bbolt.Tx, d *Device) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return tx.Bucket(devicesBucket).Put([]byte(d.ID), raw)
}

// Get returns the device record for id, if known.
func (s *Store) Get(id string) (*Device, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var d *Device
	var ok bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		d, ok = s.get(tx, id)
		return nil
	})
	return d, ok, err
}

// List returns every known device, in no particular order.
func (s *Store) List() ([]*Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Device
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(devicesBucket).ForEach(func(_, raw []byte) error {
			var d Device
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			out = append(out, &d)
			return nil
		})
	})
	return out, err
}

// UpsertFromDirectContact records that this node heard directly from device
// id — via its own announce, an mDNS sighting, a GPS/name update, or a
// signaling connection. Direct contact always takes precedence over
// anything a peer previously reported about this device.
func (s *Store) UpsertFromDirectContact(id, name, platform string, capabilities []string, discoveryMethod string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bbolt.Tx) error {
		d, ok := s.get(tx, id)
		if !ok {
			d = &Device{ID: id, ProtocolVersion: proto.Version}
		}
		d.directSeen = true
		if name != "" {
			d.Name = name
		}
		if platform != "" {
			d.Platform = platform
		}
		if len(capabilities) > 0 {
			d.Capabilities = capabilities
		}
		d.Status = StatusConnected
		d.LastSeen = now
		if discoveryMethod != "" {
			d.DiscoveryMethods = addMethod(d.DiscoveryMethods, discoveryMethod)
		}
		return s.put(tx, d)
	})
}

// SetName records a name change self-reported directly by device id (the
// device sends this immediately on next connect if the user renamed it
// while offline — see the plan's NameUpdate message). The web UI does not
// call this; it's device-originated only.
func (s *Store) SetName(id, name string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bbolt.Tx) error {
		d, ok := s.get(tx, id)
		if !ok {
			d = &Device{ID: id, ProtocolVersion: proto.Version}
		}
		d.directSeen = true
		d.Name = name
		d.Status = StatusConnected
		d.LastSeen = now
		return s.put(tx, d)
	})
}

// SetLocation records a GPS reading self-reported directly by device id.
func (s *Store) SetLocation(id string, point proto.GeoPoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bbolt.Tx) error {
		d, ok := s.get(tx, id)
		if !ok {
			d = &Device{ID: id, ProtocolVersion: proto.Version}
		}
		d.directSeen = true
		d.Status = StatusConnected
		p := point
		d.CurrentLocation = &p
		d.LastKnownLocation = &p
		d.LastSeen = point.Timestamp
		return s.put(tx, d)
	})
}

// SetDisconnected marks device id disconnected and freezes its last-known
// location (CurrentLocation is cleared, LastKnownLocation is preserved).
func (s *Store) SetDisconnected(id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bbolt.Tx) error {
		d, ok := s.get(tx, id)
		if !ok {
			return nil
		}
		d.Status = StatusDisconnected
		d.LastSeen = now
		d.CurrentLocation = nil
		return s.put(tx, d)
	})
}

// UpsertFromReport applies a PeerReport: reporter has directly discovered
// (mDNS or BLE) a device that isn't itself connected to this server.
//
// Precedence rule: a device's own direct data always outranks a peer report
// about it — a stale BLE sighting from the reporter must never flip an
// already-connected device to disconnected, or clobber its self-reported
// GPS. Among peer reports (no direct contact at all), the most recent
// LastSeenByReporter wins.
func (s *Store) UpsertFromReport(reporter string, peer proto.PeerSummary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bbolt.Tx) error {
		d, ok := s.get(tx, peer.ID)
		if !ok {
			d = &Device{ID: peer.ID, ProtocolVersion: proto.Version}
		}

		// Direct contact always wins: still record the reporter (useful for
		// UI, e.g. "also seen via A"), but don't touch status/location/name.
		if d.directSeen {
			d.ReportedBy = addMethod(d.ReportedBy, reporter)
			return s.put(tx, d)
		}

		// No direct contact yet — apply the report if it's newer than
		// whatever we already have for this device.
		if !d.LastSeen.IsZero() && peer.LastSeenByReporter.Before(d.LastSeen) {
			return nil
		}

		if peer.Name != "" {
			d.Name = peer.Name
		}
		if peer.Platform != "" {
			d.Platform = peer.Platform
		}
		d.Status = StatusConnected
		d.LastSeen = peer.LastSeenByReporter
		d.DiscoveryMethods = addMethod(d.DiscoveryMethods, peer.DiscoveryMethod)
		d.ReportedBy = addMethod(d.ReportedBy, reporter)

		if peer.GPS != nil {
			p := *peer.GPS
			d.CurrentLocation = &p
			d.LastKnownLocation = &p
			if len(d.Capabilities) == 0 {
				d.Capabilities = []string{"audio"}
			}
		} else if len(d.Capabilities) == 0 {
			// BLE presence-only sightings carry no GPS and no audio path.
			d.Capabilities = []string{"presence-only"}
		}

		return s.put(tx, d)
	})
}

// GetSettings returns the persisted settings, or config.Default() if none
// have been saved yet.
func (s *Store) GetSettings() (config.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings := config.Default()
	err := s.db.View(func(tx *bbolt.Tx) error {
		raw := tx.Bucket(configBucket).Get(settingsKey)
		if raw == nil {
			return nil
		}
		return json.Unmarshal(raw, &settings)
	})
	return settings, err
}

// SetSettings persists settings, overwriting whatever was there before.
func (s *Store) SetSettings(settings config.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(configBucket).Put(settingsKey, raw)
	})
}

// MergeRemoteRegistry reconciles this Base Station's registry against
// another Base Station's full device list, per
// docs/2026-07-13-implementation-plan.md ("Multi-Base-Station
// synchronization"). Unlike UpsertFromReport (one device vouching for a
// single peer, where direct contact always outranks secondhand reports),
// this is two equally-authoritative registries reconciling their entire
// device lists against each other — so the rule here is simply
// last-seen-wins, wholesale: for each incoming device, replace the local
// record if the incoming one is unknown locally or has a strictly newer
// LastSeen; otherwise leave the local record untouched. Returns how many
// local records were updated.
func (s *Store) MergeRemoteRegistry(remote []Device) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	updated := 0
	err := s.db.Update(func(tx *bbolt.Tx) error {
		for _, incoming := range remote {
			local, ok := s.get(tx, incoming.ID)
			if ok && !incoming.LastSeen.After(local.LastSeen) {
				continue
			}
			d := incoming
			if err := s.put(tx, &d); err != nil {
				return err
			}
			updated++
		}
		return nil
	})
	return updated, err
}
