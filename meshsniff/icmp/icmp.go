package icmp

import (
	"fmt"
	"net"
	"os"
	"time"
)

// Enabled is true when running as root (raw ICMP available on most Unixes).
func Enabled() bool {
	return os.Geteuid() == 0
}

// Ping sends a crude ICMP echo. Non-root returns false.
func Ping(ip string, timeout time.Duration) bool {
	if !Enabled() {
		return false
	}
	conn, err := net.DialTimeout("ip4:icmp", ip, timeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	msg := []byte{
		8, 0, 0, 0,
		0, 1, 0, 1,
		'W', 'T', 'S', 'N', 'I', 'F', 'F', '!',
	}
	sum := checksum(msg)
	msg[2] = byte(sum >> 8)
	msg[3] = byte(sum)
	if _, err := conn.Write(msg); err != nil {
		return false
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	return err == nil && n > 0
}

func checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// Sweep pings hosts; skips when not elevated.
func Sweep(hosts []string, timeout time.Duration, max int) []string {
	if !Enabled() {
		return nil
	}
	if max <= 0 {
		max = 64
	}
	var alive []string
	for i, h := range hosts {
		if i >= max {
			break
		}
		if Ping(h, timeout) {
			alive = append(alive, h)
		}
	}
	return alive
}

// StatusMessage explains ICMP availability.
func StatusMessage() string {
	if Enabled() {
		return "ICMP enabled (root)"
	}
	return fmt.Sprintf("ICMP skipped (uid=%d; run as root for ping sweep)", os.Geteuid())
}
