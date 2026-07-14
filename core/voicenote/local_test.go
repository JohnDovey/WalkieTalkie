package voicenote

import (
	"testing"
	"time"
)

func TestLocalInboxPutListAck(t *testing.T) {
	dir := t.TempDir()
	in, err := OpenLocalInbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	audio := []byte("opus-bytes")
	n := Note{
		ID: "n1", FromID: "a", ToID: "b", CreatedAt: time.Now(), Status: StatusQueued,
	}
	if err := in.Put(n, audio); err != nil {
		t.Fatal(err)
	}
	if err := in.Put(n, audio); err != nil {
		t.Fatal(err) // idempotent
	}
	list, err := in.List("b", "", "")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	got, err := in.ReadAudio("n1")
	if err != nil || string(got) != string(audio) {
		t.Fatalf("audio=%q err=%v", got, err)
	}
	if err := in.Ack("n1"); err != nil {
		t.Fatal(err)
	}
	list, _ = in.List("b", "a", "")
	if len(list) != 1 || list[0].Status != StatusDelivered {
		t.Fatalf("after ack: %+v", list)
	}
	if err := in.Delete("n1"); err != nil {
		t.Fatal(err)
	}
	list, _ = in.List("b", "", "")
	if len(list) != 0 {
		t.Fatalf("after delete want empty, got %v", list)
	}
}
