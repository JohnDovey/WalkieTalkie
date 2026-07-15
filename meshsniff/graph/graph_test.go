package graph

import "testing"

func TestPreferKindPhoneOverComputer(t *testing.T) {
	if preferKind(KindComputer, KindWalkie) != KindWalkie {
		t.Fatal("walkie should rank above computer")
	}
}

func TestMergeKeepsPhoneKind(t *testing.T) {
	dst := &Node{Kind: KindComputer, Platform: "", Label: "192.168.0.50"}
	mergeNode(dst, Node{Kind: KindWalkie, Platform: "android", Label: "Pixel", Nickname: "Pixel"})
	if dst.Kind != KindWalkie {
		t.Fatalf("kind=%s want walkie", dst.Kind)
	}
	if dst.Platform != "android" {
		t.Fatalf("platform=%s", dst.Platform)
	}
}

func TestIsPhonePlatform(t *testing.T) {
	for _, p := range []string{"android", "ios", "wear", "Android"} {
		if !isPhonePlatform(p) {
			t.Fatalf("%q should be phone", p)
		}
	}
	if isPhonePlatform("desktop-darwin") {
		t.Fatal("desktop should not be phone")
	}
}

func TestUpsertRelocatesWalkieOntoHostByMAC(t *testing.T) {
	s := NewStore()
	baseID := "walkiebase:local"
	s.Upsert(Node{ID: baseID, Kind: KindWalkie, Label: "Base"})
	devID := s.Upsert(Node{
		ID: "dev:phone1", Kind: KindWalkie, Label: "Lejana", Nickname: "Lejana",
		MeshID: "phone1", Platform: "android", MACs: []string{"aa:bb:cc:dd:ee:ff"},
	})
	if devID != "dev:phone1" {
		t.Fatalf("seed id=%s", devID)
	}
	s.Link(baseID, devID, "walkietalkie", false)

	hostID := s.Upsert(Node{
		ID: "host:192.168.0.50", Kind: KindComputer, Label: "192.168.0.50",
		IPs: []string{"192.168.0.50"}, MACs: []string{"aa:bb:cc:dd:ee:ff"},
	})
	if hostID != "host:192.168.0.50" {
		t.Fatalf("host id=%s want host:192.168.0.50", hostID)
	}

	nodes := s.Nodes()
	var phone *Node
	for i := range nodes {
		if nodes[i].ID == "dev:phone1" {
			t.Fatal("dev:phone1 should have been relocated")
		}
		if nodes[i].ID == hostID {
			phone = &nodes[i]
		}
	}
	if phone == nil {
		t.Fatal("missing host node")
	}
	if phone.Nickname != "Lejana" || phone.MeshID != "phone1" {
		t.Fatalf("identity not merged: %+v", phone)
	}
	if phone.Kind != KindWalkie {
		t.Fatalf("kind=%s want walkie", phone.Kind)
	}
	if len(phone.IPs) == 0 || phone.IPs[0] != "192.168.0.50" {
		t.Fatalf("ips=%v", phone.IPs)
	}
}

func TestUpsertRelocatesWalkieOntoHostByMeshID(t *testing.T) {
	s := NewStore()
	s.Upsert(Node{
		ID: "dev:phone2", Kind: KindWalkie, Label: "Lejana", Nickname: "Lejana",
		MeshID: "phone2", Platform: "android",
	})
	hostID := s.Upsert(Node{
		ID: "host:192.168.0.51", Kind: KindWalkie, Label: "Lejana",
		MeshID: "phone2", Platform: "android", IPs: []string{"192.168.0.51"},
	})
	if hostID != "host:192.168.0.51" {
		t.Fatalf("host id=%s", hostID)
	}
	for _, n := range s.Nodes() {
		if n.ID == "dev:phone2" {
			t.Fatal("dev node should be gone")
		}
	}
}
