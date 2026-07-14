package relay

import (
	"encoding/json"
	"testing"
)

func TestVoiceNoteDCStartEnvelope(t *testing.T) {
	raw, err := json.Marshal(voiceDCMsg{
		T: "start", ID: "n1", FromID: "a", ToID: "b", ChannelID: "ch1", Size: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		T    string `json:"t"`
		ToID string `json:"toId"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.T != "start" || m.ToID != "b" {
		t.Fatalf("got %+v", m)
	}
	if voiceNoteDCLabel != "voicenote" {
		t.Fatalf("label=%q", voiceNoteDCLabel)
	}
}

func TestSendVoiceNoteRequiresTarget(t *testing.T) {
	c := &Client{SelfID: "me"}
	err := c.SendVoiceNote(VoiceNoteMeta{ID: "n1"}, []byte("audio"))
	if err == nil {
		t.Fatal("expected error without toId/channelId")
	}
	err = c.SendVoiceNote(VoiceNoteMeta{ID: "n1", ChannelID: "ch"}, []byte("audio"))
	// SFU DC not open — still an error, but validation of ids passed.
	if err == nil {
		t.Fatal("expected DC-not-open error")
	}
}
