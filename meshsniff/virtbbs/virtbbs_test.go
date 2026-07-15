package virtbbs

import (
	"testing"
)

func TestLooksLike(t *testing.T) {
	if !LooksLike([]int{80, 8081}) {
		t.Fatal("expected 8081 to look like VirtBBS")
	}
	if LooksLike([]int{80, 443}) {
		t.Fatal("plain web should not look like VirtBBS")
	}
}

func TestStripTelnetIAC(t *testing.T) {
	// IAC WILL ECHO (255 251 1) then text
	in := []byte{255, 251, 1, 'H', 'i'}
	got := stripTelnetIAC(in)
	if got != "Hi" {
		t.Fatalf("got %q", got)
	}
}
