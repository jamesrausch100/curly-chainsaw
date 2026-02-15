// Package trust integrates curly-chainsaw with the Muster Protocol
// (DaaSTrustLayer). Full entity lifecycle:
//
//  1. Hello      — establish a session with the Muster gateway
//  2. Claim      — register a new entity (person, company, domain)
//  3. Evidence   — submit GitHub, LinkedIn, portfolio as proof
//  4. Verify     — initiate and complete domain/identity verification
//  5. Score      — run 11-sensor scoring pipeline, get 0-100 + grade
//  6. Outcome    — feed application results back into the trust chain
//
// Without steps 1-3, the coffee lady is never IN the system.
// Without step 6, the trust chain never grows.
//
// Muster grades: A (85-100), B (70-84), C (50-69), D (25-49), F (0-24)
// Actions:       PROCEED,     PROCEED,    REVIEW,     REJECT,    REJECT
package trust

import (
	"bytes"
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

// --- Data types matching DaaSTrustLayer API ---

// Score holds the result of a Muster Protocol scoring.
type Score struct {
	Entity     string    `json:"entity"`
	Score      int       `json:"score"`      // 0-100
	Grade      Grade     `json:"grade"`      // A-F
	Trusted    bool      `json:"trusted"`    // score >= 70
	Breakdown  Breakdown `json:"breakdown"`  // per-category scores
	VerifiedAt time.Time `json:"verified_at"`
	ProofHash  string    `json:"proof_hash,omitempty"` // SHA-256 chain proof
}

// Breakdown holds the four Muster evidence categories.
type Breakdown struct {
	ExistenceAge        int `json:"existence_age"`        // max 30
	SecurityIntegrity   int `json:"security_integrity"`   // max 30
	ReputationScale     int `json:"reputation_scale"`     // max 25
	OperationalMaturity int `json:"operational_maturity"` // max 15
}

// Entity represents a claimed entity in the Muster system.
type Entity struct {
	ID           string            `json:"id"`
	Identifier   string            `json:"identifier"`   // domain, github URL, etc.
	Category     string            `json:"category"`     // "person", "company", "domain"
	Verified     bool              `json:"verified"`
	TrustScore   int               `json:"trust_score,omitempty"`
	TrustGrade   string            `json:"trust_grade,omitempty"`
	SocialLinks  map[string]string `json:"social_links,omitempty"`
	BusinessInfo map[string]string `json:"business_info,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

// Session holds the result of a Muster hello handshake.
type Session struct {
	SessionToken string `json:"session_token"`
	Tier         string `json:"tier"`          // "free", "builder", "scale"
	DailyLimit   int    `json:"daily_limit"`
	RPM          int    `json:"rpm"`
}

// Outcome records an interaction result to feed back into the trust chain.
type Outcome struct {
	EntityID   string            `json:"entity_id"`
	Action     string            `json:"action"`     // "application_sent", "application_accepted", "application_rejected"
	Target     string            `json:"target"`     // who the interaction was with
	Success    bool              `json:"success"`
	Evidence   map[string]string `json:"evidence,omitempty"`
	RecordedAt time.Time         `json:"recorded_at"`
}

// Config holds settings for connecting to the Muster gateway.
type Config struct {
	GatewayURL string `json:"gateway_url"` // DaaSTrustLayer Python API or Go gateway address
	APIKey     string `json:"api_key,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

// DefaultConfig returns config pointing to a local Muster instance.
func DefaultConfig() Config {
	return Config{
		GatewayURL: "http://localhost:8081",
		TimeoutSec: 10,
	}
}

// Client talks to the Muster Protocol via DaaSTrustLayer.
// Covers the full entity lifecycle: hello, claim, evidence, verify, score, outcomes.
type Client struct {
	httpClient   *http.Client
	gatewayURL   string
	apiKey       string
	sessionToken string
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
		apiKey:     cfg.APIKey,
	}
}

// --- Step 1: Session Establishment ---

// Hello establishes a session with the Muster Protocol.
// Returns a session token and capability tier. Without this, you get
// anonymous rate limits (10 requests/day).
func (c *Client) Hello(ctx context.Context) (*Session, error) {
	body := map[string]string{
		"protocol_version": "1.0",
		"client":           "curly-chainsaw",
	}

	var session Session
	if err := c.post(ctx, "/v1/muster/hello", body, &session); err != nil {
		return nil, fmt.Errorf("muster hello: %w", err)
	}

	c.sessionToken = session.SessionToken
	return &session, nil
}

// --- Step 2: Entity Registration ---

// ClaimEntity registers a new entity with the Muster Protocol.
// This is how the coffee lady enters the system. Without this, there's
// nothing to score.
func (c *Client) ClaimEntity(ctx context.Context, identifier, category string, metadata map[string]string) (*Entity, error) {
	body := map[string]interface{}{
		"identifier": identifier,
		"category":   category, // "person", "company", "domain"
	}
	if metadata != nil {
		body["metadata"] = metadata
	}

	var entity Entity
	if err := c.post(ctx, "/claim", body, &entity); err != nil {
		return nil, fmt.Errorf("claim entity: %w", err)
	}
	return &entity, nil
}

// --- Step 3: Evidence Submission ---

// SubmitEvidence attaches social links and profile data to an entity.
// This is what makes the coffee lady scorable — her GitHub, LinkedIn,
// portfolio become the evidence Muster's 11 sensors evaluate.
func (c *Client) SubmitEvidence(ctx context.Context, entityID string, socialLinks, businessInfo map[string]string) error {
	body := map[string]interface{}{}
	if socialLinks != nil {
		body["social_links"] = socialLinks
	}
	if businessInfo != nil {
		body["business_info"] = businessInfo
	}

	return c.put(ctx, fmt.Sprintf("/%s", entityID), body)
}

// --- Step 4: Verification ---

// StartVerification initiates domain/identity verification for an entity.
// Method can be "dns", "file", or "email".
func (c *Client) StartVerification(ctx context.Context, entityID, method string) (string, error) {
	body := map[string]string{"method": method}

	var resp struct {
		Token string `json:"token"`
	}
	if err := c.post(ctx, fmt.Sprintf("/%s/verify", entityID), body, &resp); err != nil {
		return "", fmt.Errorf("start verification: %w", err)
	}
	return resp.Token, nil
}

// CompleteVerification completes the verification flow after the user
// has placed the token (DNS TXT record, file, or email response).
func (c *Client) CompleteVerification(ctx context.Context, entityID string) error {
	return c.post(ctx, fmt.Sprintf("/%s/verify/complete", entityID), nil, nil)
}

// --- Step 5: Scoring ---

// ScoreEntity runs the full 11-sensor scoring pipeline against an entity.
// This hits POST /v1/trust/score — the actual DaaSTrustLayer scoring endpoint.
func (c *Client) ScoreEntity(ctx context.Context, target string) (*Score, error) {
	body := map[string]string{"target": target}

	var score Score
	if err := c.post(ctx, "/v1/trust/score", body, &score); err != nil {
		return nil, fmt.Errorf("score entity: %w", err)
	}
	score.VerifiedAt = time.Now()
	return &score, nil
}

// PreviewScore gets a free-tier score preview (rate-limited, no auth required).
func (c *Client) PreviewScore(ctx context.Context, target string) (*Score, error) {
	var score Score
	if err := c.get(ctx, fmt.Sprintf("/v1/trust/preview?target=%s", target), &score); err != nil {
		return nil, fmt.Errorf("preview score: %w", err)
	}
	score.VerifiedAt = time.Now()
	return &score, nil
}

// BatchScore scores up to 25 entities in a single call.
func (c *Client) BatchScore(ctx context.Context, targets []string) (map[string]*Score, error) {
	body := map[string]interface{}{"targets": targets}

	var resp struct {
		Results map[string]*Score `json:"results"`
	}
	if err := c.post(ctx, "/v1/trust/batch", body, &resp); err != nil {
		return nil, fmt.Errorf("batch score: %w", err)
	}

	for _, s := range resp.Results {
		s.VerifiedAt = time.Now()
	}
	return resp.Results, nil
}

// --- Step 6: Outcome Recording ---

// RecordOutcome feeds an application result back into the trust chain.
// This is the feedback loop — every application CC sends, every response
// received, becomes evidence that strengthens or weakens trust scores.
func (c *Client) RecordOutcome(ctx context.Context, outcome Outcome) error {
	outcome.RecordedAt = time.Now()
	return c.post(ctx, "/v1/membrane/outcome", outcome, nil)
}

// --- Chain Operations ---

// VerifyChain validates the cryptographic integrity of an entity's trust chain.
func (c *Client) VerifyChain(ctx context.Context, entityID string) (bool, error) {
	var resp struct {
		Valid bool `json:"valid"`
	}
	if err := c.get(ctx, fmt.Sprintf("/v1/trust/chain/%s/verify", entityID), &resp); err != nil {
		return false, fmt.Errorf("verify chain: %w", err)
	}
	return resp.Valid, nil
}

// GetChainHistory retrieves the observation history for an entity.
func (c *Client) GetChainHistory(ctx context.Context, entityID string) ([]map[string]interface{}, error) {
	var resp struct {
		Blocks []map[string]interface{} `json:"blocks"`
	}
	if err := c.get(ctx, fmt.Sprintf("/v1/trust/chain/%s/history", entityID), &resp); err != nil {
		return nil, fmt.Errorf("chain history: %w", err)
	}
	return resp.Blocks, nil
}

// --- Convenience Wrappers (backward compat with existing CC code) ---

// Verify scores an entity. Alias for ScoreEntity.
func (c *Client) Verify(ctx context.Context, entity string) (*Score, error) {
	return c.ScoreEntity(ctx, entity)
}

// VerifyCompany scores a company domain/name.
func (c *Client) VerifyCompany(ctx context.Context, company string) (*Score, error) {
	return c.ScoreEntity(ctx, company)
}

// VerifyCandidate scores a candidate by their primary link.
func (c *Client) VerifyCandidate(ctx context.Context, identifier string) (*Score, error) {
	return c.ScoreEntity(ctx, identifier)
}

// BatchVerify scores multiple entities concurrently via individual calls.
// For bulk scoring, prefer BatchScore which uses the batch endpoint.
func (c *Client) BatchVerify(ctx context.Context, entities []string) (map[string]*Score, error) {
	type result struct {
		entity string
		score  *Score
		err    error
	}

	ch := make(chan result, len(entities))
	for _, e := range entities {
		go func(entity string) {
			s, err := c.ScoreEntity(ctx, entity)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.gatewayURL+"/health", nil)
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

// --- HTTP helpers ---

func (c *Client) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.gatewayURL+path, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("muster %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, payload interface{}, out interface{}) error {
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gatewayURL+path, bodyReader)
	if err != nil {
		return err
	}
	c.setHeaders(req)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("muster %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func (c *Client) put(ctx context.Context, path string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.gatewayURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("muster PUT %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	if c.sessionToken != "" {
		req.Header.Set("X-Session-Token", c.sessionToken)
	}
}
