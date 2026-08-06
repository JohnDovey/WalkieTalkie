package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// ScanLANBases probes private /24 subnets of up interfaces for WalkieTalkie
// Base Stations (GET /api/about on common API ports). Used as a Windows-friendly
// fallback when mDNS browse is empty or flaky.
func ScanLANBases(ctx context.Context, ports []int, onBase func(BasePeer)) error {
	if len(ports) == 0 {
		ports = []int{9091}
	}
	subnets := localPrivateSubnets()
	if len(subnets) == 0 {
		return fmt.Errorf("no private LAN subnets to scan")
	}

	client := &http.Client{Timeout: 400 * time.Millisecond}
	sem := make(chan struct{}, 64)
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]bool{}

	report := func(p BasePeer) {
		mu.Lock()
		defer mu.Unlock()
		key := p.ID
		if key == "" {
			key = fmt.Sprintf("%s:%d", p.Host, p.APIPort)
		}
		if seen[key] {
			return
		}
		seen[key] = true
		onBase(p)
	}

	for _, sn := range subnets {
		for _, host := range hostsIn24(sn) {
			for _, port := range ports {
				if ctx.Err() != nil {
					break
				}
				h, p := host, port
				wg.Add(1)
				sem <- struct{}{}
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					if ctx.Err() != nil {
						return
					}
					peer, ok := probeAbout(ctx, client, h, p)
					if ok {
						report(peer)
					}
				}()
			}
		}
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		// let in-flight probes finish briefly
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		return ctx.Err()
	case <-done:
		return nil
	}
}

func probeAbout(ctx context.Context, client *http.Client, host string, port int) (BasePeer, bool) {
	url := fmt.Sprintf("http://%s:%d/api/about", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return BasePeer{}, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return BasePeer{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return BasePeer{}, false
	}
	var about struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&about); err != nil {
		return BasePeer{}, false
	}
	// Base Stations use desktop-* platforms; phones never expose /api/about the same way.
	if about.ID == "" {
		return BasePeer{}, false
	}
	return BasePeer{
		ID:      about.ID,
		Name:    about.Name,
		Host:    host,
		APIPort: port,
	}, true
}

func localPrivateSubnets() []net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.IP
	seen := map[string]bool{}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			// skip link-local 169.254/16 and the common Windows ICS 192.168.137.x host-only
			if ip[0] == 169 {
				continue
			}
			// /24 base
			base := net.IPv4(ip[0], ip[1], ip[2], 0).To4()
			key := base.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, base)
		}
	}
	return out
}

// hostsIn24 yields .1–.254 for a /24 network base address.
func hostsIn24(base net.IP) []string {
	b := base.To4()
	if b == nil {
		return nil
	}
	out := make([]string, 0, 254)
	for i := 1; i <= 254; i++ {
		out = append(out, fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], i))
	}
	return out
}
