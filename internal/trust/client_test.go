package trust

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGradeFromScore(t *testing.T) {
	tests := []struct {
		score int
		want  Grade
	}{
		{100, GradeA},
		{85, GradeA},
		{84, GradeB},
		{70, GradeB},
		{69, GradeC},
		{50, GradeC},
		{49, GradeD},
		{25, GradeD},
		{24, GradeF},
		{0, GradeF},
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
		t.Errorf("A action: %s", GradeA.Action())
	}
	if GradeB.Action() != "PROCEED" {
		t.Errorf("B action: %s", GradeB.Action())
	}
	if GradeC.Action() != "REVIEW" {
		t.Errorf("C action: %s", GradeC.Action())
	}
	if GradeD.Action() != "REJECT" {
		t.Errorf("D action: %s", GradeD.Action())
	}
	if GradeF.Action() != "REJECT" {
		t.Errorf("F action: %s", GradeF.Action())
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
	if GradeD.ShouldProceed() {
		t.Error("D should not proceed")
	}
}

func TestVerify(t *testing.T) {
	// Mock Muster gateway.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entity := r.URL.Query().Get("entity")
		score := Score{
			Entity: entity,
			Score:  88,
			Grade:  GradeA,
			Trusted: true,
			Breakdown: Breakdown{
				ExistenceAge:        28,
				SecurityIntegrity:   25,
				ReputationScale:     22,
				OperationalMaturity: 13,
			},
			ProofHash: "abc123def456",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(score)
	}))
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL, TimeoutSec: 5})

	score, err := client.Verify(context.Background(), "stripe.com")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if score.Entity != "stripe.com" {
		t.Errorf("entity: got %q", score.Entity)
	}
	if score.Score != 88 {
		t.Errorf("score: got %d", score.Score)
	}
	if score.Grade != GradeA {
		t.Errorf("grade: got %s", score.Grade)
	}
	if !score.Trusted {
		t.Error("expected trusted")
	}
	if score.Breakdown.ExistenceAge != 28 {
		t.Errorf("existence_age: got %d", score.Breakdown.ExistenceAge)
	}
	if score.VerifiedAt.IsZero() {
		t.Error("verified_at should be set")
	}
}

func TestVerifyCompany(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		score := Score{Entity: r.URL.Query().Get("entity"), Score: 72, Grade: GradeB, Trusted: true}
		json.NewEncoder(w).Encode(score)
	}))
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	score, err := client.VerifyCompany(context.Background(), "ibm.com")
	if err != nil {
		t.Fatalf("VerifyCompany: %v", err)
	}
	if score.Score != 72 || score.Grade != GradeB {
		t.Errorf("got score=%d grade=%s", score.Score, score.Grade)
	}
}

func TestVerifyCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The coffee lady — no fancy resume, but real digital footprint.
		score := Score{
			Entity:  r.URL.Query().Get("entity"),
			Score:   74,
			Grade:   GradeB,
			Trusted: true,
			Breakdown: Breakdown{
				ExistenceAge:        20,
				SecurityIntegrity:   18,
				ReputationScale:     22,
				OperationalMaturity: 14,
			},
		}
		json.NewEncoder(w).Encode(score)
	}))
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

func TestBatchVerify(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entity := r.URL.Query().Get("entity")
		scores := map[string]int{"stripe.com": 88, "ibm.com": 82, "scam.xyz": 15}
		s := scores[entity]
		score := Score{Entity: entity, Score: s, Grade: GradeFromScore(s), Trusted: s >= 70}
		json.NewEncoder(w).Encode(score)
	}))
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	results, err := client.BatchVerify(context.Background(), []string{"stripe.com", "ibm.com", "scam.xyz"})
	if err != nil {
		t.Fatalf("BatchVerify: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results["stripe.com"].Grade != GradeA {
		t.Errorf("stripe: %s", results["stripe.com"].Grade)
	}
	if results["ibm.com"].Grade != GradeB {
		t.Errorf("ibm: %s", results["ibm.com"].Grade)
	}
	if results["scam.xyz"].Grade != GradeF {
		t.Errorf("scam: %s", results["scam.xyz"].Grade)
	}
	if results["scam.xyz"].Trusted {
		t.Error("scam.xyz should not be trusted")
	}
}

func TestIsAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	if !client.IsAvailable(context.Background()) {
		t.Error("should be available")
	}

	// Unreachable gateway.
	dead := NewClient(Config{GatewayURL: "http://127.0.0.1:1", TimeoutSec: 1})
	if dead.IsAvailable(context.Background()) {
		t.Error("should not be available")
	}
}
