// Package usage tracks Base Station traffic and activity counters in hourly
// bbolt buckets for the Stats web UI. See docs/2026-07-13-usage-stats.md.
package usage

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
	"go.etcd.io/bbolt"
)

// Counters is one hour's worth of incremental stats.
type Counters struct {
	Hour string `json:"hour"` // UTC "2006-01-02T15"

	VoiceNotesUploaded     int64 `json:"voiceNotesUploaded"`
	VoiceNotesUploadBytes  int64 `json:"voiceNotesUploadBytes"`
	VoiceNotesDownloaded   int64 `json:"voiceNotesDownloaded"`
	VoiceNotesDownloadBytes int64 `json:"voiceNotesDownloadBytes"`
	VoiceNotesAcked        int64 `json:"voiceNotesAcked"`

	ChannelClipsUploaded    int64 `json:"channelClipsUploaded"`
	ChannelClipsUploadBytes int64 `json:"channelClipsUploadBytes"`
	ChannelInvites          int64 `json:"channelInvites"`
	ChannelAccepts          int64 `json:"channelAccepts"`
	ChannelCloses           int64 `json:"channelCloses"`

	// Live group PTT on THIS Base Station process only (P2P mesh peer).
	PttTalkSessions  int64 `json:"pttTalkSessions"`
	PttBytesSent     int64 `json:"pttBytesSent"`
	PttBytesReceived int64 `json:"pttBytesReceived"`

	DevicesSeenNew   int64 `json:"devicesSeenNew"`
	DevicesSeenTouch int64 `json:"devicesSeenTouch"`
}

// Snapshot is the API response: totals for a range plus optional series.
type Snapshot struct {
	Range           string              `json:"range"` // today|week|month|all
	From            string              `json:"from"`
	To              string              `json:"to"`
	Totals          Counters            `json:"totals"`
	DevicesKnownNow int                 `json:"devicesKnownNow"`
	Series          []Counters          `json:"series,omitempty"` // daily rollups for charts
	Note            string              `json:"note,omitempty"`
}

// Store accumulates counters in memory and flushes to bbolt hourly keys.
type Store struct {
	db *bbolt.DB

	mu     sync.Mutex
	curKey string
	cur    Counters

	// Lifetime unique devices ever recorded (survives hour rolls).
	uniqueDevices int64

	stop chan struct{}
	wg   sync.WaitGroup
}

// NewStore shares the registry bbolt DB. Starts a background flusher.
func NewStore(reg *registry.Store) (*Store, error) {
	s := &Store{
		db:   reg.Bolt(),
		stop: make(chan struct{}),
	}
	if err := s.loadUnique(); err != nil {
		return nil, err
	}
	// First run after upgrade: seed lifetime unique from devices already in the registry.
	if atomic.LoadInt64(&s.uniqueDevices) == 0 {
		if devices, err := reg.List(); err == nil && len(devices) > 0 {
			atomic.StoreInt64(&s.uniqueDevices, int64(len(devices)))
			_ = s.persistUnique()
		}
	}
	s.curKey = hourKey(time.Now().UTC())
	_ = s.loadHourIntoCurrent(s.curKey)
	s.wg.Add(1)
	go s.flushLoop()
	return s, nil
}

const uniqueKey = "_meta_unique_devices"

func (s *Store) loadUnique() error {
	return s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(registry.UsageStatsBucket())
		if b == nil {
			return nil
		}
		raw := b.Get([]byte(uniqueKey))
		if raw == nil {
			return nil
		}
		var n int64
		if err := json.Unmarshal(raw, &n); err != nil {
			return err
		}
		atomic.StoreInt64(&s.uniqueDevices, n)
		return nil
	})
}

func (s *Store) persistUnique() error {
	n := atomic.LoadInt64(&s.uniqueDevices)
	raw, _ := json.Marshal(n)
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.UsageStatsBucket()).Put([]byte(uniqueKey), raw)
	})
}

func hourKey(t time.Time) string {
	return t.UTC().Format("2006-01-02T15")
}

func (s *Store) loadHourIntoCurrent(key string) error {
	return s.db.View(func(tx *bbolt.Tx) error {
		raw := tx.Bucket(registry.UsageStatsBucket()).Get([]byte(key))
		if raw == nil {
			s.cur = Counters{Hour: key}
			return nil
		}
		var c Counters
		if err := json.Unmarshal(raw, &c); err != nil {
			return err
		}
		c.Hour = key
		s.cur = c
		return nil
	})
}

func (s *Store) flushLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			s.mu.Lock()
			_ = s.flushLocked()
			s.mu.Unlock()
			_ = s.persistUnique()
			return
		case <-ticker.C:
			s.mu.Lock()
			_ = s.flushLocked()
			s.mu.Unlock()
			_ = s.persistUnique()
		}
	}
}

// Close flushes and stops the background writer.
func (s *Store) Close() {
	close(s.stop)
	s.wg.Wait()
}

func (s *Store) rollIfNeeded() {
	key := hourKey(time.Now().UTC())
	if key == s.curKey {
		return
	}
	_ = s.flushLocked()
	s.curKey = key
	_ = s.loadHourIntoCurrent(key)
}

func (s *Store) flushLocked() error {
	s.cur.Hour = s.curKey
	raw, err := json.Marshal(s.cur)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.UsageStatsBucket()).Put([]byte(s.curKey), raw)
	})
}

func (s *Store) add(mutate func(*Counters)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollIfNeeded()
	mutate(&s.cur)
}

// --- event recorders ---

func (s *Store) VoiceNoteUploaded(bytes int64, channelID string) {
	s.add(func(c *Counters) {
		if channelID != "" {
			c.ChannelClipsUploaded++
			c.ChannelClipsUploadBytes += bytes
		} else {
			c.VoiceNotesUploaded++
			c.VoiceNotesUploadBytes += bytes
		}
	})
}

func (s *Store) VoiceNoteDownloaded(bytes int64) {
	s.add(func(c *Counters) {
		c.VoiceNotesDownloaded++
		c.VoiceNotesDownloadBytes += bytes
	})
}

func (s *Store) VoiceNoteAcked() {
	s.add(func(c *Counters) { c.VoiceNotesAcked++ })
}

func (s *Store) ChannelInvite() {
	s.add(func(c *Counters) { c.ChannelInvites++ })
}

func (s *Store) ChannelAccept() {
	s.add(func(c *Counters) { c.ChannelAccepts++ })
}

func (s *Store) ChannelClose() {
	s.add(func(c *Counters) { c.ChannelCloses++ })
}

func (s *Store) PTTSession() {
	s.add(func(c *Counters) { c.PttTalkSessions++ })
}

func (s *Store) PTTBytesSent(n int64) {
	if n <= 0 {
		return
	}
	s.add(func(c *Counters) { c.PttBytesSent += n })
}

func (s *Store) PTTBytesReceived(n int64) {
	if n <= 0 {
		return
	}
	s.add(func(c *Counters) { c.PttBytesReceived += n })
}

func (s *Store) DeviceSeen(created bool) {
	s.add(func(c *Counters) {
		c.DevicesSeenTouch++
		if created {
			c.DevicesSeenNew++
			atomic.AddInt64(&s.uniqueDevices, 1)
		}
	})
}

func (s *Store) UniqueDevicesEver() int64 {
	return atomic.LoadInt64(&s.uniqueDevices)
}

func addCounters(dst *Counters, src Counters) {
	dst.VoiceNotesUploaded += src.VoiceNotesUploaded
	dst.VoiceNotesUploadBytes += src.VoiceNotesUploadBytes
	dst.VoiceNotesDownloaded += src.VoiceNotesDownloaded
	dst.VoiceNotesDownloadBytes += src.VoiceNotesDownloadBytes
	dst.VoiceNotesAcked += src.VoiceNotesAcked
	dst.ChannelClipsUploaded += src.ChannelClipsUploaded
	dst.ChannelClipsUploadBytes += src.ChannelClipsUploadBytes
	dst.ChannelInvites += src.ChannelInvites
	dst.ChannelAccepts += src.ChannelAccepts
	dst.ChannelCloses += src.ChannelCloses
	dst.PttTalkSessions += src.PttTalkSessions
	dst.PttBytesSent += src.PttBytesSent
	dst.PttBytesReceived += src.PttBytesReceived
	dst.DevicesSeenNew += src.DevicesSeenNew
	dst.DevicesSeenTouch += src.DevicesSeenTouch
}

// Query aggregates counters for range: today, week, month, or all.
func (s *Store) Query(rangeName string, devicesKnownNow int) (Snapshot, error) {
	s.mu.Lock()
	_ = s.flushLocked()
	s.mu.Unlock()

	now := time.Now().UTC()
	var from time.Time
	switch rangeName {
	case "week":
		from = now.AddDate(0, 0, -7)
	case "month":
		from = now.AddDate(0, -1, 0)
	case "all":
		from = time.Time{}
	default:
		rangeName = "today"
		from = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	}

	var hours []Counters
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(registry.UsageStatsBucket())
		return b.ForEach(func(k, v []byte) error {
			key := string(k)
			if key == uniqueKey {
				return nil
			}
			t, err := time.Parse("2006-01-02T15", key)
			if err != nil {
				return nil
			}
			if !from.IsZero() && t.Before(from) {
				return nil
			}
			var c Counters
			if err := json.Unmarshal(v, &c); err != nil {
				return err
			}
			c.Hour = key
			hours = append(hours, c)
			return nil
		})
	})
	if err != nil {
		return Snapshot{}, err
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i].Hour < hours[j].Hour })

	var totals Counters
	daily := map[string]Counters{}
	for _, h := range hours {
		addCounters(&totals, h)
		day := h.Hour
		if len(day) >= 10 {
			day = day[:10]
		}
		d := daily[day]
		d.Hour = day
		addCounters(&d, h)
		daily[day] = d
	}
	var series []Counters
	for _, d := range daily {
		series = append(series, d)
	}
	sort.Slice(series, func(i, j int) bool { return series[i].Hour < series[j].Hour })

	fromStr := ""
	if !from.IsZero() {
		fromStr = from.Format(time.RFC3339)
	} else if len(hours) > 0 {
		fromStr = hours[0].Hour + ":00:00Z"
	}
	snap := Snapshot{
		Range:           rangeName,
		From:            fromStr,
		To:              now.Format(time.RFC3339),
		Totals:          totals,
		DevicesKnownNow: devicesKnownNow,
		Series:          series,
		Note:            "PTT byte counts are for this Base Station's own mesh participation only — peer-to-peer phone traffic that never reaches this process is not included. Unique devices ever: " + fmt.Sprintf("%d", s.UniqueDevicesEver()) + ".",
	}
	return snap, nil
}
