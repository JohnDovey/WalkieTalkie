package arp

import (
	"os/exec"
	"regexp"
	"strings"
)

// Entry is one ARP/ND cache row.
type Entry struct {
	IP  string
	MAC string
}

var (
	reMacBSD   = regexp.MustCompile(`\(([0-9.]+)\)\s+at\s+([0-9a-fA-F:]+)`)
	reMacLinux = regexp.MustCompile(`^([0-9.]+)\s+\S+\s+([0-9a-fA-F:]+)`)
)

// Table reads the OS ARP cache (unprivileged).
func Table() ([]Entry, error) {
	out, err := exec.Command("arp", "-an").CombinedOutput()
	if err != nil {
		out, err = exec.Command("arp", "-a").CombinedOutput()
		if err != nil {
			return nil, err
		}
	}
	var entries []Entry
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ip, mac string
		if m := reMacBSD.FindStringSubmatch(line); len(m) == 3 {
			ip, mac = m[1], strings.ToLower(m[2])
		} else if m := reMacLinux.FindStringSubmatch(line); len(m) == 3 {
			ip, mac = m[1], strings.ToLower(m[2])
		}
		if ip == "" || mac == "" || mac == "(incomplete)" || strings.Contains(mac, "incomplete") {
			continue
		}
		if seen[ip] {
			continue
		}
		seen[ip] = true
		entries = append(entries, Entry{IP: ip, MAC: mac})
	}
	return entries, nil
}
