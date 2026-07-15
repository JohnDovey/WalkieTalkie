// Package portmem persists previously-open TCP ports per host so MeshSniff can
// re-check them after restart without a full 1–65535 sweep.
package portmem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Store is a thread-safe known-ports cache on disk.
type Store struct {
	path string
	mu   sync.Mutex
	data fileData
}

type fileData struct {
	Hosts map[string]hostEntry `json:"hosts"`
}

type hostEntry struct {
	Ports     []int     `json:"ports"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Open loads known-ports.json from dir (creating an empty store if missing).
func Open(dir string) (*Store, error) {
	s := &Store{
		path: filepath.Join(dir, "known-ports.json"),
		data: fileData{Hosts: map[string]hostEntry{}},
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, err
	}
	if s.data.Hosts == nil {
		s.data.Hosts = map[string]hostEntry{}
	}
	return s, nil
}

// PortsFor returns a copy of remembered ports for host.
func (s *Store) PortsFor(host string) []int {
	if s == nil || host == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ent, ok := s.data.Hosts[host]
	if !ok || len(ent.Ports) == 0 {
		return nil
	}
	out := make([]int, len(ent.Ports))
	copy(out, ent.Ports)
	return out
}

// Remember adds ports to the host's remembered set and saves.
func (s *Store) Remember(host string, ports []int) {
	if s == nil || host == "" || len(ports) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ent := s.data.Hosts[host]
	ent.Ports = unionSorted(ent.Ports, ports)
	ent.UpdatedAt = time.Now()
	s.data.Hosts[host] = ent
	_ = s.saveLocked()
}

// SetExact replaces the host's remembered ports with ports (sorted unique) and saves.
// Empty ports clears the entry.
func (s *Store) SetExact(host string, ports []int) {
	if s == nil || host == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ports = unionSorted(nil, ports)
	if len(ports) == 0 {
		delete(s.data.Hosts, host)
	} else {
		s.data.Hosts[host] = hostEntry{Ports: ports, UpdatedAt: time.Now()}
	}
	_ = s.saveLocked()
}

// PruneClosed removes from memory any port in probed that is not in stillOpen.
// Ports not in probed are left alone (they were not checked this pass).
func (s *Store) PruneClosed(host string, probed, stillOpen []int) {
	if s == nil || host == "" || len(probed) == 0 {
		return
	}
	openSet := map[int]bool{}
	for _, p := range stillOpen {
		openSet[p] = true
	}
	probedSet := map[int]bool{}
	for _, p := range probed {
		probedSet[p] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ent, ok := s.data.Hosts[host]
	if !ok {
		return
	}
	var keep []int
	changed := false
	for _, p := range ent.Ports {
		if probedSet[p] && !openSet[p] {
			changed = true
			continue
		}
		keep = append(keep, p)
	}
	if !changed {
		return
	}
	if len(keep) == 0 {
		delete(s.data.Hosts, host)
	} else {
		ent.Ports = keep
		ent.UpdatedAt = time.Now()
		s.data.Hosts[host] = ent
	}
	_ = s.saveLocked()
}

// Hosts returns all remembered host IPs.
func (s *Store) Hosts() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.data.Hosts))
	for h := range s.data.Hosts {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func unionSorted(a, b []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, p := range append(a, b...) {
		if p <= 0 || p > 65535 || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}
