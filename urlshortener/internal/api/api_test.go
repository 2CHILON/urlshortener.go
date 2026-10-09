package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"urlshortener/internal/shortener"
	"urlshortener/internal/store"
)

func newTestServer(t *testing.T, key string, limiter *RateLimiter) http.Handler {
	t.Helper()
	st, _ := store.Open("", 0)
	t.Cleanup(func() { st.Close() })
	svc := shortener.New(st, 7, "short.test")
	return New(svc, Options{
		BaseURL: "http://short.test", APIKey: key, RedirectStatus: http.StatusFound,
		Limiter: limiter, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).Routes()
}

func do(h http.Handler, method, path, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEndToEnd(t *testing.T) {
	h := newTestServer(t, "secret", nil)

	rec := do(h, "POST", "/api/links", `{"url":"https://example.com/x","alias":"exm"}`)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created linkResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ShortURL != "http://short.test/exm" {
		t.Fatalf("short url: %q", created.ShortURL)
	}

	if rec = do(h, "GET", "/exm", ""); rec.Code != 302 || rec.Header().Get("Location") != "https://example.com/x" {
		t.Fatalf("redirect: %d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
	do(h, "HEAD", "/exm", "") // must not count

	var stats linkResponse
	rec = do(h, "GET", "/api/links/exm", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &stats)
	if rec.Code != 200 || stats.Clicks != 1 {
		t.Fatalf("stats: %d clicks=%d", rec.Code, stats.Clicks)
	}

	if rec = do(h, "POST", "/api/links", `{"url":"https://example.com","alias":"exm"}`); rec.Code != 409 {
		t.Fatalf("duplicate alias: %d", rec.Code)
	}
	if rec = do(h, "DELETE", "/api/links/exm", ""); rec.Code != 401 {
		t.Fatalf("delete without key: %d", rec.Code)
	}
	if rec = do(h, "DELETE", "/api/links/exm", "", "X-API-Key", "secret"); rec.Code != 204 {
		t.Fatalf("delete with key: %d", rec.Code)
	}
	if rec = do(h, "GET", "/exm", ""); rec.Code != 404 {
		t.Fatalf("after delete: %d", rec.Code)
	}
}

func TestBadRequests(t *testing.T) {
	h := newTestServer(t, "", nil)
	cases := map[string]string{
		"not json":      `nope`,
		"unknown field": `{"url":"https://example.com","x":1}`,
		"bad scheme":    `{"url":"ftp://example.com"}`,
		"bad alias":     `{"url":"https://example.com","alias":"a b"}`,
		"negative ttl":  `{"url":"https://example.com","ttl_seconds":-5}`,
		"self loop":     `{"url":"http://short.test/abc"}`,
	}
	for name, body := range cases {
		if rec := do(h, "POST", "/api/links", body); rec.Code != 400 {
			t.Errorf("%s: want 400, got %d (%s)", name, rec.Code, rec.Body)
		}
	}
	if rec := do(h, "DELETE", "/api/links/x", ""); rec.Code != 403 {
		t.Errorf("delete without API_KEY configured: want 403, got %d", rec.Code)
	}
}

func TestRateLimit(t *testing.T) {
	h := newTestServer(t, "", NewRateLimiter(1, 2)) // burst of 2
	codes := []int{}
	for i := 0; i < 3; i++ {
		codes = append(codes, do(h, "POST", "/api/links", `{"url":"https://example.com"}`).Code)
	}
	if codes[0] != 201 || codes[1] != 201 || codes[2] != 429 {
		t.Fatalf("got %v", codes)
	}
}

func TestMisc(t *testing.T) {
	h := newTestServer(t, "", nil)
	if rec := do(h, "GET", "/healthz", ""); rec.Code != 200 {
		t.Fatalf("health: %d", rec.Code)
	}
	if rec := do(h, "GET", "/", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "URL shortener") {
		t.Fatalf("index: %d", rec.Code)
	}
	if rec := do(h, "GET", "/nope123", ""); rec.Code != 404 {
		t.Fatalf("missing: %d", rec.Code)
	}
}