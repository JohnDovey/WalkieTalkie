package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/JohnDovey/WalkieTalkie/meshsniff/config"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/engine"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/graph"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/icmp"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/netinfo"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/portmem"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/ver"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/web"
)

func main() {
	dataDirFlag := flag.String("data-dir", "", "override MeshSniff data directory")
	flag.Parse()

	dataDir := *dataDirFlag
	if dataDir == "" {
		var err error
		dataDir, err = config.DataDir()
		if err != nil {
			log.Fatal(err)
		}
	}
	_ = os.MkdirAll(dataDir, 0o755)
	cfgPath := filepath.Join(dataDir, "settings.json")
	settings, err := config.Load(cfgPath)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	g := graph.NewStore()
	portsMem, err := portmem.Open(dataDir)
	if err != nil {
		log.Printf("meshsniff: known-ports load failed (%v); renaming and starting empty", err)
		_ = os.Rename(filepath.Join(dataDir, "known-ports.json"), filepath.Join(dataDir, "known-ports.json.bak"))
		portsMem, err = portmem.Open(dataDir)
		if err != nil {
			log.Fatal(err)
		}
	}
	eng := &engine.Engine{Settings: settings, Graph: g, Ports: portsMem}
	go eng.Run(ctx)

	h := &web.Handlers{Graph: g, Engine: eng}
	mux := http.NewServeMux()
	h.Register(mux)

	addr := settings.ListenAddr()
	srv := &http.Server{Addr: addr, Handler: mux}

	printStartupBanner(startupInfo{
		Version:         ver.Version,
		BindHost:        settings.BindHost,
		StatusPort:      settings.StatusPort,
		LocalBaseURL:    settings.LocalBaseURL,
		MeshBridgeURL:   settings.MeshBridgeURL,
		ScanIntervalSec: settings.ScanIntervalSec,
		ScanCIDRs:       settings.ScanCIDRs,
		ICMPEnabled:     icmp.Enabled(),
		DataDir:         dataDir,
		LANIPs:          netinfo.ThisMachine().IPs,
	})

	go func() {
		log.Printf("web UI listening on http://%s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	shutdown, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	_ = srv.Shutdown(shutdown)
}
