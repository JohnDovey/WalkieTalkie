package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/config"
	"github.com/JohnDovey/WalkieTalkie/meshbridge/bridge"
	mbconfig "github.com/JohnDovey/WalkieTalkie/meshbridge/config"
	"github.com/JohnDovey/WalkieTalkie/meshbridge/punch"
	"github.com/JohnDovey/WalkieTalkie/meshbridge/ver"
	"github.com/google/uuid"
)

func main() {
	dataDirFlag := flag.String("data-dir", "", "override MeshBridge data directory")
	flag.Parse()

	dataDir := *dataDirFlag
	if dataDir == "" {
		var err error
		dataDir, err = mbconfig.DataDir()
		if err != nil {
			log.Fatal(err)
		}
	}
	_ = os.MkdirAll(dataDir, 0o755)
	cfgPath := filepath.Join(dataDir, "settings.json")
	settings, err := mbconfig.Load(cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if settings.NodeID == "" {
		if appDir, aerr := config.AppDataDir(); aerr == nil {
			if id, ierr := config.LoadOrCreateDeviceID(appDir); ierr == nil {
				settings.NodeID = id + "-bridge"
			}
		}
		if settings.NodeID == "" {
			settings.NodeID = uuid.NewString()
		}
		_ = mbconfig.Save(cfgPath, settings)
	}

	printStartupBanner(startupInfo{
		Version:         ver.Version,
		BindHost:        settings.BindHost,
		StatusPort:      settings.StatusPort,
		NodeID:          settings.NodeID,
		LocalBaseURL:    settings.LocalBaseURL,
		SyncIntervalSec: settings.SyncIntervalSec,
		Manual:          settings.Manual,
		WiFi:            settings.WiFi,
		Ethernet:        settings.Ethernet,
		Punch:           settings.Punch,
		RunHub:          settings.RunHub,
		HubListenPort:   settings.HubListenPort,
		DataDir:         dataDir,
		ConfigPath:      cfgPath,
		LANIPs:          localLANIPs(),
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if settings.RunHub {
		hub, err := punch.Listen(settings.HubListenPort)
		if err != nil {
			log.Fatalf("punch hub: %v", err)
		}
		defer hub.Close()
	}

	for _, pb := range settings.Punch {
		if pb.RoleHub && !settings.RunHub {
			hub, err := punch.Listen(pb.ListenPort)
			if err != nil {
				log.Printf("punch hub for %s: %v", pb.Name, err)
			} else {
				defer hub.Close()
			}
		}
		if pb.HubHost == "" || pb.PeerID == "" {
			continue
		}
		cli := &punch.Client{SelfID: settings.NodeID, HubHost: pb.HubHost, HubPort: pb.HubPort}
		local := &bridge.LocalClient{BaseURL: settings.LocalBaseURL}
		cli.OnPeer = func(peerID string, addr *net.UDPAddr) {
			log.Printf("meshbridge: punched peer %s at %s", peerID, addr)
			_ = cli.SendBaseURL(settings.LocalBaseURL, settings.NodeID, "MeshBridge")
		}
		cli.OnBaseURL = func(peerID string, payload punch.BaseURLPayload) {
			log.Printf("meshbridge: peer %s advertised Base %s", peerID, payload.URL)
			if _, _, _, err := local.SyncRemoteBase(payload.URL, payload.ID, payload.Name); err != nil {
				log.Printf("meshbridge punch sync: %v", err)
			}
		}
		if err := cli.Start(ctx); err != nil {
			log.Printf("punch client: %v", err)
			continue
		}
		peer := pb.PeerID
		go func() {
			time.Sleep(time.Second)
			_ = cli.Connect(peer)
		}()
		defer cli.Close()
	}

	pipe := &bridge.Pipeline{
		Settings: settings,
		Local:    &bridge.LocalClient{BaseURL: settings.LocalBaseURL},
		NodeID:   settings.NodeID,
	}
	go pipe.Run(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><title>MeshBridge %s</title>
<h1>MeshBridge %s</h1>
<p>Local Base: %s</p>
<p><a href="/api/status">/api/status</a> · <a href="/api/inventory">/api/inventory</a> · <a href="/sniff">/sniff</a></p>
<p>Config: %s</p>`, ver.Version, ver.Version, settings.LocalBaseURL, cfgPath)
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":      ver.Version,
			"nodeId":       settings.NodeID,
			"localBaseURL": settings.LocalBaseURL,
			"transports":   pipe.StatusSnapshot(),
			"runHub":       settings.RunHub,
		})
	})
	mux.HandleFunc("GET /api/inventory", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pipe.InventorySnapshot())
	})
	mux.HandleFunc("GET /sniff", func(w http.ResponseWriter, r *http.Request) {
		statusURL := fmt.Sprintf("http://127.0.0.1:%d/", settings.StatusPort)
		if host := requestHost(r); host != "" && host != "127.0.0.1" && host != "localhost" {
			statusURL = fmt.Sprintf("http://%s/", net.JoinHostPort(host, strconv.Itoa(settings.StatusPort)))
		} else if ips := localLANIPs(); len(ips) > 0 {
			statusURL = fmt.Sprintf("http://%s/", net.JoinHostPort(ips[0], strconv.Itoa(settings.StatusPort)))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"meshId":     settings.NodeID,
			"name":       "MeshBridge",
			"platform":   "meshbridge",
			"appVersion": ver.Version,
			"urls": map[string]string{
				"status": statusURL,
			},
			"services": []map[string]any{
				{"name": "status", "port": settings.StatusPort, "url": statusURL},
			},
		})
	})

	addr := settings.ListenAddr()
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("meshbridge status http://%s (config %s)", addr, cfgPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	shutdown, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	_ = srv.Shutdown(shutdown)
}

func localLANIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipNet.IP.To4()
			if v4 == nil || v4.IsLoopback() || !v4.IsPrivate() {
				continue
			}
			s := v4.String()
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func requestHost(r *http.Request) string {
	h := r.Host
	if h == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return strings.Trim(h, "[]")
}
