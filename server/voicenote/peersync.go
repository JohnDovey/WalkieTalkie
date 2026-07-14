package voicenote

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
	"go.etcd.io/bbolt"
)

// SyncNote is note metadata for Base Station ↔ Base Station replication
// (no local filesystem Path).
type SyncNote struct {
	ID        string    `json:"id"`
	FromID    string    `json:"fromId"`
	ToID      string    `json:"toId"`
	ChannelID string    `json:"channelId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Size      int64     `json:"size"`
	Status    string    `json:"status"`
}

func noteToSync(n *Note) SyncNote {
	return SyncNote{
		ID: n.ID, FromID: n.FromID, ToID: n.ToID, ChannelID: n.ChannelID,
		CreatedAt: n.CreatedAt, ExpiresAt: n.ExpiresAt, Size: n.Size, Status: n.Status,
	}
}

// ListAllChannelsForSync returns every stored channel (including closed).
func (s *Store) ListAllChannelsForSync() ([]Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Channel
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.ChannelsBucket()).ForEach(func(_, raw []byte) error {
			var c Channel
			if err := json.Unmarshal(raw, &c); err != nil {
				return err
			}
			c.normalizeFocus()
			out = append(out, c)
			return nil
		})
	})
	return out, err
}

// ListAllNotesForSync returns every note metadata record (including deleted
// tombstones so peers can replicate soft-deletes).
func (s *Store) ListAllNotesForSync() ([]SyncNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []SyncNote
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(registry.VoiceNotesBucket()).ForEach(func(_, raw []byte) error {
			var n Note
			if err := json.Unmarshal(raw, &n); err != nil {
				return err
			}
			out = append(out, noteToSync(&n))
			return nil
		})
	})
	return out, err
}

func channelStatusRank(status string) int {
	switch status {
	case ChannelActive:
		return 3
	case ChannelPending:
		return 2
	case ChannelClosed:
		return 1
	default:
		return 0
	}
}

// UpsertChannelFromSync merges a peer Base Station's channel record.
// Returns true if local state changed.
func (s *Store) UpsertChannelFromSync(remote Channel) (bool, error) {
	remote.normalizeFocus()
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	err := s.db.Update(func(tx *bbolt.Tx) error {
		local, ok := s.getChannel(tx, remote.ID)
		if !ok {
			cp := remote
			cp.UnreadFor = 0
			changed = true
			return s.putChannel(tx, &cp)
		}
		next := *local
		if channelStatusRank(remote.Status) > channelStatusRank(local.Status) {
			next.Status = remote.Status
			changed = true
		}
		// Union focused sets.
		for _, id := range remote.Focused {
			before := len(next.Focused)
			next.addFocused(id)
			if len(next.Focused) != before {
				changed = true
			}
		}
		// Union participants.
		remote.normalizeParticipants()
		next.normalizeParticipants()
		for _, id := range remote.Participants {
			before := len(next.Participants)
			next.addParticipant(id)
			if len(next.Participants) != before {
				changed = true
			}
		}
		for _, id := range remote.PendingInvites {
			before := len(next.PendingInvites)
			next.addPendingInvite(id)
			if len(next.PendingInvites) != before {
				changed = true
			}
		}
		if remote.CreatedAt.Before(next.CreatedAt) && !remote.CreatedAt.IsZero() {
			next.CreatedAt = remote.CreatedAt
			changed = true
		}
		if remote.ParticipantA != "" && next.ParticipantA == "" {
			next.ParticipantA = remote.ParticipantA
			changed = true
		}
		if remote.ParticipantB != "" && next.ParticipantB == "" {
			next.ParticipantB = remote.ParticipantB
			changed = true
		}
		if !changed {
			return nil
		}
		next.UnreadFor = 0
		return s.putChannel(tx, &next)
	})
	return changed, err
}

// UpsertNoteFromSync inserts a note by ID if missing, or applies a remote
// soft-delete / delivered status. audio may be nil for tombstones or when
// the blob is already local. Returns true if local state changed.
func (s *Store) UpsertNoteFromSync(remote SyncNote, audio []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	err := s.db.Update(func(tx *bbolt.Tx) error {
		local, ok := s.getNote(tx, remote.ID)
		if ok {
			if remote.Status == StatusDeleted && local.Status != StatusDeleted {
				local.Status = StatusDeleted
				_ = os.Remove(local.Path)
				local.Path = ""
				changed = true
				return s.putNote(tx, local)
			}
			if remote.Status == StatusDelivered && local.Status == StatusQueued {
				local.Status = StatusDelivered
				changed = true
				return s.putNote(tx, local)
			}
			return nil
		}
		path := ""
		if remote.Status != StatusDeleted {
			if len(audio) == 0 {
				return fmt.Errorf("voicenote: sync note %s missing audio", remote.ID)
			}
			path = filepath.Join(s.blobDir, remote.ID+".opus")
			if err := os.WriteFile(path, audio, 0o600); err != nil {
				return err
			}
		}
		n := &Note{
			ID: remote.ID, FromID: remote.FromID, ToID: remote.ToID, ChannelID: remote.ChannelID,
			CreatedAt: remote.CreatedAt, ExpiresAt: remote.ExpiresAt, Size: remote.Size,
			Path: path, Status: remote.Status,
		}
		if n.ExpiresAt.IsZero() {
			n.ExpiresAt = n.CreatedAt.Add(Retention)
		}
		if n.Size == 0 && len(audio) > 0 {
			n.Size = int64(len(audio))
		}
		changed = true
		return s.putNote(tx, n)
	})
	return changed, err
}

// PullFromPeer fetches channels and voice notes from another Base Station and
// merges them locally. Satisfies sync.VoicePuller.
func (s *Store) PullFromPeer(host string, apiPort int) (channelsImported, notesImported int, err error) {
	base := fmt.Sprintf("http://%s:%d", host, apiPort)
	client := &http.Client{Timeout: 15 * time.Second}

	chURL := base + "/api/sync/channels"
	resp, err := client.Get(chURL)
	if err != nil {
		return 0, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return 0, 0, fmt.Errorf("GET %s: status %d", chURL, resp.StatusCode)
	}
	var channels []Channel
	decErr := json.NewDecoder(resp.Body).Decode(&channels)
	resp.Body.Close()
	if decErr != nil {
		return 0, 0, fmt.Errorf("decode channels: %w", decErr)
	}
	for _, c := range channels {
		ok, uerr := s.UpsertChannelFromSync(c)
		if uerr != nil {
			log.Printf("voicenote sync: channel %s: %v", c.ID, uerr)
			continue
		}
		if ok {
			channelsImported++
		}
	}

	notesURL := base + "/api/sync/voice-notes"
	resp, err = client.Get(notesURL)
	if err != nil {
		return channelsImported, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return channelsImported, 0, fmt.Errorf("GET %s: status %d", notesURL, resp.StatusCode)
	}
	var notes []SyncNote
	decErr = json.NewDecoder(resp.Body).Decode(&notes)
	resp.Body.Close()
	if decErr != nil {
		return channelsImported, 0, fmt.Errorf("decode notes: %w", decErr)
	}

	for _, sn := range notes {
		local, ok, gerr := s.GetNote(sn.ID)
		if gerr != nil {
			continue
		}
		needAudio := false
		if !ok {
			needAudio = sn.Status != StatusDeleted
		} else if local.Status == StatusDeleted && sn.Status != StatusDeleted {
			continue // keep tombstone
		} else if sn.Status == StatusDeleted && local.Status != StatusDeleted {
			needAudio = false
		} else if sn.Status == StatusDelivered && local.Status == StatusQueued {
			needAudio = false
		} else {
			continue // already have equivalent note
		}

		var audio []byte
		if needAudio {
			audioURL := fmt.Sprintf("%s/api/voice-notes/%s/audio", base, sn.ID)
			aresp, aerr := client.Get(audioURL)
			if aerr != nil {
				log.Printf("voicenote sync: audio %s: %v", sn.ID, aerr)
				continue
			}
			audio, aerr = io.ReadAll(io.LimitReader(aresp.Body, 8<<20))
			status := aresp.StatusCode
			aresp.Body.Close()
			if aerr != nil || status != http.StatusOK {
				log.Printf("voicenote sync: audio %s status=%d err=%v", sn.ID, status, aerr)
				continue
			}
		}
		imported, uerr := s.UpsertNoteFromSync(sn, audio)
		if uerr != nil {
			log.Printf("voicenote sync: note %s: %v", sn.ID, uerr)
			continue
		}
		if imported {
			notesImported++
		}
	}
	return channelsImported, notesImported, nil
}

// IngestBridgeVoice applies MeshBridge-pushed channel/note JSON (same shapes
// as /api/sync/*) and optional audio blobs keyed by note ID.
func (s *Store) IngestBridgeVoice(remoteBaseURL string, channelsJSON, notesJSON []byte, audio map[string][]byte) (channels, notes int, err error) {
	_ = remoteBaseURL
	var channelsList []Channel
	if len(channelsJSON) > 0 && string(channelsJSON) != "null" {
		if err := json.Unmarshal(channelsJSON, &channelsList); err != nil {
			return 0, 0, fmt.Errorf("voicenote: bridge channels: %w", err)
		}
	}
	var notesList []SyncNote
	if len(notesJSON) > 0 && string(notesJSON) != "null" {
		if err := json.Unmarshal(notesJSON, &notesList); err != nil {
			return 0, 0, fmt.Errorf("voicenote: bridge notes: %w", err)
		}
	}
	for _, ch := range channelsList {
		ok, uerr := s.UpsertChannelFromSync(ch)
		if uerr != nil {
			return channels, notes, uerr
		}
		if ok {
			channels++
		}
	}
	for _, sn := range notesList {
		blob := audio[sn.ID]
		ok, uerr := s.UpsertNoteFromSync(sn, blob)
		if uerr != nil {
			return channels, notes, uerr
		}
		if ok {
			notes++
		}
	}
	return channels, notes, nil
}
