// Package web serves the server's Bootstrap+jQuery dashboard: the device
// list, the network map, and the settings page. No authentication, per the
// spec. Bootstrap, jQuery, and Leaflet are vendored under static/vendor
// (not loaded from a CDN) so the dashboard works with no internet access —
// the whole point of a LAN-first app. The one deliberate exception is the
// map page's basemap tile imagery (OpenStreetMap), which can't be
// meaningfully vendored for arbitrary real-world locations — see
// docs/2026-07-13-implementation-plan.md ("Web UI: network map").
package web

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Handlers serves the dashboard pages and static assets.
type Handlers struct {
	tmpl *template.Template
}

// New parses the embedded templates.
func New() (*Handlers, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Handlers{tmpl: tmpl}, nil
}

// Register attaches every route to mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.index)
	mux.HandleFunc("GET /map", h.mapPage)
	mux.HandleFunc("GET /old-nodes", h.oldNodesPage)
	mux.HandleFunc("GET /stats", h.statsPage)
	mux.HandleFunc("GET /settings", h.settingsPage)
	mux.HandleFunc("GET /about", h.aboutPage)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
}

func (h *Handlers) index(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "index.html", nil)
}

func (h *Handlers) mapPage(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "map.html", nil)
}

func (h *Handlers) settingsPage(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "settings.html", nil)
}

func (h *Handlers) aboutPage(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "about.html", nil)
}

func (h *Handlers) oldNodesPage(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "old-nodes.html", nil)
}

func (h *Handlers) statsPage(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "stats.html", nil)
}
