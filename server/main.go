// Command server is the Go desktop app for Mac/Windows/Linux — and the "Go
// server" referenced throughout the spec. It's a single process that: hosts
// the device registry + Bootstrap/jQuery web UI (default port 9091, no
// auth), discovers other nodes via mDNS, and is itself a full talk/listen
// participant in the PTT mesh (see docs/2026-07-13-implementation-plan.md).
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/config"
	"github.com/JohnDovey/WalkieTalkie/core/discovery/mdns"
	"github.com/JohnDovey/WalkieTalkie/core/media"
	"github.com/JohnDovey/WalkieTalkie/core/proto"
	"github.com/JohnDovey/WalkieTalkie/core/registry"
	corerelay "github.com/JohnDovey/WalkieTalkie/core/relay"
	"github.com/JohnDovey/WalkieTalkie/server/api"
	"github.com/JohnDovey/WalkieTalkie/server/audio"
	"github.com/JohnDovey/WalkieTalkie/server/relay"
	"github.com/JohnDovey/WalkieTalkie/server/sync"
	"github.com/JohnDovey/WalkieTalkie/server/usage"
	"github.com/JohnDovey/WalkieTalkie/server/voicenote"
	"github.com/JohnDovey/WalkieTalkie/server/web"
	"github.com/pion/webrtc/v4"
)

func platformName() string {
	return "desktop-" + runtime.GOOS
}

func main() {
	dataDirFlag := flag.String("data-dir", "", "override the app data directory (default: OS-appropriate per-user dir); mainly for running multiple instances on one machine during development")
	portFlag := flag.Int("port", 0, "override the web UI/API port for this run (default: persisted setting, or 9091 on first run)")
	nameFlag := flag.String("name", "", "override this node's display name (default: \"Base Station: <hostname>\")")
	noAudio := flag.Bool("no-audio", false, "skip mic/speaker init (registry+signaling only); useful when testing multiple instances against one machine's audio hardware")
	noTray := flag.Bool("no-tray", false, "disable the system tray icon (for headless/CI/SSH runs)")
	flag.Parse()

	dataDir := *dataDirFlag
	if dataDir == "" {
		var err error
		dataDir, err = config.AppDataDir()
		if err != nil {
			log.Fatalf("resolve app data dir: %v", err)
		}
	} else if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("create data dir %s: %v", dataDir, err)
	}

	store, err := registry.Open(filepath.Join(dataDir, "walkietalkie.db"))
	if err != nil {
		log.Fatalf("open registry: %v", err)
	}
	defer store.Close()

	settings, err := store.GetSettings()
	if err != nil {
		log.Fatalf("load settings: %v", err)
	}
	if *portFlag != 0 {
		settings.Port = *portFlag
	}

	selfID, err := config.LoadOrCreateDeviceID(dataDir)
	if err != nil {
		log.Fatalf("load device id: %v", err)
	}

	selfName := *nameFlag
	if selfName == "" {
		hostname, _ := os.Hostname()
		selfName = "Base Station: " + hostname
	}

	// Audio: mic capture + Opus encode / decode + speaker playback, native
	// to this desktop process (cgo is fine here — see server/audio).
	var source media.AudioSource
	var sink media.AudioSink
	audioDisabled := *noAudio
	if *noAudio {
		log.Printf("audio disabled via --no-audio, running registry/relay-only")
	} else {
		source, sink, err = audio.NewLocalIO()
		if err != nil {
			log.Printf("audio unavailable, running registry/relay-only (no mic/speaker): %v", err)
			audioDisabled = true
		}
	}

	session, err := media.NewPTTSession(selfID, source, sink)
	if err != nil {
		log.Fatalf("create PTT session: %v", err)
	}
	session.OnConnectionStateChange = func(peerID string, state webrtc.PeerConnectionState) {
		log.Printf("peer %s connection state: %s", peerID, state)
	}
	session.SetRelayThreshold(settings.RelayThreshold)
	session.SetRelayEnabled(settings.RelayEnabled)
	session.OnRelayThresholdExceeded = func(count, threshold int) {
		log.Printf("relay threshold exceeded (%d connected peers >= %d) but relay dialer unavailable — still connecting directly", count, threshold)
	}

	hub, err := corerelay.NewHub()
	if err != nil {
		log.Fatalf("init SFU hub: %v", err)
	}
	defer hub.Close()
	hub.OnParticipantJoined = session.MarkRelayPeer
	var voiceStore *voicenote.Store
	hub.OnRemoteFrame = func(fromID string, payload []byte) {
		session.MarkRelayPeer(fromID)
		if session.OnOpusFrameReceived != nil {
			session.OnOpusFrameReceived(fromID, len(payload))
		}
		// Private unicast to someone else must not play on the Base Station.
		// If the target is DirectConnected but not on the Hub, bridge Hub→direct.
		if to, routed := hub.RouteOf(fromID); routed && to != selfID {
			if !hub.Has(to) && session.DirectConnected(to) {
				_ = session.SendTo(to, payload)
			}
			return
		}
		// Named Hub rooms: bridge room Opus to DirectConnected participants
		// who are not on the Hub, and skip Base Station speaker when we are
		// not focused on that channel room.
		if room := hub.RoomOf(fromID); room != "" {
			bridgeRoomToDirect(voiceStore, hub, session.MeshManager, fromID, room, selfID, payload)
			if hub.RoomOf(selfID) != room {
				return
			}
		}
		if sink != nil {
			_ = sink.WriteOpusFrame(fromID, payload)
		}
	}
	hostBridge := &corerelay.HostBridge{
		Hub:    hub,
		SelfID: selfID,
		Mark:   session.MarkRelayPeer,
	}
	session.SetRelay(hostBridge)
	session.OnRelayBroadcast = func(frame []byte) {
		hub.InjectLocal(selfID, frame)
	}
	session.OnRelayUnicast = func(peerID string, frame []byte) {
		hub.InjectTo(selfID, peerID, frame)
	}
	session.OnRelaySetRoute = func(toID string) error {
		hub.SetRoute(selfID, toID)
		return nil
	}
	session.OnRelayClearRoute = func() {
		hub.ClearRoute(selfID)
	}

	relaySrv := relay.New(hub)
	relayPort, err := relaySrv.Start(0)
	if err != nil {
		log.Fatalf("start SFU relay: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = relaySrv.Shutdown(ctx)
	}()
	log.Printf("SFU relay listening on port %d", relayPort)

	usageStats, err := usage.NewStore(store)
	if err != nil {
		log.Fatalf("init usage stats: %v", err)
	}
	defer usageStats.Close()

	session.OnTalkStarted = func() { usageStats.PTTSession() }
	session.OnOpusFrameSent = func(frameBytes, peerCount int) {
		usageStats.PTTBytesSent(int64(frameBytes) * int64(peerCount))
	}
	session.OnOpusFrameReceived = func(_ string, frameBytes int) {
		usageStats.PTTBytesReceived(int64(frameBytes))
	}

	sigPort, err := session.Start(0)
	if err != nil {
		log.Fatalf("start signaling: %v", err)
	}

	now := time.Now()
	selfCreated, err := store.UpsertFromDirectContact(selfID, selfName, platformName(), Version, []string{"audio"}, "direct", now)
	if err != nil {
		log.Fatalf("register self: %v", err)
	}
	usageStats.DeviceSeen(selfCreated)

	mdnsSrv, err := mdns.Register(mdns.AnnounceInfo{
		ID:         selfID,
		Name:       selfName,
		Platform:   platformName(),
		AppVersion: Version,
		ProtoVer:   proto.Version,
		Port:       sigPort,
		SignalPort: sigPort,
		APIPort:    settings.Port, // marks this node as a Base Station for peer sync — see server/sync
		RelayPort:  relayPort,
	})
	if err != nil {
		log.Fatalf("mdns register: %v", err)
	}
	defer mdnsSrv.Shutdown()

	announce := func(port int) mdns.AnnounceInfo {
		return mdns.AnnounceInfo{
			ID:         selfID,
			Name:       selfName,
			Platform:   platformName(),
			AppVersion: Version,
			ProtoVer:   proto.Version,
			Port:       sigPort,
			SignalPort: sigPort,
			APIPort:    port,
			RelayPort:  relayPort,
		}
	}

	syncer := sync.New(store, time.Duration(settings.SyncIntervalSeconds)*time.Second)
	defer syncer.Stop()

	staleCtx, cancelStaleSweep := context.WithCancel(context.Background())
	defer cancelStaleSweep()
	go runStaleSweep(staleCtx, store, selfID)

	browseCtx, cancelBrowse := context.WithCancel(context.Background())
	defer cancelBrowse()
	go func() {
		err := mdns.Browse(browseCtx, selfID, func(p mdns.Peer) {
			discoveredAt := time.Now()
			caps := []string{"audio"}
			created, err := store.UpsertFromDirectContact(p.ID, p.Name, p.Platform, p.AppVersion, caps, "mdns", discoveredAt)
			if err != nil {
				log.Printf("registry upsert for discovered peer %s: %v", p.ID, err)
			} else if usageStats != nil {
				usageStats.DeviceSeen(created)
			}
			if p.GPS != nil {
				if err := store.SetLocation(p.ID, *p.GPS); err != nil {
					log.Printf("registry location update for %s: %v", p.ID, err)
				}
				updateServerLocationEstimate(store, selfID)
			}
			if p.APIPort != 0 && len(p.IPv4) > 0 {
				syncer.EnsureSyncing(p.ID, p.IPv4[0].String(), p.APIPort)
			}
			if len(p.IPv4) == 0 || p.SignalPort == 0 {
				return
			}
			go func() {
				hosts := make([]string, len(p.IPv4))
				for i, ip := range p.IPv4 {
					hosts[i] = ip.String()
				}
				if err := session.ConnectAny(hosts, p.SignalPort, p.ID); err != nil {
					log.Printf("connect to %s (%s): %v", p.ID, p.Name, err)
				}
			}()
		})
		if err != nil {
			log.Printf("mdns browse: %v", err)
		}
	}()

	voiceStore, err = voicenote.NewStore(store, dataDir)
	if err != nil {
		log.Fatalf("init voice-note store: %v", err)
	}
	session.OnVoiceNoteReceived = func(meta media.VoiceNoteMeta, audio []byte) {
		n, err := voiceStore.ImportNote(meta.ID, meta.FromID, meta.ToID, meta.ChannelID, meta.CreatedAt, audio)
		if err != nil {
			log.Printf("P2P voice note from %s: %v", meta.FromID, err)
			return
		}
		if usageStats != nil {
			usageStats.VoiceNoteUploaded(n.Size, n.ChannelID)
		}
		log.Printf("P2P voice note %s stored (%d bytes)", n.ID, n.Size)
		pushVoiceNoteToDirect(session.MeshManager, n, audio)
	}
	syncer.SetVoice(voiceStore)
	voiceHandlers := &voicenote.Handlers{
		Voice: voiceStore, Reg: store, SelfID: selfID, Usage: usageStats,
		OnSelfHubRoom: func(roomID string) {
			hostBridge.SetRoom(roomID)
		},
		Pusher: session,
	}
	purgeStop := make(chan struct{})
	defer close(purgeStop)
	go voicenote.RunPurge(purgeStop, voiceStore)
	go runGPSHistoryPurge(purgeStop, store)

	usageHandlers := &usage.Handlers{Usage: usageStats, Reg: store}

	apiHandlers := &api.Handlers{
		Store: store, Talker: session, SelfID: selfID, SelfName: selfName,
		Platform: platformName(), Version: Version, EnrichDevices: voiceHandlers,
		OnDeviceSeen: usageStats.DeviceSeen,
		OnLocationUpdated: func(string) {
			updateServerLocationEstimate(store, selfID)
		},
		ChannelTalkPeers: func(channelID string) []string {
			return channelLiveTalkPeers(voiceStore, session.MeshManager, selfID, channelID)
		},
	}
	webHandlers, err := web.New()
	if err != nil {
		log.Fatalf("init web UI: %v", err)
	}

	mux := http.NewServeMux()
	apiHandlers.Register(mux)
	voiceHandlers.Register(mux)
	usageHandlers.Register(mux)
	webHandlers.Register(mux)

	httpSrv := &http.Server{Handler: mux}
	apiHandlers.OnSettingsChanged = func(newSettings config.Settings) {
		log.Printf("settings changed, restarting web server on port %d", newSettings.Port)
		session.SetRelayThreshold(newSettings.RelayThreshold)
		session.SetRelayEnabled(newSettings.RelayEnabled)
		syncer.SetInterval(time.Duration(newSettings.SyncIntervalSeconds) * time.Second)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdownCtx)
		httpSrv = &http.Server{Handler: mux}
		go serve(httpSrv, newSettings.Port)

		mdnsSrv.UpdateInfo(announce(newSettings.Port))
	}

	_ = noTray
	dashboardPort.Store(int32(settings.Port))

	apiHandlers.OnSettingsChanged = func(newSettings config.Settings) {
		log.Printf("settings changed, restarting web server on port %d", newSettings.Port)
		session.SetRelayThreshold(newSettings.RelayThreshold)
		session.SetRelayEnabled(newSettings.RelayEnabled)
		syncer.SetInterval(time.Duration(newSettings.SyncIntervalSeconds) * time.Second)
		dashboardPort.Store(int32(newSettings.Port))
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdownCtx)
		httpSrv = &http.Server{Handler: mux}
		go serve(httpSrv, newSettings.Port)

		mdnsSrv.UpdateInfo(announce(newSettings.Port))
	}

	printStartupBanner(startupInfo{
		Version:             Version,
		Name:                selfName,
		DeviceID:            selfID,
		Platform:            platformName(),
		WebPort:             settings.Port,
		SignalPort:          sigPort,
		RelayPort:           relayPort,
		DataDir:             dataDir,
		AudioDisabled:       audioDisabled,
		RelayEnabled:        settings.RelayEnabled,
		RelayThreshold:      settings.RelayThreshold,
		SyncIntervalSeconds: settings.SyncIntervalSeconds,
	})
	if *noTray {
		serve(httpSrv, settings.Port)
		return
	}
	go serve(httpSrv, settings.Port)
	startSystemTray()
}

// updateServerLocationEstimate recomputes this Base Station's own GPS
// position as the arithmetic mean of every currently-connected device's
// location, per docs/2026-07-13-implementation-plan.md ("Server GPS
// estimation") — most server machines have no GPS hardware of their own.
// If no connected device currently reports a location, the server's
// existing estimate (if any) is left alone rather than blanked out; it
// just becomes stale, which Device.LastKnownLocation already models.
func updateServerLocationEstimate(store *registry.Store, selfID string) {
	devices, err := store.List()
	if err != nil {
		log.Printf("location estimate: list devices: %v", err)
		return
	}

	var sumLat, sumLon, sumAcc float64
	var n int
	for _, d := range devices {
		if d.ID == selfID || d.Status != registry.StatusConnected || d.CurrentLocation == nil {
			continue
		}
		sumLat += d.CurrentLocation.Lat
		sumLon += d.CurrentLocation.Lon
		sumAcc += d.CurrentLocation.Accuracy
		n++
	}
	if n == 0 {
		return
	}

	mean := proto.GeoPoint{
		Lat:       sumLat / float64(n),
		Lon:       sumLon / float64(n),
		Accuracy:  sumAcc / float64(n),
		Timestamp: time.Now(),
	}
	if err := store.SetLocation(selfID, mean); err != nil {
		log.Printf("location estimate: set self location: %v", err)
	}
}

// runStaleSweep periodically retires devices that have gone quiet without a
// graceful disconnect — see registry.Store.SweepStale. Re-reads
// StaleAfterSeconds from settings on every tick so a live settings change
// takes effect without a restart.
func runStaleSweep(ctx context.Context, store *registry.Store, selfID string) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			settings, err := store.GetSettings()
			if err != nil {
				log.Printf("stale sweep: load settings: %v", err)
				continue
			}
			timeout := time.Duration(settings.StaleAfterSeconds) * time.Second
			swept, err := store.SweepStale(selfID, time.Now(), timeout)
			if err != nil {
				log.Printf("stale sweep: %v", err)
				continue
			}
			if swept > 0 {
				log.Printf("stale sweep: marked %d device(s) disconnected", swept)
			}
		}
	}
}

func runGPSHistoryPurge(stop <-chan struct{}, store *registry.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if n, err := store.PurgeGPSHistory(time.Now()); err == nil && n > 0 {
				log.Printf("gps history: purged %d sample(s)", n)
			}
		}
	}
}

func bridgeRoomToDirect(
	voice *voicenote.Store,
	hub *corerelay.Hub,
	session *media.MeshManager,
	fromID, room, selfID string,
	payload []byte,
) {
	if voice == nil || room == "" {
		return
	}
	ch, ok, err := voice.GetChannel(room)
	if err != nil || !ok || ch == nil {
		return
	}
	seen := map[string]struct{}{}
	var candidates []string
	add := func(id string) {
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		candidates = append(candidates, id)
	}
	for _, id := range ch.Focused {
		add(id)
	}
	add(ch.ParticipantA)
	add(ch.ParticipantB)
	for _, id := range ch.OtherParticipants(fromID) {
		add(id)
	}
	for _, id := range candidates {
		if id == fromID || id == selfID {
			continue
		}
		if !hub.Has(id) && session.DirectConnected(id) {
			_ = session.SendTo(id, payload)
		}
	}
}

func channelLiveTalkPeers(voice *voicenote.Store, session *media.MeshManager, selfID, channelID string) []string {
	if voice == nil || session == nil || channelID == "" {
		return nil
	}
	ch, ok, err := voice.GetChannel(channelID)
	if err != nil || !ok || ch == nil {
		return nil
	}
	var targets []string
	seen := map[string]struct{}{}
	add := func(id string) {
		if id == "" || id == selfID {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		if !session.LiveTalkAvailable(id) {
			return
		}
		seen[id] = struct{}{}
		targets = append(targets, id)
	}
	for _, id := range ch.Focused {
		add(id)
	}
	if len(targets) == 0 {
		for _, id := range ch.OtherParticipants(selfID) {
			add(id)
		}
	}
	return targets
}

func pushVoiceNoteToDirect(session *media.MeshManager, n *voicenote.Note, audio []byte) {
	if session == nil || n == nil || n.ToID == "" {
		return
	}
	if !session.DirectConnected(n.ToID) {
		return
	}
	meta := media.VoiceNoteMeta{
		ID: n.ID, FromID: n.FromID, ToID: n.ToID, ChannelID: n.ChannelID, CreatedAt: n.CreatedAt,
	}
	if err := session.SendVoiceNoteP2P(meta, audio); err != nil {
		log.Printf("voice note bridge to %s: %v", n.ToID, err)
	}
}

func serve(srv *http.Server, port int) {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		log.Fatalf("listen on port %d: %v", port, err)
	}
	log.Printf("web UI listening on http://0.0.0.0:%d", port)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}
