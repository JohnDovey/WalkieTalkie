package voicenote

import (
	"testing"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/media"
)

type mockPusher struct {
	direct map[string]bool
	sent   []media.VoiceNoteMeta
}

func (m *mockPusher) DirectConnected(peerID string) bool { return m.direct[peerID] }
func (m *mockPusher) SendVoiceNoteP2P(meta media.VoiceNoteMeta, audio []byte) error {
	m.sent = append(m.sent, meta)
	return nil
}

func TestPushNoteToDirectConnected(t *testing.T) {
	p := &mockPusher{direct: map[string]bool{"bob": true}}
	h := &Handlers{Pusher: p}
	note := &Note{
		ID: "n1", FromID: "alice", ToID: "bob", CreatedAt: time.Now(),
	}
	h.pushNote(note, []byte{1, 2, 3})
	if len(p.sent) != 1 || p.sent[0].ID != "n1" || p.sent[0].ToID != "bob" {
		t.Fatalf("sent=%v", p.sent)
	}
}

func TestPushNoteSkipsOffline(t *testing.T) {
	p := &mockPusher{direct: map[string]bool{}}
	h := &Handlers{Pusher: p}
	h.pushNote(&Note{ID: "n1", ToID: "bob"}, []byte{1})
	if len(p.sent) != 0 {
		t.Fatalf("expected no push, got %v", p.sent)
	}
}
