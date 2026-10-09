// Package server exposes the application over REST and serves the embedded panel.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/echoface/ozddns/internal/app"
)

// Server wires the HTTP API and the panel.
type Server struct {
	App   *app.App
	Log   *slog.Logger
	Panel http.Handler
	mux   *http.ServeMux
}

// New builds a server. panel may be nil to disable the UI.
func New(a *app.App, panel http.Handler, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{App: a, Log: log, Panel: panel, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the fully-wrapped handler.
func (s *Server) Handler() http.Handler {
	return s.recoverer(s.logger(s.auth(s.mux)))
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /api/v1/bootstrap", s.handleBootstrap)
	m.HandleFunc("GET /api/v1/provider-types", s.handleProviderTypes)
	m.HandleFunc("GET /api/v1/ip-source-types", s.handleSourceTypes)
	m.HandleFunc("GET /api/v1/ip-sources/interfaces", s.handleInterfaces)
	m.HandleFunc("GET /api/v1/status", s.handleStatus)
	m.HandleFunc("GET /api/v1/ip", s.handleIP)
	m.HandleFunc("POST /api/v1/scheduler", s.handleScheduler)

	m.HandleFunc("GET /api/v1/providers", s.handleListProviders)
	m.HandleFunc("POST /api/v1/providers", s.handleCreateProvider)
	m.HandleFunc("GET /api/v1/providers/{id}", s.handleGetProvider)
	m.HandleFunc("PATCH /api/v1/providers/{id}", s.handleUpdateProvider)
	m.HandleFunc("DELETE /api/v1/providers/{id}", s.handleDeleteProvider)
	m.HandleFunc("POST /api/v1/providers/{id}/validate", s.handleValidateProvider)
	m.HandleFunc("GET /api/v1/providers/{id}/zones", s.handleProviderZones)

	m.HandleFunc("GET /api/v1/ip-sources", s.handleListSources)
	m.HandleFunc("POST /api/v1/ip-sources", s.handleCreateSource)
	m.HandleFunc("PATCH /api/v1/ip-sources/{id}", s.handleUpdateSource)
	m.HandleFunc("DELETE /api/v1/ip-sources/{id}", s.handleDeleteSource)
	m.HandleFunc("POST /api/v1/ip-sources/{id}/test", s.handleTestSource)

	m.HandleFunc("GET /api/v1/jobs", s.handleListJobs)
	m.HandleFunc("POST /api/v1/jobs", s.handleCreateJob)
	m.HandleFunc("GET /api/v1/jobs/{id}", s.handleGetJob)
	m.HandleFunc("PATCH /api/v1/jobs/{id}", s.handleUpdateJob)
	m.HandleFunc("DELETE /api/v1/jobs/{id}", s.handleDeleteJob)
	m.HandleFunc("POST /api/v1/jobs/{id}/run", s.handleRunJob)
	m.HandleFunc("POST /api/v1/jobs/{id}/dry-run", s.handleDryRun)
	m.HandleFunc("POST /api/v1/jobs/run-all", s.handleRunAll)

	m.HandleFunc("GET /api/v1/runs", s.handleListRuns)
	m.HandleFunc("GET /api/v1/runs/{id}", s.handleGetRun)
	m.HandleFunc("POST /api/v1/runs/purge", s.handlePurgeRuns)

	m.HandleFunc("GET /api/v1/settings", s.handleGetSettings)
	m.HandleFunc("PATCH /api/v1/settings", s.handleUpdateSettings)
	m.HandleFunc("POST /api/v1/settings/password", s.handleSetPassword)
	m.HandleFunc("POST /api/v1/settings/test-notify", s.handleTestNotify)

	if s.Panel != nil {
		m.Handle("GET /", s.Panel)
	}
}

// ---- middleware ----------------------------------------------------------

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		settings, err := s.App.GetSettings(r.Context())
		if err != nil || !settings.PasswordSet {
			next.ServeHTTP(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if ok && constantEq(user, settings.Username) && s.App.CheckPassword(r.Context(), user, pass) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="OzDDNS"`)
		writeError(w, http.StatusUnauthorized, "authentication required")
	})
}

func constantEq(a, b string) bool {
	if b == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAPI(r) {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		s.Log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"dur", time.Since(start).String(), "from", remoteIP(r))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.Log.Error("panic", "err", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }

func isAPI(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/v1/")
}

// ---- helpers -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
