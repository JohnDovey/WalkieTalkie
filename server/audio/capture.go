package audio

import (
	"fmt"
	"sync"

	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/opus"
	"github.com/pion/mediadevices/pkg/driver/microphone"
)

// micSource captures the default microphone and encodes it to Opus via
// pion/mediadevices, satisfying core/media.AudioSource. cgo (libopus) is
// fine here — this only ever builds into the desktop binary, never into
// the gomobile-bound core.
//
// The mic track is acquired lazily (on the first ReadOpusFrame after
// construction or after Stop) and released by Stop, rather than held for
// the whole process lifetime — see AudioSource.Stop's doc comment.
type micSource struct {
	mu     sync.Mutex
	track  mediadevices.Track
	reader mediadevices.EncodedReadCloser
}

func newMicSource() (*micSource, error) {
	m := &micSource{}
	// Acquire once eagerly at construction, so a missing/broken microphone
	// is reported at startup (see server/audio.NewLocalIO's caller, which
	// logs and disables audio on error) rather than silently on first talk.
	if err := m.acquire(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *micSource) acquire() error {
	microphone.Initialize()

	params, err := opus.NewParams()
	if err != nil {
		return fmt.Errorf("audio: opus encoder params: %w", err)
	}
	selector := mediadevices.NewCodecSelector(mediadevices.WithAudioEncoders(&params))

	stream, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Audio: func(c *mediadevices.MediaTrackConstraints) {},
		Codec: selector,
	})
	if err != nil {
		return fmt.Errorf("audio: get user media (microphone): %w", err)
	}

	tracks := stream.GetAudioTracks()
	if len(tracks) == 0 {
		return fmt.Errorf("audio: no microphone track available")
	}

	reader, err := tracks[0].NewEncodedReader("opus")
	if err != nil {
		return fmt.Errorf("audio: new encoded (opus) reader: %w", err)
	}

	m.track = tracks[0]
	m.reader = reader
	return nil
}

// ReadOpusFrame implements core/media.AudioSource.
func (m *micSource) ReadOpusFrame() ([]byte, error) {
	m.mu.Lock()
	if m.reader == nil {
		if err := m.acquire(); err != nil {
			m.mu.Unlock()
			return nil, err
		}
	}
	reader := m.reader
	m.mu.Unlock()

	buf, _, err := reader.Read()
	if err != nil {
		return nil, err
	}
	return buf.Data, nil
}

// Stop implements core/media.AudioSource: releases the mic track so other
// applications can use it between talk sessions. The next ReadOpusFrame
// call transparently re-acquires it.
func (m *micSource) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reader == nil {
		return nil
	}
	readerErr := m.reader.Close()
	trackErr := m.track.Close()
	m.reader = nil
	m.track = nil
	if readerErr != nil {
		return readerErr
	}
	return trackErr
}
