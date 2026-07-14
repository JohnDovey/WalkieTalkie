package media

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/signaling"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const opusFrameDuration = 20 * time.Millisecond

// connectTimeout bounds how long a direct connection attempt is allowed to
// take before falling back to DialViaRelay (when relay is enabled).
const connectTimeout = 3 * time.Second

// RelayDialer bridges peers via the Base Station SFU when direct P2P is
// skipped (force-relay) or fails. Avoids context.Context so gomobile can bind it.
type RelayDialer interface {
	DialViaRelay(peerID string) error
}

type peerConn struct {
	id    string
	pc    *webrtc.PeerConnection
	track *webrtc.TrackLocalStaticSample
}

// MeshManager holds one pion PeerConnection per reachable peer (full mesh)
// and the local signaling endpoint peers dial into. One MeshManager exists
// per local device/session.
type MeshManager struct {
	selfID string
	api    *webrtc.API
	source AudioSource
	sink   AudioSink
	relay  RelayDialer // optional

	sig *signaling.Server

	mu         sync.Mutex
	peers      map[string]*peerConn
	relayPeers map[string]struct{} // peers reached only via SFU

	talking    int32
	stopTalk   chan struct{}
	talkTarget string // empty = Broadcast (group PTT); set = unicast to that peer ID

	relayThreshold int  // 0 = disabled
	relayEnabled   bool // when false, DialViaRelay is never used

	// OnRelayBroadcast, if set, receives every Broadcast frame so a local SFU
	// host can InjectLocal into the Hub.
	OnRelayBroadcast func(frame []byte)

	OnConnectionStateChange func(peerID string, state webrtc.PeerConnectionState)

	// OnRelayThresholdExceeded is called when force-relay would apply but no
	// RelayDialer is configured — connection proceeds direct with a warning.
	OnRelayThresholdExceeded func(peerCount, threshold int)

	OnOpusFrameSent     func(frameBytes, peerCount int)
	OnOpusFrameReceived func(peerID string, frameBytes int)
	OnTalkStarted       func()
}

// SetRelayThreshold configures the peer count above which new connections
// prefer the relay when RelayEnabled. 0 disables the check.
func (mm *MeshManager) SetRelayThreshold(n int) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.relayThreshold = n
}

// SetRelayEnabled toggles DialViaRelay (force-threshold and ICE-fail fallback).
// false disables all relay dialing per plan decision.
func (mm *MeshManager) SetRelayEnabled(v bool) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.relayEnabled = v
}

// PeerCount returns the number of currently connected peers (P2P + relay).
func (mm *MeshManager) PeerCount() int {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	return len(mm.peers) + len(mm.relayPeers)
}

func (mm *MeshManager) shouldForceRelay() bool {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if !mm.relayEnabled || mm.relay == nil || mm.relayThreshold <= 0 {
		return false
	}
	return len(mm.peers)+len(mm.relayPeers) >= mm.relayThreshold
}

// MarkRelayPeer records that peerID is reachable via the SFU (no direct PC).
func (mm *MeshManager) MarkRelayPeer(peerID string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if mm.relayPeers == nil {
		mm.relayPeers = make(map[string]struct{})
	}
	mm.relayPeers[peerID] = struct{}{}
}

// NewMeshManager builds a MeshManager for the local device selfID.
func NewMeshManager(selfID string, source AudioSource, sink AudioSink) (*MeshManager, error) {
	m := webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("media: register default codecs: %w", err)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(&m))

	return &MeshManager{
		selfID:         selfID,
		api:            api,
		source:         source,
		sink:           sink,
		peers:          make(map[string]*peerConn),
		relayPeers:     make(map[string]struct{}),
		relayEnabled:   true,
		relayThreshold: 0,
	}, nil
}

// SetRelay wires in a relay fallback (see RelayDialer). Optional.
func (mm *MeshManager) SetRelay(r RelayDialer) {
	mm.relay = r
}

// Start begins listening for incoming offers on the given port.
func (mm *MeshManager) Start(signalPort int) (int, error) {
	mm.sig = signaling.NewServer(mm.handleIncomingOffer)
	return mm.sig.Start(signalPort)
}

// Shutdown tears down the signaling listener and every peer connection.
func (mm *MeshManager) Shutdown(ctx context.Context) error {
	mm.mu.Lock()
	for id, p := range mm.peers {
		_ = p.pc.Close()
		delete(mm.peers, id)
	}
	mm.relayPeers = make(map[string]struct{})
	mm.mu.Unlock()

	if mm.sig != nil {
		return mm.sig.Shutdown(ctx)
	}
	return nil
}

// Connected reports whether we already have a path to peerID (P2P or relay).
func (mm *MeshManager) Connected(peerID string) bool {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if _, ok := mm.peers[peerID]; ok {
		return true
	}
	_, ok := mm.relayPeers[peerID]
	return ok
}

// DirectConnected reports a direct PeerConnection (not SFU-only) for peerID.
// Live private unicast Talk requires this; relay-only peers fall back to clips.
func (mm *MeshManager) DirectConnected(peerID string) bool {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	_, ok := mm.peers[peerID]
	return ok
}

// Connect dials peerID's signaling endpoint directly, or via SFU when
// force-relay applies or direct ICE fails (and relay is enabled).
func (mm *MeshManager) Connect(host string, port int, peerID string) error {
	if mm.Connected(peerID) {
		return nil
	}

	if mm.shouldForceRelay() {
		return mm.dialRelay(peerID)
	}

	err := mm.connectDirect(host, port, peerID)
	if err == nil {
		return nil
	}
	mm.mu.Lock()
	canRelay := mm.relayEnabled && mm.relay != nil
	mm.mu.Unlock()
	if canRelay {
		if rerr := mm.dialRelay(peerID); rerr == nil {
			return nil
		}
	}
	return err
}

func (mm *MeshManager) dialRelay(peerID string) error {
	mm.mu.Lock()
	r := mm.relay
	enabled := mm.relayEnabled
	threshold := mm.relayThreshold
	count := len(mm.peers) + len(mm.relayPeers)
	mm.mu.Unlock()
	if !enabled || r == nil {
		if threshold > 0 && count >= threshold && mm.OnRelayThresholdExceeded != nil {
			mm.OnRelayThresholdExceeded(count, threshold)
		}
		return fmt.Errorf("media: relay required but not available for %s", peerID)
	}
	if err := r.DialViaRelay(peerID); err != nil {
		return err
	}
	mm.MarkRelayPeer(peerID)
	return nil
}

func (mm *MeshManager) connectDirect(host string, port int, peerID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	pc, track, err := mm.newPeerConnection(peerID)
	if err != nil {
		return err
	}

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		_ = pc.Close()
		return fmt.Errorf("media: create offer for %s: %w", peerID, err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		_ = pc.Close()
		return fmt.Errorf("media: set local description for %s: %w", peerID, err)
	}
	select {
	case <-gatherComplete:
	case <-ctx.Done():
		_ = pc.Close()
		return fmt.Errorf("media: ICE gathering timed out for %s", peerID)
	}

	answerSDP, err := signaling.PostOffer(ctx, host, port, mm.selfID, pc.LocalDescription().SDP)
	if err != nil {
		_ = pc.Close()
		return fmt.Errorf("media: signaling %s: %w", peerID, err)
	}

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		_ = pc.Close()
		return fmt.Errorf("media: set remote description for %s: %w", peerID, err)
	}

	mm.storePeer(peerID, pc, track)
	return nil
}

// ConnectAny tries each host in order (same port) and stops at the first
// success. Force-relay short-circuits before trying hosts.
func (mm *MeshManager) ConnectAny(hosts []string, port int, peerID string) error {
	if mm.Connected(peerID) {
		return nil
	}
	if mm.shouldForceRelay() {
		return mm.dialRelay(peerID)
	}
	var lastErr error
	for _, host := range hosts {
		if err := mm.connectDirect(host, port, peerID); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	mm.mu.Lock()
	canRelay := mm.relayEnabled && mm.relay != nil
	mm.mu.Unlock()
	if canRelay {
		if err := mm.dialRelay(peerID); err == nil {
			return nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("media: no hosts to try for %s", peerID)
	}
	return lastErr
}

func (mm *MeshManager) storePeer(peerID string, pc *webrtc.PeerConnection, track *webrtc.TrackLocalStaticSample) {
	mm.mu.Lock()
	old, existed := mm.peers[peerID]
	mm.peers[peerID] = &peerConn{id: peerID, pc: pc, track: track}
	delete(mm.relayPeers, peerID)
	mm.mu.Unlock()
	if existed {
		_ = old.pc.Close()
	}
}

// Disconnect closes and forgets the peer connection to peerID, if any.
func (mm *MeshManager) Disconnect(peerID string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if p, ok := mm.peers[peerID]; ok {
		_ = p.pc.Close()
		delete(mm.peers, peerID)
	}
	delete(mm.relayPeers, peerID)
}

func (mm *MeshManager) handleIncomingOffer(senderID, offerSDP string) (string, error) {
	pc, track, err := mm.newPeerConnection(senderID)
	if err != nil {
		return "", err
	}

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}); err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("media: set remote description from %s: %w", senderID, err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("media: create answer for %s: %w", senderID, err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("media: set local description for %s: %w", senderID, err)
	}
	<-gatherComplete

	mm.storePeer(senderID, pc, track)

	return pc.LocalDescription().SDP, nil
}

func (mm *MeshManager) newPeerConnection(peerID string) (*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, error) {
	pc, err := mm.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, fmt.Errorf("media: new peer connection for %s: %w", peerID, err)
	}

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"wt-"+mm.selfID,
	)
	if err != nil {
		_ = pc.Close()
		return nil, nil, fmt.Errorf("media: new local track: %w", err)
	}
	if _, err := pc.AddTrack(track); err != nil {
		_ = pc.Close()
		return nil, nil, fmt.Errorf("media: add track: %w", err)
	}

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go mm.readRemoteTrack(peerID, remote)
	})

	if mm.OnConnectionStateChange != nil {
		pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
			mm.OnConnectionStateChange(peerID, state)
		})
	}

	return pc, track, nil
}

func (mm *MeshManager) readRemoteTrack(peerID string, remote *webrtc.TrackRemote) {
	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			return
		}
		if mm.OnOpusFrameReceived != nil {
			mm.OnOpusFrameReceived(peerID, len(pkt.Payload))
		}
		if mm.sink != nil {
			_ = mm.sink.WriteOpusFrame(peerID, pkt.Payload)
		}
	}
}

// Broadcast writes one Opus frame to every currently connected peer (P2P)
// and to the local SFU host hook / client send track via OnRelayBroadcast.
func (mm *MeshManager) Broadcast(frame []byte) {
	mm.mu.Lock()
	sample := media.Sample{Data: frame, Duration: opusFrameDuration}
	n := 0
	for _, p := range mm.peers {
		if err := p.track.WriteSample(sample); err == nil {
			n++
		}
	}
	relayN := len(mm.relayPeers)
	mm.mu.Unlock()

	if mm.OnRelayBroadcast != nil {
		mm.OnRelayBroadcast(frame)
		if relayN > 0 {
			n += relayN
		}
	}
	if mm.OnOpusFrameSent != nil && n > 0 {
		mm.OnOpusFrameSent(len(frame), n)
	}
}

// SendTo writes one Opus frame to a single direct peer's send track.
// Returns false if that peer has no direct PeerConnection (e.g. SFU-only).
func (mm *MeshManager) SendTo(peerID string, frame []byte) bool {
	mm.mu.Lock()
	p, ok := mm.peers[peerID]
	mm.mu.Unlock()
	if !ok || p == nil || p.track == nil {
		return false
	}
	sample := media.Sample{Data: frame, Duration: opusFrameDuration}
	if err := p.track.WriteSample(sample); err != nil {
		return false
	}
	if mm.OnOpusFrameSent != nil {
		mm.OnOpusFrameSent(len(frame), 1)
	}
	return true
}

func (mm *MeshManager) isTalking() bool {
	return atomic.LoadInt32(&mm.talking) == 1
}
