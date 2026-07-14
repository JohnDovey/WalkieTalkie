package tcpprobe

import (
	"net"
	"strconv"
	"sync"
	"time"
)

// OpenPorts dials host on each port concurrently; returns those that accept.
func OpenPorts(host string, ports []int, timeout time.Duration) []int {
	if timeout <= 0 {
		timeout = 400 * time.Millisecond
	}
	var (
		mu   sync.Mutex
		open []int
		wg   sync.WaitGroup
	)
	sem := make(chan struct{}, 32)
	for _, p := range ports {
		p := p
		if p <= 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			addr := net.JoinHostPort(host, strconv.Itoa(p))
			c, err := net.DialTimeout("tcp", addr, timeout)
			if err != nil {
				return
			}
			_ = c.Close()
			mu.Lock()
			open = append(open, p)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return open
}
