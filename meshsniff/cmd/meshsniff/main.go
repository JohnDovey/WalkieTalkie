package main

import (
	"context"
	"flag"
	"fmt"
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
	"github.com/JohnDovey/WalkieTalkie/meshsniff/ver"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/web"
)

func main() {
	dataDirFlag := flag.String("data-dir", "", "override MeshSniff data directory")
	flag.Parse()

	fmt.Printf("MeshSniff %s — network discovery map\n", ver.Version)

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
	eng := &engine.Engine{Settings: settings, Graph: g}
	go eng.Run(ctx)

	h := &web.Handlers{Graph: g}
	mux := http.NewServeMux()
	h.Register(mux)

	addr := fmt.Sprintf("127.0.0.1:%d", settings.StatusPort)
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("meshsniff http://%s (config %s)", addr, cfgPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	shutdown, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	_ = srv.Shutdown(shutdown)
}
