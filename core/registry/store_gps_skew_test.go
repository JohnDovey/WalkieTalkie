package registry

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/config"
	"github.com/JohnDovey/WalkieTalkie/core/proto"
)

func TestGPSHistoryAppendPurgeAndDedupe(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().UTC()
	p1 := proto.GeoPoint{Lat: 1.0, Lon: 2.0, Timestamp: now}
	if err := s.SetLocation("dev1", p1); err != nil {
		t.Fatal(err)
	}
	// Near-identical within GPS interval → deduped.
	p2 := proto.GeoPoint{Lat: 1.0, Lon: 2.0, Timestamp: now.Add(2 * time.Second)}
	if err := s.SetLocation("dev1", p2); err != nil {
		t.Fatal(err)
	}
	hist, err := s.ListGPSHistory("dev1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("after dedupe len=%d want 1", len(hist))
	}

	p3 := proto.GeoPoint{Lat: 1.1, Lon: 2.1, Timestamp: now.Add(40 * time.Second)}
	if err := s.SetLocation("dev1", p3); err != nil {
		t.Fatal(err)
	}
	hist, err = s.ListGPSHistory("dev1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("len=%d want 2", len(hist))
	}
	if hist[0].Lat != 1.0 || hist[1].Lat != 1.1 {
		t.Fatalf("order oldest→newest: %+v", hist)
	}

	// Old sample should purge.
	old := proto.GeoPoint{Lat: 9, Lon: 9, Timestamp: now.Add(-8 * 24 * time.Hour)}
	if err := s.SetLocation("dev1", old); err != nil {
		t.Fatal(err)
	}
	n, err := s.PurgeGPSHistory(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected purge of old sample, got %d", n)
	}
	hist, err = s.ListGPSHistory("dev1", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range hist {
		if p.Lat == 9 {
			t.Fatal("old sample still present after purge")
		}
	}
}

func TestMergeRemoteRegistrySoftSkew(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	settings := config.Default()
	settings.SyncClockSkewSeconds = 3
	if err := s.SetSettings(settings); err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC()
	local := Device{ID: "d1", Name: "local", LastSeen: base, Status: StatusConnected}
	if _, err := s.MergeRemoteRegistry([]Device{local}); err != nil {
		t.Fatal(err)
	}

	// Remote only 1s ahead — should not win under 3s skew.
	stale := Device{ID: "d1", Name: "remote-close", LastSeen: base.Add(time.Second), Status: StatusConnected}
	n, err := s.MergeRemoteRegistry([]Device{stale})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("close remote should not merge, n=%d", n)
	}
	got, ok, err := s.Get("d1")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if got.Name != "local" {
		t.Fatalf("name=%q want local", got.Name)
	}

	// Remote 4s ahead — wins.
	fresh := Device{ID: "d1", Name: "remote-fresh", LastSeen: base.Add(4 * time.Second), Status: StatusConnected}
	n, err = s.MergeRemoteRegistry([]Device{fresh})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("fresh remote should merge, n=%d", n)
	}
	got, ok, err = s.Get("d1")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if got.Name != "remote-fresh" {
		t.Fatalf("name=%q want remote-fresh", got.Name)
	}
}
