package safety

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// RateLimiter enforces per-platform request throttling using a token bucket.
// Each platform gets its own bucket with configurable rates.
type RateLimiter struct {
	buckets map[models.Platform]*bucket
	mu      sync.Mutex
}

type bucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// PlatformLimit defines rate limits for a specific ATS platform.
type PlatformLimit struct {
	Platform       models.Platform
	RequestsPerMin int // max requests per minute
	BurstSize      int // max burst above steady rate
}

// DefaultLimits returns conservative rate limits that won't trigger bans.
// These are well below what would cause issues on any platform.
func DefaultLimits() []PlatformLimit {
	return []PlatformLimit{
		{
			Platform:       models.PlatformAshby,
			RequestsPerMin: 10, // Ashby has a public API, but be respectful
			BurstSize:      3,
		},
		{
			Platform:       models.PlatformGreenhouse,
			RequestsPerMin: 10, // Greenhouse boards API is public
			BurstSize:      3,
		},
		{
			Platform:       models.PlatformWorkday,
			RequestsPerMin: 6, // Workday is the most aggressive about rate limiting
			BurstSize:      2,
		},
	}
}

// NewRateLimiter creates a rate limiter with the given platform limits.
func NewRateLimiter(limits []PlatformLimit) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[models.Platform]*bucket),
	}
	for _, l := range limits {
		rl.buckets[l.Platform] = &bucket{
			tokens:     float64(l.BurstSize),
			maxTokens:  float64(l.BurstSize),
			refillRate: float64(l.RequestsPerMin) / 60.0,
			lastRefill: time.Now(),
		}
	}
	return rl
}

// Wait blocks until a request is allowed for the given platform.
// Returns an error if the context is cancelled while waiting.
func (rl *RateLimiter) Wait(ctx context.Context, platform models.Platform) error {
	for {
		rl.mu.Lock()
		b, ok := rl.buckets[platform]
		if !ok {
			rl.mu.Unlock()
			return nil // no limit configured, allow
		}

		// Refill tokens based on elapsed time.
		now := time.Now()
		elapsed := now.Sub(b.lastRefill).Seconds()
		b.tokens += elapsed * b.refillRate
		if b.tokens > b.maxTokens {
			b.tokens = b.maxTokens
		}
		b.lastRefill = now

		if b.tokens >= 1.0 {
			b.tokens -= 1.0
			rl.mu.Unlock()
			return nil
		}

		// Calculate how long until a token is available.
		waitTime := time.Duration((1.0 - b.tokens) / b.refillRate * float64(time.Second))
		rl.mu.Unlock()

		select {
		case <-ctx.Done():
			return fmt.Errorf("rate limiter: context cancelled while waiting for %s", platform)
		case <-time.After(waitTime):
			// Try again.
		}
	}
}

// Remaining returns how many requests can be made immediately for a platform.
func (rl *RateLimiter) Remaining(platform models.Platform) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[platform]
	if !ok {
		return 999 // unlimited
	}

	// Refill.
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now

	return int(b.tokens)
}
