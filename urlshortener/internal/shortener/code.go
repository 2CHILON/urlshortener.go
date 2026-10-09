// Package shortener holds the business logic: code generation, validation and
// the Service that ties it to a store.
package shortener

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	alphabet     = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	MaxURLLength = 2048
)

var (
	ErrInvalidURL   = errors.New("invalid url")
	ErrInvalidAlias = errors.New("invalid alias")
	ErrInvalidTTL   = errors.New("invalid ttl")
	ErrExpired      = errors.New("link expired")

	aliasRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{3,32}$`)
	reserved = map[string]bool{"api": true, "healthz": true, "admin": true, "links": true, "static": true}
)

// GenerateCode returns an n-character base62 string from crypto/rand.
// Rejection sampling avoids modulo bias (248 = largest multiple of 62 below 256).
func GenerateCode(n int) (string, error) {
	const limit = 248
	out := make([]byte, 0, n)
	buf := make([]byte, n*2)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b >= limit {
				continue
			}
			out = append(out, alphabet[int(b)%len(alphabet)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}

// ValidateAlias checks a user-chosen custom code.
func ValidateAlias(alias string) error {
	if !aliasRe.MatchString(alias) {
		return fmt.Errorf("%w: use 3-32 characters from A-Z, a-z, 0-9, '_' and '-'", ErrInvalidAlias)
	}
	if reserved[strings.ToLower(alias)] {
		return fmt.Errorf("%w: %q is reserved", ErrInvalidAlias, alias)
	}
	return nil
}

// ValidateURL normalises and validates a destination. selfHost (host[:port] of
// this service) is rejected to prevent redirect loops.
func ValidateURL(raw, selfHost string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: url is required", ErrInvalidURL)
	}
	if len(raw) > MaxURLLength {
		return "", fmt.Errorf("%w: longer than %d characters", ErrInvalidURL, MaxURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%w: only http and https are allowed", ErrInvalidURL)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("%w: missing host", ErrInvalidURL)
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: credentials in url are not allowed", ErrInvalidURL)
	}
	if selfHost != "" && strings.EqualFold(u.Host, selfHost) {
		return "", fmt.Errorf("%w: cannot shorten a link to this service", ErrInvalidURL)
	}
	u.Scheme = scheme
	return u.String(), nil
}