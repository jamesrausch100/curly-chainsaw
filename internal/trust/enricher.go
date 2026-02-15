package trust

import (
	"context"
	"log"
	"sync"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Enricher adds Muster trust scores to jobs and match results.
// It wraps any existing pipeline and enriches results with verified
// trust data from DaaSTrustLayer.
type Enricher struct {
	client *Client
	cache  sync.Map // entity -> *Score
}

// NewEnricher creates a trust enricher backed by a Muster client.
func NewEnricher(client *Client) *Enricher {
	return &Enricher{client: client}
}

// EnrichJobs attaches company trust scores to a batch of jobs.
// Scores are cached so repeated calls for the same company are free.
func (e *Enricher) EnrichJobs(ctx context.Context, jobs []models.Job) []models.Job {
	// Collect unique companies.
	companies := make(map[string]struct{})
	for _, j := range jobs {
		if j.Company != "" {
			companies[j.Company] = struct{}{}
		}
	}

	// Verify each unique company (concurrent, results cached).
	var wg sync.WaitGroup
	for company := range companies {
		if _, ok := e.cache.Load(company); ok {
			continue
		}
		wg.Add(1)
		go func(c string) {
			defer wg.Done()
			score, err := e.client.VerifyCompany(ctx, c)
			if err != nil {
				log.Printf("[trust] verify %s: %v", c, err)
				return
			}
			e.cache.Store(c, score)
		}(company)
	}
	wg.Wait()

	// Attach scores.
	enriched := make([]models.Job, len(jobs))
	copy(enriched, jobs)
	for i := range enriched {
		if val, ok := e.cache.Load(enriched[i].Company); ok {
			score := val.(*Score)
			enriched[i].CompanyTrustScore = score.Score
			enriched[i].CompanyTrustGrade = string(score.Grade)
		}
	}
	return enriched
}

// EnrichMatches attaches trust data to match results.
// Company trust comes from the job's company. Candidate trust comes from
// the profile's links (GitHub, LinkedIn, portfolio).
func (e *Enricher) EnrichMatches(ctx context.Context, matches []models.MatchResult, profile models.Profile) []models.MatchResult {
	enriched := make([]models.MatchResult, len(matches))
	copy(enriched, matches)

	// Ensure all companies in this batch are verified (fills cache).
	jobs := make([]models.Job, len(matches))
	for i, m := range matches {
		jobs[i] = m.Job
	}
	e.EnrichJobs(ctx, jobs)

	// Score the candidate once via their primary link.
	var candidateTrusted bool
	for _, link := range profile.Links {
		if link == "" {
			continue
		}
		score, err := e.client.VerifyCandidate(ctx, link)
		if err != nil {
			continue
		}
		candidateTrusted = score.Grade.ShouldProceed()
		if candidateTrusted {
			break // one verified link is enough
		}
	}

	for i := range enriched {
		// Company trust.
		if val, ok := e.cache.Load(enriched[i].Job.Company); ok {
			score := val.(*Score)
			enriched[i].CompanyTrusted = score.Grade.ShouldProceed()
			enriched[i].Job.CompanyTrustScore = score.Score
			enriched[i].Job.CompanyTrustGrade = string(score.Grade)
		}

		// Candidate trust.
		enriched[i].CandidateTrusted = candidateTrusted
	}

	return enriched
}

// TrustBoost returns a score multiplier based on company trust.
// Trusted companies get a boost; untrusted ones get penalized.
// This lets the matcher factor in trust without replacing its core algorithm.
func TrustBoost(companyTrustScore int) float64 {
	switch {
	case companyTrustScore >= 85:
		return 1.10 // A-grade: 10% boost
	case companyTrustScore >= 70:
		return 1.05 // B-grade: 5% boost
	case companyTrustScore >= 50:
		return 1.00 // C-grade: neutral
	case companyTrustScore >= 25:
		return 0.85 // D-grade: 15% penalty
	default:
		return 0.70 // F-grade: 30% penalty
	}
}
