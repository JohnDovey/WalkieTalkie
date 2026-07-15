package identify

import "testing"

func TestSkipHTTPIdentify(t *testing.T) {
	for _, p := range []int{22, 2323, 3232, 5900, 9998, 24554, 24555} {
		if !SkipHTTPIdentify(p) {
			t.Fatalf("expected skip for port %d", p)
		}
	}
	for _, p := range []int{80, 8080, 8081, 9091, 9095, 9096, 50713} {
		if SkipHTTPIdentify(p) {
			t.Fatalf("expected HTTP probe allowed for port %d", p)
		}
	}
}
