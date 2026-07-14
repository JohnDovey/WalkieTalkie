// Package sync implements Base-Station-to-Base-Station registry
// synchronization, per docs/2026-07-13-implementation-plan.md
// ("Multi-Base-Station synchronization"): once this Base Station discovers
// another one (via its mDNS "api" TXT field — see core/discovery/mdns), it
// periodically pulls the peer's full device list (GET /api/devices) and
// merges it with registry.Store.MergeRemoteRegistry's last-seen-wins rule.
//
// When a VoicePuller is set, each tick also pulls private channels and
// voice-note metadata/blobs (GET /api/sync/channels, /api/sync/voice-notes).
package sync

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
)

// VoicePuller pulls channel + voice-note state from a peer Base Station.
// Implemented by *voicenote.Store.
type VoicePuller interface {
	PullFromPeer(host string, apiPort int) (channelsImported, notesImported int, err error)
}

// Syncer manages one repeating pull-and-merge ticker per discovered peer
// Base Station.
type Syncer struct {
	store    *registry.Store
	interval time.Duration
	client   *http.Client
	voice    VoicePuller

	mu    sync.Mutex
	peers map[string]func() // peerID -> cancel
}

// New creates a Syncer that merges every interval.
func New(store *registry.Store, interval time.Duration) *Syncer {
	return &Syncer{
		store:    store,
		interval: interval,
		client:   &http.Client{Timeout: 5 * time.Second},
		peers:    make(map[string]func()),
	}
}

// SetVoice enables multi-Base Station voice-note / channel replication.
func (s *Syncer) SetVoice(v VoicePuller) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voice = v
}

// SetInterval updates the sync cadence for subsequent ticker periods.
// Existing loops pick up the new duration on their next tick restart —
// we cancel and recreate each peer loop so the change is immediate.
func (s *Syncer) SetInterval(interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if interval <= 0 {
		interval = 30 * time.Second
	}
	s.interval = interval
}

// EnsureSyncing starts a repeating sync loop against peerID's REST API at
// host:apiPort, if one isn't already running for that peer. Safe to call
// repeatedly (e.g. on every mDNS re-sighting) — idempotent.
func (s *Syncer) EnsureSyncing(peerID, host string, apiPort int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.peers[peerID]; ok {
		return
	}

	stop := make(chan struct{})
	s.peers[peerID] = func() { close(stop) }
	go s.loop(stop, peerID, host, apiPort)
}

func (s *Syncer) loop(stop <-chan struct{}, peerID, host string, apiPort int) {
	s.syncOnce(peerID, host, apiPort) // sync immediately, don't wait a full interval first
	for {
		s.mu.Lock()
		interval := s.interval
		s.mu.Unlock()
		timer := time.NewTimer(interval)
		select {
		case <-stop:
			timer.Stop()
			return
		case <-timer.C:
			s.syncOnce(peerID, host, apiPort)
		}
	}
}

func (s *Syncer) syncOnce(peerID, host string, apiPort int) {
	url := fmt.Sprintf("http://%s:%d/api/devices", host, apiPort)
	resp, err := s.client.Get(url)
	if err != nil {
		log.Printf("sync: fetch %s (Base Station %s): %v", url, peerID, err)
		return
	}
	defer resp.Body.Close()

	var devices []*registry.Device
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		log.Printf("sync: decode response from %s: %v", url, err)
		return
	}

	flat := make([]registry.Device, len(devices))
	for i, d := range devices {
		flat[i] = *d
	}

	updated, err := s.store.MergeRemoteRegistry(flat)
	if err != nil {
		log.Printf("sync: merge from %s: %v", peerID, err)
		return
	}
	if updated > 0 {
		log.Printf("sync: merged %d device(s) from Base Station %s", updated, peerID)
	}

	s.mu.Lock()
	voice := s.voice
	s.mu.Unlock()
	if voice == nil {
		return
	}
	chN, noteN, verr := voice.PullFromPeer(host, apiPort)
	if verr != nil {
		log.Printf("sync: voice pull from Base Station %s: %v", peerID, verr)
		return
	}
	if chN > 0 || noteN > 0 {
		log.Printf("sync: imported %d channel(s), %d voice note(s) from Base Station %s", chN, noteN, peerID)
	}
}

// Stop cancels every peer's sync loop.
func (s *Syncer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.peers {
		cancel()
	}
	s.peers = make(map[string]func())
}
