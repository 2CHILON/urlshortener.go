package shortener

import (
	"errors"
	"fmt"
	"time"

	"urlshortener/internal/store"
)

// MaxTTL is the longest expiry a caller may request.
const MaxTTL = 10 * 365 * 24 * time.Hour

const maxCollisionRetries = 8

type Service struct {
	store    store.Store
	codeLen  int
	selfHost string
	now      func() time.Time
}

type Option func(*Service)

// WithClock overrides the time source (used by tests).
func WithClock(fn func() time.Time) Option { return func(s *Service) { s.now = fn } }

func New(st store.Store, codeLen int, selfHost string, opts ...Option) *Service {
	s := &Service{store: st, codeLen: codeLen, selfHost: selfHost, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Create shortens rawURL. alias is optional; ttl <= 0 means "never expires".
func (s *Service) Create(rawURL, alias string, ttl time.Duration) (store.Link, error) {
	dest, err := ValidateURL(rawURL, s.selfHost)
	if err != nil {
		return store.Link{}, err
	}
	if ttl > MaxTTL {
		return store.Link{}, fmt.Errorf("%w: max is %d seconds", ErrInvalidTTL, int64(MaxTTL/time.Second))
	}

	now := s.now().UTC()
	l := store.Link{URL: dest, CreatedAt: now}
	if ttl > 0 {
		exp := now.Add(ttl)
		l.ExpiresAt = &exp
	}

	if alias != "" {
		if err := ValidateAlias(alias); err != nil {
			return store.Link{}, err
		}
		l.Code = alias
		if err := s.store.Create(l); err != nil {
			return store.Link{}, err
		}
		return l, nil
	}

	for i := 0; i < maxCollisionRetries; i++ {
		code, err := GenerateCode(s.codeLen)
		if err != nil {
			return store.Link{}, err
		}
		l.Code = code
		err = s.store.Create(l)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, store.ErrExists) {
			return store.Link{}, err
		}
	}
	return store.Link{}, errors.New("could not allocate a unique code; try a longer CODE_LENGTH")
}

// Visit resolves a code for redirecting and records the click.
func (s *Service) Visit(code string) (store.Link, error) {
	l, err := s.store.Get(code)
	if err != nil {
		return store.Link{}, err
	}
	now := s.now()
	if l.Expired(now) {
		return store.Link{}, ErrExpired
	}
	_ = s.store.RecordClick(code, now.UTC()) // a lost click must not break the redirect
	return l, nil
}

// Peek resolves a code without recording a click (HEAD requests, previews).
func (s *Service) Peek(code string) (store.Link, error) {
	l, err := s.store.Get(code)
	if err != nil {
		return store.Link{}, err
	}
	if l.Expired(s.now()) {
		return store.Link{}, ErrExpired
	}
	return l, nil
}

// Stats returns the link even if expired, so owners can still read its numbers.
func (s *Service) Stats(code string) (store.Link, bool, error) {
	l, err := s.store.Get(code)
	if err != nil {
		return store.Link{}, false, err
	}
	return l, l.Expired(s.now()), nil
}

func (s *Service) Delete(code string) error { return s.store.Delete(code) }

// Cleanup removes links that expired more than grace ago (they answer 410 until then).
func (s *Service) Cleanup(grace time.Duration) int {
	return s.store.DeleteExpiredBefore(s.now().Add(-grace))
}