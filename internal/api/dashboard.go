package api

import (
	_ "embed"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/VenkatGGG/Browser-use/pkg/httpx"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/dashboard" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	if path, ok := resolvedDashboardDistPath(); ok {
		http.ServeFile(w, r, filepath.Join(path, "index.html"))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

func (s *Server) handleDashboardAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/assets/") {
		http.NotFound(w, r)
		return
	}
	path, ok := resolvedDashboardDistPath()
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.FileServer(http.Dir(path)).ServeHTTP(w, r)
}

func resolvedDashboardDistPath() (string, bool) {
	distEnv := strings.TrimSpace(os.Getenv("ORCHESTRATOR_DASHBOARD_DIST"))
	candidates := []string{}
	if distEnv != "" {
		candidates = append(candidates, distEnv)
	} else {
		candidates = append(candidates, filepath.Join("web", "dist"), "dist")
	}
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		indexPath := filepath.Join(abs, "index.html")
		info, err := os.Stat(indexPath)
		if err != nil || info.IsDir() {
			continue
		}
		return abs, true
	}
	return "", false
}

//go:embed assets/dashboard.html
var dashboardHTML string
