package registry

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMergeRemoteDevices(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	n, err := s.MergeRemoteDevices("base-b", "Base B", []Device{
		{ID: "phone1", Name: "Phone", LastSeen: time.Now(), Status: StatusConnected},
	})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	list, err := s.ListRemoteDevices()
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if list[0].RemoteBaseID != "base-b" {
		t.Fatalf("origin=%s", list[0].RemoteBaseID)
	}
}
