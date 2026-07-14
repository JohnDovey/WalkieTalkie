package punch

import (
	"log"
	"net"
	"sync"
	"time"
)

// Hub is a QuakeMesh-style rendezvous + optional packet relay on UDP.
type Hub struct {
	mu    sync.Mutex
	nodes map[string]*hubNode
	conn  *net.UDPConn
}

type hubNode struct {
	addr   *net.UDPAddr
	seen   time.Time
	wantTo string
}

// Listen starts the hub on UDP port.
func Listen(port int) (*Hub, error) {
	if port <= 0 {
		port = DefaultPort
	}
	addr, err := net.ResolveUDPAddr("udp", ":"+itoa(port))
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	h := &Hub{nodes: map[string]*hubNode{}, conn: conn}
	go h.loop()
	log.Printf("meshbridge punch hub listening udp/%d", port)
	return h, nil
}

// Close stops the hub.
func (h *Hub) Close() error {
	return h.conn.Close()
}

func (h *Hub) loop() {
	buf := make([]byte, 65535)
	for {
		n, addr, err := h.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		msg, err := decode(buf[:n])
		if err != nil || msg.FromID == "" {
			continue
		}
		h.handle(msg, addr)
	}
}

func (h *Hub) handle(msg Envelope, addr *net.UDPAddr) {
	h.mu.Lock()
	defer h.mu.Unlock()
	node := h.nodes[msg.FromID]
	if node == nil {
		node = &hubNode{}
		h.nodes[msg.FromID] = node
	}
	node.addr = addr
	node.seen = time.Now()

	switch msg.T {
	case MsgRegister, MsgKeepalive:
		// mapped from hub's view
		ack := Envelope{T: MsgKeepalive, FromID: "hub", MappedIP: addr.IP.String(), MappedPort: addr.Port}
		_, _ = h.conn.WriteToUDP(encode(ack), addr)
	case MsgConnect:
		node.wantTo = msg.ToID
		peer := h.nodes[msg.ToID]
		if peer == nil || peer.addr == nil {
			return
		}
		cookie := msg.FromID + ":" + msg.ToID
		introA := Envelope{T: MsgIntroduce, FromID: "hub", ToID: msg.ToID,
			MappedIP: peer.addr.IP.String(), MappedPort: peer.addr.Port, Cookie: cookie, RTTMs: 40}
		introB := Envelope{T: MsgIntroduce, FromID: "hub", ToID: msg.FromID,
			MappedIP: addr.IP.String(), MappedPort: addr.Port, Cookie: cookie, RTTMs: 40}
		_, _ = h.conn.WriteToUDP(encode(introA), addr)
		_, _ = h.conn.WriteToUDP(encode(introB), peer.addr)
		// DCUtR: schedule punch_now after ~0.5*RTT
		go func(a, b *net.UDPAddr, c string) {
			time.Sleep(20 * time.Millisecond)
			pn := Envelope{T: MsgPunchNow, FromID: "hub", Cookie: c}
			_, _ = h.conn.WriteToUDP(encode(pn), a)
			_, _ = h.conn.WriteToUDP(encode(pn), b)
		}(addr, peer.addr, cookie)
	case MsgPunchFail:
		// TURN-style: forward subsequent application datagrams between peers.
		peer := h.nodes[msg.ToID]
		if peer != nil && peer.addr != nil {
			_, _ = h.conn.WriteToUDP(encode(msg), peer.addr)
		}
	default:
		// Relay opaque / introduce echoes
		if msg.ToID != "" {
			if peer := h.nodes[msg.ToID]; peer != nil && peer.addr != nil {
				_, _ = h.conn.WriteToUDP(encode(msg), peer.addr)
			}
		}
	}
}
