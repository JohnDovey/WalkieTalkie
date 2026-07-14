package punch

import (
	"encoding/json"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	raw := encode(Envelope{T: MsgConnect, FromID: "a", ToID: "b", RTTMs: 40})
	msg, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if msg.T != MsgConnect || msg.FromID != "a" || msg.ToID != "b" {
		t.Fatalf("%+v", msg)
	}
	p := BaseURLPayload{URL: "http://127.0.0.1:9091", ID: "x"}
	b, _ := json.Marshal(p)
	var out BaseURLPayload
	_ = json.Unmarshal(b, &out)
	if out.URL != p.URL {
		t.Fatal(out)
	}
}
