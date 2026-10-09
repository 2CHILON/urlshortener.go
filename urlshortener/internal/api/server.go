// Package api exposes the HTTP interface: redirect, JSON API and a tiny web UI.
package api

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"urlshortener/internal/shortener"
	"urlshortener/internal/store"
)

//go:embed web/index.html
var indexHTML []byte

type Options struct {
	BaseURL        string       // public origin, no trailing slash
	APIKey         string       // protects DELETE; empty disables DELETE
	RedirectStatus int          // 301/302/307/308
	TrustProxy     bool         // use X-Forwarded-For for client IP
	Limiter        *RateLimiter // nil = no rate limiting
	Logger         *slog.Logger
}

type Server struct {
	svc  *shortener.Service
	opts Options
	log  *slog.Logger
}

func New(svc *shortener.Service, opts Options) *Server {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	if opts.RedirectStatus == 0 {
		opts.RedirectStatus = http.StatusFound
	}
	return &Server{svc: svc, opts: opts, log: log}
}

// Routes builds the handler tree (Go 1.22 method+wildcard patterns).
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/links", s.handleCreate)
	mux.HandleFunc("GET /api/links/{code}", s.handleStats)
	mux.HandleFunc("DELETE /api/links/{code}", s.handleDelete)
	mux.HandleFunc("GET /{code}", s.handleRedirect) // also serves HEAD

	var h http.Handler = mux
	h = secureHeaders(h)
	h = recoverer(s.log, h)
	h = logging(s.log, s.clientIP, h)
	return h
}

// ---- response helpers ------------------------------------------------------

type linkResponse struct {
	Code       string     `json:"code"`
	ShortURL   string     `json:"short_url"`
	URL        string     `json:"url"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Expired    bool       `json:"expired"`
	Clicks     int64      `json:"clicks"`
	LastAccess *time.Time `json:"last_access,omitempty"`
}

func (s *Server) toResponse(l store.Link, expired bool) linkResponse {
	return linkResponse{
		Code: l.Code, ShortURL: s.opts.BaseURL + "/" + l.Code, URL: l.URL,
		CreatedAt: l.CreatedAt, ExpiresAt: l.ExpiresAt, Expired: expired,
		Clicks: l.Clicks, LastAccess: l.LastAccess,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) clientIP(r *http.Request) string {
	if s.opts.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) authorized(r *http.Request) bool {
	if s.opts.APIKey == "" {
		return false
	}
	got := r.Header.Get("X-API-Key")
	if got == "" {
		got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.APIKey)) == 1
}

// ---- handlers --------------------------------------------------------------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'")
	_, _ = w.Write(indexHTML)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createRequest struct {
	URL        string `json:"url"`
	Alias      string `json:"alias"`
	TTLSeconds int64  `json:"ttl_seconds"`
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	if s.opts.Limiter != nil {
		if ok, wait := s.opts.Limiter.Allow(s.clientIP(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded, slow down")
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req createRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.TTLSeconds < 0 || req.TTLSeconds > int64(shortener.MaxTTL/time.Second) {
		writeError(w, http.StatusBadRequest, "ttl_seconds out of range")
		return
	}

	l, err := s.svc.Create(req.URL, req.Alias, time.Duration(req.TTLSeconds)*time.Second)
	switch {
	case err == nil:
		resp := s.toResponse(l, false)
		w.Header().Set("Location", resp.ShortURL)
		writeJSON(w, http.StatusCreated, resp)
	case errors.Is(err, shortener.ErrInvalidURL),
		errors.Is(err, shortener.ErrInvalidAlias),
		errors.Is(err, shortener.ErrInvalidTTL):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrExists):
		writeError(w, http.StatusConflict, "that alias is already taken")
	default:
		s.log.Error("create failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func (s *Server) handleRedirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")

	var (
		l   store.Link
		err error
	)
	if r.Method == http.MethodHead {
		l, err = s.svc.Peek(code) // HEAD must not inflate click counts
	} else {
		l, err = s.svc.Visit(code)
	}

	switch {
	case err == nil:
		// no-store so browsers/CDNs don't hide clicks (matters for 301/308 too)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, l.URL, s.opts.RedirectStatus)
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "short link not found", http.StatusNotFound)
	case errors.Is(err, shortener.ErrExpired):
		http.Error(w, "this short link has expired", http.StatusGone)
	default:
		s.log.Error("redirect failed", "err", err, "code", code)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	l, expired, err := s.svc.Stats(r.PathValue("code"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "short link not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, s.toResponse(l, expired))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if s.opts.APIKey == "" {
		writeError(w, http.StatusForbidden, "deletion is disabled: set API_KEY to enable it")
		return
	}
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="urlshortener"`)
		writeError(w, http.StatusUnauthorized, "missing or invalid API key")
		return
	}
	err := s.svc.Delete(r.PathValue("code"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "short link not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}