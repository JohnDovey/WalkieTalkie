package relay

import (
	"fmt"
	"sync"
)

// HostBridge implements media.RelayDialer for the Base Station that also
// runs the SFU Hub. DialViaRelay marks the peer as SFU-routed; audio flows
// when the peer POSTs to server/relay and via Hub.InjectLocal from Broadcast.
type HostBridge struct {
	Hub    *Hub
	SelfID string

	mu     sync.Mutex
	marked map[string]struct{}

	// Mark is called when a peer is routed via SFU (typically MeshManager.MarkRelayPeer).
	Mark func(peerID string)
}

// DialViaRelay expects peerID to join the Hub and marks them connected via relay.
func (b *HostBridge) DialViaRelay(peerID string) error {
	if b.Hub == nil {
		return fmt.Errorf("relay host: no hub")
	}
	b.mu.Lock()
	if b.marked == nil {
		b.marked = make(map[string]struct{})
	}
	b.marked[peerID] = struct{}{}
	b.mu.Unlock()
	if b.Mark != nil {
		b.Mark(peerID)
	}
	return nil
}

// ConnectedViaRelay reports whether peerID was marked through DialViaRelay
// and is (or was) expected on the Hub.
func (b *HostBridge) ConnectedViaRelay(peerID string) bool {
	b.mu.Lock()
	_, marked := b.marked[peerID]
	b.mu.Unlock()
	if !marked || b.Hub == nil {
		return false
	}
	return b.Hub.Has(peerID)
}

// InjectTo forwards one local Opus frame to a single Hub participant.
func (b *HostBridge) InjectTo(toID string, frame []byte) {
	if b.Hub == nil {
		return
	}
	b.Hub.InjectTo(b.SelfID, toID, frame)
}

// ClearRoute clears the Base Station's private unicast Hub route (if any).
func (b *HostBridge) ClearRoute() {
	if b.Hub == nil {
		return
	}
	b.Hub.ClearRoute(b.SelfID)
}

// SetRoom places the Base Station local publisher in a named Hub room.
func (b *HostBridge) SetRoom(roomID string) {
	if b.Hub == nil {
		return
	}
	b.Hub.SetRoom(b.SelfID, roomID)
}

// ClearRoom returns the Base Station publisher to the group mesh room.
func (b *HostBridge) ClearRoom() {
	b.SetRoom("")
}
