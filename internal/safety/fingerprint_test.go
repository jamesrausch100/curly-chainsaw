package safety

import (
	"net/http"
	"testing"
	"time"
)

func TestRequestProtection_PrepareRequest(t *testing.T) {
	rp := NewRequestProtection()
	req, _ := http.NewRequest("GET", "https://boards.greenhouse.io/test", nil)

	rp.PrepareRequest(req)

	// Must set a User-Agent.
	ua := req.Header.Get("User-Agent")
	if ua == "" {
		t.Fatal("expected User-Agent to be set")
	}

	// Must look like a real browser, not Go-http-client.
	if ua == "Go-http-client/1.1" || ua == "Go-http-client/2.0" {
		t.Fatalf("User-Agent should not be Go default, got %q", ua)
	}

	// Must have standard browser headers.
	for _, header := range []string{"Accept", "Accept-Language", "Accept-Encoding", "Connection"} {
		if req.Header.Get(header) == "" {
			t.Fatalf("expected %s header to be set", header)
		}
	}
}

func TestRequestProtection_RotatesUserAgents(t *testing.T) {
	rp := NewRequestProtection()
	seen := make(map[string]bool)

	// Make 20 requests and check we get variety.
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequest("GET", "https://example.com", nil)
		rp.PrepareRequest(req)
		seen[req.Header.Get("User-Agent")] = true
	}

	if len(seen) < 2 {
		t.Fatalf("expected multiple different user agents, got %d unique", len(seen))
	}
}

func TestRequestProtection_Jitter(t *testing.T) {
	rp := NewRequestProtection()
	base := 1 * time.Second

	// Jitter should always be >= base.
	for i := 0; i < 50; i++ {
		d := rp.Jitter(base)
		if d < base {
			t.Fatalf("jitter %s should be >= base %s", d, base)
		}
		// Jitter adds 0.5-2.5s, so max should be base + 2.5s.
		if d > base+3*time.Second {
			t.Fatalf("jitter %s is too large (base %s)", d, base)
		}
	}
}

func TestRequestProtection_JitterHasVariance(t *testing.T) {
	rp := NewRequestProtection()
	base := 500 * time.Millisecond
	seen := make(map[time.Duration]bool)

	for i := 0; i < 50; i++ {
		d := rp.Jitter(base)
		// Round to 100ms buckets to check variety.
		bucket := d.Round(100 * time.Millisecond)
		seen[bucket] = true
	}

	if len(seen) < 3 {
		t.Fatalf("expected jitter variance, only got %d distinct buckets", len(seen))
	}
}

func TestRequestProtection_HumanDelay(t *testing.T) {
	rp := NewRequestProtection()

	for i := 0; i < 50; i++ {
		d := rp.HumanDelay()
		if d < 2*time.Second {
			t.Fatalf("human delay %s is too short", d)
		}
		if d > 8*time.Second {
			t.Fatalf("human delay %s is too long", d)
		}
	}
}

func TestRequestProtection_HumanDelayHasVariance(t *testing.T) {
	rp := NewRequestProtection()
	seen := make(map[time.Duration]bool)

	for i := 0; i < 50; i++ {
		d := rp.HumanDelay()
		bucket := d.Round(500 * time.Millisecond)
		seen[bucket] = true
	}

	if len(seen) < 3 {
		t.Fatalf("expected human delay variance, only got %d distinct buckets", len(seen))
	}
}
