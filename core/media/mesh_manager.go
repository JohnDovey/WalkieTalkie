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
// take before the caller should consider falling back to a relay (see the
// plan's "Server-relay fallback" — that fallback path itself is not yet
// implemented here; RelayDialer is the seam it will hang off of).
const connectTimeout = 3 * time.Second

// RelayDialer is asked to bridge two peers via the server's relay when a
// direct connection can't be established (different subnets/NAT). Not yet
// implemented (see core/relay, a later phase) — MeshManager works fully
// P2P without one, which is sufficient for same-subnet Phase 1 use.
//
// Deliberately avoids context.Context in its signature — gomobile bind
// can't generate a working proxy for interface methods that take one (a
// broken proxy silently missing the method is the failure mode), and this
// interface needs to stay gomobile-bindable since a mobile client may
// eventually implement it too.
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

	mu    sync.Mutex
	peers map[string]*peerConn

	talking  int32
	stopTalk chan struct{}

	relayThreshold int // 0 = disabled; see SetRelayThreshold

	// OnConnectionStateChange, if set, is called whenever a peer
	// connection's ICE/DTLS state changes — useful for logging/UI, e.g.
	// distinguishing "signaling succeeded" from "media path actually up."
	OnConnectionStateChange func(peerID string, state webrtc.PeerConnectionState)

	// OnRelayThresholdExceeded, if set, is called when a new connection is
	// about to be made while the peer count is already at or past
	// RelayThreshold but no RelayDialer is configured (see core/relay,
	// still a TODO) — the connection proceeds directly anyway (better to
	// stay connected than refuse service), but this lets the caller warn
	// that the setting isn't actually being honored yet.
	OnRelayThresholdExceeded func(peerCount, threshold int)
}

// SetRelayThreshold configures the peer count above which new connections
// should prefer the relay over direct P2P (see docs/2026-07-13-implementation-plan.md,
// "Audio layer"). 0 disables the check.
func (mm *MeshManager) SetRelayThreshold(n int) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.relayThreshold = n
}

// PeerCount returns the number of currently connected peers.
func (mm *MeshManager) PeerCount() int {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	return len(mm.peers)
}

// checkRelayThreshold warns (via OnRelayThresholdExceeded) if a new
// connection would push the peer count at/past the configured threshold
// with no relay available. See the RelayThreshold field doc.
func (mm *MeshManager) checkRelayThreshold() {
	mm.mu.Lock()
	threshold := mm.relayThreshold
	count := len(mm.peers)
	hasRelay := mm.relay != nil
	mm.mu.Unlock()

	if threshold > 0 && count >= threshold && !hasRelay && mm.OnRelayThresholdExceeded != nil {
		mm.OnRelayThresholdExceeded(count, threshold)
	}
}

// NewMeshManager builds a MeshManager for the local device selfID. source
// and sink may be nil if this session only ever receives or only ever
// sends (uncommon, but avoids forcing callers to stub both).
func NewMeshManager(selfID string, source AudioSource, sink AudioSink) (*MeshManager, error) {
	m := webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("media: register default codecs: %w", err)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(&m))

	return &MeshManager{
		selfID: selfID,
		api:    api,
		source: source,
		sink:   sink,
		peers:  make(map[string]*peerConn),
	}, nil
}

// SetRelay wires in a relay fallback (see RelayDialer). Optional.
func (mm *MeshManager) SetRelay(r RelayDialer) {
	mm.relay = r
}

// Start begins listening for incoming offers on the given port (0 = pick a
// free port) and returns the bound port, for publishing via the mDNS "sig"
// TXT field.
func (mm *MeshManager) Start(signalPort int) (int, error) {
	mm.sig = signaling.NewServer(mm.handleIncomingOffer)
	return mm.sig.Start(signalPort)
}

// Shutdown tears down the signaling listener and every peer connection.
func (mm *MeshManager) Shutdown(ctx context.Context) error {
	mm.mu.Lock()
	for id, p := range mm.peers {
		p.pc.Close()
		delete(mm.peers, id)
	}
	mm.mu.Unlock()

	if mm.sig != nil {
		return mm.sig.Shutdown(ctx)
	}
	return nil
}

// Connected reports whether we already have a peer connection to peerID
// (dialed or answered).
func (mm *MeshManager) Connected(peerID string) bool {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	_, ok := mm.peers[peerID]
	return ok
}

// Connect dials peerID's signaling endpoint directly. Intended to be called
// by the discovery layer (mDNS/BLE) once a peer is found and known to be
// reachable. Bounded by connectTimeout; callers should treat a timeout/error
// as a signal to fall back to the relay (once implemented) rather than
// retrying indefinitely.
func (mm *MeshManager) Connect(host string, port int, peerID string) error {
	if mm.Connected(peerID) {
		return nil
	}
	mm.checkRelayThreshold()

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	pc, track, err := mm.newPeerConnection(peerID)
	if err != nil {
		return err
	}

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		pc.Close()
		return fmt.Errorf("media: create offer for %s: %w", peerID, err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		pc.Close()
		return fmt.Errorf("media: set local description for %s: %w", peerID, err)
	}
	select {
	case <-gatherComplete:
	case <-ctx.Done():
		pc.Close()
		return fmt.Errorf("media: ICE gathering timed out for %s", peerID)
	}

	answerSDP, err := signaling.PostOffer(ctx, host, port, mm.selfID, pc.LocalDescription().SDP)
	if err != nil {
		pc.Close()
		return fmt.Errorf("media: signaling %s: %w", peerID, err)
	}

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		pc.Close()
		return fmt.Errorf("media: set remote description for %s: %w", peerID, err)
	}

	mm.storePeer(peerID, pc, track)
	return nil
}

// ConnectAny tries each host in order (same port) and stops at the first
// one that connects, per docs/2026-07-13-implementation-plan.md's note on
// picking the right advertised address: a peer can legitimately have
// several IPv4 addresses (multiple interfaces), and blindly trusting the
// first one in the list found the hard way, on real Android hardware, to
// sometimes be a cellular-modem address that will never be reachable on
// the LAN — trying every candidate is more robust than perfectly filtering
// which interface "should" be right at discovery time.
func (mm *MeshManager) ConnectAny(hosts []string, port int, peerID string) error {
	var lastErr error
	for _, host := range hosts {
		if err := mm.Connect(host, port, peerID); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("media: no hosts to try for %s", peerID)
	}
	return lastErr
}

// storePeer atomically installs pc as the connection for peerID, closing
// whatever connection (if any) was there before — the "glare" case where
// both sides discover each other at once and each independently dial the
// other, producing two separate PeerConnections for the same peer.
// Without this, the losing connection would be silently overwritten in the
// map but never closed: a leaked PeerConnection (and its ICE agent and
// readRemoteTrack goroutine) running forever.
func (mm *MeshManager) storePeer(peerID string, pc *webrtc.PeerConnection, track *webrtc.TrackLocalStaticSample) {
	mm.mu.Lock()
	old, existed := mm.peers[peerID]
	mm.peers[peerID] = &peerConn{id: peerID, pc: pc, track: track}
	mm.mu.Unlock()
	if existed {
		old.pc.Close()
	}
}

// Disconnect closes and forgets the peer connection to peerID, if any.
func (mm *MeshManager) Disconnect(peerID string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if p, ok := mm.peers[peerID]; ok {
		p.pc.Close()
		delete(mm.peers, peerID)
	}
}

// handleIncomingOffer answers an offer from senderID, arriving via this
// node's own signaling.Server.
func (mm *MeshManager) handleIncomingOffer(senderID, offerSDP string) (string, error) {
	mm.checkRelayThreshold()
	pc, track, err := mm.newPeerConnection(senderID)
	if err != nil {
		return "", err
	}

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}); err != nil {
		pc.Close()
		return "", fmt.Errorf("media: set remote description from %s: %w", senderID, err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		return "", fmt.Errorf("media: create answer for %s: %w", senderID, err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		return "", fmt.Errorf("media: set local description for %s: %w", senderID, err)
	}
	<-gatherComplete

	mm.storePeer(senderID, pc, track)

	return pc.LocalDescription().SDP, nil
}

func (mm *MeshManager) newPeerConnection(peerID string) (*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, error) {
	// No ICEServers: pure-LAN host candidates are enough for same-subnet
	// peers (see the plan's WebRTC-on-LAN research); cross-subnet pairs are
	// exactly what the relay fallback (RelayDialer) is for, once built.
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
		pc.Close()
		return nil, nil, fmt.Errorf("media: new local track: %w", err)
	}
	if _, err := pc.AddTrack(track); err != nil {
		pc.Close()
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
		if mm.sink != nil {
			_ = mm.sink.WriteOpusFrame(peerID, pkt.Payload)
		}
	}
}

// Broadcast writes one Opus frame to every currently connected peer.
func (mm *MeshManager) Broadcast(frame []byte) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	sample := media.Sample{Data: frame, Duration: opusFrameDuration}
	for _, p := range mm.peers {
		_ = p.track.WriteSample(sample)
	}
}

// talking reports whether StartTalking has been called without a matching
// StopTalking yet (see ptt_controller.go).
func (mm *MeshManager) isTalking() bool {
	return atomic.LoadInt32(&mm.talking) == 1
}
