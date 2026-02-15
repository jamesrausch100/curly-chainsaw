// Package trust integrates curly-chainsaw with the Muster Protocol
// (DaaSTrustLayer). Muster scores any entity — domains, people, companies,
// APIs — against 9 independent sensors and returns a 0-100 trust score
// with cryptographic proof.
//
// CC uses this to:
//   - Score companies before auto-applying (is this employer legit?)
//   - Score candidates for the recruiter side (prove real capabilities)
//   - Enrich match results with verified trust data
//
// Muster grades: A (85-100), B (70-84), C (50-69), D (25-49), F (0-24)
// Actions:       PROCEED,     PROCEED,    REVIEW,     REJECT,    REJECT
package trust

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Grade represents a Muster trust grade.
type Grade string

const (
	GradeA Grade = "A" // 85-100 PROCEED
	GradeB Grade = "B" // 70-84  PROCEED
	GradeC Grade = "C" // 50-69  REVIEW
	GradeD Grade = "D" // 25-49  REJECT
	GradeF Grade = "F" // 0-24   REJECT
)

// Action returns the recommended action for a grade.
func (g Grade) Action() string {
	switch g {
	case GradeA, GradeB:
		return "PROCEED"
	case GradeC:
		return "REVIEW"
	default:
		return "REJECT"
	}
}

// ShouldProceed returns true if the grade indicates safe to proceed.
func (g Grade) ShouldProceed() bool {
	return g == GradeA || g == GradeB
}

// Score holds the result of a Muster Protocol verification.
type Score struct {
	Entity    string    `json:"entity"`
	Score     int       `json:"score"`      // 0-100
	Grade     Grade     `json:"grade"`      // A-F
	Trusted   bool      `json:"trusted"`    // score >= 70
	Breakdown Breakdown `json:"breakdown"`  // per-category scores
	VerifiedAt time.Time `json:"verified_at"`
	ProofHash  string    `json:"proof_hash,omitempty"` // SHA-256 chain proof
}

// Breakdown holds the four Muster evidence categories.
type Breakdown struct {
	ExistenceAge         int `json:"existence_age"`         // max 30
	SecurityIntegrity    int `json:"security_integrity"`    // max 30
	ReputationScale      int `json:"reputation_scale"`      // max 25
	OperationalMaturity  int `json:"operational_maturity"`  // max 15
}

// GradeFromScore converts a numeric score to a letter grade.
func GradeFromScore(score int) Grade {
	switch {
	case score >= 85:
		return GradeA
	case score >= 70:
		return GradeB
	case score >= 50:
		return GradeC
	case score >= 25:
		return GradeD
	default:
		return GradeF
	}
}

// Client talks to the Muster Protocol via the DaaSTrustLayer Go gateway.
type Client struct {
	httpClient *http.Client
	gatewayURL string // e.g. http://localhost:8080 for the Go gateway
}

// Config holds settings for connecting to the Muster gateway.
type Config struct {
	GatewayURL string `json:"gateway_url"` // DaaSTrustLayer Go gateway address
	APIKey     string `json:"api_key,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

// DefaultConfig returns config pointing to a local Muster gateway.
func DefaultConfig() Config {
	return Config{
		GatewayURL: "http://localhost:8081",
		TimeoutSec: 10,
	}
}

// NewClient creates a Muster Protocol client.
func NewClient(cfg Config) *Client {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		gatewayURL: strings.TrimRight(cfg.GatewayURL, "/"),
	}
}

// Verify scores an entity through the Muster Protocol.
// Entity can be a domain ("stripe.com"), company name, email, URL, etc.
func (c *Client) Verify(ctx context.Context, entity string) (*Score, error) {
	url := fmt.Sprintf("%s/api/v1/verify?entity=%s", c.gatewayURL, entity)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("muster gateway: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("muster gateway returned %d: %s", resp.StatusCode, string(body))
	}

	var score Score
	if err := json.Unmarshal(body, &score); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	score.VerifiedAt = time.Now()
	return &score, nil
}

// VerifyCompany scores a company domain/name. Convenience wrapper around Verify.
func (c *Client) VerifyCompany(ctx context.Context, company string) (*Score, error) {
	return c.Verify(ctx, company)
}

// VerifyCandidate scores a candidate by their primary link (GitHub, LinkedIn, portfolio).
// This is the proving ground — the coffee lady gets scored by her real digital footprint.
func (c *Client) VerifyCandidate(ctx context.Context, identifier string) (*Score, error) {
	return c.Verify(ctx, identifier)
}

// BatchVerify scores multiple entities concurrently.
func (c *Client) BatchVerify(ctx context.Context, entities []string) (map[string]*Score, error) {
	type result struct {
		entity string
		score  *Score
		err    error
	}

	ch := make(chan result, len(entities))
	for _, e := range entities {
		go func(entity string) {
			s, err := c.Verify(ctx, entity)
			ch <- result{entity: entity, score: s, err: err}
		}(e)
	}

	scores := make(map[string]*Score, len(entities))
	var errs []string
	for range entities {
		r := <-ch
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.entity, r.err))
			continue
		}
		scores[r.entity] = r.score
	}

	if len(errs) > 0 && len(scores) == 0 {
		return nil, fmt.Errorf("all verifications failed: %s", strings.Join(errs, "; "))
	}

	return scores, nil
}

// IsAvailable checks if the Muster gateway is reachable.
func (c *Client) IsAvailable(ctx context.Context) bool {
	url := fmt.Sprintf("%s/health", c.gatewayURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
