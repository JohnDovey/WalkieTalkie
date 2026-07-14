package media

import "sync/atomic"

// StartTalking begins pulling Opus frames from the local AudioSource and
// broadcasting them to every connected peer — half-duplex, PTT-style: no
// frames are sent while not talking. Safe to call repeatedly; a second call
// while already talking is a no-op.
//
// Clears any prior scoped Talk targets so this path is always mesh-wide.
//
// Platform wiring: Android/desktop call this on PTT-button-down; iOS calls
// it from PTChannelManager's transmit-start delegate callback instead of a
// raw button (see the plan's iOS phase).
func (mm *MeshManager) StartTalking() {
	if atomic.LoadInt32(&mm.talking) == 1 {
		return
	}
	mm.clearRelayRoute()
	mm.mu.Lock()
	mm.talkTargets = nil
	mm.talkRoute = false
	mm.mu.Unlock()
	mm.startTalkLoop()
}

// StartTalkingTo is like StartTalking but unicast frames to one peer
// (private-channel 1:1 live Talk). Uses a direct PeerConnection when available,
// otherwise SFU Hub unicast when LiveTalkAvailable. Callers should check
// LiveTalkAvailable first; otherwise keep using clip upload.
func (mm *MeshManager) StartTalkingTo(peerID string) {
	if peerID == "" {
		mm.StartTalking()
		return
	}
	if atomic.LoadInt32(&mm.talking) == 1 {
		return
	}
	mm.clearRelayRoute()
	mm.mu.Lock()
	mm.talkTargets = []string{peerID}
	mm.talkRoute = true
	direct := false
	if _, ok := mm.peers[peerID]; ok {
		direct = true
	}
	relay := false
	if _, ok := mm.relayPeers[peerID]; ok {
		relay = true
	}
	mm.mu.Unlock()
	if !direct && relay && mm.OnRelaySetRoute != nil {
		_ = mm.OnRelaySetRoute(peerID)
	}
	mm.startTalkLoop()
}

// StartTalkingToPeers scopes Talk to the given peers without Hub SetRoute.
// Direct peers get SendTo; any SFU-reachable targets share one
// OnRelayBroadcast (Hub room isolation applies when the caller has SetRoom).
func (mm *MeshManager) StartTalkingToPeers(peerIDs []string) {
	clean := make([]string, 0, len(peerIDs))
	seen := make(map[string]struct{}, len(peerIDs))
	for _, id := range peerIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		mm.StartTalking()
		return
	}
	if atomic.LoadInt32(&mm.talking) == 1 {
		return
	}
	mm.clearRelayRoute()
	mm.mu.Lock()
	mm.talkTargets = clean
	mm.talkRoute = false
	mm.mu.Unlock()
	mm.startTalkLoop()
}

func (mm *MeshManager) startTalkLoop() {
	if mm.source == nil {
		return
	}
	if !atomic.CompareAndSwapInt32(&mm.talking, 0, 1) {
		return
	}
	stop := make(chan struct{})
	mm.mu.Lock()
	mm.stopTalk = stop
	mm.mu.Unlock()
	if mm.OnTalkStarted != nil {
		mm.OnTalkStarted()
	}
	go mm.talkLoop(stop)
}

// StopTalking stops the talk loop started by StartTalking / StartTalkingTo /
// StartTalkingToPeers. Safe to call even if not currently talking.
func (mm *MeshManager) StopTalking() {
	mm.clearRelayRoute()
	if !atomic.CompareAndSwapInt32(&mm.talking, 1, 0) {
		mm.mu.Lock()
		mm.talkTargets = nil
		mm.talkRoute = false
		mm.mu.Unlock()
		return
	}
	mm.mu.Lock()
	stop := mm.stopTalk
	mm.talkTargets = nil
	mm.talkRoute = false
	mm.mu.Unlock()
	if stop != nil {
		close(stop)
	}
}

func (mm *MeshManager) clearRelayRoute() {
	if mm.OnRelayClearRoute != nil {
		mm.OnRelayClearRoute()
	}
}

func (mm *MeshManager) talkLoop(stop chan struct{}) {
	// Release the mic between talk sessions rather than holding it for the
	// app's whole lifetime — see AudioSource.Stop's doc comment. Called from
	// this same goroutine, after it's done reading, rather than directly
	// from StopTalking, to avoid stopping the capture device from a
	// different goroutine while a read might still be in flight.
	defer mm.source.Stop()
	for {
		select {
		case <-stop:
			return
		default:
		}
		frame, err := mm.source.ReadOpusFrame()
		if err != nil {
			return
		}
		mm.mu.Lock()
		targets := append([]string(nil), mm.talkTargets...)
		useRoute := mm.talkRoute
		mm.mu.Unlock()
		if len(targets) == 0 {
			mm.Broadcast(frame)
			continue
		}
		if useRoute && len(targets) == 1 {
			target := targets[0]
			if mm.SendTo(target, frame) {
				continue
			}
			if mm.OnRelayUnicast != nil && mm.RelayConnected(target) {
				mm.OnRelayUnicast(target, frame)
				if mm.OnOpusFrameSent != nil {
					mm.OnOpusFrameSent(len(frame), 1)
				}
			}
			continue
		}
		// Room / multi-peer path: SendTo each direct peer; one Hub Broadcast
		// for anyone still needing SFU (Hub room scopes fan-out).
		needRelay := false
		sent := 0
		for _, id := range targets {
			if mm.SendTo(id, frame) {
				sent++
				continue
			}
			if mm.RelayConnected(id) {
				needRelay = true
			}
		}
		if needRelay && mm.OnRelayBroadcast != nil {
			mm.OnRelayBroadcast(frame)
			sent++
		}
		if mm.OnOpusFrameSent != nil && sent > 0 && needRelay {
			// SendTo already counted direct peers; count one for the SFU hop.
			mm.OnOpusFrameSent(len(frame), 1)
		}
	}
}
