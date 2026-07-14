//go:build !darwin

package wifi

import "fmt"

// Associate is only implemented on macOS for v0.1.0.
func Associate(iface, ssid, password string) error {
	return fmt.Errorf("wifi: associate not supported on this OS yet")
}
