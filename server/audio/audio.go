// Package audio implements this desktop process's mic capture + Opus
// encode (capture.go, via pion/mediadevices), and Opus decode + speaker
// playback (playback.go, via hraban/opus + malgo) — i.e. the
// AudioSource/AudioSink pair the shared core/media package needs, kept out
// of core itself so core stays cgo-free for gomobile (see the plan's
// "Codec/capture split"). cgo is fine here; this only ever builds for
// desktop. Requires libopus to be installed (e.g. `brew install opus`).
package audio

import "github.com/JohnDovey/WalkieTalkie/core/media"

// NewLocalIO opens the local microphone and speaker and returns the
// AudioSource/AudioSink pair core/media needs to participate in the mesh.
func NewLocalIO() (media.AudioSource, media.AudioSink, error) {
	src, err := newMicSource()
	if err != nil {
		return nil, nil, err
	}
	sink, err := newPlaybackSink()
	if err != nil {
		return nil, nil, err
	}
	return src, sink, nil
}
