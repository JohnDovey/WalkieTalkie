package usage

import (
	"encoding/json"
	"net/http"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
)

// Handlers exposes GET /api/stats.
type Handlers struct {
	Usage *Store
	Reg   *registry.Store
}

// Register attaches stats routes.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stats", h.getStats)
}

func (h *Handlers) getStats(w http.ResponseWriter, r *http.Request) {
	rangeName := r.URL.Query().Get("range")
	if rangeName == "" {
		rangeName = "today"
	}
	devices, err := h.Reg.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	snap, err := h.Usage.Query(rangeName, len(devices))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snap)
}
