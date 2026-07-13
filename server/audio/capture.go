package audio

import (
	"fmt"

	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/opus"
	"github.com/pion/mediadevices/pkg/driver/microphone"
)

// micSource captures the default microphone and encodes it to Opus via
// pion/mediadevices, satisfying core/media.AudioSource. cgo (libopus) is
// fine here — this only ever builds into the desktop binary, never into
// the gomobile-bound core.
type micSource struct {
	reader mediadevices.EncodedReadCloser
}

func newMicSource() (*micSource, error) {
	microphone.Initialize()

	params, err := opus.NewParams()
	if err != nil {
		return nil, fmt.Errorf("audio: opus encoder params: %w", err)
	}
	selector := mediadevices.NewCodecSelector(mediadevices.WithAudioEncoders(&params))

	stream, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Audio: func(c *mediadevices.MediaTrackConstraints) {},
		Codec: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("audio: get user media (microphone): %w", err)
	}

	tracks := stream.GetAudioTracks()
	if len(tracks) == 0 {
		return nil, fmt.Errorf("audio: no microphone track available")
	}

	reader, err := tracks[0].NewEncodedReader("opus")
	if err != nil {
		return nil, fmt.Errorf("audio: new encoded (opus) reader: %w", err)
	}

	return &micSource{reader: reader}, nil
}

// ReadOpusFrame implements core/media.AudioSource.
func (m *micSource) ReadOpusFrame() ([]byte, error) {
	buf, _, err := m.reader.Read()
	if err != nil {
		return nil, err
	}
	return buf.Data, nil
}
