package tcpprobe

import (
	"context"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
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

// SweepProgress is periodic full-scan progress.
type SweepProgress struct {
	Checked int
	Total   int
	Open    int
}

// Sweep probes TCP ports [from,to] inclusive on host with high concurrency.
// onOpen is called (possibly concurrently) for each accepting port.
// onProgress is called periodically (not for every port).
func Sweep(ctx context.Context, host string, from, to int, timeout time.Duration, workers int, onOpen func(port int), onProgress func(SweepProgress)) error {
	if from < 1 {
		from = 1
	}
	if to > 65535 {
		to = 65535
	}
	if to < from {
		return nil
	}
	if timeout <= 0 {
		timeout = 150 * time.Millisecond
	}
	if workers <= 0 {
		workers = 256
	}
	total := to - from + 1
	var checked atomic.Int64
	var openN atomic.Int64

	jobs := make(chan int, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				if ctx.Err() != nil {
					continue
				}
				addr := net.JoinHostPort(host, strconv.Itoa(p))
				d := net.Dialer{Timeout: timeout}
				c, err := d.DialContext(ctx, "tcp", addr)
				n := int(checked.Add(1))
				if err == nil {
					_ = c.Close()
					openN.Add(1)
					if onOpen != nil {
						onOpen(p)
					}
				}
				if onProgress != nil && (n%500 == 0 || n == total) {
					onProgress(SweepProgress{Checked: n, Total: total, Open: int(openN.Load())})
				}
			}
		}()
	}

loop:
	for p := from; p <= to; p++ {
		select {
		case <-ctx.Done():
			break loop
		case jobs <- p:
		}
	}
	close(jobs)
	wg.Wait()
	if onProgress != nil {
		onProgress(SweepProgress{
			Checked: int(checked.Load()),
			Total:   total,
			Open:    int(openN.Load()),
		})
	}
	return ctx.Err()
}
