package voicenote

import (
	"path/filepath"
	"testing"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
)

func TestNormalizeParticipantsFromAB(t *testing.T) {
	c := Channel{ParticipantA: "a", ParticipantB: "b"}
	c.normalizeParticipants()
	if len(c.Participants) != 2 || c.Participants[0] != "a" || c.Participants[1] != "b" {
		t.Fatalf("participants=%v", c.Participants)
	}
	c.Participants = []string{"x", "y", "z"}
	c.normalizeParticipants()
	if c.ParticipantA != "x" || c.ParticipantB != "y" {
		t.Fatalf("A/B re-derive: A=%s B=%s", c.ParticipantA, c.ParticipantB)
	}
}

func TestInviteMoreAcceptAndSyncUnion(t *testing.T) {
	dir := t.TempDir()
	reg, err := registry.Open(filepath.Join(dir, "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	store, err := NewStore(reg, dir)
	if err != nil {
		t.Fatal(err)
	}

	ch, err := store.Invite("a", "b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Accept(ch.ID, "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InviteMore(ch.ID, "a", "c"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.GetChannel(ch.ID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if !got.isPendingInvite("c") || got.isParticipant("c") {
		t.Fatalf("c should be pending only: %+v", got)
	}
	if _, err := store.Accept(ch.ID, "c"); err != nil {
		t.Fatal(err)
	}
	got, _, _ = store.GetChannel(ch.ID)
	if len(got.Participants) != 3 || !got.isParticipant("c") {
		t.Fatalf("participants=%v", got.Participants)
	}

	remote := *got
	remote.Participants = []string{"a", "b", "d"}
	remote.PendingInvites = []string{"e"}
	okUp, err := store.UpsertChannelFromSync(remote)
	if err != nil || !okUp {
		t.Fatalf("sync union: ok=%v err=%v", okUp, err)
	}
	got, _, _ = store.GetChannel(ch.ID)
	if !got.isParticipant("d") {
		t.Fatalf("expected d from union: %v", got.Participants)
	}
	if !got.isPendingInvite("e") {
		t.Fatalf("expected pending e: %v", got.PendingInvites)
	}

	views, err := store.ListChannels("a")
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || len(views[0].Peers) < 2 {
		t.Fatalf("peers=%v", views)
	}
}
