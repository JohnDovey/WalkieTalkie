// Package web serves the server's Bootstrap+jQuery dashboard: the device
// list and the settings page. No authentication, per the spec. Bootstrap
// and jQuery are vendored under static/vendor (not loaded from a CDN) so
// the dashboard works with no internet access — the whole point of a
// LAN-first app.
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
	mux.HandleFunc("GET /settings", h.settingsPage)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
}

func (h *Handlers) index(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "index.html", nil)
}

func (h *Handlers) settingsPage(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "settings.html", nil)
}
