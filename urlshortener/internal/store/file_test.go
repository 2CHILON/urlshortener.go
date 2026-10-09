package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "links.json")
	s, err := Open(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.Create(Link{Code: "abc", URL: "https://example.com", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(Link{Code: "abc", URL: "x"}); !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	_ = s.RecordClick("abc", now)
	if err := s.Close(); err != nil { // final flush
		t.Fatal(err)
	}

	s2, err := Open(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	l, err := s2.Get("abc")
	if err != nil || l.Clicks != 1 || l.URL != "https://example.com" {
		t.Fatalf("reload mismatch: %+v err=%v", l, err)
	}
	if err := s2.Delete("abc"); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Get("abc"); !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found")
	}
}