package voicenote

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	StatusQueued    = "queued"
	StatusDelivered = "delivered"
	StatusDeleted   = "deleted"
	localRetention  = 21 * 24 * time.Hour
)

// LocalInbox stores voice notes received (or sent) over P2P DataChannel on
// the device, so list/play/ack work without a Base Station.
type LocalInbox struct {
	dir string
	mu  sync.Mutex
}

// OpenLocalInbox creates/opens an inbox under dataDir/voice-notes-local/.
func OpenLocalInbox(dataDir string) (*LocalInbox, error) {
	dir := filepath.Join(dataDir, "voice-notes-local")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &LocalInbox{dir: dir}, nil
}

func (in *LocalInbox) metaPath() string { return filepath.Join(in.dir, "notes.json") }
func (in *LocalInbox) blobPath(id string) string {
	return filepath.Join(in.dir, id+".opus")
}

func (in *LocalInbox) load() (map[string]Note, error) {
	raw, err := os.ReadFile(in.metaPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Note{}, nil
		}
		return nil, err
	}
	var list []Note
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make(map[string]Note, len(list))
	for _, n := range list {
		out[n.ID] = n
	}
	return out, nil
}

func (in *LocalInbox) save(notes map[string]Note) error {
	list := make([]Note, 0, len(notes))
	for _, n := range notes {
		list = append(list, n)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(in.metaPath(), raw, 0o600)
}

// Put stores a received (or outbound-copy) note. Idempotent by ID.
func (in *LocalInbox) Put(n Note, audio []byte) error {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	if n.Status == "" {
		n.Status = StatusQueued
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	if n.ExpiresAt.IsZero() {
		n.ExpiresAt = n.CreatedAt.Add(localRetention)
	}
	if n.Size == 0 {
		n.Size = int64(len(audio))
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	notes, err := in.load()
	if err != nil {
		return err
	}
	if existing, ok := notes[n.ID]; ok && existing.Status != StatusDeleted {
		return nil // already have it
	}
	if len(audio) > 0 && n.Status != StatusDeleted {
		if err := os.WriteFile(in.blobPath(n.ID), audio, 0o600); err != nil {
			return err
		}
	}
	notes[n.ID] = n
	return in.save(notes)
}

// NewID returns a fresh note UUID for outbound P2P sends.
func NewID() string { return uuid.NewString() }

// List returns non-deleted notes filtered like Base Station Inbox.
func (in *LocalInbox) List(selfID, withPeerID, channelID string) ([]Note, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	notes, err := in.load()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	changed := false
	var out []Note
	for id, n := range notes {
		if !n.ExpiresAt.IsZero() && now.After(n.ExpiresAt) {
			_ = os.Remove(in.blobPath(id))
			delete(notes, id)
			changed = true
			continue
		}
		if n.Status == StatusDeleted {
			continue
		}
		if channelID != "" {
			if n.ChannelID != channelID {
				continue
			}
		} else if withPeerID != "" {
			if n.ChannelID != "" {
				continue
			}
			if !((n.FromID == selfID && n.ToID == withPeerID) || (n.FromID == withPeerID && n.ToID == selfID)) {
				continue
			}
		} else {
			// Inbox badges: notes addressed to self.
			if n.ToID != selfID {
				continue
			}
		}
		out = append(out, n)
	}
	if changed {
		_ = in.save(notes)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// ReadAudio returns Opus bytes for a local note.
func (in *LocalInbox) ReadAudio(id string) ([]byte, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	notes, err := in.load()
	if err != nil {
		return nil, err
	}
	n, ok := notes[id]
	if !ok || n.Status == StatusDeleted {
		return nil, fmt.Errorf("voicenote: local note %s not found", id)
	}
	return os.ReadFile(in.blobPath(id))
}

// Has reports whether a non-deleted local note exists.
func (in *LocalInbox) Has(id string) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	notes, _ := in.load()
	n, ok := notes[id]
	return ok && n.Status != StatusDeleted
}

// Ack marks a local note delivered.
func (in *LocalInbox) Ack(id string) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	notes, err := in.load()
	if err != nil {
		return err
	}
	n, ok := notes[id]
	if !ok {
		return fmt.Errorf("voicenote: local note %s not found", id)
	}
	n.Status = StatusDelivered
	notes[id] = n
	return in.save(notes)
}

// Delete soft-deletes a local note and removes the blob.
func (in *LocalInbox) Delete(id string) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	notes, err := in.load()
	if err != nil {
		return err
	}
	n, ok := notes[id]
	if !ok {
		return nil
	}
	n.Status = StatusDeleted
	notes[id] = n
	_ = os.Remove(in.blobPath(id))
	return in.save(notes)
}
