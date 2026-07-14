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
