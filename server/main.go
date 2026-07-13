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
	"github.com/JohnDovey/WalkieTalkie/server/api"
	"github.com/JohnDovey/WalkieTalkie/server/audio"
	"github.com/JohnDovey/WalkieTalkie/server/web"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func platformName() string {
	return "desktop-" + runtime.GOOS
}

// loadOrCreateDeviceID persists a UUID for this install so the device's
// identity survives restarts (see the plan: ID is generated once per
// install, not MAC-derived).
func loadOrCreateDeviceID(dataDir string) (string, error) {
	path := filepath.Join(dataDir, "device-id")
	if raw, err := os.ReadFile(path); err == nil {
		return string(raw), nil
	}
	id := uuid.NewString()
	if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func main() {
	dataDirFlag := flag.String("data-dir", "", "override the app data directory (default: OS-appropriate per-user dir); mainly for running multiple instances on one machine during development")
	portFlag := flag.Int("port", 0, "override the web UI/API port for this run (default: persisted setting, or 9091 on first run)")
	nameFlag := flag.String("name", "", "override this node's display name (default: \"Base Station: <hostname>\")")
	noAudio := flag.Bool("no-audio", false, "skip mic/speaker init (registry+signaling only); useful when testing multiple instances against one machine's audio hardware")
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

	selfID, err := loadOrCreateDeviceID(dataDir)
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
	if *noAudio {
		log.Printf("audio disabled via --no-audio, running registry/relay-only")
	} else {
		source, sink, err = audio.NewLocalIO()
		if err != nil {
			log.Printf("audio unavailable, running registry/relay-only (no mic/speaker): %v", err)
		}
	}

	session, err := media.NewPTTSession(selfID, source, sink)
	if err != nil {
		log.Fatalf("create PTT session: %v", err)
	}
	session.OnConnectionStateChange = func(peerID string, state webrtc.PeerConnectionState) {
		log.Printf("peer %s connection state: %s", peerID, state)
	}

	sigPort, err := session.Start(0)
	if err != nil {
		log.Fatalf("start signaling: %v", err)
	}

	now := time.Now()
	if err := store.UpsertFromDirectContact(selfID, selfName, platformName(), []string{"audio"}, "direct", now); err != nil {
		log.Fatalf("register self: %v", err)
	}

	mdnsSrv, err := mdns.Register(mdns.AnnounceInfo{
		ID:         selfID,
		Name:       selfName,
		Platform:   platformName(),
		ProtoVer:   proto.Version,
		Port:       sigPort,
		SignalPort: sigPort,
	})
	if err != nil {
		log.Fatalf("mdns register: %v", err)
	}
	defer mdnsSrv.Shutdown()

	browseCtx, cancelBrowse := context.WithCancel(context.Background())
	defer cancelBrowse()
	go func() {
		err := mdns.Browse(browseCtx, selfID, func(p mdns.Peer) {
			discoveredAt := time.Now()
			caps := []string{"audio"}
			if err := store.UpsertFromDirectContact(p.ID, p.Name, p.Platform, caps, "mdns", discoveredAt); err != nil {
				log.Printf("registry upsert for discovered peer %s: %v", p.ID, err)
			}
			if len(p.IPv4) == 0 || p.SignalPort == 0 {
				return
			}
			go func() {
				if err := session.Connect(p.IPv4[0].String(), p.SignalPort, p.ID); err != nil {
					log.Printf("connect to %s (%s): %v", p.ID, p.Name, err)
				}
			}()
		})
		if err != nil {
			log.Printf("mdns browse: %v", err)
		}
	}()

	apiHandlers := &api.Handlers{Store: store}
	webHandlers, err := web.New()
	if err != nil {
		log.Fatalf("init web UI: %v", err)
	}

	mux := http.NewServeMux()
	apiHandlers.Register(mux)
	webHandlers.Register(mux)

	httpSrv := &http.Server{Handler: mux}
	apiHandlers.OnSettingsChanged = func(newSettings config.Settings) {
		log.Printf("settings changed, restarting web server on port %d", newSettings.Port)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdownCtx)
		httpSrv = &http.Server{Handler: mux}
		go serve(httpSrv, newSettings.Port)
	}

	log.Printf("%s starting — device id %s", selfName, selfID)
	serve(httpSrv, settings.Port)
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
