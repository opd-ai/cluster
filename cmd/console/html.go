package main

import (
	"html/template"
	"net/http"
	"time"

	"github.com/opd-ai/cluster/internal/uiapi"
)

// htmlTemplates contains the plain-HTML fallback pages for accessibility.
// These pages are rendered server-side, require no JavaScript, and work with
// screen readers and assistive technologies.
var htmlTemplates = template.Must(template.New("").Parse(`
{{define "base"}}<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} — Cluster Console</title>
  <style>
    :root { font-family: system-ui, sans-serif; color-scheme: dark; }
    body { background:#0d0d17; color:#ccc; max-width:960px; margin:0 auto; padding:1rem; }
    nav a { margin-right:1rem; color:#8af; }
    table { border-collapse:collapse; width:100%; }
    th,td { border:1px solid #333; padding:.4rem .8rem; text-align:left; }
    th { background:#1a1a2e; }
    .ok   { color:#5d5; }
    .fail { color:#d55; }
    pre   { background:#111; padding:1rem; overflow-x:auto; font-size:.85rem; }
    form  { margin:.5rem 0; }
    input[type=text]  { background:#111; color:#ccc; border:1px solid #444; padding:.3rem .6rem; }
    input[type=submit]{ padding:.3rem .8rem; }
  </style>
</head>
<body>
<nav>
  <a href="/html/">Cluster</a>
  <a href="/html/registry">Registry</a>
  <a href="/html/audit">Audit Log</a>
</nav>
<hr>
{{template "content" .}}
</body></html>
{{end}}

{{define "cluster"}}
{{template "base" .}}
{{define "content"}}
<h1>Cluster Status</h1>
<p>Updated: {{.Data.UpdatedAt.Format "2006-01-02 15:04:05 UTC"}}</p>
<table>
  <thead><tr><th>Node</th><th>Role</th><th>Status</th><th>Models</th><th>VRAM Used</th></tr></thead>
  <tbody>
  {{range .Data.Nodes}}
  <tr>
    <td>{{.Name}}</td>
    <td>{{.Role}}</td>
    <td class="{{if .Healthy}}ok{{else}}fail{{end}}">{{if .Healthy}}healthy{{else}}down{{end}}</td>
    <td>{{range .Models}}{{.}} {{end}}</td>
    <td>{{.VRAMUsed}} MB</td>
  </tr>
  {{else}}<tr><td colspan="5">No nodes reported</td></tr>{{end}}
  </tbody>
</table>
{{end}}
{{end}}

{{define "registry"}}
{{template "base" .}}
{{define "content"}}
<h1>Model Registry</h1>
<table>
  <thead><tr><th>Name</th><th>Tag</th><th>SHA256</th><th>Size</th><th>License</th><th>Nodes</th></tr></thead>
  <tbody>
  {{range .Data}}
  <tr>
    <td>{{.Name}}</td>
    <td>{{.Tag}}</td>
    <td><code>{{.SHA}}</code></td>
    <td>{{.SizeMB}} MB</td>
    <td>{{.License}}</td>
    <td>{{range .Nodes}}{{.}} {{end}}</td>
  </tr>
  {{else}}<tr><td colspan="6">No models</td></tr>{{end}}
  </tbody>
</table>
{{end}}
{{end}}

{{define "audit"}}
{{template "base" .}}
{{define "content"}}
<h1>Audit Log</h1>
<table>
  <thead><tr><th>Time</th><th>Actor</th><th>Role</th><th>Action</th><th>Resource</th><th>Result</th></tr></thead>
  <tbody>
  {{range .Data}}
  <tr>
    <td>{{.Time.Format "15:04:05"}}</td>
    <td>{{.Actor}}</td>
    <td>{{.Role}}</td>
    <td>{{.Action}}</td>
    <td>{{.Resource}}</td>
    <td class="{{if .OK}}ok{{else}}fail{{end}}">{{if .OK}}ok{{else}}denied{{end}}</td>
  </tr>
  {{else}}<tr><td colspan="6">No entries</td></tr>{{end}}
  </tbody>
</table>
{{end}}
{{end}}
`))

// -------------------------------------------------------------------------
// Plain-HTML route handlers (accessibility fallback)
// -------------------------------------------------------------------------

// registerHTMLRoutes adds the plain-HTML fallback routes under /html/.
func (s *Server) registerHTMLRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/html/", s.withAuth(s.htmlCluster))
	mux.HandleFunc("/html/registry", s.withAuth(s.htmlRegistry))
	mux.HandleFunc("/html/audit", s.withAuth(s.htmlAudit))
}

type htmlPage struct {
	Title string
	Data  any
}

func (s *Server) htmlCluster(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	state := s.state
	s.mu.RUnlock()
	renderHTML(w, "cluster", htmlPage{Title: "Cluster", Data: state})
}

func (s *Server) htmlRegistry(w http.ResponseWriter, r *http.Request) {
	// Proxy /v1/models and render.
	_ = r
	renderHTML(w, "registry", htmlPage{Title: "Registry", Data: []modelEntry{}})
}

func (s *Server) htmlAudit(w http.ResponseWriter, _ *http.Request) {
	entries := s.audit.Recent(200)
	renderHTML(w, "audit", htmlPage{Title: "Audit Log", Data: entries})
}

func renderHTML(w http.ResponseWriter, tmplName string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := htmlTemplates.ExecuteTemplate(w, tmplName, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// modelEntry mirrors the WASM scene type for server-side rendering.
type modelEntry struct {
	Name    string    `json:"name"`
	Tag     string    `json:"tag"`
	SHA     string    `json:"sha256"`
	SizeMB  int64     `json:"size_mb"`
	License string    `json:"license"`
	Nodes   []string  `json:"nodes"`
	AddedAt time.Time `json:"added_at"`
}

// auditPageEntry wraps auditEntry for template rendering.
type auditPageEntry = auditEntry

// uiRole re-exports for template access.
type uiRole = uiapi.Role
