package graph

import (
	"strings"
	"sync"
	"time"
)

// Kind classifies a topology node.
type Kind string

const (
	KindNetwork    Kind = "network"
	KindSubnet     Kind = "subnet"
	KindRouter     Kind = "router"
	KindHost       Kind = "host"
	KindComputer   Kind = "computer" // desktop / laptop (general machine)
	KindWalkie     Kind = "walkie"
	KindBridge     Kind = "bridge"
	KindRemoteHint Kind = "remoteHint"
)

// Node is one vertex on the MeshSniff map.
type Node struct {
	ID               string            `json:"id"`
	Kind             Kind              `json:"kind"`
	Label            string            `json:"label"`
	Hostname         string            `json:"hostname,omitempty"`
	IPs              []string          `json:"ips,omitempty"`
	MACs             []string          `json:"macs,omitempty"`
	MeshID           string            `json:"meshId,omitempty"`
	Platform         string            `json:"platform,omitempty"`
	AppVersion       string            `json:"appVersion,omitempty"`
	Nickname         string            `json:"nickname,omitempty"`
	OpenPorts        []int             `json:"openPorts,omitempty"`
	Services         []Service         `json:"services,omitempty"`
	URLs             map[string]string `json:"urls,omitempty"`
	GPS              *GPS              `json:"gps,omitempty"`
	DiscoveryMethods []string          `json:"discoveryMethods,omitempty"`
	RemoteBaseID     string            `json:"remoteBaseId,omitempty"`
	RemoteBaseName   string            `json:"remoteBaseName,omitempty"`
	Subnet           string            `json:"subnet,omitempty"`
	ViaRouter        string            `json:"viaRouter,omitempty"` // gateway IP or router node id
	SameHostAs       string            `json:"sameHostAs,omitempty"` // canonical host id when colocated
	SSID             string            `json:"ssid,omitempty"`
	BSSID            string            `json:"bssid,omitempty"`
	Channel          string            `json:"channel,omitempty"`
	Security         string            `json:"security,omitempty"`
	LastSeen         time.Time         `json:"lastSeen"`
	Detail           map[string]any    `json:"detail,omitempty"`
}

// Service is a named port on a node.
type Service struct {
	Name string `json:"name"`
	Port int    `json:"port"`
	URL  string `json:"url,omitempty"`
}

// GPS is a compact location.
type GPS struct {
	Lat      float64   `json:"lat"`
	Lon      float64   `json:"lon"`
	Accuracy float64   `json:"accuracy,omitempty"`
	At       time.Time `json:"timestamp,omitempty"`
}

// Edge connects two nodes.
type Edge struct {
	ID     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"` // lan, gateway, bridge, remote
	Dashed bool   `json:"dashed,omitempty"`
}

// Snapshot is the full topology for the UI.
type Snapshot struct {
	Nodes      []Node         `json:"nodes"`
	Edges      []Edge         `json:"edges"`
	Status     Status         `json:"status"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

// Status is scan/seed health for the UI strip.
type Status struct {
	MeshBridgeOK     bool     `json:"meshBridgeOk"`
	BaseOK           bool     `json:"baseOk"`
	WalkieTalkieOK   bool     `json:"walkieTalkieOk"`
	WalkieSeeded     int      `json:"walkieSeeded"`
	WalkieBaseName   string   `json:"walkieBaseName,omitempty"`
	ICMPEnabled      bool     `json:"icmpEnabled"`
	CIDRs            []string `json:"cidrs"`
	LastScan         string   `json:"lastScan,omitempty"`
	Message          string   `json:"message,omitempty"`
}

// Store holds the live correlated graph.
type Store struct {
	mu     sync.RWMutex
	nodes  map[string]*Node
	edges  map[string]*Edge
	status Status
	seq    int64
}

// NewStore creates an empty graph.
func NewStore() *Store {
	return &Store{
		nodes: map[string]*Node{},
		edges: map[string]*Edge{},
	}
}

// SetStatus updates the status strip fields.
func (s *Store) SetStatus(st Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = st
	s.seq++
}

// Upsert merges n into the graph by MeshID, MAC, then ID.
func (s *Store) Upsert(n Node) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if n.LastSeen.IsZero() {
		n.LastSeen = now
	}
	id := s.findMergeIDLocked(n)
	if id == "" {
		id = n.ID
	}
	if id == "" {
		return ""
	}
	existing, ok := s.nodes[id]
	if !ok {
		cp := n
		cp.ID = id
		s.nodes[id] = &cp
		s.seq++
		return id
	}
	mergeNode(existing, n)
	s.seq++
	return id
}

func (s *Store) findMergeIDLocked(n Node) string {
	// Prefer a stable host:<ip> machine node when any IP matches.
	for _, ip := range n.IPs {
		if ip == "" {
			continue
		}
		canon := "host:" + ip
		if _, ok := s.nodes[canon]; ok {
			return canon
		}
		if strings.HasPrefix(n.ID, "host:") {
			return n.ID
		}
	}
	if n.MeshID != "" {
		for id, ex := range s.nodes {
			if ex.MeshID == n.MeshID {
				return id
			}
		}
	}
	for _, mac := range n.MACs {
		if mac == "" {
			continue
		}
		for id, ex := range s.nodes {
			for _, em := range ex.MACs {
				if em == mac {
					return preferredHostID(id, n.ID, ex, n)
				}
			}
		}
	}
	for _, ip := range n.IPs {
		if ip == "" {
			continue
		}
		for id, ex := range s.nodes {
			for _, ei := range ex.IPs {
				if ei == ip {
					return preferredHostID(id, n.ID, ex, n)
				}
			}
		}
	}
	if n.ID != "" {
		if _, ok := s.nodes[n.ID]; ok {
			return n.ID
		}
	}
	return n.ID
}

func preferredHostID(existingID, incomingID string, existing *Node, incoming Node) string {
	if strings.HasPrefix(existingID, "host:") {
		return existingID
	}
	if strings.HasPrefix(incomingID, "host:") {
		return incomingID
	}
	if existing.Kind == KindComputer || existing.Kind == KindHost || existing.Kind == KindRouter {
		return existingID
	}
	if incoming.Kind == KindComputer || incoming.Kind == KindHost || incoming.Kind == KindRouter {
		return incomingID
	}
	return existingID
}

func mergeNode(dst *Node, src Node) {
	// Machine kinds win over role-only walkie/bridge when colocated on one IP.
	dst.Kind = preferKind(dst.Kind, src.Kind)
	if src.Label != "" && (dst.Label == "" || dst.Kind == KindComputer || dst.Kind == KindHost || dst.Kind == KindRouter ||
		(src.Kind != KindWalkie && src.Kind != KindBridge && src.Kind != KindRemoteHint)) {
		// Keep richer computer/router labels; allow walkie names when that's the identity.
		if src.Kind == KindWalkie || src.Kind == KindBridge || src.Nickname != "" {
			if src.Nickname != "" {
				dst.Nickname = src.Nickname
			}
			if dst.Kind == KindWalkie || dst.Kind == KindBridge || dst.Label == "" || strings.HasPrefix(dst.Label, "host:") || looksLikeIP(dst.Label) {
				dst.Label = src.Label
			}
		} else if src.Hostname != "" || src.Kind == KindComputer || src.Kind == KindRouter {
			if dst.SSID != "" && src.SSID == "" && (dst.Kind == KindRouter || dst.Kind == KindNetwork) {
				// Keep Wi‑Fi SSID as the primary label.
			} else {
				dst.Label = src.Label
			}
		} else if dst.Label == "" || looksLikeIP(dst.Label) {
			dst.Label = src.Label
		}
	}
	if src.Hostname != "" {
		dst.Hostname = src.Hostname
		if looksLikeIP(dst.Label) || dst.Label == "" {
			dst.Label = src.Hostname
		}
	}
	if src.Nickname != "" {
		dst.Nickname = src.Nickname
	}
	if src.MeshID != "" {
		dst.MeshID = src.MeshID
	}
	if src.Platform != "" {
		dst.Platform = src.Platform
		if strings.HasPrefix(src.Platform, "desktop-") && (dst.Kind == KindHost || dst.Kind == "" || dst.Kind == KindWalkie) {
			// Desktop platforms win over generic walkie only when not a phone platform.
			if !isPhonePlatform(dst.Platform) {
				dst.Kind = KindComputer
			}
		}
	}
	// Phones/Wear must stay visually distinct from computers even if ARP/TCP
	// first classified the IP as a generic host/computer.
	if isPhonePlatform(dst.Platform) || isPhonePlatform(src.Platform) {
		dst.Kind = KindWalkie
		if src.Platform != "" {
			dst.Platform = src.Platform
		}
	}
	if src.AppVersion != "" {
		dst.AppVersion = src.AppVersion
	}
	dst.IPs = unionStr(dst.IPs, src.IPs)
	dst.MACs = unionStr(dst.MACs, src.MACs)
	dst.OpenPorts = unionInt(dst.OpenPorts, src.OpenPorts)
	dst.DiscoveryMethods = unionStr(dst.DiscoveryMethods, src.DiscoveryMethods)
	if len(src.Services) > 0 {
		dst.Services = mergeServices(dst.Services, src.Services)
	}
	if len(src.URLs) > 0 {
		if dst.URLs == nil {
			dst.URLs = map[string]string{}
		}
		for k, v := range src.URLs {
			dst.URLs[k] = v
		}
	}
	if src.GPS != nil {
		dst.GPS = src.GPS
	}
	if src.RemoteBaseID != "" {
		dst.RemoteBaseID = src.RemoteBaseID
		dst.RemoteBaseName = src.RemoteBaseName
	}
	if src.Subnet != "" {
		dst.Subnet = src.Subnet
	}
	if src.ViaRouter != "" {
		dst.ViaRouter = src.ViaRouter
	}
	if src.SameHostAs != "" {
		dst.SameHostAs = src.SameHostAs
	}
	if src.SSID != "" {
		dst.SSID = src.SSID
		// Prefer the human SSID on AP/router labels over "Router <ip>".
		if dst.Kind == KindRouter || src.Kind == KindRouter || dst.Kind == KindNetwork {
			dst.Label = src.SSID
		}
	}
	if src.BSSID != "" {
		dst.BSSID = src.BSSID
	}
	if src.Channel != "" {
		dst.Channel = src.Channel
	}
	if src.Security != "" {
		dst.Security = src.Security
	}
	if src.LastSeen.After(dst.LastSeen) {
		dst.LastSeen = src.LastSeen
	}
	if len(src.Detail) > 0 {
		if dst.Detail == nil {
			dst.Detail = map[string]any{}
		}
		for k, v := range src.Detail {
			dst.Detail[k] = v
		}
	}
}

func preferKind(a, b Kind) Kind {
	rank := func(k Kind) int {
		switch k {
		case KindRouter:
			return 6
		case KindWalkie: // phones / wear — prefer over generic computer
			return 5
		case KindComputer:
			return 4
		case KindBridge:
			return 3
		case KindHost:
			return 2
		case KindRemoteHint:
			return 1
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

func isPhonePlatform(p string) bool {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "android", "ios", "wear":
		return true
	default:
		return false
	}
}

func looksLikeIP(s string) bool {
	return netIPLike(s)
}

func netIPLike(s string) bool {
	if s == "" {
		return false
	}
	dots := 0
	for _, c := range s {
		if c == '.' {
			dots++
		} else if c < '0' || c > '9' {
			return false
		}
	}
	return dots == 3
}

// Delete removes a node and edges touching it.
func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nodes, id)
	for eid, e := range s.edges {
		if e.From == id || e.To == id {
			delete(s.edges, eid)
		}
	}
	s.seq++
}

// Nodes returns a snapshot of nodes (for reconciliation).
func (s *Store) Nodes() []Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Node, 0, len(s.nodes))
	for _, n := range s.nodes {
		out = append(out, *n)
	}
	return out
}

// Relink moves edges from oldID onto newID.
func (s *Store) Relink(oldID, newID string) {
	if oldID == "" || newID == "" || oldID == newID {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for eid, e := range s.edges {
		changed := false
		if e.From == oldID {
			e.From = newID
			changed = true
		}
		if e.To == oldID {
			e.To = newID
			changed = true
		}
		if changed {
			delete(s.edges, eid)
			nid := e.From + "->" + e.To + ":" + e.Kind
			e.ID = nid
			s.edges[nid] = e
		}
	}
	s.seq++
}

func mergeServices(a, b []Service) []Service {
	seen := map[string]bool{}
	var out []Service
	for _, s := range append(a, b...) {
		k := s.Name + ":" + itoa(s.Port)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func unionStr(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(a, b...) {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func unionInt(a, b []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, n := range append(a, b...) {
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// Link adds or updates an edge.
func (s *Store) Link(from, to, kind string, dashed bool) {
	if from == "" || to == "" || from == to {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := from + "->" + to + ":" + kind
	s.edges[id] = &Edge{ID: id, From: from, To: to, Kind: kind, Dashed: dashed}
	s.seq++
}

// Snapshot returns a copy of the graph.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := Snapshot{Status: s.status, UpdatedAt: time.Now()}
	for _, n := range s.nodes {
		cp := *n
		out.Nodes = append(out.Nodes, cp)
	}
	for _, e := range s.edges {
		cp := *e
		out.Edges = append(out.Edges, cp)
	}
	return out
}

// Seq returns a monotonic version counter for SSE.
func (s *Store) Seq() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.seq
}
