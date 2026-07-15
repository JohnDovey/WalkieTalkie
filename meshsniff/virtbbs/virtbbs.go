// Package virtbbs probes VirtBBS hosts discovered on the LAN.
package virtbbs

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JohnDovey/WalkieTalkie/meshsniff/graph"
)

// Info is enriched VirtBBS identity for a host.
type Info struct {
	Name      string
	Version   string
	Sysop     string
	Software  string
	Platform  string
	Addresses []string
	Networks  []Network
	Services  []graph.Service
	URLs      map[string]string
	Detail    map[string]any
	Methods   []string
}

// Network is one Fido-compatible network advertised by VirtBBS.
type Network struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	BinkpPort int    `json:"binkpPort"`
	Role      string `json:"role"`
	Uplink    string `json:"uplink,omitempty"`
}

// LooksLike reports whether open ports suggest a VirtBBS host.
func LooksLike(open []int) bool {
	for _, p := range open {
		switch p {
		case 2323, 3232, 8081, 9998, 24554, 24555:
			return true
		}
	}
	return false
}

// Probe gathers VirtBBS metadata from open ports on host.
func Probe(host string, open []int, timeout time.Duration) (Info, bool) {
	if timeout <= 0 {
		timeout = 1200 * time.Millisecond
	}
	openSet := map[int]bool{}
	for _, p := range open {
		openSet[p] = true
	}
	var info Info
	info.Software = "VirtBBS"
	info.Platform = "virtbbs"
	info.Detail = map[string]any{"software": "VirtBBS"}
	info.URLs = map[string]string{}

	ok := false
	if openSet[8081] {
		if sniffOK := probeHTTPSniff(host, 8081, timeout, &info); sniffOK {
			ok = true
		} else if probeManifest(host, 8081, timeout, &info) {
			ok = true
		}
	}
	for _, p := range []int{24554, 24555} {
		if !openSet[p] {
			continue
		}
		if probeBinkP(host, p, timeout, &info) {
			ok = true
		}
	}
	if info.Name == "" && openSet[2323] {
		if probeTelnet(host, 2323, timeout, &info) {
			ok = true
		}
	}
	if !ok && !LooksLike(open) {
		return Info{}, false
	}
	if !ok {
		// Ports look like VirtBBS but probes failed — still mark software lightly.
		info.Methods = append(info.Methods, "ports")
		ok = true
	}
	if info.Name != "" {
		info.Detail["bbsName"] = info.Name
	}
	if info.Sysop != "" {
		info.Detail["sysop"] = info.Sysop
	}
	if len(info.Addresses) > 0 {
		info.Detail["fidoAddresses"] = info.Addresses
	}
	if len(info.Networks) > 0 {
		info.Detail["networks"] = info.Networks
	}
	return info, ok
}

type sniffPayload struct {
	MeshID     string            `json:"meshId"`
	Name       string            `json:"name"`
	Platform   string            `json:"platform"`
	AppVersion string            `json:"appVersion"`
	Sysop      string            `json:"sysop"`
	Software   string            `json:"software"`
	URLs       map[string]string `json:"urls"`
	Services   []struct {
		Name string `json:"name"`
		Port int    `json:"port"`
		URL  string `json:"url"`
	} `json:"services"`
	Networks []Network `json:"networks"`
}

func httpClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: timeout,
			ForceAttemptHTTP2:     false,
		},
	}
}

func probeHTTPSniff(host string, port int, timeout time.Duration, info *Info) bool {
	client := httpClient(timeout)
	for _, path := range []string{"/sniff", "/api/sniff"} {
		url := fmt.Sprintf("http://%s:%d%s", host, port, path)
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode >= 300 {
			continue
		}
		var p sniffPayload
		if err := json.Unmarshal(body, &p); err != nil {
			continue
		}
		if !strings.EqualFold(p.Platform, "virtbbs") && p.Software != "VirtBBS" && p.Name == "" {
			continue
		}
		mergeSniff(info, p)
		info.Methods = append(info.Methods, "sniff")
		return true
	}
	return false
}

func mergeSniff(info *Info, p sniffPayload) {
	if p.Name != "" {
		info.Name = p.Name
	}
	if p.AppVersion != "" {
		info.Version = p.AppVersion
	}
	if p.Sysop != "" {
		info.Sysop = p.Sysop
	}
	if p.Software != "" {
		info.Software = p.Software
	}
	if p.Platform != "" {
		info.Platform = p.Platform
	}
	for k, v := range p.URLs {
		info.URLs[k] = v
	}
	for _, s := range p.Services {
		info.Services = append(info.Services, graph.Service{Name: s.Name, Port: s.Port, URL: s.URL})
	}
	for _, n := range p.Networks {
		info.Networks = append(info.Networks, n)
		if n.Address != "" {
			info.Addresses = unionStr(info.Addresses, []string{n.Address})
		}
	}
}

func probeManifest(host string, port int, timeout time.Duration, info *Info) bool {
	client := httpClient(timeout)
	url := fmt.Sprintf("http://%s:%d/manifest.webmanifest", host, port)
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return false
	}
	var m struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<10)).Decode(&m); err != nil {
		return false
	}
	if !strings.Contains(m.Description, "VirtBBS") && m.Name == "" {
		return false
	}
	if m.Name != "" {
		info.Name = m.Name
	}
	info.URLs["web"] = fmt.Sprintf("http://%s:%d/", host, port)
	info.Services = append(info.Services, graph.Service{
		Name: "VirtBBS Web", Port: port, URL: info.URLs["web"],
	})
	info.Methods = append(info.Methods, "manifest")
	return strings.Contains(m.Description, "VirtBBS") || m.Name != ""
}

const (
	bpMNUL = 0
	bpMADR = 1
)

func probeBinkP(host string, port int, timeout time.Duration, info *Info) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	foundVirt := false
	var addrs []string
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		var hdr uint16
		if err := binary.Read(conn, binary.BigEndian, &hdr); err != nil {
			break
		}
		isCmd := hdr&0x8000 != 0
		length := int(hdr & 0x7FFF)
		if length <= 0 || length > 4096 {
			break
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(conn, payload); err != nil {
			break
		}
		if !isCmd || len(payload) == 0 {
			continue
		}
		cmd := payload[0]
		arg := string(payload[1:])
		switch cmd {
		case bpMNUL:
			if strings.HasPrefix(arg, "SYS ") && strings.Contains(arg, "VirtBBS") {
				foundVirt = true
			}
			if strings.HasPrefix(arg, "ZYZ ") {
				a := strings.TrimSpace(strings.TrimPrefix(arg, "ZYZ "))
				if a != "" {
					addrs = append(addrs, a)
				}
			}
		case bpMADR:
			for _, a := range strings.Fields(arg) {
				addrs = append(addrs, a)
			}
			// Got addresses; enough for ID.
			if foundVirt || len(addrs) > 0 {
				foundVirt = true
				goto done
			}
		}
	}
done:
	if !foundVirt && len(addrs) == 0 {
		return false
	}
	info.Addresses = unionStr(info.Addresses, addrs)
	info.Services = append(info.Services, graph.Service{
		Name: "VirtBBS BinkP", Port: port,
	})
	info.Methods = append(info.Methods, fmt.Sprintf("binkp:%d", port))
	return true
}

var (
	poweredRE = regexp.MustCompile(`(?i)Powered by VirtBBS`)
	boxNameRE = regexp.MustCompile(`║\s*([^\s║][^║]*?)\s*║`)
)

func probeTelnet(host string, port int, timeout time.Duration, info *Info) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	r := bufio.NewReader(conn)
	var raw []byte
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && len(raw) < 8192 {
		_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		b, err := r.ReadByte()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if len(raw) > 0 {
					break
				}
				continue
			}
			break
		}
		raw = append(raw, b)
		if len(raw) > 40 && strings.Contains(string(raw), "Enter your name") {
			break
		}
	}
	text := stripTelnetIAC(raw)
	if !poweredRE.MatchString(text) {
		return false
	}
	if info.Name == "" {
		for _, m := range boxNameRE.FindAllStringSubmatch(text, -1) {
			name := strings.TrimSpace(m[1])
			if name == "" || poweredRE.MatchString(name) {
				continue
			}
			// Skip pure border leftovers
			if strings.Contains(name, "═") {
				continue
			}
			info.Name = name
			break
		}
	}
	info.Services = append(info.Services, graph.Service{Name: "VirtBBS Telnet", Port: port})
	info.Methods = append(info.Methods, "telnet")
	return true
}

func stripTelnetIAC(in []byte) string {
	var out []byte
	for i := 0; i < len(in); i++ {
		if in[i] != 255 { // IAC
			out = append(out, in[i])
			continue
		}
		if i+1 >= len(in) {
			break
		}
		cmd := in[i+1]
		i++
		switch cmd {
		case 255: // escaped 0xFF
			out = append(out, 255)
		case 250: // SB ... IAC SE
			for i+1 < len(in) {
				i++
				if in[i] == 255 && i+1 < len(in) && in[i+1] == 240 {
					i++
					break
				}
			}
		case 251, 252, 253, 254: // WILL/WONT/DO/DONT + option
			if i+1 < len(in) {
				i++
			}
		}
	}
	// Drop ANSI CSI
	s := string(out)
	s = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`).ReplaceAllString(s, "")
	return s
}

func unionStr(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(a, b...) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
