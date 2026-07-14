// Package voicenote stores async Opus voice notes and private-channel
// metadata on the Base Station. See
// docs/2026-07-13-voice-message-and-private-channels.md.
package voicenote

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
	"github.com/google/uuid"
	"go.etcd.io/bbolt"
)

const Retention = 21 * 24 * time.Hour

const (
	StatusQueued    = "queued"
	StatusDelivered = "delivered"
	StatusDeleted   = "deleted"

	ChannelPending = "pending"
	ChannelActive  = "active"
	ChannelClosed  = "closed"
)

// Note is one store-and-forward Opus clip.
type Note struct {
	ID        string    `json:"id"`
	FromID    string    `json:"fromId"`
	ToID      string    `json:"toId"`
	ChannelID string    `json:"channelId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Size      int64     `json:"size"`
	Path      string    `json:"path"`
	Status    string    `json:"status"`
}

// Channel is a private invite session among one or more devices.
type Channel struct {
	ID           string    `json:"id"`
	ParticipantA string    `json:"participantA"` // derived: participants[0] for older readers
	ParticipantB string    `json:"participantB"` // derived: participants[1] when present
	// Participants is the canonical membership list (N-party).
	Participants []string `json:"participants,omitempty"`
	// PendingInvites lists device IDs invited to join an active channel
	// who have not Accepted yet.
	PendingInvites []string  `json:"pendingInvites,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	Status         string    `json:"status"`
	// Focused lists device IDs currently viewing this channel (both parties
	// may be focused at once). FocusedBy remains the most recent focus for
	// older clients / tooling that only read a single string.
	Focused   []string `json:"focused,omitempty"`
	FocusedBy string   `json:"focusedBy,omitempty"`
	// UnreadFor is filled only in ListChannels responses.
	UnreadFor int `json:"unreadFor,omitempty"`
}

// PeerInfo is one other participant for ChannelView UIs.
type PeerInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ChannelView is ListChannels output including peer name hints from the caller.
type ChannelView struct {
	Channel
	PeerID    string     `json:"peerId"` // first other peer (1:1 compat)
	Peers     []PeerInfo `json:"peers,omitempty"`
	UnreadFor int        `json:"unreadFor"`
}

// Store persists notes + channels in the shared registry bbolt DB and Opus
// blobs under blobDir.
type Store struct {
	db      *bbolt.DB
	blobDir string
	mu      sync.Mutex
}

// NewStore uses the registry's open DB and stores Opus files under
// dataDir/voice-notes/.
func NewStore(reg *registry.Store, dataDir string) (*Store, error) {
	blobDir := filepath.Join(dataDir, "voice-notes")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		return nil, fmt.Errorf("voicenote: blob dir: %w", err)
	}
	return &Store{db: reg.Bolt(), blobDir: blobDir}, nil
}

func (s *Store) putNote(tx *bbolt.Tx, n *Note) error {
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return tx.Bucket(registry.VoiceNotesBucket()).Put([]byte(n.ID), raw)
}

func (s *Store) getNote(tx *bbolt.Tx, id string) (*Note, bool) {
	raw := tx.Bucket(registry.VoiceNotesBucket()).Get([]byte(id))
	if raw == nil {
		return nil, false
	}
	var n Note
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, false
	}
	return &n, true
}

func (s *Store) putChannel(tx *bbolt.Tx, c *Channel) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return tx.Bucket(registry.ChannelsBucket()).Put([]byte(c.ID), raw)
}

func (s *Store) getChannel(tx *bbolt.Tx, id string) (*Channel, bool) {
	raw := tx.Bucket(registry.ChannelsBucket()).Get([]byte(id))
	if raw == nil {
		return nil, false
	}
	var c Channel
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, false
	}
	c.normalizeFocus()
	c.normalizeParticipants()
	return &c, true
}

func (c *Channel) normalizeParticipants() {
	if len(c.Participants) == 0 {
		if c.ParticipantA != "" {
			c.Participants = append(c.Participants, c.ParticipantA)
		}
		if c.ParticipantB != "" && c.ParticipantB != c.ParticipantA {
			c.Participants = append(c.Participants, c.ParticipantB)
		}
	}
	// Dedup while preserving order.
	seen := map[string]struct{}{}
	out := c.Participants[:0]
	for _, id := range c.Participants {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	c.Participants = out
	if len(c.Participants) > 0 {
		c.ParticipantA = c.Participants[0]
	} else {
		c.ParticipantA = ""
	}
	if len(c.Participants) > 1 {
		c.ParticipantB = c.Participants[1]
	} else {
		c.ParticipantB = ""
	}
}

func (c *Channel) isParticipant(deviceID string) bool {
	c.normalizeParticipants()
	for _, id := range c.Participants {
		if id == deviceID {
			return true
		}
	}
	return false
}

func (c *Channel) addParticipant(deviceID string) bool {
	if deviceID == "" || c.isParticipant(deviceID) {
		return false
	}
	c.Participants = append(c.Participants, deviceID)
	c.normalizeParticipants()
	return true
}

func (c *Channel) addPendingInvite(deviceID string) bool {
	if deviceID == "" || c.isParticipant(deviceID) {
		return false
	}
	for _, id := range c.PendingInvites {
		if id == deviceID {
			return false
		}
	}
	c.PendingInvites = append(c.PendingInvites, deviceID)
	return true
}

func (c *Channel) removePendingInvite(deviceID string) {
	out := c.PendingInvites[:0]
	for _, id := range c.PendingInvites {
		if id != deviceID {
			out = append(out, id)
		}
	}
	c.PendingInvites = out
}

func (c *Channel) isPendingInvite(deviceID string) bool {
	for _, id := range c.PendingInvites {
		if id == deviceID {
			return true
		}
	}
	return false
}

func (c *Channel) normalizeFocus() {
	if len(c.Focused) == 0 && c.FocusedBy != "" {
		c.Focused = []string{c.FocusedBy}
	}
	c.syncFocusedBy()
}

func (c *Channel) syncFocusedBy() {
	if len(c.Focused) == 0 {
		c.FocusedBy = ""
		return
	}
	c.FocusedBy = c.Focused[len(c.Focused)-1]
}

func (c *Channel) addFocused(deviceID string) {
	for _, id := range c.Focused {
		if id == deviceID {
			c.syncFocusedBy()
			return
		}
	}
	c.Focused = append(c.Focused, deviceID)
	c.syncFocusedBy()
}

func (c *Channel) removeFocused(deviceID string) {
	out := c.Focused[:0]
	for _, id := range c.Focused {
		if id != deviceID {
			out = append(out, id)
		}
	}
	c.Focused = out
	c.syncFocusedBy()
}

func (c *Channel) isFocused(deviceID string) bool {
	for _, id := range c.Focused {
		if id == deviceID {
			return true
		}
	}
	return false
}

// SaveNote writes opusBytes to disk and records metadata. Retention is 21 days.
func (s *Store) SaveNote(fromID, toID, channelID string, opusBytes []byte) (*Note, error) {
	return s.ImportNote(uuid.NewString(), fromID, toID, channelID, time.Now(), opusBytes)
}

// ImportNote stores a note with a caller-provided ID (P2P DataChannel receive).
// Idempotent: if the ID already exists, returns the existing note without error.
func (s *Store) ImportNote(id, fromID, toID, channelID string, createdAt time.Time, opusBytes []byte) (*Note, error) {
	if fromID == "" || toID == "" {
		return nil, fmt.Errorf("voicenote: from and to are required")
	}
	if len(opusBytes) == 0 {
		return nil, fmt.Errorf("voicenote: empty audio")
	}
	if id == "" {
		id = uuid.NewString()
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var existing *Note
	_ = s.db.View(func(tx *bbolt.Tx) error {
		if n, ok := s.getNote(tx, id); ok {
			existing = n
		}
		return nil
	})
	if existing != nil {
		return existing, nil
	}

	path := filepath.Join(s.blobDir, id+".opus")
	if err := os.WriteFile(path, opusBytes, 0o600); err != nil {
		return nil, fmt.Errorf("voicenote: write blob: %w", err)
	}
	n := &Note{
		ID:        id,
		FromID:    fromID,
		ToID:      toID,
		ChannelID: channelID,
		CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(Retention),
		Size:      int64(len(opusBytes)),
		Path:      path,
		Status:    StatusQueued,
	}
	err := s.db.Update(func(tx *bbolt.Tx) error {
		return s.putNote(tx, n)
	})
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	return n, nil
}

// ListFor returns non-deleted notes addressed to deviceID (optionally
// filtered by fromID or channelID query handled by caller). Newest first.
func (s *Store) ListFor(deviceID, fromID, channelID string) ([]*Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Note
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.VoiceNotesBucket()).ForEach(func(_, raw []byte) error {
			var n Note
			if err := json.Unmarshal(raw, &n); err != nil {
				return err
			}
			if n.Status == StatusDeleted {
				return nil
			}
			if n.ToID != deviceID && n.FromID != deviceID {
				return nil
			}
			// Inbox view: when listing "for" a device as recipient-focused,
			// include notes to them; thread views pass fromID.
			if fromID != "" && n.FromID != fromID && n.ToID != fromID {
				return nil
			}
			if channelID != "" && n.ChannelID != channelID {
				return nil
			}
			if channelID == "" && fromID == "" {
				// Default inbox: only notes addressed to this device.
				if n.ToID != deviceID {
					return nil
				}
			}
			cp := n
			out = append(out, &cp)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// ThreadBetween returns notes between two devices (either direction),
// excluding channel-scoped clips unless channelID is set.
func (s *Store) ThreadBetween(a, b, channelID string) ([]*Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Note
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.VoiceNotesBucket()).ForEach(func(_, raw []byte) error {
			var n Note
			if err := json.Unmarshal(raw, &n); err != nil {
				return err
			}
			if n.Status == StatusDeleted {
				return nil
			}
			if channelID != "" {
				if n.ChannelID != channelID {
					return nil
				}
			} else if n.ChannelID != "" {
				return nil
			}
			pair := (n.FromID == a && n.ToID == b) || (n.FromID == b && n.ToID == a)
			if !pair {
				return nil
			}
			cp := n
			out = append(out, &cp)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// GetNote returns metadata for one note.
func (s *Store) GetNote(id string) (*Note, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n *Note
	var ok bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		n, ok = s.getNote(tx, id)
		return nil
	})
	return n, ok, err
}

// ReadAudio returns the Opus blob for a note.
func (s *Store) ReadAudio(id string) ([]byte, *Note, error) {
	n, ok, err := s.GetNote(id)
	if err != nil {
		return nil, nil, err
	}
	if !ok || n.Status == StatusDeleted {
		return nil, nil, fmt.Errorf("voicenote: not found")
	}
	data, err := os.ReadFile(n.Path)
	if err != nil {
		return nil, nil, err
	}
	return data, n, nil
}

// Ack marks a note delivered/played.
func (s *Store) Ack(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bbolt.Tx) error {
		n, ok := s.getNote(tx, id)
		if !ok {
			return fmt.Errorf("voicenote: not found")
		}
		n.Status = StatusDelivered
		return s.putNote(tx, n)
	})
}

// Delete soft-deletes a note and removes the blob file.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var path string
	err := s.db.Update(func(tx *bbolt.Tx) error {
		n, ok := s.getNote(tx, id)
		if !ok {
			return fmt.Errorf("voicenote: not found")
		}
		path = n.Path
		n.Status = StatusDeleted
		return s.putNote(tx, n)
	})
	if err != nil {
		return err
	}
	_ = os.Remove(path)
	return nil
}

// PendingCountFor returns queued notes waiting for deviceID.
func (s *Store) PendingCountFor(deviceID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.VoiceNotesBucket()).ForEach(func(_, raw []byte) error {
			var n Note
			if err := json.Unmarshal(raw, &n); err != nil {
				return err
			}
			if n.ToID == deviceID && n.Status == StatusQueued {
				count++
			}
			return nil
		})
	})
	return count, err
}

// PendingCounts returns queued counts keyed by recipient device ID.
func (s *Store) PendingCounts() (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.VoiceNotesBucket()).ForEach(func(_, raw []byte) error {
			var n Note
			if err := json.Unmarshal(raw, &n); err != nil {
				return err
			}
			if n.Status == StatusQueued {
				out[n.ToID]++
			}
			return nil
		})
	})
	return out, err
}

// UnreadFromSender counts queued notes from fromID to toID (direct DMs only).
func (s *Store) UnreadFromSender(toID, fromID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.VoiceNotesBucket()).ForEach(func(_, raw []byte) error {
			var n Note
			if err := json.Unmarshal(raw, &n); err != nil {
				return err
			}
			if n.ToID == toID && n.FromID == fromID && n.ChannelID == "" && n.Status == StatusQueued {
				count++
			}
			return nil
		})
	})
	return count, err
}

// Invite creates a pending private channel; peer must already be connected
// (caller checks registry). Rejects if an active/pending channel already exists.
func (s *Store) Invite(fromID, toID string) (*Channel, error) {
	if fromID == "" || toID == "" || fromID == toID {
		return nil, fmt.Errorf("voicenote: invalid invite participants")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var existing *Channel
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.ChannelsBucket()).ForEach(func(_, raw []byte) error {
			var c Channel
			if err := json.Unmarshal(raw, &c); err != nil {
				return err
			}
			if c.Status == ChannelClosed {
				return nil
			}
			pair := false
			c.normalizeParticipants()
			hasFrom, hasTo := false, false
			for _, id := range c.Participants {
				if id == fromID {
					hasFrom = true
				}
				if id == toID {
					hasTo = true
				}
			}
			if hasFrom && hasTo && len(c.Participants) == 2 {
				pair = true
			}
			if pair {
				cp := c
				existing = &cp
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	c := &Channel{
		ID:           uuid.NewString(),
		Participants: []string{fromID, toID},
		CreatedAt:    time.Now(),
		Status:       ChannelPending,
	}
	c.normalizeParticipants()
	err = s.db.Update(func(tx *bbolt.Tx) error {
		return s.putChannel(tx, c)
	})
	return c, err
}

// InviteMore adds toID as a pending invite on an existing channel. fromID must
// already be a participant. Accept moves them into Participants.
func (s *Store) InviteMore(channelID, fromID, toID string) (*Channel, error) {
	if toID == "" || fromID == "" || channelID == "" {
		return nil, fmt.Errorf("voicenote: invite requires ids")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out *Channel
	err := s.db.Update(func(tx *bbolt.Tx) error {
		c, ok := s.getChannel(tx, channelID)
		if !ok {
			return fmt.Errorf("voicenote: channel not found")
		}
		if !c.isParticipant(fromID) {
			return fmt.Errorf("voicenote: not a participant")
		}
		if c.Status == ChannelClosed {
			return fmt.Errorf("voicenote: channel closed")
		}
		if c.isParticipant(toID) {
			out = c
			return nil
		}
		c.addPendingInvite(toID)
		out = c
		return s.putChannel(tx, c)
	})
	return out, err
}

// Accept marks a pending channel active. acceptor must be a participant.
func (s *Store) Accept(channelID, acceptorID string) (*Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out *Channel
	err := s.db.Update(func(tx *bbolt.Tx) error {
		c, ok := s.getChannel(tx, channelID)
		if !ok {
			return fmt.Errorf("voicenote: channel not found")
		}
		if !c.isParticipant(acceptorID) && !c.isPendingInvite(acceptorID) {
			return fmt.Errorf("voicenote: not a participant")
		}
		if c.Status == ChannelClosed {
			return fmt.Errorf("voicenote: channel closed")
		}
		if c.isPendingInvite(acceptorID) {
			c.removePendingInvite(acceptorID)
			c.addParticipant(acceptorID)
		}
		c.Status = ChannelActive
		out = c
		return s.putChannel(tx, c)
	})
	return out, err
}

// CloseChannel marks a channel closed.
func (s *Store) CloseChannel(channelID, actorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bbolt.Tx) error {
		c, ok := s.getChannel(tx, channelID)
		if !ok {
			return fmt.Errorf("voicenote: channel not found")
		}
		if !c.isParticipant(actorID) {
			return fmt.Errorf("voicenote: not a participant")
		}
		c.Status = ChannelClosed
		c.Focused = nil
		c.FocusedBy = ""
		c.PendingInvites = nil
		return s.putChannel(tx, c)
	})
}

// SetFocus records whether a participant is viewing a channel. Both
// participants may be focused at the same time.
func (s *Store) SetFocus(channelID, deviceID string, focused bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bbolt.Tx) error {
		c, ok := s.getChannel(tx, channelID)
		if !ok {
			return fmt.Errorf("voicenote: channel not found")
		}
		if !c.isParticipant(deviceID) {
			return fmt.Errorf("voicenote: not a participant")
		}
		if focused {
			c.addFocused(deviceID)
		} else {
			c.removeFocused(deviceID)
		}
		return s.putChannel(tx, c)
	})
}

// GetChannel returns one channel.
func (s *Store) GetChannel(id string) (*Channel, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c *Channel
	var ok bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		c, ok = s.getChannel(tx, id)
		return nil
	})
	return c, ok, err
}

// ListChannels returns non-closed channels involving deviceID, with unread counts.
func (s *Store) ListChannels(deviceID string) ([]ChannelView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var channels []Channel
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.ChannelsBucket()).ForEach(func(_, raw []byte) error {
			var c Channel
			if err := json.Unmarshal(raw, &c); err != nil {
				return err
			}
			if c.Status == ChannelClosed {
				return nil
			}
			if !c.isParticipant(deviceID) && !c.isPendingInvite(deviceID) {
				return nil
			}
			channels = append(channels, c)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	var views []ChannelView
	for _, c := range channels {
		c.normalizeParticipants()
		var peers []PeerInfo
		var peerID string
		for _, id := range c.Participants {
			if id == deviceID {
				continue
			}
			peers = append(peers, PeerInfo{ID: id})
			if peerID == "" {
				peerID = id
			}
		}
		unread := 0
		_ = s.db.View(func(tx *bbolt.Tx) error {
			return tx.Bucket(registry.VoiceNotesBucket()).ForEach(func(_, raw []byte) error {
				var n Note
				if err := json.Unmarshal(raw, &n); err != nil {
					return err
				}
				if n.ChannelID == c.ID && n.ToID == deviceID && n.Status == StatusQueued {
					unread++
				}
				return nil
			})
		})
		views = append(views, ChannelView{
			Channel:   c,
			PeerID:    peerID,
			Peers:     peers,
			UnreadFor: unread,
		})
	}
	sort.Slice(views, func(i, j int) bool {
		return views[i].CreatedAt.After(views[j].CreatedAt)
	})
	return views, nil
}

// PeerOf returns another participant (first other), for 1:1-compatible callers.
func (c *Channel) PeerOf(selfID string) string {
	c.normalizeParticipants()
	for _, id := range c.Participants {
		if id != selfID {
			return id
		}
	}
	return ""
}

// OtherParticipants returns every member except selfID.
func (c *Channel) OtherParticipants(selfID string) []string {
	c.normalizeParticipants()
	var out []string
	for _, id := range c.Participants {
		if id != selfID {
			out = append(out, id)
		}
	}
	return out
}

// PurgeExpired deletes notes past ExpiresAt and removes blob files.
func (s *Store) PurgeExpired(now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var expired []*Note
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.VoiceNotesBucket()).ForEach(func(_, raw []byte) error {
			var n Note
			if err := json.Unmarshal(raw, &n); err != nil {
				return err
			}
			if n.Status != StatusDeleted && !n.ExpiresAt.After(now) {
				cp := n
				expired = append(expired, &cp)
			}
			return nil
		})
	})
	if err != nil {
		return 0, err
	}
	for _, n := range expired {
		_ = os.Remove(n.Path)
		_ = s.db.Update(func(tx *bbolt.Tx) error {
			n.Status = StatusDeleted
			return s.putNote(tx, n)
		})
	}
	return len(expired), nil
}
