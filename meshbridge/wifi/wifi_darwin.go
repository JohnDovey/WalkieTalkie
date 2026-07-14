//go:build darwin

package wifi

import (
	"fmt"
	"os/exec"
	"strings"
)

// Associate joins iface to ssid with password via networksetup.
// The hardware interface must already exist (built-in or USB adapter).
func Associate(iface, ssid, password string) error {
	if iface == "" || ssid == "" {
		return fmt.Errorf("wifi: interface and ssid required")
	}
	// Prefer airport network set on the service matching the device.
	svc, err := serviceForDevice(iface)
	if err != nil {
		return err
	}
	args := []string{"-setairportnetwork", svc, ssid}
	if password != "" {
		args = append(args, password)
	}
	out, err := exec.Command("networksetup", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wifi: associate %s→%s: %w (%s)", iface, ssid, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func serviceForDevice(device string) (string, error) {
	out, err := exec.Command("networksetup", "-listallhardwareports").CombinedOutput()
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(out), "\n")
	var service string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Hardware Port:") {
			service = strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port:"))
		}
		if strings.HasPrefix(line, "Device:") {
			dev := strings.TrimSpace(strings.TrimPrefix(line, "Device:"))
			if dev == device && service != "" {
				return service, nil
			}
		}
	}
	return "", fmt.Errorf("wifi: no networkservice for device %s", device)
}
