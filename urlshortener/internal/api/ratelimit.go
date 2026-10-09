package api

import (
	"math"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// RateLimiter is a per-key token bucket.
type RateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	rate      float64 // tokens per second
	burst     float64
	now       func() time.Time
	lastSweep time.Time
}

func NewRateLimiter(perMinute, burst int) *RateLimiter {
	return &RateLimiter{
		buckets:   make(map[string]*bucket),
		rate:      float64(perMinute) / 60.0,
		burst:     float64(burst),
		now:       time.Now,
		lastSweep: time.Now(),
	}
}

// Allow consumes one token for key. When denied it returns how long to wait.
func (rl *RateLimiter) Allow(key string) (bool, time.Duration) {
	now := rl.now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if now.Sub(rl.lastSweep) > 5*time.Minute {
		for k, b := range rl.buckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.lastSweep = now
	}

	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{tokens: rl.burst, last: now}
		rl.buckets[key] = b
	}
	b.tokens = math.Min(rl.burst, b.tokens+now.Sub(b.last).Seconds()*rl.rate)
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / rl.rate * float64(time.Second))
	return false, wait
}