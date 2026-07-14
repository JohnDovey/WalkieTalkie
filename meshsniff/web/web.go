package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/JohnDovey/WalkieTalkie/meshsniff/graph"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/ver"
)

//go:embed static/*
var staticFS embed.FS

// Handlers serves the MeshSniff UI and graph APIs.
type Handlers struct {
	Graph *graph.Store
}

// Register attaches routes to mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("GET /{$}", h.index)
	mux.HandleFunc("GET /api/graph", h.graphJSON)
	mux.HandleFunc("GET /api/events", h.events)
	mux.HandleFunc("GET /api/status", h.status)
	mux.HandleFunc("GET /sniff", h.selfSniff)
}

func (h *Handlers) index(w http.ResponseWriter, r *http.Request) {
	b, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (h *Handlers) graphJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.Graph.Snapshot())
}

func (h *Handlers) status(w http.ResponseWriter, r *http.Request) {
	snap := h.Graph.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version":   ver.Version,
		"status":    snap.Status,
		"nodeCount": len(snap.Nodes),
		"edgeCount": len(snap.Edges),
		"updatedAt": snap.UpdatedAt,
	})
}

func (h *Handlers) selfSniff(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"meshId":     "meshsniff-local",
		"name":       "MeshSniff",
		"platform":   "meshsniff",
		"appVersion": ver.Version,
		"services":   []map[string]any{{"name": "ui", "port": 0}},
	})
}

func (h *Handlers) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var last int64
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			seq := h.Graph.Seq()
			if seq == last {
				fmt.Fprintf(w, ": ping\n\n")
				flusher.Flush()
				continue
			}
			last = seq
			snap := h.Graph.Snapshot()
			raw, _ := json.Marshal(snap)
			fmt.Fprintf(w, "event: graph\ndata: %s\n\n", raw)
			flusher.Flush()
		}
	}
}
