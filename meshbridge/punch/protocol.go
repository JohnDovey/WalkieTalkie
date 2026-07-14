package punch

import (
	"encoding/json"
	"net"
	"strconv"
)

const DefaultPort = 29191

// MsgType is a control-plane message kind (QuakeMesh-inspired).
type MsgType string

const (
	MsgRegister  MsgType = "register"
	MsgKeepalive MsgType = "keepalive"
	MsgConnect   MsgType = "connect"
	MsgIntroduce MsgType = "introduce"
	MsgPunchNow  MsgType = "punch_now"
	MsgPunchFail MsgType = "punch_fail"
	MsgBaseURL   MsgType = "base_url"
)

// Envelope is the UDP JSON control message.
type Envelope struct {
	T          MsgType `json:"t"`
	FromID     string  `json:"fromId"`
	ToID       string  `json:"toId,omitempty"`
	MappedIP   string  `json:"mappedIp,omitempty"`
	MappedPort int     `json:"mappedPort,omitempty"`
	RTTMs      int64   `json:"rttMs,omitempty"`
	Cookie     string  `json:"cookie,omitempty"`
}

// BaseURLPayload advertises a Base Station HTTP URL after punch.
type BaseURLPayload struct {
	URL  string `json:"url"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

func encode(e Envelope) []byte {
	b, _ := json.Marshal(e)
	return b
}

func decode(b []byte) (Envelope, error) {
	var e Envelope
	err := json.Unmarshal(b, &e)
	return e, err
}

func udpAddr(host string, port int) (*net.UDPAddr, error) {
	return net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
