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

func TestHubSetRouteAndClear(t *testing.T) {
	h, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	h.SetRoute("aaa", "bbb")
	to, ok := h.RouteOf("aaa")
	if !ok || to != "bbb" {
		t.Fatalf("RouteOf=%q ok=%v", to, ok)
	}
	h.ClearRoute("aaa")
	if _, ok := h.RouteOf("aaa"); ok {
		t.Fatal("route should be cleared")
	}
}

func TestHubRemoveClearsRoutes(t *testing.T) {
	h, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.SetRoute("aaa", "bbb")
	h.SetRoute("ccc", "bbb")
	h.Remove("bbb")
	if _, ok := h.RouteOf("aaa"); ok {
		t.Fatal("route to removed peer should clear")
	}
	if _, ok := h.RouteOf("ccc"); ok {
		t.Fatal("route to removed peer should clear")
	}
}

func TestHubRoomsIsolateFanOut(t *testing.T) {
	h, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.SetRoom("aaa", "ch1")
	h.SetRoom("bbb", "ch1")
	h.SetRoom("ccc", "ch2")
	if h.RoomOf("aaa") != "ch1" || h.RoomOf("ccc") != "ch2" {
		t.Fatal("room assignment")
	}
	h.ClearRoom("aaa")
	if h.RoomOf("aaa") != "" {
		t.Fatal("clear room")
	}
}
