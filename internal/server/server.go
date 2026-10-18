// Package server exposes the application over REST and serves the embedded panel.
package server

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/echoface/ozddns/internal/app"
)

const (
	maxAPIRequestBody  = 8 << 20
	maxAuthRequestBody = 64 << 10
)

// Server wires the HTTP API and the panel.
type Server struct {
	App *app.App
	Log *slog.Logger
	// BasePath is the public prefix the panel and API are served under ("/" for
	// a root mount). It is deployment configuration, resolved by the CLI, and
	// lets a reverse proxy mount OzDDNS at a sub-path without any rewriting on
	// the proxy side. See basePath below.
	BasePath string
	Panel    http.Handler
	// TrustProxy makes the server honor X-Forwarded-Proto for the Secure cookie
	// flag and X-Forwarded-For for client classification. Leave it off unless
	// every request really comes through a trusted reverse proxy that overwrites
	// both headers.
	TrustProxy bool
	mux        *http.ServeMux
	limiter    *attemptLimiter
}

// New builds a server. panel may be nil to disable the UI.
func New(a *app.App, panel http.Handler, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{App: a, Log: log, Panel: panel, BasePath: "/", mux: http.NewServeMux(), limiter: newAttemptLimiter()}
	s.routes()
	return s
}

// Handler returns the fully-wrapped handler.
func (s *Server) Handler() http.Handler {
	return s.recoverer(s.securityHeaders(s.basePath(s.logger(s.limitRequestBody(s.sameOrigin(s.auth(s.mux)))))))
}

// basePath makes the daemon own its public prefix instead of pushing that job
// onto the reverse proxy. The proxy can then forward requests untouched
// (`location /ddns { proxy_pass http://127.0.0.1:9876; }`) and does not need a
// prefix-stripping rewrite that is easy to get subtly wrong.
//
// Requests carrying the prefix are normalized before routing; everything else
// passes through unchanged, so the root mount, a stripped proxy, and the
// unauthenticated liveness probe at /healthz all keep working.
func (s *Server) basePath(next http.Handler) http.Handler {
	prefix := strings.TrimSuffix(s.BasePath, "/")
	if prefix == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == prefix:
			// Canonicalize the mount point so relative URLs have a directory to
			// resolve against.
			target := prefix + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		case strings.HasPrefix(r.URL.Path, prefix+"/"):
			stripped := r.Clone(r.Context())
			stripped.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
			// Path is authoritative again; a stale RawPath would re-encode it.
			stripped.URL.RawPath = ""
			next.ServeHTTP(w, stripped)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func (s *Server) routes() {
	m := s.mux
	// Liveness probe: unauthenticated on purpose so container/orchestrator
	// health checks keep working even when a password is set.
	m.HandleFunc("GET /healthz", s.handleHealth)
	m.HandleFunc("GET /api/v1/auth/state", s.handleAuthState)
	m.HandleFunc("POST /api/v1/auth/setup/start", s.handleAuthSetupStart)
	m.HandleFunc("POST /api/v1/auth/setup/verify", s.handleAuthSetupVerify)
	m.HandleFunc("POST /api/v1/auth/setup/cancel", s.handleAuthSetupCancel)
	m.HandleFunc("POST /api/v1/auth/login", s.handleAuthLogin)
	m.HandleFunc("POST /api/v1/auth/mfa", s.handleAuthMFA)
	m.HandleFunc("POST /api/v1/auth/logout", s.handleAuthLogout)
	m.HandleFunc("GET /api/v1/auth/me", s.handleAuthMe)
	m.HandleFunc("POST /api/v1/auth/disable", s.handleAuthDisable)
	m.HandleFunc("POST /api/v1/auth/recovery-codes", s.handleAuthRecoveryCodes)
	m.HandleFunc("GET /api/v1/auth/events", s.handleAuthEvents)
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
	m.HandleFunc("POST /api/v1/settings/test-notify", s.handleTestNotify)
	m.HandleFunc("GET /api/v1/config/export", s.handleExportConfig)
	m.HandleFunc("POST /api/v1/config/import", s.handleImportConfig)

	if s.Panel != nil {
		m.Handle("GET /", s.Panel)
	}
}

// ---- middleware ----------------------------------------------------------

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

// securityHeaders applies browser hardening to both the panel and the API.
// The CSP intentionally only constrains framing, base URLs and plugin content;
// the panel uses inline style attributes and Google-hosted fonts, so a stricter
// resource policy would require a separate frontend compatibility pass.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'self'; object-src 'none'")
		next.ServeHTTP(w, r)
	})
}

// limitRequestBody bounds unauthenticated JSON parsing and prevents a client
// from making the daemon buffer an arbitrarily large request. Auth payloads are
// tiny; configuration imports are the only API that legitimately needs room.
func (s *Server) limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAPI(r) || r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}
		limit := int64(maxAPIRequestBody)
		if strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
			limit = maxAuthRequestBody
		}
		if r.ContentLength > limit {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
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
