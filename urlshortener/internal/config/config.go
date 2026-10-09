// Package config reads settings from environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr            string        // ADDR, listen address
	BaseURL         string        // BASE_URL, public origin used to build short links
	BaseHost        string        // derived from BaseURL
	DataFile        string        // DATA_FILE, "" = in-memory only
	APIKey          string        // API_KEY, required for DELETE; empty disables deletion
	CodeLength      int           // CODE_LENGTH, 4-32
	RedirectStatus  int           // REDIRECT_STATUS: 301, 302, 307 or 308
	RateLimitPerMin int           // RATE_LIMIT_PER_MIN per IP for link creation, 0 = off
	RateBurst       int           // RATE_BURST
	TrustProxy      bool          // TRUST_PROXY, honour X-Forwarded-For
	FlushInterval   time.Duration // FLUSH_INTERVAL
}

func Load() (Config, error) {
	c := Config{
		Addr:     env("ADDR", ":8080"),
		BaseURL:  strings.TrimRight(env("BASE_URL", "http://localhost:8080"), "/"),
		DataFile: env("DATA_FILE", "data/links.json"),
		APIKey:   os.Getenv("API_KEY"),
	}
	var err error
	if c.CodeLength, err = envInt("CODE_LENGTH", 7); err != nil {
		return c, err
	}
	if c.RedirectStatus, err = envInt("REDIRECT_STATUS", 302); err != nil {
		return c, err
	}
	if c.RateLimitPerMin, err = envInt("RATE_LIMIT_PER_MIN", 30); err != nil {
		return c, err
	}
	if c.RateBurst, err = envInt("RATE_BURST", 10); err != nil {
		return c, err
	}
	if c.TrustProxy, err = envBool("TRUST_PROXY", false); err != nil {
		return c, err
	}
	d, err := time.ParseDuration(env("FLUSH_INTERVAL", "5s"))
	if err != nil {
		return c, fmt.Errorf("FLUSH_INTERVAL: %w", err)
	}
	c.FlushInterval = d

	if c.CodeLength < 4 || c.CodeLength > 32 {
		return c, fmt.Errorf("CODE_LENGTH must be between 4 and 32")
	}
	switch c.RedirectStatus {
	case 301, 302, 307, 308:
	default:
		return c, fmt.Errorf("REDIRECT_STATUS must be 301, 302, 307 or 308")
	}
	if c.RateBurst < 1 {
		c.RateBurst = 1
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return c, fmt.Errorf("BASE_URL must be an absolute http(s) URL, got %q", c.BaseURL)
	}
	c.BaseHost = u.Host
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func envBool(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}