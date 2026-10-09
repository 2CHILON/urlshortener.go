package shortener

import (
	"errors"
	"strings"
	"testing"
	"time"

	"urlshortener/internal/store"
)

func TestGenerateCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		c, err := GenerateCode(7)
		if err != nil || len(c) != 7 {
			t.Fatalf("bad code %q err=%v", c, err)
		}
		for _, r := range c {
			if !strings.ContainsRune(alphabet, r) {
				t.Fatalf("char %q outside alphabet", r)
			}
		}
		seen[c] = true
	}
	if len(seen) < 990 {
		t.Fatalf("too many collisions: %d unique of 1000", len(seen))
	}
}

func TestValidateAlias(t *testing.T) {
	good := []string{"abc", "my-link_1", strings.Repeat("a", 32)}
	bad := []string{"", "ab", "has space", "a/b", "api", "HEALTHZ", strings.Repeat("a", 33)}
	for _, a := range good {
		if err := ValidateAlias(a); err != nil {
			t.Errorf("%q should be valid: %v", a, err)
		}
	}
	for _, a := range bad {
		if err := ValidateAlias(a); !errors.Is(err, ErrInvalidAlias) {
			t.Errorf("%q should be invalid, got %v", a, err)
		}
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"https://example.com/a?b=c#d", false},
		{"  HTTP://Example.com  ", false},
		{"", true},
		{"example.com", true},
		{"ftp://example.com", true},
		{"javascript:alert(1)", true},
		{"https://", true},
		{"https://user:pass@example.com", true},
		{"https://short.test/abc", true}, // self host
		{"https://example.com/" + strings.Repeat("a", MaxURLLength), true},
	}
	for _, tc := range tests {
		_, err := ValidateURL(tc.in, "short.test")
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateURL(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}

func newSvc(t *testing.T, now *time.Time) *Service {
	t.Helper()
	st, err := store.Open("", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, 7, "short.test", WithClock(func() time.Time { return *now }))
}

func TestServiceLifecycle(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newSvc(t, &now)

	l, err := svc.Create("https://example.com", "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Visit(l.Code); err != nil {
		t.Fatal(err)
	}
	got, _, _ := svc.Stats(l.Code)
	if got.Clicks != 1 || got.LastAccess == nil {
		t.Fatalf("click not recorded: %+v", got)
	}
	if _, err := svc.Peek(l.Code); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = svc.Stats(l.Code); got.Clicks != 1 {
		t.Fatal("Peek must not count clicks")
	}

	now = now.Add(2 * time.Hour) // past expiry
	if _, err := svc.Visit(l.Code); !errors.Is(err, ErrExpired) {
		t.Fatalf("want ErrExpired, got %v", err)
	}
	if _, expired, _ := svc.Stats(l.Code); !expired {
		t.Fatal("stats should flag expired")
	}
	if n := svc.Cleanup(24 * time.Hour); n != 0 {
		t.Fatalf("grace period ignored, purged %d", n)
	}
	now = now.Add(48 * time.Hour)
	if n := svc.Cleanup(24 * time.Hour); n != 1 {
		t.Fatalf("want 1 purged, got %d", n)
	}
	if _, err := svc.Visit(l.Code); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound after purge, got %v", err)
	}
}

func TestAliasConflictAndTTLLimit(t *testing.T) {
	now := time.Now()
	svc := newSvc(t, &now)
	if _, err := svc.Create("https://example.com", "docs", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create("https://example.org", "docs", 0); !errors.Is(err, store.ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if _, err := svc.Create("https://example.com", "", MaxTTL+time.Second); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("want ErrInvalidTTL, got %v", err)
	}
}