// Package mobile is the gomobile-bind facade over core: gomobile only
// supports a restricted subset of Go (no generics, limited struct/
// interface support, primitive-typed function signatures — string, bool,
// numeric types, []byte, error, and other bound types), so this package
// exists to keep that constraint from leaking into the rest of core's
// design. See docs/2026-07-13-implementation-plan.md ("Monorepo layout").
//
// Node is the mobile equivalent of server/main.go's wiring: the same
// shared core (registry, mDNS discovery/announce, WebRTC mesh), with the
// platform-native mic/speaker (media.AudioSource/AudioSink), BLE scanning,
// and GPS left to the native Android/iOS shell to provide.
package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/config"
	"github.com/JohnDovey/WalkieTalkie/core/discovery/mdns"
	"github.com/JohnDovey/WalkieTalkie/core/media"
	"github.com/JohnDovey/WalkieTalkie/core/proto"
	"github.com/JohnDovey/WalkieTalkie/core/registry"
	corerelay "github.com/JohnDovey/WalkieTalkie/core/relay"
	"github.com/JohnDovey/WalkieTalkie/core/sniff"
	"github.com/JohnDovey/WalkieTalkie/core/voicenote"
)

// Node is the handle a native app holds for its whole lifetime.
type Node struct {
	selfID     string
	platform   string
	appVersion string
	sigPort    int

	store   *registry.Store
	session *media.PTTSession

	cancelBrowse context.CancelFunc

	mu           sync.Mutex
	name         string
	lastGPS      *proto.GeoPoint
	mdnsSrv      *mdns.Server
	baseURL      string // last seen Base Station http://host:apiPort
	voiceClient  *voicenote.Client
	relayClient  *corerelay.Client
	inbox        *voicenote.LocalInbox
	dataDir      string
}

// StartNode boots the shared core for one local device: opens the
// registry at dataDir, starts the WebRTC mesh's signaling listener,
// advertises this node over mDNS, and begins browsing for peers to
// connect to. source/sink are the platform-native audio bridge (mic
// capture+Opus encode / Opus decode+speaker playback) — see
// core/media.AudioSource/AudioSink, implemented in Kotlin/Swift and passed
// in via the gomobile-generated callback bindings.
func StartNode(dataDir, name, platform, appVersion string, source media.AudioSource, sink media.AudioSink) (*Node, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("mobile: create data dir: %w", err)
	}

	store, err := registry.Open(filepath.Join(dataDir, "walkietalkie.db"))
	if err != nil {
		return nil, fmt.Errorf("mobile: open registry: %w", err)
	}

	selfID, err := config.LoadOrCreateDeviceID(dataDir)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("mobile: device id: %w", err)
	}

	session, err := media.NewPTTSession(selfID, source, sink)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("mobile: create PTT session: %w", err)
	}

	sigPort, err := session.Start(0)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("mobile: start signaling: %w", err)
	}

	n := &Node{
		selfID:     selfID,
		platform:   platform,
		appVersion: appVersion,
		sigPort:    sigPort,
		store:      store,
		session:    session,
		name:       name,
		dataDir:    dataDir,
	}
	_ = store.SetMacAddresses(selfID, sniff.LocalMACs(), time.Now())
	session.SetIdentify(func() sniff.IdentifyPayload {
		n.mu.Lock()
		defer n.mu.Unlock()
		urls := map[string]string{}
		var services []sniff.Service
		if n.sigPort > 0 {
			u := fmt.Sprintf("http://127.0.0.1:%d/", n.sigPort)
			urls["signaling"] = u
			services = append(services, sniff.Service{Name: "signaling", Port: n.sigPort, URL: u})
		}
		return sniff.IdentifyPayload{
			MeshID:     n.selfID,
			Name:       n.name,
			Platform:   n.platform,
			AppVersion: n.appVersion,
			MACs:       sniff.LocalMACs(),
			GPS:        sniff.Stamp(n.lastGPS),
			URLs:       urls,
			Services:   services,
		}
	})
	inbox, err := voicenote.OpenLocalInbox(dataDir)
	if err != nil {
		n.Stop()
		return nil, fmt.Errorf("mobile: local inbox: %w", err)
	}
	n.inbox = inbox
	session.OnVoiceNoteReceived = func(meta media.VoiceNoteMeta, audio []byte) {
		note := voicenote.Note{
			ID: meta.ID, FromID: meta.FromID, ToID: meta.ToID, ChannelID: meta.ChannelID,
			CreatedAt: meta.CreatedAt, Size: meta.Size, Status: voicenote.StatusQueued,
		}
		_ = inbox.Put(note, audio)
		n.mirrorNoteToBase(note, audio)
	}

	settings := config.Default()
	if s, err := store.GetSettings(); err == nil {
		settings = s
	}
	session.SetRelayEnabled(settings.RelayEnabled)
	session.SetRelayThreshold(settings.RelayThreshold)

	rc := &corerelay.Client{SelfID: selfID}
	rc.OnRemoteFrame = func(fromID string, payload []byte) {
		if fromID == "" {
			fromID = "sfu"
		}
		if session.OnOpusFrameReceived != nil {
			session.OnOpusFrameReceived(fromID, len(payload))
		}
		if sink != nil {
			_ = sink.WriteOpusFrame(fromID, payload)
		}
	}
	rc.OnVoiceNote = func(meta corerelay.VoiceNoteMeta, audio []byte) {
		note := voicenote.Note{
			ID: meta.ID, FromID: meta.FromID, ToID: meta.ToID, ChannelID: meta.ChannelID,
			CreatedAt: meta.CreatedAt, Size: meta.Size, Status: voicenote.StatusQueued,
		}
		_ = inbox.Put(note, audio)
		n.mirrorNoteToBase(note, audio)
	}
	n.relayClient = rc
	session.SetRelay(rc)
	session.OnRelayBroadcast = func(frame []byte) {
		rc.Broadcast(frame)
	}
	session.OnRelayUnicast = func(peerID string, frame []byte) {
		// Hub route already armed via OnRelaySetRoute; same send track.
		rc.Broadcast(frame)
	}
	session.OnRelaySetRoute = func(toID string) error {
		return rc.SetRoute(toID)
	}
	session.OnRelayClearRoute = func() {
		_ = rc.ClearRoute()
	}

	if _, err := store.UpsertFromDirectContact(selfID, name, platform, appVersion, []string{"audio"}, "direct", time.Now()); err != nil {
		n.Stop()
		return nil, fmt.Errorf("mobile: register self: %w", err)
	}

	if err := n.reannounce(nil); err != nil {
		n.Stop()
		return nil, fmt.Errorf("mobile: mdns register: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	n.cancelBrowse = cancel
	go func() {
		if err := mdns.Browse(ctx, selfID, n.onPeerFound); err != nil {
			log.Printf("mobile: mdns browse: %v", err)
		}
	}()
	go n.runStaleSweep(ctx)

	return n, nil
}

// oldNodeTimeout: a device not seen in this long is permanently removed
// from this node's own registry — see runStaleSweep. Mobile-only (unlike
// the desktop Base Station, which keeps stale devices around forever for
// the web UI's Old Nodes page), since a phone wants a bounded,
// clutter-free on-device list rather than an indefinitely-retained history.
const oldNodeTimeout = 48 * time.Hour

// runStaleSweep periodically retires devices this node hasn't heard from in
// a while — see registry.Store.SweepStale and the equivalent sweep in
// server/main.go. Without this, a device that vanished without a graceful
// disconnect (out of mDNS range, crashed) stays "connected" in this node's
// own local registry forever, and (for a node that syncs with a Base
// Station) can keep re-spreading that stale status around. Also purges
// (deletes outright) any device not seen in oldNodeTimeout.
func (n *Node) runStaleSweep(ctx context.Context) {
	timeout := time.Duration(config.Default().StaleAfterSeconds) * time.Second
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			if _, err := n.store.SweepStale(n.selfID, now, timeout); err != nil {
				log.Printf("mobile: stale sweep: %v", err)
			}
			if _, err := n.store.PurgeOlderThan(n.selfID, now, oldNodeTimeout); err != nil {
				log.Printf("mobile: purge old nodes: %v", err)
			}
		}
	}
}

func (n *Node) onPeerFound(p mdns.Peer) {
	_, _ = n.store.UpsertFromDirectContact(p.ID, p.Name, p.Platform, p.AppVersion, []string{"audio"}, "mdns", time.Now())
	if p.GPS != nil {
		_ = n.store.SetLocation(p.ID, *p.GPS)
	}
	if p.PrimaryMAC != "" {
		_ = n.store.SetMacAddresses(p.ID, []string{p.PrimaryMAC}, time.Now())
	}
	if p.APIPort != 0 && len(p.IPv4) > 0 {
		n.setBaseStation(fmt.Sprintf("http://%s:%d", p.IPv4[0].String(), p.APIPort))
	}
	if p.RelayPort != 0 && len(p.IPv4) > 0 && n.relayClient != nil {
		n.relayClient.SetEndpoint(p.IPv4[0].String(), p.RelayPort)
	}
	if len(p.IPv4) == 0 || p.SignalPort == 0 {
		return
	}
	go func() {
		hosts := make([]string, len(p.IPv4))
		for i, ip := range p.IPv4 {
			hosts[i] = ip.String()
		}
		_ = n.session.ConnectAny(hosts, p.SignalPort, p.ID)
	}()
}

func (n *Node) setBaseStation(baseURL string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.baseURL = baseURL
	if n.voiceClient == nil {
		n.voiceClient = &voicenote.Client{DeviceID: n.selfID}
	}
	n.voiceClient.BaseURL = baseURL
	n.voiceClient.DeviceID = n.selfID
}

func (n *Node) client() (*voicenote.Client, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.voiceClient == nil || n.voiceClient.BaseURL == "" {
		return nil, fmt.Errorf("no Base Station reachable (need a LAN Base Station for voice notes)")
	}
	return n.voiceClient, nil
}

// BaseStationURL returns the discovered Base Station API root, or empty.
func (n *Node) BaseStationURL() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.baseURL
}

// SendVoiceNote delivers an async clip via Direct DataChannel, else SFU
// DataChannel when joined to the Hub, else Base Station HTTP upload.
// Successful Direct/SFU sends are mirrored to Base (best-effort).
func (n *Node) SendVoiceNote(toDeviceID string, audio []byte) error {
	meta := media.VoiceNoteMeta{
		ID: voicenote.NewID(), FromID: n.selfID, ToID: toDeviceID, CreatedAt: time.Now(),
	}
	if n.session != nil && n.session.DirectConnected(toDeviceID) {
		if err := n.session.SendVoiceNoteP2P(meta, audio); err == nil {
			note := voicenote.Note{
				ID: meta.ID, FromID: meta.FromID, ToID: meta.ToID,
				CreatedAt: meta.CreatedAt, Size: int64(len(audio)), Status: voicenote.StatusQueued,
			}
			_ = n.inbox.Put(note, audio)
			n.mirrorNoteToBase(note, audio)
			return nil
		}
	}
	if n.relayClient != nil && n.relayClient.Joined() {
		rmeta := corerelay.VoiceNoteMeta{
			ID: meta.ID, FromID: meta.FromID, ToID: meta.ToID, CreatedAt: meta.CreatedAt,
		}
		if err := n.relayClient.SendVoiceNote(rmeta, audio); err == nil {
			note := voicenote.Note{
				ID: meta.ID, FromID: meta.FromID, ToID: meta.ToID,
				CreatedAt: meta.CreatedAt, Size: int64(len(audio)), Status: voicenote.StatusQueued,
			}
			_ = n.inbox.Put(note, audio)
			n.mirrorNoteToBase(note, audio)
			return nil
		}
	}
	c, err := n.client()
	if err != nil {
		return err
	}
	_, err = c.Upload(toDeviceID, "", audio, "note.webm")
	return err
}

// SendChannelClip delivers a private-channel clip via Direct DC (1:1 when the
// peer is mesh-connected), SFU room fan-out when joined, or Base HTTP upload
// (which fans out one Note per other participant).
func (n *Node) SendChannelClip(channelID string, audio []byte) error {
	peerID := n.channelPeerID(channelID)
	meta := media.VoiceNoteMeta{
		ID: voicenote.NewID(), FromID: n.selfID, ToID: peerID, ChannelID: channelID, CreatedAt: time.Now(),
	}
	if peerID != "" && n.session != nil && n.session.DirectConnected(peerID) {
		if err := n.session.SendVoiceNoteP2P(meta, audio); err == nil {
			note := voicenote.Note{
				ID: meta.ID, FromID: meta.FromID, ToID: meta.ToID, ChannelID: channelID,
				CreatedAt: meta.CreatedAt, Size: int64(len(audio)), Status: voicenote.StatusQueued,
			}
			_ = n.inbox.Put(note, audio)
			n.mirrorNoteToBase(note, audio)
			return nil
		}
	}
	if n.relayClient != nil && n.relayClient.Joined() {
		// Empty ToID → Hub fans out within the focused channel room.
		rmeta := corerelay.VoiceNoteMeta{
			ID: meta.ID, FromID: meta.FromID, ChannelID: channelID, CreatedAt: meta.CreatedAt,
		}
		if err := n.relayClient.SendVoiceNote(rmeta, audio); err == nil {
			note := voicenote.Note{
				ID: meta.ID, FromID: meta.FromID, ToID: peerID, ChannelID: channelID,
				CreatedAt: meta.CreatedAt, Size: int64(len(audio)), Status: voicenote.StatusQueued,
			}
			_ = n.inbox.Put(note, audio)
			n.mirrorNoteToBase(note, audio)
			return nil
		}
	}
	c, err := n.client()
	if err != nil {
		return err
	}
	_, err = c.Upload("", channelID, audio, "note.webm")
	return err
}

// mirrorNoteToBase best-effort uploads a P2P note with a stable ID so other
// Base Stations can sync it. Failures are ignored (delivery already succeeded).
func (n *Node) mirrorNoteToBase(note voicenote.Note, audio []byte) {
	go func() {
		c, err := n.client()
		if err != nil {
			return
		}
		_, _ = c.UploadNote(note, audio, "note.webm")
	}()
}

func (n *Node) channelPeerID(channelID string) string {
	c, err := n.client()
	if err != nil {
		return ""
	}
	chs, err := c.ListChannels()
	if err != nil {
		return ""
	}
	for _, ch := range chs {
		if ch.ID == channelID {
			return ch.PeerID
		}
	}
	return ""
}

func mergeNotes(local, remote []voicenote.Note) []voicenote.Note {
	byID := make(map[string]voicenote.Note, len(local)+len(remote))
	for _, n := range remote {
		byID[n.ID] = n
	}
	for _, n := range local {
		if _, ok := byID[n.ID]; !ok {
			byID[n.ID] = n
		}
	}
	out := make([]voicenote.Note, 0, len(byID))
	for _, n := range byID {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// ListVoiceNotesJSON returns inbox notes, or the thread with withPeerID when set.
// Merges local P2P inbox with Base Station when reachable.
func (n *Node) ListVoiceNotesJSON(withPeerID string) (string, error) {
	var local []voicenote.Note
	if n.inbox != nil {
		local, _ = n.inbox.List(n.selfID, withPeerID, "")
	}
	var remote []voicenote.Note
	if c, err := n.client(); err == nil {
		remote, _ = c.Inbox(withPeerID, "")
	}
	raw, err := json.Marshal(mergeNotes(local, remote))
	return string(raw), err
}

// ListChannelNotesJSON returns clips for a private channel.
func (n *Node) ListChannelNotesJSON(channelID string) (string, error) {
	var local []voicenote.Note
	if n.inbox != nil {
		local, _ = n.inbox.List(n.selfID, "", channelID)
	}
	var remote []voicenote.Note
	if c, err := n.client(); err == nil {
		remote, _ = c.Inbox("", channelID)
	}
	raw, err := json.Marshal(mergeNotes(local, remote))
	return string(raw), err
}

// DownloadVoiceNote returns the audio bytes for a note (local first, then Base).
func (n *Node) DownloadVoiceNote(noteID string) ([]byte, error) {
	if n.inbox != nil && n.inbox.Has(noteID) {
		return n.inbox.ReadAudio(noteID)
	}
	c, err := n.client()
	if err != nil {
		return nil, err
	}
	return c.DownloadAudio(noteID)
}

// AckVoiceNote marks a note as played (local and/or Base Station).
func (n *Node) AckVoiceNote(noteID string) error {
	localOK := false
	if n.inbox != nil && n.inbox.Has(noteID) {
		if err := n.inbox.Ack(noteID); err != nil {
			return err
		}
		localOK = true
	}
	if c, err := n.client(); err == nil {
		if err := c.Ack(noteID); err != nil && !localOK {
			return err
		}
		return nil
	}
	if localOK {
		return nil
	}
	return fmt.Errorf("no Base Station reachable and note not in local inbox")
}

// DeleteVoiceNote soft-deletes a note locally and on the Base Station when present.
func (n *Node) DeleteVoiceNote(noteID string) error {
	if n.inbox != nil {
		_ = n.inbox.Delete(noteID)
	}
	if c, err := n.client(); err == nil {
		return c.Delete(noteID)
	}
	return nil
}

// InviteChannel invites a connected peer to a private channel; returns channel JSON.
func (n *Node) InviteChannel(toDeviceID string) (string, error) {
	c, err := n.client()
	if err != nil {
		return "", err
	}
	ch, err := c.Invite(toDeviceID)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(ch)
	return string(raw), err
}

// InviteChannelMore invites another peer onto an existing channel (pending until accept).
func (n *Node) InviteChannelMore(channelID, toDeviceID string) (string, error) {
	c, err := n.client()
	if err != nil {
		return "", err
	}
	ch, err := c.InviteMore(channelID, toDeviceID)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(ch)
	return string(raw), err
}

// AcceptChannel accepts a pending private-channel invite.
func (n *Node) AcceptChannel(channelID string) error {
	c, err := n.client()
	if err != nil {
		return err
	}
	return c.Accept(channelID)
}

// ListChannelsJSON returns private channels + unread counts for this device.
func (n *Node) ListChannelsJSON() (string, error) {
	c, err := n.client()
	if err != nil {
		return "[]", err
	}
	ch, err := c.ListChannels()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(ch)
	return string(raw), err
}

// CloseChannel leaves/closes a private channel.
func (n *Node) CloseChannel(channelID string) error {
	c, err := n.client()
	if err != nil {
		return err
	}
	return c.Close(channelID)
}

// FocusChannel marks a channel as currently viewed (auto-play vs queue) and
// joins the matching Hub room when this node is on the SFU (multi-party
// private SFU isolation).
func (n *Node) FocusChannel(channelID string) error {
	if n.relayClient != nil && channelID != "" {
		_ = n.relayClient.SetRoom(channelID)
	}
	c, err := n.client()
	if err != nil {
		return err
	}
	return c.Focus(channelID)
}

// BlurChannel clears channel focus and returns to the group Hub mesh room.
func (n *Node) BlurChannel(channelID string) error {
	if n.relayClient != nil {
		_ = n.relayClient.ClearRoom()
	}
	c, err := n.client()
	if err != nil {
		return err
	}
	return c.Blur(channelID)
}

// StartTalking begins transmitting mic audio to every reachable peer
// (call on PTT-button-down).
func (n *Node) StartTalking() {
	n.session.StartTalking()
}

// StartTalkingTo transmits mic audio only to peerID (private-channel 1:1 live
// Talk) over a direct mesh PeerConnection or the SFU Hub. Prefer
// IsLiveTalkAvailable first; otherwise keep using clip upload.
func (n *Node) StartTalkingTo(peerID string) {
	n.session.StartTalkingTo(peerID)
}

// StartTalkingChannel transmits mic audio to other live peers in a focused
// private channel (DirectConnected SendTo + Hub room Broadcast, no SetRoute).
// Assumes FocusChannel already ran for channelID. Falls back to StartTalkingTo
// with the channel peer when the focused set is empty.
func (n *Node) StartTalkingChannel(channelID string) {
	if n.session == nil || channelID == "" {
		return
	}
	targets := n.channelTalkTargets(channelID)
	if len(targets) == 0 {
		return
	}
	n.session.StartTalkingToPeers(targets)
}

func (n *Node) channelTalkTargets(channelID string) []string {
	c, err := n.client()
	self := n.selfID
	var targets []string
	if err == nil {
		chs, lerr := c.ListChannels()
		if lerr == nil {
			for _, ch := range chs {
				if ch.ID != channelID {
					continue
				}
				for _, id := range ch.Focused {
					if id == "" || id == self {
						continue
					}
					if n.session.LiveTalkAvailable(id) {
						targets = append(targets, id)
					}
				}
				if len(targets) == 0 && ch.PeerID != "" && ch.PeerID != self && n.session.LiveTalkAvailable(ch.PeerID) {
					targets = append(targets, ch.PeerID)
				}
				break
			}
		}
	}
	if len(targets) == 0 {
		if peer := n.channelPeerID(channelID); peer != "" && n.session.LiveTalkAvailable(peer) {
			targets = append(targets, peer)
		}
	}
	return targets
}

// StopTalking stops transmitting (call on PTT-button-up).
func (n *Node) StopTalking() {
	n.session.StopTalking()
}

// IsDirectlyConnected reports a direct WebRTC path to peerID (not SFU-only).
func (n *Node) IsDirectlyConnected(peerID string) bool {
	if n.session == nil {
		return false
	}
	return n.session.DirectConnected(peerID)
}

// IsRelayConnected reports peerID is reached via the Base Station SFU.
func (n *Node) IsRelayConnected(peerID string) bool {
	if n.session == nil {
		return false
	}
	return n.session.RelayConnected(peerID)
}

// IsLiveTalkAvailable reports private live Talk can reach peerID (direct or SFU).
func (n *Node) IsLiveTalkAvailable(peerID string) bool {
	if n.session == nil {
		return false
	}
	return n.session.LiveTalkAvailable(peerID)
}

// UpdateLocation records a new GPS fix for this device and re-announces it
// over mDNS so peers (and any Base Station dashboard watching this device)
// pick it up on their next sighting — see the mdns package's GPS-in-TXT-
// record design, which avoids needing a separate push API on nodes (like a
// phone) that don't run the full server/api. Call on a timer per the
// configured GPS update interval.
func (n *Node) UpdateLocation(lat, lon, accuracy float64) error {
	point := proto.GeoPoint{Lat: lat, Lon: lon, Accuracy: accuracy, Timestamp: time.Now()}
	if err := n.store.SetLocation(n.selfID, point); err != nil {
		return err
	}
	return n.reannounce(&point)
}

// UpdateName changes this device's display name and re-announces it. Per
// the plan, names are device-originated only.
func (n *Node) UpdateName(name string) error {
	if err := n.store.SetName(n.selfID, name, time.Now()); err != nil {
		return err
	}
	n.mu.Lock()
	n.name = name
	gps := n.lastGPS
	n.mu.Unlock()
	return n.reannounce(gps)
}

func (n *Node) reannounce(gps *proto.GeoPoint) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastGPS = gps

	info := mdns.AnnounceInfo{
		ID:         n.selfID,
		Name:       n.name,
		Platform:   n.platform,
		AppVersion: n.appVersion,
		ProtoVer:   proto.Version,
		Port:       n.sigPort,
		SignalPort: n.sigPort,
		GPS:        gps,
		PrimaryMAC: sniff.PrimaryMAC(),
	}

	if n.mdnsSrv != nil {
		n.mdnsSrv.UpdateInfo(info)
		return nil
	}

	srv, err := mdns.Register(info)
	if err != nil {
		return fmt.Errorf("mobile: mdns register: %w", err)
	}
	n.mdnsSrv = srv
	return nil
}

// ReportBLESighting feeds a BLE-discovered peer (presence-only — no GPS,
// no audio path, since BLE advertisement payloads are too small to carry
// either) into the registry. This is the concrete mechanism for "device A
// forwards device B's details to the server" when there's no shared LAN:
// the native BLE scanner calls this for every sighting, regardless of
// whether this node currently has a server connection — the sighting is
// recorded locally immediately either way (see core/discovery/ble.Bridge).
func (n *Node) ReportBLESighting(peerID, peerName, peerPlatform string, rssi int) error {
	return n.store.UpsertFromReport(n.selfID, proto.PeerSummary{
		ID:                 peerID,
		Name:               peerName,
		Platform:           peerPlatform,
		DiscoveryMethod:    "ble",
		LastSeenByReporter: time.Now(),
	})
}

// ListDevicesJSON returns every known device as a JSON array. gomobile
// can't cleanly bind arbitrary Go struct/slice return types, so — the
// standard gomobile pattern — callers decode this JSON on the native side
// rather than receiving bound Device objects directly.
func (n *Node) ListDevicesJSON() (string, error) {
	devices, err := n.store.List()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(devices)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// SelfID returns this device's stable, persisted UUID.
func (n *Node) SelfID() string {
	return n.selfID
}

// Stop tears down this node: mDNS advertisement/browsing, the WebRTC mesh
// and its signaling listener, and the registry database. Call when the app
// is shutting down (or the foreground service is stopped).
func (n *Node) Stop() error {
	if n.cancelBrowse != nil {
		n.cancelBrowse()
	}

	n.mu.Lock()
	if n.mdnsSrv != nil {
		n.mdnsSrv.Shutdown()
		n.mdnsSrv = nil
	}
	n.mu.Unlock()

	if n.session != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		n.session.Shutdown(ctx)
	}

	if n.store != nil {
		return n.store.Close()
	}
	return nil
}
