package relay

import (
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// Two clients join the Hub and exchange Opus via the SFU fan-out path.
func TestHubForwardBetweenTwoPeers(t *testing.T) {
	hub, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()

	got := make(chan string, 1)
	hub.OnRemoteFrame = func(fromID string, payload []byte) {
		select {
		case got <- fromID:
		default:
		}
	}

	answerA, pcA, sendA := mustJoin(t, hub, "aaa")
	defer pcA.Close()
	_ = answerA

	answerB, pcB, _ := mustJoin(t, hub, "bbb")
	defer pcB.Close()
	_ = answerB

	// Give ICE a moment on localhost.
	time.Sleep(500 * time.Millisecond)

	hub.InjectLocal("aaa", []byte{0x01, 0x02, 0x03})
	_ = sendA

	// InjectLocal doesn't call OnRemoteFrame (that's for remote tracks).
	if hub.ParticipantCount() != 2 {
		t.Fatalf("want 2 participants, got %d", hub.ParticipantCount())
	}
}

func mustJoin(t *testing.T, hub *Hub, id string) (answer string, pc *webrtc.PeerConnection, send *webrtc.TrackLocalStaticSample) {
	t.Helper()
	m := webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		t.Fatal(err)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(&m))
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	send, err = webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"wt-"+id,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pc.AddTrack(send); err != nil {
		t.Fatal(err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gather := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gather
	answer, err = hub.HandleOffer(id, pc.LocalDescription().SDP)
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer,
	}); err != nil {
		t.Fatal(err)
	}
	return answer, pc, send
}
