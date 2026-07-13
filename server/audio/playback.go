package audio

import (
	"fmt"
	"sync"

	"github.com/gen2brain/malgo"
	opus "gopkg.in/hraban/opus.v2"
)

const (
	sampleRate     = 48000
	channels       = 1
	samplesPerOpus = 960 // 20ms @ 48kHz mono, the frame size WriteOpusFrame decodes into
)

// playbackSink decodes incoming Opus frames (per-peer, since Opus decoding
// is stateful per stream) and plays the resulting PCM out the default
// speaker via malgo (miniaudio cgo bindings), satisfying
// core/media.AudioSink.
//
// Concurrent talkers are not mixed — frames are queued FIFO into one
// playback buffer. Acceptable for half-duplex PTT, where normally only one
// peer transmits at a time; simultaneous transmitters would sound
// interleaved rather than blended. A future enhancement could mix them.
type playbackSink struct {
	ctx *malgo.AllocatedContext
	dev *malgo.Device

	mu       sync.Mutex
	decoders map[string]*opus.Decoder
	pcmCh    chan []int16
}

func newPlaybackSink() (*playbackSink, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("audio: init malgo context: %w", err)
	}

	s := &playbackSink{
		ctx:      ctx,
		decoders: make(map[string]*opus.Decoder),
		pcmCh:    make(chan []int16, 64),
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatS16
	cfg.Playback.Channels = channels
	cfg.SampleRate = sampleRate

	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: s.onData})
	if err != nil {
		ctx.Free()
		return nil, fmt.Errorf("audio: init playback device: %w", err)
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("audio: start playback device: %w", err)
	}

	s.dev = dev
	return s, nil
}

// onData is called on miniaudio's realtime audio thread — it must never
// block, hence the non-blocking channel read with a silence fallback.
func (s *playbackSink) onData(out, _ []byte, frameCount uint32) {
	need := int(frameCount)
	pos := 0
	for pos < need {
		select {
		case frame, ok := <-s.pcmCh:
			if !ok {
				return
			}
			n := len(frame)
			if pos+n > need {
				n = need - pos
			}
			for i := 0; i < n; i++ {
				v := uint16(frame[i])
				out[(pos+i)*2] = byte(v)
				out[(pos+i)*2+1] = byte(v >> 8)
			}
			pos += n
		default:
			for i := pos * 2; i < need*2; i++ {
				out[i] = 0
			}
			pos = need
		}
	}
}

// WriteOpusFrame implements core/media.AudioSink.
func (s *playbackSink) WriteOpusFrame(peerID string, frame []byte) error {
	s.mu.Lock()
	dec, ok := s.decoders[peerID]
	if !ok {
		var err error
		dec, err = opus.NewDecoder(sampleRate, channels)
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("audio: new decoder for %s: %w", peerID, err)
		}
		s.decoders[peerID] = dec
	}
	s.mu.Unlock()

	pcm := make([]int16, samplesPerOpus)
	n, err := dec.Decode(frame, pcm)
	if err != nil {
		return fmt.Errorf("audio: decode frame from %s: %w", peerID, err)
	}

	select {
	case s.pcmCh <- pcm[:n]:
	default:
		// Playback buffer full — drop rather than block the caller
		// (typically core/media's RTP read loop).
	}
	return nil
}
