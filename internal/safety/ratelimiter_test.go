package safety

import (
	"context"
	"testing"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

func TestRateLimiter_AllowsBurst(t *testing.T) {
	rl := NewRateLimiter([]PlatformLimit{
		{Platform: models.PlatformAshby, RequestsPerMin: 60, BurstSize: 3},
	})

	ctx := context.Background()

	// Should allow 3 requests immediately (burst size).
	for i := 0; i < 3; i++ {
		start := time.Now()
		if err := rl.Wait(ctx, models.PlatformAshby); err != nil {
			t.Fatalf("burst request %d should not error: %v", i, err)
		}
		if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
			t.Fatalf("burst request %d took %s, expected near-instant", i, elapsed)
		}
	}
}

func TestRateLimiter_ThrottlesAfterBurst(t *testing.T) {
	rl := NewRateLimiter([]PlatformLimit{
		{Platform: models.PlatformGreenhouse, RequestsPerMin: 60, BurstSize: 1},
	})

	ctx := context.Background()

	// First request is instant (consumes the 1 burst token).
	if err := rl.Wait(ctx, models.PlatformGreenhouse); err != nil {
		t.Fatalf("first request should not error: %v", err)
	}

	// Second request should block until a token refills (~1s at 60/min).
	start := time.Now()
	if err := rl.Wait(ctx, models.PlatformGreenhouse); err != nil {
		t.Fatalf("second request should not error: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 500*time.Millisecond {
		t.Fatalf("expected throttle delay, got %s", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("throttle took too long: %s", elapsed)
	}
}

func TestRateLimiter_RespectsContextCancellation(t *testing.T) {
	rl := NewRateLimiter([]PlatformLimit{
		{Platform: models.PlatformWorkday, RequestsPerMin: 6, BurstSize: 0},
	})

	// Bucket starts with 0 tokens, so first request must wait.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := rl.Wait(ctx, models.PlatformWorkday)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestRateLimiter_UnknownPlatformAllowed(t *testing.T) {
	rl := NewRateLimiter(DefaultLimits())
	ctx := context.Background()

	// A platform not in the config should pass through with no delay.
	err := rl.Wait(ctx, models.Platform("lever"))
	if err != nil {
		t.Fatalf("unknown platform should be allowed: %v", err)
	}
}

func TestRateLimiter_PlatformsAreIndependent(t *testing.T) {
	rl := NewRateLimiter([]PlatformLimit{
		{Platform: models.PlatformAshby, RequestsPerMin: 60, BurstSize: 2},
		{Platform: models.PlatformGreenhouse, RequestsPerMin: 60, BurstSize: 2},
	})

	ctx := context.Background()

	// Drain Ashby's burst.
	for i := 0; i < 2; i++ {
		if err := rl.Wait(ctx, models.PlatformAshby); err != nil {
			t.Fatalf("ashby request %d: %v", i, err)
		}
	}

	// Greenhouse should still have full burst available.
	if remaining := rl.Remaining(models.PlatformGreenhouse); remaining < 2 {
		t.Fatalf("greenhouse should have 2 remaining, got %d", remaining)
	}
}

func TestRateLimiter_Remaining(t *testing.T) {
	rl := NewRateLimiter([]PlatformLimit{
		{Platform: models.PlatformAshby, RequestsPerMin: 60, BurstSize: 5},
	})

	if r := rl.Remaining(models.PlatformAshby); r != 5 {
		t.Fatalf("expected 5 remaining, got %d", r)
	}

	ctx := context.Background()
	_ = rl.Wait(ctx, models.PlatformAshby)

	if r := rl.Remaining(models.PlatformAshby); r != 4 {
		t.Fatalf("expected 4 remaining after 1 request, got %d", r)
	}

	// Unknown platform returns 999.
	if r := rl.Remaining(models.Platform("lever")); r != 999 {
		t.Fatalf("expected 999 for unknown platform, got %d", r)
	}
}
