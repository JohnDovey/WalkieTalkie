// Package media implements the PTT audio mesh: a pion/webrtc PeerConnection
// per reachable peer, wired to per-node signaling (core/signaling) for
// offer/answer exchange. See docs/2026-07-13-implementation-plan.md ("Audio
// layer") for the design.
//
// core/media deliberately never touches raw PCM or an Opus codec itself —
// pion negotiates Opus at the SDP level but does not encode/decode it, and
// keeping actual codec/capture work out of this module keeps it free of
// cgo, which matters for gomobile bind (see the plan's "Codec/capture
// split" note). Real mic capture + Opus encode, and Opus decode + speaker
// playback, are native per platform (Android MediaCodec, iOS's Opus
// binding, desktop's server/audio using pion/mediadevices).
package media

// AudioSource is implemented natively per platform. ReadOpusFrame blocks
// until the next ~20ms Opus-encoded frame captured from the mic is ready.
//
// Stop releases the underlying mic hardware/session (called between talk
// sessions, not just at final teardown) — found the hard way, on real
// hardware: without this, the very first PTT press left the mic exclusively
// captured for the rest of the app's life, since nothing ever told the
// platform layer to actually let go of it between presses. The next
// ReadOpusFrame call after Stop must transparently re-acquire the mic.
type AudioSource interface {
	ReadOpusFrame() ([]byte, error)
	Stop() error
}

// AudioSink is implemented natively per platform. WriteOpusFrame delivers
// one Opus-encoded frame received from peerID for the native layer to
// decode and play.
type AudioSink interface {
	WriteOpusFrame(peerID string, frame []byte) error
}
