package relay

import (
	"testing"
)

func TestNewHubEmpty(t *testing.T) {
	h, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	if h.ParticipantCount() != 0 {
		t.Fatalf("want 0 participants, got %d", h.ParticipantCount())
	}
	h.InjectLocal("host", []byte{1, 2, 3}) // no-op when empty
	h.Close()
}

func TestHubHasAndRemove(t *testing.T) {
	h, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if h.Has("x") {
		t.Fatal("unexpected")
	}
	h.Remove("missing") // no-op
}
