package voicenote

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
)

func TestUpsertChannelAndNoteFromSync(t *testing.T) {
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

	ch := Channel{
		ID: "ch1", ParticipantA: "a", ParticipantB: "b",
		Status: ChannelPending, CreatedAt: time.Now().Add(-time.Hour),
		Focused: []string{"a"},
	}
	ok, err := store.UpsertChannelFromSync(ch)
	if err != nil || !ok {
		t.Fatalf("insert channel: ok=%v err=%v", ok, err)
	}

	ch2 := ch
	ch2.Status = ChannelActive
	ch2.Focused = []string{"b"}
	ok, err = store.UpsertChannelFromSync(ch2)
	if err != nil || !ok {
		t.Fatalf("promote channel: ok=%v err=%v", ok, err)
	}
	got, found, err := store.GetChannel("ch1")
	if err != nil || !found {
		t.Fatalf("get channel: found=%v err=%v", found, err)
	}
	if got.Status != ChannelActive {
		t.Fatalf("status=%s want active", got.Status)
	}
	if len(got.Focused) != 2 {
		t.Fatalf("focused=%v want both peers", got.Focused)
	}

	audio := []byte("fake-opus")
	sn := SyncNote{
		ID: "n1", FromID: "a", ToID: "b", ChannelID: "ch1",
		CreatedAt: time.Now(), Status: StatusQueued, Size: int64(len(audio)),
	}
	ok, err = store.UpsertNoteFromSync(sn, audio)
	if err != nil || !ok {
		t.Fatalf("insert note: ok=%v err=%v", ok, err)
	}
	ok, err = store.UpsertNoteFromSync(sn, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("duplicate note should not change")
	}

	sn.Status = StatusDelivered
	ok, err = store.UpsertNoteFromSync(sn, nil)
	if err != nil || !ok {
		t.Fatalf("mark delivered: ok=%v err=%v", ok, err)
	}

	sn.Status = StatusDeleted
	ok, err = store.UpsertNoteFromSync(sn, nil)
	if err != nil || !ok {
		t.Fatalf("tombstone: ok=%v err=%v", ok, err)
	}
	n, found, err := store.GetNote("n1")
	if err != nil || !found {
		t.Fatalf("get note: found=%v err=%v", found, err)
	}
	if n.Status != StatusDeleted {
		t.Fatalf("status=%s want deleted", n.Status)
	}
}
