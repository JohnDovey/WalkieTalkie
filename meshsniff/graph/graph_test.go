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
