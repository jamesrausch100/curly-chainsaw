package applicant

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/internal/ats"
	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Engine manages job applications across all ATS platforms.
type Engine struct {
	registry     *ats.Registry
	applications map[string]*models.Application // by application ID
	applied      map[string]bool                 // job IDs we've already applied to
	mu           sync.Mutex
	maxPerRun    int
	delayBetween time.Duration
}

type Option func(*Engine)

func WithMaxPerRun(n int) Option {
	return func(e *Engine) { e.maxPerRun = n }
}

func WithDelay(d time.Duration) Option {
	return func(e *Engine) { e.delayBetween = d }
}

// New creates a new application engine.
func New(registry *ats.Registry, opts ...Option) *Engine {
	e := &Engine{
		registry:     registry,
		applications: make(map[string]*models.Application),
		applied:      make(map[string]bool),
		maxPerRun:    10,
		delayBetween: 2 * time.Second,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// ApplyResult captures the outcome of applying to a batch of jobs.
type ApplyResult struct {
	Applied []models.Application
	Skipped []SkippedJob
	Failed  []FailedJob
}

// SkippedJob records why a job was skipped.
type SkippedJob struct {
	Job    models.Job
	Reason string
}

// FailedJob records why an application failed.
type FailedJob struct {
	Job models.Job
	Err error
}

// ApplyToMatches takes a list of match results and applies to the top ones.
func (e *Engine) ApplyToMatches(ctx context.Context, matches []models.MatchResult, profile models.Profile, minScore float64) ApplyResult {
	var result ApplyResult
	applied := 0

	for _, match := range matches {
		if applied >= e.maxPerRun {
			result.Skipped = append(result.Skipped, SkippedJob{
				Job:    match.Job,
				Reason: "max applications per run reached",
			})
			continue
		}

		if match.Score < minScore {
			result.Skipped = append(result.Skipped, SkippedJob{
				Job:    match.Job,
				Reason: fmt.Sprintf("score %.2f below threshold %.2f", match.Score, minScore),
			})
			continue
		}

		// Check if already applied.
		e.mu.Lock()
		if e.applied[match.Job.ID] {
			e.mu.Unlock()
			result.Skipped = append(result.Skipped, SkippedJob{
				Job:    match.Job,
				Reason: "already applied",
			})
			continue
		}
		e.mu.Unlock()

		// Get the platform client.
		client, ok := e.registry.Get(match.Job.Platform)
		if !ok {
			result.Failed = append(result.Failed, FailedJob{
				Job: match.Job,
				Err: fmt.Errorf("no client for platform %s", match.Job.Platform),
			})
			continue
		}

		// Apply.
		app, err := client.Apply(ctx, match.Job, profile, nil)
		if err != nil {
			result.Failed = append(result.Failed, FailedJob{
				Job: match.Job,
				Err: err,
			})
			continue
		}

		app.MatchScore = match.Score

		e.mu.Lock()
		e.applied[match.Job.ID] = true
		e.applications[app.ID] = app
		e.mu.Unlock()

		result.Applied = append(result.Applied, *app)
		applied++

		// Polite delay between applications.
		if applied < e.maxPerRun {
			select {
			case <-ctx.Done():
				return result
			case <-time.After(e.delayBetween):
			}
		}
	}

	return result
}

// HasApplied checks if we've already applied to a job.
func (e *Engine) HasApplied(jobID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.applied[jobID]
}

// GetApplication retrieves a tracked application.
func (e *Engine) GetApplication(appID string) (*models.Application, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	app, ok := e.applications[appID]
	return app, ok
}

// AllApplications returns all tracked applications.
func (e *Engine) AllApplications() []models.Application {
	e.mu.Lock()
	defer e.mu.Unlock()
	apps := make([]models.Application, 0, len(e.applications))
	for _, app := range e.applications {
		apps = append(apps, *app)
	}
	return apps
}

// Stats returns summary statistics.
func (e *Engine) Stats() (total, applied, failed int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, app := range e.applications {
		total++
		switch app.Status {
		case models.StatusApplied:
			applied++
		case models.StatusFailed:
			failed++
		}
	}
	return
}
