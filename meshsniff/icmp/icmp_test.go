package icmp

import "testing"

func TestSweepAllSkippedWithoutRoot(t *testing.T) {
	if Enabled() {
		t.Skip("running as root")
	}
	got := SweepAll([]string{"127.0.0.1", "192.168.0.1"}, 0, 4)
	if got != nil {
		t.Fatalf("expected nil without root, got %v", got)
	}
}

func TestSweepSkippedWithoutRoot(t *testing.T) {
	if Enabled() {
		t.Skip("running as root")
	}
	got := Sweep([]string{"127.0.0.1"}, 0, 64)
	if got != nil {
		t.Fatalf("expected nil without root, got %v", got)
	}
}
