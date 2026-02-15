package safety

import (
	"math/rand"
	"net/http"
	"time"
)

// RequestProtection makes HTTP requests look like a real human browsing,
// not a bot hammering an API. This is critical for Workday especially,
// which aggressively fingerprints and blocks automated traffic.
type RequestProtection struct {
	rng *rand.Rand
}

// NewRequestProtection creates a new request protection instance.
func NewRequestProtection() *RequestProtection {
	return &RequestProtection{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// realistic browser user agents — rotated per request
var userAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:122.0) Gecko/20100101 Firefox/122.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
}

// PrepareRequest adds realistic browser headers to an HTTP request.
// This doesn't forge anything malicious — it just sets the same headers
// a normal browser would send when visiting a job board.
func (rp *RequestProtection) PrepareRequest(req *http.Request) {
	ua := userAgents[rp.rng.Intn(len(userAgents))]

	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json, text/html, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Cache-Control", "no-cache")

	// Don't set Referer or Origin unless we're on an actual page flow —
	// random referers are a bot signal.
}

// Jitter returns a randomized delay to make request timing look human.
// Adds 0.5-3 seconds of random jitter on top of any base delay.
func (rp *RequestProtection) Jitter(base time.Duration) time.Duration {
	jitter := time.Duration(500+rp.rng.Intn(2500)) * time.Millisecond
	return base + jitter
}

// HumanDelay returns a delay that mimics a human reading a page before
// taking the next action. Used between search -> view -> apply flows.
func (rp *RequestProtection) HumanDelay() time.Duration {
	// 2-8 seconds, like a person reading a job description.
	return time.Duration(2000+rp.rng.Intn(6000)) * time.Millisecond
}
