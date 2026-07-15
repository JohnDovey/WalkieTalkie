package portmem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRememberPruneAndExact(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Remember("192.168.0.10", []int{80, 443, 22})
	s.Remember("192.168.0.10", []int{8080})
	got := s.PortsFor("192.168.0.10")
	if len(got) != 4 {
		t.Fatalf("ports=%v", got)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.PortsFor("192.168.0.10")) != 4 {
		t.Fatal("persist failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "known-ports.json")); err != nil {
		t.Fatal(err)
	}

	s2.PruneClosed("192.168.0.10", []int{80, 443, 9999}, []int{80})
	got = s2.PortsFor("192.168.0.10")
	want := map[int]bool{22: true, 80: true, 8080: true}
	if len(got) != 3 {
		t.Fatalf("after prune %v", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("unexpected %d in %v", p, got)
		}
	}

	s2.SetExact("192.168.0.10", []int{22})
	if len(s2.PortsFor("192.168.0.10")) != 1 || s2.PortsFor("192.168.0.10")[0] != 22 {
		t.Fatal(s2.PortsFor("192.168.0.10"))
	}
}
