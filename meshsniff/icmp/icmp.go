package icmp

import (
	"fmt"
	"net"
	"os"
	"sync"
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

// Sweep pings hosts; skips when not elevated. When elevated, max is ignored
// (use 0 for full list). When not root, returns nil.
func Sweep(hosts []string, timeout time.Duration, max int) []string {
	if !Enabled() {
		return nil
	}
	if max <= 0 {
		return SweepAll(hosts, timeout, 64)
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

// SweepAll pings every host with a worker pool. No-op when not root.
func SweepAll(hosts []string, timeout time.Duration, workers int) []string {
	if !Enabled() || len(hosts) == 0 {
		return nil
	}
	if workers <= 0 {
		workers = 32
	}
	if timeout <= 0 {
		timeout = 150 * time.Millisecond
	}
	type result struct {
		ip    string
		alive bool
	}
	jobs := make(chan string, len(hosts))
	for _, h := range hosts {
		jobs <- h
	}
	close(jobs)
	results := make(chan result, len(hosts))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				results <- result{ip: ip, alive: Ping(ip, timeout)}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	var alive []string
	for r := range results {
		if r.alive {
			alive = append(alive, r.ip)
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
