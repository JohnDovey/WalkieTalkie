package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// Client dials a remote Hub (Base Station SFU) and exposes a single send
// track plus remote-frame callbacks. Implements the connect-once model:
// one PeerConnection carries mesh audio for all relay-routed peers.
type Client struct {
	SelfID string

	mu       sync.Mutex
	host     string
	port     int
	pc       *webrtc.PeerConnection
	send     *webrtc.TrackLocalStaticSample
	joined   bool
	peerIDs  map[string]struct{}
	api      *webrtc.API

	// OnRemoteFrame delivers Opus from StreamID "wt-<fromID>".
	OnRemoteFrame func(fromID string, payload []byte)
	// OnJoined is called once after the SFU PeerConnection is up.
	OnJoined func()
}

type offerRequest struct {
	Sender string `json:"sender"`
	SDP    string `json:"sdp"`
}

type offerResponse struct {
	SDP string `json:"sdp"`
}

// SetEndpoint sets the Base Station SFU host:port (from mDNS relay= TXT).
func (c *Client) SetEndpoint(host string, port int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.host = host
	c.port = port
}

// Endpoint returns the configured SFU address.
func (c *Client) Endpoint() (host string, port int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.host, c.port
}

// HasEndpoint reports whether a Base Station SFU address is known.
func (c *Client) HasEndpoint() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.host != "" && c.port != 0
}

// DialViaRelay ensures we are joined to the SFU and marks peerID as
// reachable via the hub. Safe to call repeatedly.
func (c *Client) DialViaRelay(peerID string) error {
	c.mu.Lock()
	if c.peerIDs == nil {
		c.peerIDs = make(map[string]struct{})
	}
	c.peerIDs[peerID] = struct{}{}
	host, port := c.host, c.port
	joined := c.joined
	c.mu.Unlock()

	if host == "" || port == 0 {
		return fmt.Errorf("relay: no Base Station SFU endpoint known")
	}
	if joined {
		return nil
	}
	return c.join(host, port)
}

// ConnectedViaRelay reports whether peerID was registered through DialViaRelay
// and the SFU session is up.
func (c *Client) ConnectedViaRelay(peerID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.peerIDs[peerID]
	return ok && c.joined
}

// Broadcast writes one Opus frame to the SFU send track.
func (c *Client) Broadcast(frame []byte) {
	c.mu.Lock()
	send := c.send
	c.mu.Unlock()
	if send == nil {
		return
	}
	_ = send.WriteSample(media.Sample{Data: frame, Duration: opusFrameDuration})
}

// Close tears down the SFU PeerConnection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pc != nil {
		_ = c.pc.Close()
		c.pc = nil
	}
	c.send = nil
	c.joined = false
	c.peerIDs = make(map[string]struct{})
}

func (c *Client) join(host string, port int) error {
	c.mu.Lock()
	if c.joined {
		c.mu.Unlock()
		return nil
	}
	if c.api == nil {
		m := webrtc.MediaEngine{}
		if err := m.RegisterDefaultCodecs(); err != nil {
			c.mu.Unlock()
			return err
		}
		c.api = webrtc.NewAPI(webrtc.WithMediaEngine(&m))
	}
	api := c.api
	selfID := c.SelfID
	c.mu.Unlock()

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return fmt.Errorf("relay client: new pc: %w", err)
	}
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"wt-"+selfID,
	)
	if err != nil {
		_ = pc.Close()
		return err
	}
	if _, err := pc.AddTrack(track); err != nil {
		_ = pc.Close()
		return err
	}

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		fromID := remote.StreamID()
		if len(fromID) > 3 && fromID[:3] == "wt-" {
			fromID = fromID[3:]
		}
		go func(id string, r *webrtc.TrackRemote) {
			for {
				pkt, _, err := r.ReadRTP()
				if err != nil {
					return
				}
				if c.OnRemoteFrame != nil {
					c.OnRemoteFrame(id, pkt.Payload)
				}
			}
		}(fromID, remote)
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		_ = pc.Close()
		return err
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		_ = pc.Close()
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-gatherComplete:
	case <-ctx.Done():
		_ = pc.Close()
		return fmt.Errorf("relay client: ICE gather timeout")
	}

	answerSDP, err := postOffer(ctx, host, port, selfID, pc.LocalDescription().SDP)
	if err != nil {
		_ = pc.Close()
		return err
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		_ = pc.Close()
		return err
	}

	c.mu.Lock()
	c.pc = pc
	c.send = track
	c.joined = true
	c.mu.Unlock()

	if c.OnJoined != nil {
		c.OnJoined()
	}
	return nil
}

func postOffer(ctx context.Context, host string, port int, senderID, offerSDP string) (string, error) {
	body, err := json.Marshal(offerRequest{Sender: senderID, SDP: offerSDP})
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("http://%s:%d/offer", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("relay client: post offer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("relay client: %s returned %s", url, resp.Status)
	}
	var res offerResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return res.SDP, nil
}
