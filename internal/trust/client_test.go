package trust

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockDaaSTrustLayer simulates a DaaSTrustLayer instance with the real API surface.
func mockDaaSTrustLayer() *httptest.Server {
	entities := make(map[string]*Entity)
	outcomes := make([]Outcome, 0)

	scores := map[string]*Score{
		"stripe.com":             {Entity: "stripe.com", Score: 92, Grade: GradeA, Trusted: true},
		"ibm.com":                {Entity: "ibm.com", Score: 85, Grade: GradeA, Trusted: true},
		"scam.xyz":               {Entity: "scam.xyz", Score: 15, Grade: GradeF, Trusted: false},
		"github.com/coffeelady":  {Entity: "github.com/coffeelady", Score: 76, Grade: GradeB, Trusted: true},
		"shadycorp.io":           {Entity: "shadycorp.io", Score: 18, Grade: GradeF, Trusted: false},
	}

	mux := http.NewServeMux()

	// POST /v1/muster/hello — session establishment
	mux.HandleFunc("/v1/muster/hello", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Session{
			SessionToken: "test-session-abc123",
			Tier:         "builder",
			DailyLimit:   1000,
			RPM:          100,
		})
	})

	// POST /claim — entity registration
	mux.HandleFunc("/claim", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Identifier string            `json:"identifier"`
			Category   string            `json:"category"`
			Metadata   map[string]string `json:"metadata"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		entity := &Entity{
			ID:         "ent/" + strings.ReplaceAll(body.Identifier, ".", "_"),
			Identifier: body.Identifier,
			Category:   body.Category,
		}
		entities[entity.ID] = entity

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(entity)
	})

	// PUT /ent/{id} — evidence submission, POST /ent/{id}/verify
	mux.HandleFunc("/ent/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			// Accept evidence.
			entityID := strings.TrimPrefix(r.URL.Path, "/")
			var body struct {
				SocialLinks  map[string]string `json:"social_links"`
				BusinessInfo map[string]string `json:"business_info"`
			}
			json.NewDecoder(r.Body).Decode(&body)

			if e, ok := entities[entityID]; ok {
				e.SocialLinks = body.SocialLinks
				e.BusinessInfo = body.BusinessInfo
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		// POST /{entity_id}/verify or /{entity_id}/verify/complete
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/verify") {
			json.NewEncoder(w).Encode(map[string]string{"token": "verify-token-xyz"})
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/verify/complete") {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	// POST /v1/trust/score — scoring
	mux.HandleFunc("/v1/trust/score", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Target string `json:"target"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		score, ok := scores[body.Target]
		if !ok {
			score = &Score{Entity: body.Target, Score: 50, Grade: GradeC, Trusted: false}
		}
		json.NewEncoder(w).Encode(score)
	})

	// GET /v1/trust/preview — free preview
	mux.HandleFunc("/v1/trust/preview", func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		score, ok := scores[target]
		if !ok {
			score = &Score{Entity: target, Score: 50, Grade: GradeC, Trusted: false}
		}
		json.NewEncoder(w).Encode(score)
	})

	// POST /v1/trust/batch — batch scoring
	mux.HandleFunc("/v1/trust/batch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Targets []string `json:"targets"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		results := make(map[string]*Score)
		for _, t := range body.Targets {
			s, ok := scores[t]
			if !ok {
				s = &Score{Entity: t, Score: 50, Grade: GradeC, Trusted: false}
			}
			results[t] = s
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
	})

	// POST /v1/membrane/outcome — outcome recording
	mux.HandleFunc("/v1/membrane/outcome", func(w http.ResponseWriter, r *http.Request) {
		var o Outcome
		json.NewDecoder(r.Body).Decode(&o)
		outcomes = append(outcomes, o)
		w.WriteHeader(http.StatusOK)
	})

	// GET /v1/trust/chain/{entityID}/verify
	mux.HandleFunc("/v1/trust/chain/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/verify") {
			json.NewEncoder(w).Encode(map[string]bool{"valid": true})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/history") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"blocks": []map[string]interface{}{
					{"sensor": "DNS", "score": 25},
					{"sensor": "WHOIS", "score": 20},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	// GET /health
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return httptest.NewServer(mux)
}

func TestGradeFromScore(t *testing.T) {
	tests := []struct {
		score int
		want  Grade
	}{
		{100, GradeA}, {85, GradeA},
		{84, GradeB}, {70, GradeB},
		{69, GradeC}, {50, GradeC},
		{49, GradeD}, {25, GradeD},
		{24, GradeF}, {0, GradeF},
	}
	for _, tt := range tests {
		got := GradeFromScore(tt.score)
		if got != tt.want {
			t.Errorf("GradeFromScore(%d) = %s, want %s", tt.score, got, tt.want)
		}
	}
}

func TestGrade_Action(t *testing.T) {
	if GradeA.Action() != "PROCEED" {
		t.Errorf("A: %s", GradeA.Action())
	}
	if GradeB.Action() != "PROCEED" {
		t.Errorf("B: %s", GradeB.Action())
	}
	if GradeC.Action() != "REVIEW" {
		t.Errorf("C: %s", GradeC.Action())
	}
	if GradeD.Action() != "REJECT" {
		t.Errorf("D: %s", GradeD.Action())
	}
}

func TestGrade_ShouldProceed(t *testing.T) {
	if !GradeA.ShouldProceed() {
		t.Error("A should proceed")
	}
	if !GradeB.ShouldProceed() {
		t.Error("B should proceed")
	}
	if GradeC.ShouldProceed() {
		t.Error("C should not proceed")
	}
}

// --- Full Lifecycle Tests ---

func TestHello(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL, TimeoutSec: 5})
	session, err := client.Hello(context.Background())
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}

	if session.SessionToken != "test-session-abc123" {
		t.Errorf("token: got %q", session.SessionToken)
	}
	if session.Tier != "builder" {
		t.Errorf("tier: got %q", session.Tier)
	}
	if session.DailyLimit != 1000 {
		t.Errorf("daily_limit: got %d", session.DailyLimit)
	}
	if client.sessionToken != "test-session-abc123" {
		t.Error("session token not stored on client")
	}
}

func TestClaimEntity(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})

	// The coffee lady enters the system.
	entity, err := client.ClaimEntity(context.Background(), "coffeelady.dev", "person", map[string]string{
		"name":  "Sarah",
		"title": "Full-Stack Developer",
	})
	if err != nil {
		t.Fatalf("ClaimEntity: %v", err)
	}
	if entity.ID == "" {
		t.Error("entity ID should not be empty")
	}
	if entity.Identifier != "coffeelady.dev" {
		t.Errorf("identifier: got %q", entity.Identifier)
	}
	if entity.Category != "person" {
		t.Errorf("category: got %q", entity.Category)
	}
}

func TestSubmitEvidence(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})

	// First claim, then submit evidence.
	entity, _ := client.ClaimEntity(context.Background(), "coffeelady.dev", "person", nil)

	err := client.SubmitEvidence(context.Background(), entity.ID,
		map[string]string{
			"github":    "https://github.com/coffeelady",
			"linkedin":  "https://linkedin.com/in/coffeelady",
			"portfolio": "https://coffeelady.dev",
		},
		map[string]string{
			"years_experience": "5",
			"specialty":        "React, Go, Python",
		},
	)
	if err != nil {
		t.Fatalf("SubmitEvidence: %v", err)
	}
}

func TestStartVerification(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	entity, _ := client.ClaimEntity(context.Background(), "coffeelady.dev", "person", nil)

	token, err := client.StartVerification(context.Background(), entity.ID, "email")
	if err != nil {
		t.Fatalf("StartVerification: %v", err)
	}
	if token == "" {
		t.Error("token should not be empty")
	}
}

func TestScoreEntity(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})

	score, err := client.ScoreEntity(context.Background(), "stripe.com")
	if err != nil {
		t.Fatalf("ScoreEntity: %v", err)
	}
	if score.Score != 92 {
		t.Errorf("score: got %d", score.Score)
	}
	if score.Grade != GradeA {
		t.Errorf("grade: got %s", score.Grade)
	}
	if !score.Trusted {
		t.Error("stripe should be trusted")
	}
	if score.VerifiedAt.IsZero() {
		t.Error("verified_at should be set")
	}
}

func TestPreviewScore(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	score, err := client.PreviewScore(context.Background(), "ibm.com")
	if err != nil {
		t.Fatalf("PreviewScore: %v", err)
	}
	if score.Score != 85 {
		t.Errorf("score: got %d", score.Score)
	}
}

func TestBatchScore(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	results, err := client.BatchScore(context.Background(), []string{"stripe.com", "ibm.com", "scam.xyz"})
	if err != nil {
		t.Fatalf("BatchScore: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results["stripe.com"].Grade != GradeA {
		t.Errorf("stripe: %s", results["stripe.com"].Grade)
	}
	if results["scam.xyz"].Trusted {
		t.Error("scam.xyz should not be trusted")
	}
}

func TestRecordOutcome(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})

	err := client.RecordOutcome(context.Background(), Outcome{
		EntityID: "ent/coffeelady_dev",
		Action:   "application_sent",
		Target:   "ibm.com",
		Success:  true,
		Evidence: map[string]string{
			"job_title":  "Senior Engineer",
			"match_score": "0.87",
		},
	})
	if err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
}

func TestVerifyChain(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	valid, err := client.VerifyChain(context.Background(), "stripe_com")
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !valid {
		t.Error("chain should be valid")
	}
}

func TestGetChainHistory(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	blocks, err := client.GetChainHistory(context.Background(), "stripe_com")
	if err != nil {
		t.Fatalf("GetChainHistory: %v", err)
	}
	if len(blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(blocks))
	}
}

// --- Backward Compat Wrappers ---

func TestVerify_BackwardCompat(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	score, err := client.Verify(context.Background(), "stripe.com")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if score.Score != 92 {
		t.Errorf("score: got %d", score.Score)
	}
}

func TestVerifyCandidate_CoffeeLady(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	score, err := client.VerifyCandidate(context.Background(), "github.com/coffeelady")
	if err != nil {
		t.Fatalf("VerifyCandidate: %v", err)
	}
	if !score.Grade.ShouldProceed() {
		t.Error("coffee lady should PROCEED — she's got real skills")
	}
}

func TestBatchVerify_Concurrent(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	results, err := client.BatchVerify(context.Background(), []string{"stripe.com", "ibm.com", "scam.xyz"})
	if err != nil {
		t.Fatalf("BatchVerify: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
}

func TestIsAvailable(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	if !client.IsAvailable(context.Background()) {
		t.Error("should be available")
	}

	dead := NewClient(Config{GatewayURL: "http://127.0.0.1:1", TimeoutSec: 1})
	if dead.IsAvailable(context.Background()) {
		t.Error("should not be available")
	}
}

// --- Full Lifecycle: Coffee Lady Scenario ---

func TestFullLifecycle_CoffeeLady(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL, APIKey: "test-key"})

	// Step 1: Establish session.
	session, err := client.Hello(context.Background())
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if session.SessionToken == "" {
		t.Fatal("no session token")
	}

	// Step 2: Register the coffee lady.
	entity, err := client.ClaimEntity(context.Background(), "coffeelady.dev", "person", map[string]string{
		"name": "Sarah",
	})
	if err != nil {
		t.Fatalf("ClaimEntity: %v", err)
	}

	// Step 3: Submit her evidence — GitHub, LinkedIn, portfolio.
	err = client.SubmitEvidence(context.Background(), entity.ID,
		map[string]string{
			"github":    "https://github.com/coffeelady",
			"linkedin":  "https://linkedin.com/in/coffeelady",
			"portfolio": "https://coffeelady.dev",
		},
		nil,
	)
	if err != nil {
		t.Fatalf("SubmitEvidence: %v", err)
	}

	// Step 4: Score her.
	score, err := client.ScoreEntity(context.Background(), "github.com/coffeelady")
	if err != nil {
		t.Fatalf("ScoreEntity: %v", err)
	}
	if !score.Grade.ShouldProceed() {
		t.Errorf("coffee lady grade %s should PROCEED", score.Grade)
	}

	// Step 5: She applies to IBM through CC. Record the outcome.
	err = client.RecordOutcome(context.Background(), Outcome{
		EntityID: entity.ID,
		Action:   "application_sent",
		Target:   "ibm.com",
		Success:  true,
		Evidence: map[string]string{"job_title": "Senior Engineer", "match_score": "0.87"},
	})
	if err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}

	// Step 6: Verify the chain is intact.
	valid, err := client.VerifyChain(context.Background(), entity.ID)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !valid {
		t.Error("trust chain should be valid")
	}
}

func TestSetHeaders_APIKeyAndSession(t *testing.T) {
	var gotAPIKey, gotSession string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-Key")
		gotSession = r.Header.Get("X-Session-Token")
		json.NewEncoder(w).Encode(Score{Entity: "test", Score: 50, Grade: GradeC})
	}))
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL, APIKey: "my-key"})
	client.sessionToken = "my-session"

	_, _ = io.ReadAll(strings.NewReader(""))
	client.ScoreEntity(context.Background(), "test")

	if gotAPIKey != "my-key" {
		t.Errorf("X-API-Key: got %q", gotAPIKey)
	}
	if gotSession != "my-session" {
		t.Errorf("X-Session-Token: got %q", gotSession)
	}
}
