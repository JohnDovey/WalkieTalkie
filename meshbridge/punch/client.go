package punch

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"sync"
	"time"
)

// Client maintains UDP to a hub and punches peers (QuakeMesh DCUtR-style).
type Client struct {
	SelfID  string
	HubHost string
	HubPort int

	mu       sync.Mutex
	conn     *net.UDPConn
	hubAddr  *net.UDPAddr
	peerAddr *net.UDPAddr
	peerID   string

	OnPeer    func(peerID string, addr *net.UDPAddr)
	OnBaseURL func(peerID string, payload BaseURLPayload)
}

// Start dials the hub and begins keepalives.
func (c *Client) Start(ctx context.Context) error {
	if c.HubPort <= 0 {
		c.HubPort = DefaultPort
	}
	hub, err := udpAddr(c.HubHost, c.HubPort)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.hubAddr = hub
	c.mu.Unlock()

	_, _ = conn.WriteToUDP(encode(Envelope{T: MsgRegister, FromID: c.SelfID}), hub)
	go c.readLoop(ctx)
	go c.keepalive(ctx)
	return nil
}

// Connect asks the hub to introduce peerID.
func (c *Client) Connect(peerID string) error {
	c.mu.Lock()
	conn, hub := c.conn, c.hubAddr
	c.peerID = peerID
	c.mu.Unlock()
	if conn == nil || hub == nil {
		return net.ErrClosed
	}
	_, err := conn.WriteToUDP(encode(Envelope{T: MsgConnect, FromID: c.SelfID, ToID: peerID}), hub)
	return err
}

// SendBaseURL advertises this node's Base Station HTTP URL to the punched peer (or via hub).
func (c *Client) SendBaseURL(url, id, name string) error {
	c.mu.Lock()
	conn, peer, hub, peerID := c.conn, c.peerAddr, c.hubAddr, c.peerID
	c.mu.Unlock()
	if conn == nil {
		return net.ErrClosed
	}
	raw, _ := json.Marshal(BaseURLPayload{URL: url, ID: id, Name: name})
	msg := Envelope{T: MsgBaseURL, FromID: c.SelfID, ToID: peerID, Cookie: string(raw)}
	dst := peer
	if dst == nil {
		dst = hub
	}
	if dst == nil {
		return net.ErrClosed
	}
	_, err := conn.WriteToUDP(encode(msg), dst)
	return err
}

func (c *Client) keepalive(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.mu.Lock()
			conn, hub := c.conn, c.hubAddr
			c.mu.Unlock()
			if conn == nil || hub == nil {
				continue
			}
			_, _ = conn.WriteToUDP(encode(Envelope{T: MsgKeepalive, FromID: c.SelfID}), hub)
		}
	}
}

func (c *Client) readLoop(ctx context.Context) {
	buf := make([]byte, 65535)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		msg, err := decode(buf[:n])
		if err != nil {
			continue
		}
		switch msg.T {
		case MsgIntroduce:
			pa := &net.UDPAddr{IP: net.ParseIP(msg.MappedIP), Port: msg.MappedPort}
			c.mu.Lock()
			c.peerAddr = pa
			peerID := c.peerID
			if msg.ToID != "" {
				peerID = msg.ToID
			}
			c.mu.Unlock()
			log.Printf("meshbridge punch: introduced to %s (%s)", peerID, pa)
			go c.blast(pa)
			if c.OnPeer != nil {
				c.OnPeer(peerID, pa)
			}
		case MsgPunchNow:
			c.mu.Lock()
			pa := c.peerAddr
			c.mu.Unlock()
			if pa != nil {
				go c.blast(pa)
			}
		case MsgBaseURL:
			var p BaseURLPayload
			_ = json.Unmarshal([]byte(msg.Cookie), &p)
			if p.URL != "" && c.OnBaseURL != nil {
				c.OnBaseURL(msg.FromID, p)
			}
		}
	}
}

func (c *Client) blast(pa *net.UDPAddr) {
	c.mu.Lock()
	conn := c.conn
	id := c.SelfID
	c.mu.Unlock()
	if conn == nil || pa == nil {
		return
	}
	for i := 0; i < 5; i++ {
		_, _ = conn.WriteToUDP(encode(Envelope{T: MsgKeepalive, FromID: id}), pa)
		time.Sleep(30 * time.Millisecond)
	}
}

// Close closes the UDP socket.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
