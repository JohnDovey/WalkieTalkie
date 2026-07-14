package netinfo

import (
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Subnet describes a local interface CIDR and optional gateway.
type Subnet struct {
	Iface   string `json:"iface"`
	CIDR    string `json:"cidr"`
	IP      string `json:"ip"`
	Gateway string `json:"gateway,omitempty"`
}

// LocalSubnets returns up broadcast IPv4 prefixes (private preferred).
func LocalSubnets() ([]Subnet, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	gw := defaultGateway()
	var out []Subnet
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 || ifi.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipNet.IP.To4()
			if v4 == nil || !v4.IsPrivate() {
				continue
			}
			ones, bits := ipNet.Mask.Size()
			if bits != 32 || ones == 0 {
				continue
			}
			s := Subnet{
				Iface: ifi.Name,
				CIDR:  ipNet.String(),
				IP:    v4.String(),
			}
			if gw != "" && ipNet.Contains(net.ParseIP(gw)) {
				s.Gateway = gw
			}
			out = append(out, s)
		}
	}
	return out, nil
}

func defaultGateway() string {
	// macOS / BSD: route -n get default
	out, err := exec.Command("route", "-n", "get", "default").CombinedOutput()
	if err == nil {
		re := regexp.MustCompile(`gateway:\s*(\d+\.\d+\.\d+\.\d+)`)
		if m := re.FindStringSubmatch(string(out)); len(m) == 2 {
			return m[1]
		}
	}
	// Linux: ip route
	out, err = exec.Command("ip", "route", "show", "default").CombinedOutput()
	if err == nil {
		fields := strings.Fields(string(out))
		for i, f := range fields {
			if f == "via" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
	}
	return ""
}

// Hostname returns the OS hostname (best-effort).
func Hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// LocalHost describes this machine's LAN presence.
type LocalHost struct {
	Hostname string
	IPs      []string
	MACs     []string
	Subnets  []Subnet
}

// ThisMachine returns LAN IPs/MACs for the sniffer host itself.
func ThisMachine() LocalHost {
	lh := LocalHost{Hostname: Hostname()}
	subs, _ := LocalSubnets()
	lh.Subnets = subs
	ifaces, err := net.Interfaces()
	if err != nil {
		return lh
	}
	seenIP := map[string]bool{}
	seenMAC := map[string]bool{}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		mac := strings.ToLower(ifi.HardwareAddr.String())
		if mac != "" && mac != "00:00:00:00:00:00" && !seenMAC[mac] {
			seenMAC[mac] = true
			lh.MACs = append(lh.MACs, mac)
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipNet.IP.To4()
			if v4 == nil || v4.IsLoopback() || !v4.IsPrivate() {
				continue
			}
			s := v4.String()
			if seenIP[s] {
				continue
			}
			seenIP[s] = true
			lh.IPs = append(lh.IPs, s)
		}
	}
	return lh
}

// HostsInCIDR returns usable host IPs in cidr (skips network/broadcast; caps at 1024).
func HostsInCIDR(cidr string) ([]string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	var hosts []string
	ip := ipNet.IP.Mask(ipNet.Mask).To4()
	if ip == nil {
		return nil, nil
	}
	ones, bits := ipNet.Mask.Size()
	if bits != 32 {
		return nil, nil
	}
	count := 1 << (32 - ones)
	if count > 1024 {
		count = 1024
	}
	for i := 1; i < count-1 && len(hosts) < 1022; i++ {
		h := make(net.IP, 4)
		copy(h, ip)
		n := uint32(h[0])<<24 | uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
		n += uint32(i)
		h[0] = byte(n >> 24)
		h[1] = byte(n >> 16)
		h[2] = byte(n >> 8)
		h[3] = byte(n)
		hosts = append(hosts, h.String())
	}
	return hosts, nil
}
