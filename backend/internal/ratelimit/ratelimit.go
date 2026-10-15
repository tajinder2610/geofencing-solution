// Package ratelimit implements a small in-memory token-bucket limiter keyed by client.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter is a per-key token bucket. It is safe for concurrent use.
type Limiter struct {
	rate  float64 // tokens added per second
	burst float64

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
	now       func() time.Time
}

// New creates a limiter allowing rate sustained requests/second with the given burst size.
func New(rate float64, burst int) *Limiter {
	return &Limiter{rate: rate, burst: float64(burst), buckets: make(map[string]*bucket), now: time.Now}
}

// Allow consumes a token for key. When denied it returns how long to wait before retrying.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)

	b, found := l.buckets[key]
	if !found {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	return false, wait
}

// sweep drops buckets that have been idle long enough to be full again.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	idle := time.Duration(l.burst/l.rate*float64(time.Second)) + time.Minute
	for k, b := range l.buckets {
		if now.Sub(b.last) > idle {
			delete(l.buckets, k)
		}
	}
}
