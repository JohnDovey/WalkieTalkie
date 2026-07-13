package media

import "sync/atomic"

// StartTalking begins pulling Opus frames from the local AudioSource and
// broadcasting them to every connected peer — half-duplex, PTT-style: no
// frames are sent while not talking. Safe to call repeatedly; a second call
// while already talking is a no-op.
//
// Platform wiring: Android/desktop call this on PTT-button-down; iOS calls
// it from PTChannelManager's transmit-start delegate callback instead of a
// raw button (see the plan's iOS phase).
func (mm *MeshManager) StartTalking() {
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
	go mm.talkLoop(stop)
}

// StopTalking stops the talk loop started by StartTalking. Safe to call
// even if not currently talking.
func (mm *MeshManager) StopTalking() {
	if !atomic.CompareAndSwapInt32(&mm.talking, 1, 0) {
		return
	}
	mm.mu.Lock()
	stop := mm.stopTalk
	mm.mu.Unlock()
	if stop != nil {
		close(stop)
	}
}

func (mm *MeshManager) talkLoop(stop chan struct{}) {
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
		mm.Broadcast(frame)
	}
}
