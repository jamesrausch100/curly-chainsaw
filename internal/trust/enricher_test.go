package trust

import (
	"context"
	"testing"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

func TestEnrichJobs(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	enricher := NewEnricher(client)

	jobs := []models.Job{
		{ID: "1", Company: "stripe.com", Title: "Engineer"},
		{ID: "2", Company: "ibm.com", Title: "PR Specialist"},
		{ID: "3", Company: "shadycorp.io", Title: "Intern"},
		{ID: "4", Company: "stripe.com", Title: "Manager"}, // duplicate company
	}

	enriched := enricher.EnrichJobs(context.Background(), jobs)

	if enriched[0].CompanyTrustScore != 92 {
		t.Errorf("stripe score: got %d", enriched[0].CompanyTrustScore)
	}
	if enriched[0].CompanyTrustGrade != "A" {
		t.Errorf("stripe grade: got %s", enriched[0].CompanyTrustGrade)
	}
	if enriched[1].CompanyTrustScore != 85 {
		t.Errorf("ibm score: got %d", enriched[1].CompanyTrustScore)
	}
	if enriched[2].CompanyTrustScore != 18 {
		t.Errorf("shadycorp score: got %d", enriched[2].CompanyTrustScore)
	}
	// Same company cached — should match.
	if enriched[3].CompanyTrustScore != enriched[0].CompanyTrustScore {
		t.Errorf("cache miss: stripe got %d and %d", enriched[0].CompanyTrustScore, enriched[3].CompanyTrustScore)
	}
}

func TestEnrichMatches(t *testing.T) {
	server := mockDaaSTrustLayer()
	defer server.Close()

	client := NewClient(Config{GatewayURL: server.URL})
	enricher := NewEnricher(client)

	matches := []models.MatchResult{
		{Job: models.Job{Company: "ibm.com", Title: "PR Spokesperson"}, Score: 0.8},
		{Job: models.Job{Company: "shadycorp.io", Title: "Intern"}, Score: 0.6},
	}

	profile := models.Profile{
		Name:  "Coffee Lady",
		Links: map[string]string{"github": "github.com/coffeelady"},
	}

	enriched := enricher.EnrichMatches(context.Background(), matches, profile)

	// IBM is trusted.
	if !enriched[0].CompanyTrusted {
		t.Error("ibm should be company trusted")
	}
	// Coffee lady is candidate trusted (score 76, grade B).
	if !enriched[0].CandidateTrusted {
		t.Error("coffee lady should be candidate trusted")
	}
	// Shadycorp is NOT trusted.
	if enriched[1].CompanyTrusted {
		t.Error("shadycorp should not be company trusted")
	}
}

func TestTrustBoost(t *testing.T) {
	tests := []struct {
		score int
		want  float64
	}{
		{92, 1.10},  // A
		{75, 1.05},  // B
		{55, 1.00},  // C
		{30, 0.85},  // D
		{10, 0.70},  // F
	}
	for _, tt := range tests {
		got := TrustBoost(tt.score)
		if got != tt.want {
			t.Errorf("TrustBoost(%d) = %.2f, want %.2f", tt.score, got, tt.want)
		}
	}
}
