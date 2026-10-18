package server

import (
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/echoface/ozddns/internal/app"
	"github.com/echoface/ozddns/internal/domain"
)

const sessionCookieName = "ozddns_session"

type userContextKey struct{}

// auth gates the API only when authorization is enabled. In open mode the
// panel is local-first and every API call is allowed; once an administrator
// exists, every API route except the sign-in/setup handshake needs a valid
// server-side session cookie.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAPI(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if isPublicAuthPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		enabled, err := s.App.AuthEnabled(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !enabled {
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.App.AuthenticateSession(r.Context(), sessionToken(r))
		if err != nil {
			s.clearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "authorization required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	})
}

// sameOrigin rejects state-changing API requests whose Origin does not match
// the panel's own host. SameSite=Lax is the first line of defence; this is the
// second, and it also covers browsers that send an explicit Origin.
func (s *Server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAPI(r) || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			writeError(w, http.StatusForbidden, "cross-site request rejected")
			return
		}
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Host, r.Host) {
			writeError(w, http.StatusForbidden, "cross-origin request rejected")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isPublicAuthPath(path string) bool {
	switch path {
	case "/api/v1/auth/state",
		"/api/v1/auth/login",
		"/api/v1/auth/mfa",
		"/api/v1/auth/logout",
		"/api/v1/auth/setup/start",
		"/api/v1/auth/setup/verify",
		"/api/v1/auth/setup/cancel":
		return true
	default:
		return false
	}
}

func sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (s *Server) cookiePath() string {
	prefix := strings.TrimSuffix(s.BasePath, "/")
	if prefix == "" {
		return "/"
	}
	return prefix + "/"
}

func (s *Server) cookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return s.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     s.cookiePath(),
		HttpOnly: true,
		Secure:   s.cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     s.cookiePath(),
		HttpOnly: true,
		Secure:   s.cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

func (s *Server) clientInfo(r *http.Request) app.ClientInfo {
	ip := remoteIP(r)
	if s.TrustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			ip = strings.TrimSpace(strings.Split(forwarded, ",")[0])
		}
	}
	if parsed := net.ParseIP(strings.Trim(ip, "[]")); parsed != nil {
		ip = parsed.String()
	}
	return app.ClientInfo{IP: ip, UserAgent: r.UserAgent()}
}

func authStatus(err error) int {
	switch {
	case errors.Is(err, app.ErrInvalidInput):
		return http.StatusBadRequest
	case errors.Is(err, app.ErrAuthEnabled), errors.Is(err, app.ErrAuthDisabled):
		return http.StatusConflict
	case errors.Is(err, app.ErrInvalidCredentials), errors.Is(err, app.ErrInvalidCode),
		errors.Is(err, app.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, app.ErrSetupExpired):
		return http.StatusGone
	case errors.Is(err, app.ErrTooManyAttempts):
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

func (s *Server) rateLimited(w http.ResponseWriter, retry time.Duration) {
	seconds := int(retry.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
}

// ---- handlers ------------------------------------------------------------

func (s *Server) handleAuthState(w http.ResponseWriter, r *http.Request) {
	state, err := s.App.AuthState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if token := sessionToken(r); token != "" {
		if _, err := s.App.AuthenticateSession(r.Context(), token); err == nil {
			state.Authenticated = true
		}
	}
	// The endpoint is deliberately public so the sign-in screen can load. Only
	// reveal account details and security counters to that authenticated
	// administrator; an anonymous visitor gets mode and setup status only.
	if !state.Authenticated {
		state.Username = ""
		state.RecoveryCodesRemaining = 0
		state.Sessions = 0
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleAuthSetupStart(w http.ResponseWriter, r *http.Request) {
	if !s.localEnrollment(r) {
		writeError(w, http.StatusForbidden, "authorization enrollment is only allowed from the local host")
		return
	}
	if ok, retry := s.limiter.allow("setup:"+s.clientInfo(r).IP, 6); !ok {
		s.rateLimited(w, retry)
		return
	}
	body, err := decode[struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}](r)
	if err != nil {
		writeError(w, decodeStatus(err), err.Error())
		return
	}
	info, err := s.App.StartSetup(r.Context(), body.Username, body.Password, s.clientInfo(r))
	if err != nil {
		writeError(w, authStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleAuthSetupVerify(w http.ResponseWriter, r *http.Request) {
	if !s.localEnrollment(r) {
		writeError(w, http.StatusForbidden, "authorization enrollment is only allowed from the local host")
		return
	}
	if ok, retry := s.limiter.allow("setup-verify:"+s.clientInfo(r).IP, 10); !ok {
		s.rateLimited(w, retry)
		return
	}
	body, err := decode[struct {
		Code string `json:"code"`
	}](r)
	if err != nil {
		writeError(w, decodeStatus(err), err.Error())
		return
	}
	codes, user, err := s.App.VerifySetup(r.Context(), body.Code, s.clientInfo(r))
	if err != nil {
		writeError(w, authStatus(err), err.Error())
		return
	}
	token, err := s.App.CreateSession(r.Context(), s.clientInfo(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setSessionCookie(w, r, token)
	s.limiter.reset("setup:" + s.clientInfo(r).IP)
	writeJSON(w, http.StatusOK, map[string]any{
		"recoveryCodes": codes,
		"user":          map[string]string{"username": user.Username},
	})
}

func (s *Server) handleAuthSetupCancel(w http.ResponseWriter, r *http.Request) {
	if !s.localEnrollment(r) {
		writeError(w, http.StatusForbidden, "authorization enrollment is only allowed from the local host")
		return
	}
	if err := s.App.CancelSetup(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	body, err := decode[struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}](r)
	if err != nil {
		writeError(w, decodeStatus(err), err.Error())
		return
	}
	// Limit the whole source, not the attempted username. Credential checks run
	// PBKDF2 before returning, so a username-keyed limit could be bypassed by
	// rotating usernames and would still allow CPU-exhaustion attempts.
	key := "login:" + s.clientInfo(r).IP
	if ok, retry := s.limiter.allow(key, 8); !ok {
		s.rateLimited(w, retry)
		return
	}
	challenge, err := s.App.Login(r.Context(), body.Username, body.Password, s.clientInfo(r))
	if err != nil {
		status := authStatus(err)
		message := "invalid username or password"
		if status != http.StatusUnauthorized {
			message = err.Error()
		}
		writeError(w, status, message)
		return
	}
	s.limiter.reset(key)
	writeJSON(w, http.StatusOK, map[string]any{"mfaRequired": true, "challenge": challenge})
}

func (s *Server) handleAuthMFA(w http.ResponseWriter, r *http.Request) {
	body, err := decode[struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}](r)
	if err != nil {
		writeError(w, decodeStatus(err), err.Error())
		return
	}
	key := "mfa:" + s.clientInfo(r).IP
	if ok, retry := s.limiter.allow(key, 20); !ok {
		s.rateLimited(w, retry)
		return
	}
	token, user, err := s.App.VerifyMFA(r.Context(), body.Challenge, body.Code, s.clientInfo(r))
	if err != nil {
		if errors.Is(err, app.ErrTooManyAttempts) {
			s.rateLimited(w, 5*time.Minute)
			return
		}
		writeError(w, authStatus(err), "invalid authentication code")
		return
	}
	s.limiter.reset(key)
	s.setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"user": map[string]string{"username": user.Username},
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.App.Logout(r.Context(), sessionToken(r), s.clientInfo(r)); err != nil {
		// Keep the browser cookie so the client can retry. Reporting success
		// after a failed revocation would leave a still-valid server session.
		s.Log.Error("logout failed", "err", err)
		writeError(w, http.StatusInternalServerError, "could not revoke session")
		return
	}
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(userContextKey{}).(domain.AuthUser)
	writeJSON(w, http.StatusOK, map[string]string{"username": user.Username})
}

func (s *Server) handleAuthDisable(w http.ResponseWriter, r *http.Request) {
	key := "sensitive:" + s.clientInfo(r).IP
	if ok, retry := s.limiter.allow(key, 10); !ok {
		s.rateLimited(w, retry)
		return
	}
	body, err := decode[struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}](r)
	if err != nil {
		writeError(w, decodeStatus(err), err.Error())
		return
	}
	if err := s.App.DisableAuth(r.Context(), body.Password, body.Code, s.clientInfo(r)); err != nil {
		writeError(w, authStatus(err), err.Error())
		return
	}
	s.limiter.reset(key)
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAuthRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	key := "sensitive:" + s.clientInfo(r).IP
	if ok, retry := s.limiter.allow(key, 10); !ok {
		s.rateLimited(w, retry)
		return
	}
	body, err := decode[struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}](r)
	if err != nil {
		writeError(w, decodeStatus(err), err.Error())
		return
	}
	codes, err := s.App.RegenerateRecoveryCodes(r.Context(), body.Password, body.Code, s.clientInfo(r))
	if err != nil {
		writeError(w, authStatus(err), err.Error())
		return
	}
	s.limiter.reset(key)
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

func (s *Server) handleAuthEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.App.AuthEvents(r.Context(), 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if events == nil {
		events = []domain.AuthEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

// attemptLimiter is a bounded, per-source token bucket. It limits online
// guessing and PBKDF2 flooding without allowing a flood of source addresses to
// grow process memory without limit. Persistent lockout lives in the TOTP
// challenge attempt counter.
type attemptLimiter struct {
	mu         sync.Mutex
	window     time.Duration
	maxEntries int
	now        func() time.Time
	entries    map[string]*attemptEntry
}

type attemptEntry struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

const (
	defaultAttemptLimitEntries = 8192
	evictionSampleSize         = 32
)

func newAttemptLimiter() *attemptLimiter {
	return &attemptLimiter{
		window:     5 * time.Minute,
		maxEntries: defaultAttemptLimitEntries,
		now:        time.Now,
		entries:    make(map[string]*attemptEntry),
	}
}

// localEnrollment keeps the one-time bootstrap flow from becoming a remote
// account-takeover path when an operator exposes an instance before enabling
// authorization. Trusted reverse proxies should set TrustProxy so the forwarded
// client address, rather than the proxy's loopback address, is checked here.
func (s *Server) localEnrollment(r *http.Request) bool {
	ip := net.ParseIP(strings.TrimSpace(s.clientInfo(r).IP))
	return ip != nil && ip.IsLoopback()
}

func (l *attemptLimiter) allow(key string, max int) (bool, time.Duration) {
	if max <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.window <= 0 {
		l.window = 5 * time.Minute
	}
	now := l.now()

	entry := l.entries[key]
	if entry == nil {
		l.makeRoom(now)
		entry = &attemptEntry{tokens: float64(max), updated: now, lastSeen: now}
		l.entries[key] = entry
	} else {
		elapsed := now.Sub(entry.updated)
		if elapsed > 0 {
			entry.tokens = math.Min(
				float64(max),
				entry.tokens+elapsed.Seconds()*float64(max)/l.window.Seconds(),
			)
			entry.updated = now
		}
	}
	entry.lastSeen = now
	if entry.tokens < 1 {
		seconds := (1 - entry.tokens) * l.window.Seconds() / float64(max)
		return false, time.Duration(math.Ceil(seconds * float64(time.Second)))
	}
	entry.tokens--
	return true, 0
}

// makeRoom keeps the map strictly bounded. Expired entries are removed first;
// if the map is still full, the oldest entry from a small sample is evicted.
// Sampling avoids an O(n) scan on every new address during a distributed spray.
func (l *attemptLimiter) makeRoom(now time.Time) {
	if l.maxEntries <= 0 {
		l.maxEntries = defaultAttemptLimitEntries
	}
	if len(l.entries) < l.maxEntries {
		return
	}
	cutoff := now.Add(-l.window)
	for key, entry := range l.entries {
		if !entry.lastSeen.After(cutoff) {
			delete(l.entries, key)
		}
	}
	for len(l.entries) >= l.maxEntries {
		var (
			oldestKey string
			oldestAt  time.Time
			seen      int
		)
		for key, entry := range l.entries {
			if seen == evictionSampleSize {
				break
			}
			if oldestKey == "" || entry.lastSeen.Before(oldestAt) {
				oldestKey, oldestAt = key, entry.lastSeen
			}
			seen++
		}
		if oldestKey == "" {
			return
		}
		delete(l.entries, oldestKey)
	}
}

func (l *attemptLimiter) reset(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}
