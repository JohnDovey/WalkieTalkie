package netinfo

import (
	"testing"
)

func TestWifiDarwinSmoke(t *testing.T) {
	w, ok := Wifi()
	if !ok {
		t.Log("wifi not associated (ok on non-wifi hosts)")
		return
	}
	if w.SSID == "" {
		t.Fatal("expected SSID when Wifi() returns true")
	}
	t.Logf("ssid=%q iface=%q channel=%q security=%q", w.SSID, w.Iface, w.Channel, w.Security)
}
