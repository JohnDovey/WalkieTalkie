package discovery

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/grandcat/zeroconf"
)

// BasePeer is a Base Station found on a secondary interface.
type BasePeer struct {
	ID      string
	Name    string
	Host    string
	APIPort int
	Iface   string
}

// BrowseBases watches _walkietalkie._tcp and reports peers with api≠0.
func BrowseBases(ctx context.Context, preferIface string, onBase func(BasePeer)) error {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return err
	}
	entries := make(chan *zeroconf.ServiceEntry, 16)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-entries:
				if !ok {
					return
				}
				p := parseBase(e, preferIface)
				if p.APIPort == 0 || p.Host == "" {
					continue
				}
				onBase(p)
			}
		}
	}()
	return resolver.Browse(ctx, "_walkietalkie._tcp", "local.", entries)
}

func parseBase(e *zeroconf.ServiceEntry, preferIface string) BasePeer {
	p := BasePeer{Iface: preferIface}
	txt := map[string]string{}
	for _, t := range e.Text {
		if i := indexByte(t, '='); i > 0 {
			txt[t[:i]] = t[i+1:]
		}
	}
	p.ID = txt["id"]
	p.Name = txt["name"]
	if api := txt["api"]; api != "" {
		p.APIPort, _ = strconv.Atoi(api)
	}
	var ifaceIP net.IP
	if preferIface != "" {
		ifaceIP = ifaceIPv4(preferIface)
	}
	for _, ip := range e.AddrIPv4 {
		if ifaceIP != nil && !sameL3Subnet(ifaceIP, ip) {
			continue
		}
		p.Host = ip.String()
		break
	}
	if p.Host == "" && len(e.AddrIPv4) > 0 {
		p.Host = e.AddrIPv4[0].String()
	}
	return p
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func ifaceIPv4(name string) net.IP {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.To4() == nil {
			continue
		}
		return ipnet.IP.To4()
	}
	return nil
}

func sameL3Subnet(a, b net.IP) bool {
	a4, b4 := a.To4(), b.To4()
	if a4 == nil || b4 == nil {
		return false
	}
	return a4[0] == b4[0] && a4[1] == b4[1] && a4[2] == b4[2]
}

// WithTimeout wraps parent with a deadline.
func WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}
