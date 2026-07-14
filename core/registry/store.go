package registry

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/config"
	"github.com/JohnDovey/WalkieTalkie/core/proto"
	"go.etcd.io/bbolt"
)

var (
	devicesBucket     = []byte("devices")
	configBucket      = []byte("config")
	settingsKey       = []byte("settings")
	voiceNotesBucket  = []byte("voice_notes")
	channelsBucket    = []byte("private_channels")
	usageStatsBucket  = []byte("usage_stats")
	gpsHistoryBucket  = []byte("gps_history")
)

const (
	gpsHistoryMaxPerDevice = 500
	gpsHistoryRetention    = 7 * 24 * time.Hour
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
		for _, name := range [][]byte{devicesBucket, configBucket, voiceNotesBucket, channelsBucket, usageStatsBucket, gpsHistoryBucket} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("registry: init bucket: %w", err)
	}
	return &Store{db: db}, nil
}

// Bolt returns the underlying bbolt DB so sibling packages (e.g. voice-note
// storage) can share the same file without opening it twice.
func (s *Store) Bolt() *bbolt.DB {
	return s.db
}

// VoiceNotesBucket is the bbolt bucket name for voice-note metadata.
func VoiceNotesBucket() []byte { return voiceNotesBucket }

// ChannelsBucket is the bbolt bucket name for private-channel metadata.
func ChannelsBucket() []byte { return channelsBucket }

// UsageStatsBucket is the bbolt bucket name for hourly usage counters.
func UsageStatsBucket() []byte { return usageStatsBucket }

// GPSHistoryBucket is the bbolt bucket name for per-device GPS sample trails.
func GPSHistoryBucket() []byte { return gpsHistoryBucket }

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

// List returns every known device, most-recently-seen first. Both the web
// UI's device list/Old Nodes pages and the Android app's own on-device list
// want this same ordering, so it's sorted once here rather than by every
// caller.
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
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, nil
}

// PurgeOlderThan permanently deletes every device (other than selfID) not
// seen in timeout. Unlike SweepStale (which just flips Status to
// Disconnected so the web UI can still show a device's history, e.g. on the
// Old Nodes page), this actually removes the record — used by mobile nodes
// only, which want a bounded, clutter-free on-device list rather than an
// indefinitely-retained history. Returns how many devices were deleted.
func (s *Store) PurgeOlderThan(selfID string, now time.Time, timeout time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	purged := 0
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(devicesBucket)
		var staleIDs [][]byte
		err := bucket.ForEach(func(k, raw []byte) error {
			var d Device
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			if d.ID == selfID || now.Sub(d.LastSeen) < timeout {
				return nil
			}
			staleIDs = append(staleIDs, append([]byte(nil), k...))
			return nil
		})
		if err != nil {
			return err
		}
		for _, id := range staleIDs {
			if err := bucket.Delete(id); err != nil {
				return err
			}
			purged++
		}
		return nil
	})
	return purged, err
}

// UpsertFromDirectContact records that this node heard directly from device
// id — via its own announce, an mDNS sighting, a GPS/name update, or a
// signaling connection. Direct contact always takes precedence over
// anything a peer previously reported about this device.
// created is true when this was the first time this id was stored.
func (s *Store) UpsertFromDirectContact(id, name, platform, appVersion string, capabilities []string, discoveryMethod string, now time.Time) (created bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.db.Update(func(tx *bbolt.Tx) error {
		d, ok := s.get(tx, id)
		if !ok {
			created = true
			d = &Device{ID: id, ProtocolVersion: proto.Version}
		}
		d.directSeen = true
		if name != "" {
			d.Name = name
		}
		if platform != "" {
			d.Platform = platform
		}
		if appVersion != "" {
			d.AppVersion = appVersion
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
	return created, err
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

// SetLocation records a GPS reading self-reported directly by device id
// and appends a sample to the gps_history trail.
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
		if err := s.put(tx, d); err != nil {
			return err
		}
		return s.appendGPSHistory(tx, id, point)
	})
}

func gpsHistoryKey(deviceID string, ts time.Time) []byte {
	return []byte(fmt.Sprintf("%s/%020d", deviceID, ts.UnixNano()))
}

func (s *Store) appendGPSHistory(tx *bbolt.Tx, deviceID string, point proto.GeoPoint) error {
	b := tx.Bucket(gpsHistoryBucket)
	if b == nil {
		return nil
	}
	ts := point.Timestamp
	if ts.IsZero() {
		ts = time.Now()
		point.Timestamp = ts
	}
	// Dedupe near-identical samples within ~GPSIntervalSeconds of the latest.
	dedupeWindow := 30 * time.Second
	if raw := tx.Bucket(configBucket).Get(settingsKey); raw != nil {
		var settings config.Settings
		if json.Unmarshal(raw, &settings) == nil && settings.GPSIntervalSeconds > 0 {
			dedupeWindow = time.Duration(settings.GPSIntervalSeconds) * time.Second
		}
	}
	prefix := []byte(deviceID + "/")
	c := b.Cursor()
	var lastV []byte
	for k, v := c.Seek(prefix); k != nil && hasPrefix(k, prefix); k, v = c.Next() {
		lastV = v
	}
	if lastV != nil {
		var prev proto.GeoPoint
		if json.Unmarshal(lastV, &prev) == nil && !prev.Timestamp.IsZero() {
			if ts.Sub(prev.Timestamp) < dedupeWindow && almostSameGPS(prev, point) {
				return nil
			}
		}
	}
	raw, err := json.Marshal(point)
	if err != nil {
		return err
	}
	if err := b.Put(gpsHistoryKey(deviceID, ts), raw); err != nil {
		return err
	}
	return trimGPSHistory(tx, deviceID)
}

func hasPrefix(b, p []byte) bool {
	return len(b) >= len(p) && string(b[:len(p)]) == string(p)
}

func almostSameGPS(a, b proto.GeoPoint) bool {
	const eps = 1e-5
	return abs(a.Lat-b.Lat) < eps && abs(a.Lon-b.Lon) < eps
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func trimGPSHistory(tx *bbolt.Tx, deviceID string) error {
	b := tx.Bucket(gpsHistoryBucket)
	if b == nil {
		return nil
	}
	prefix := []byte(deviceID + "/")
	var keys [][]byte
	c := b.Cursor()
	for k, _ := c.Seek(prefix); k != nil && hasPrefix(k, prefix); k, _ = c.Next() {
		keys = append(keys, append([]byte(nil), k...))
	}
	if len(keys) <= gpsHistoryMaxPerDevice {
		return nil
	}
	drop := len(keys) - gpsHistoryMaxPerDevice
	for i := 0; i < drop; i++ {
		if err := b.Delete(keys[i]); err != nil {
			return err
		}
	}
	return nil
}

// ListGPSHistory returns chronological (oldest→newest) GPS samples for deviceID.
func (s *Store) ListGPSHistory(deviceID string, limit int) ([]proto.GeoPoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 200
	}
	if limit > gpsHistoryMaxPerDevice {
		limit = gpsHistoryMaxPerDevice
	}
	var all []proto.GeoPoint
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(gpsHistoryBucket)
		if b == nil {
			return nil
		}
		prefix := []byte(deviceID + "/")
		c := b.Cursor()
		for k, v := c.Seek(prefix); k != nil && hasPrefix(k, prefix); k, v = c.Next() {
			var p proto.GeoPoint
			if err := json.Unmarshal(v, &p); err != nil {
				continue
			}
			all = append(all, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, nil
}

// PurgeGPSHistory deletes samples older than gpsHistoryRetention. Returns count deleted.
func (s *Store) PurgeGPSHistory(now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.Add(-gpsHistoryRetention)
	deleted := 0
	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(gpsHistoryBucket)
		if b == nil {
			return nil
		}
		var doomed [][]byte
		err := b.ForEach(func(k, v []byte) error {
			var p proto.GeoPoint
			if err := json.Unmarshal(v, &p); err != nil {
				doomed = append(doomed, append([]byte(nil), k...))
				return nil
			}
			if p.Timestamp.Before(cutoff) {
				doomed = append(doomed, append([]byte(nil), k...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, k := range doomed {
			if err := b.Delete(k); err != nil {
				return err
			}
			deleted++
		}
		return nil
	})
	return deleted, err
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

// SweepStale marks every Connected device (other than selfID) whose LastSeen
// is older than timeout as Disconnected, freezing its location the same way
// SetDisconnected does. This is what actually retires a device that vanished
// without a graceful disconnect (crashed process, walked out of mDNS range,
// leftover test/synthetic data) — without it, a device marked Connected
// stays that way forever, and multi-Base-Station sync's last-seen-wins rule
// then keeps re-spreading the stale status to every other Base Station's
// registry too. Returns how many devices were swept.
func (s *Store) SweepStale(selfID string, now time.Time, timeout time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	swept := 0
	err := s.db.Update(func(tx *bbolt.Tx) error {
		// Collect first, then mutate — bbolt's ForEach docs warn that
		// modifying the bucket (even an in-place Put) during iteration can
		// invalidate the cursor and produce incorrect results.
		var stale []*Device
		err := tx.Bucket(devicesBucket).ForEach(func(_, raw []byte) error {
			var d Device
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			if d.ID == selfID || d.Status != StatusConnected {
				return nil
			}
			if now.Sub(d.LastSeen) < timeout {
				return nil
			}
			stale = append(stale, &d)
			return nil
		})
		if err != nil {
			return err
		}

		for _, d := range stale {
			d.Status = StatusDisconnected
			d.CurrentLocation = nil
			if err := s.put(tx, d); err != nil {
				return err
			}
			swept++
		}
		return nil
	})
	return swept, err
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
// another Base Station's full device list using last-seen-wins with a
// soft clock-skew floor (SyncClockSkewSeconds from settings, default 3s):
// remote must be strictly newer by more than the skew to replace local.
func (s *Store) MergeRemoteRegistry(remote []Device) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	skew := 3 * time.Second
	if settings, err := s.GetSettingsUnlocked(); err == nil && settings.SyncClockSkewSeconds > 0 {
		skew = time.Duration(settings.SyncClockSkewSeconds) * time.Second
	}
	return s.mergeRemoteRegistryLocked(remote, skew)
}

// GetSettingsUnlocked is like GetSettings but assumes the caller already
// holds s.mu (used by MergeRemoteRegistry). Prefer GetSettings otherwise.
func (s *Store) GetSettingsUnlocked() (config.Settings, error) {
	var settings config.Settings
	err := s.db.View(func(tx *bbolt.Tx) error {
		raw := tx.Bucket(configBucket).Get(settingsKey)
		if raw == nil {
			settings = config.Default()
			return nil
		}
		return json.Unmarshal(raw, &settings)
	})
	if err != nil {
		return config.Default(), err
	}
	if settings.Port == 0 {
		settings = config.Default()
	}
	if settings.SyncClockSkewSeconds <= 0 {
		settings.SyncClockSkewSeconds = config.Default().SyncClockSkewSeconds
	}
	return settings, nil
}

// MergeRemoteRegistryWithSkew is MergeRemoteRegistry with an explicit skew floor.
func (s *Store) MergeRemoteRegistryWithSkew(remote []Device, skew time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mergeRemoteRegistryLocked(remote, skew)
}

func (s *Store) mergeRemoteRegistryLocked(remote []Device, skew time.Duration) (int, error) {
	if skew < 0 {
		skew = 0
	}
	updated := 0
	err := s.db.Update(func(tx *bbolt.Tx) error {
		for _, incoming := range remote {
			local, ok := s.get(tx, incoming.ID)
			if ok && !incoming.LastSeen.After(local.LastSeen.Add(skew)) {
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
