package applicant

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/internal/ats"
	"github.com/jamesrausch100/curly-chainsaw/internal/safety"
	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Engine manages job applications across all ATS platforms.
// Every application goes through the safety guardrails and rate limiter
// before touching any ATS API.
type Engine struct {
	registry    *ats.Registry
	guardrails  *safety.Guardrails
	rateLimiter *safety.RateLimiter
	reqProtect  *safety.RequestProtection

	applications map[string]*models.Application // by application ID
	applied      map[string]bool                 // job IDs we've already applied to
	pending      map[string]*models.Application  // pending human review
	mu           sync.Mutex
	maxPerRun    int
}

type Option func(*Engine)

func WithMaxPerRun(n int) Option {
	return func(e *Engine) { e.maxPerRun = n }
}

func WithGuardrailConfig(cfg safety.GuardrailConfig) Option {
	return func(e *Engine) { e.guardrails = safety.NewGuardrails(cfg) }
}

func WithRateLimits(limits []safety.PlatformLimit) Option {
	return func(e *Engine) { e.rateLimiter = safety.NewRateLimiter(limits) }
}

// New creates a new application engine with safety protections enabled.
func New(registry *ats.Registry, opts ...Option) *Engine {
	e := &Engine{
		registry:     registry,
		guardrails:   safety.NewGuardrails(safety.DefaultGuardrailConfig()),
		rateLimiter:  safety.NewRateLimiter(safety.DefaultLimits()),
		reqProtect:   safety.NewRequestProtection(),
		applications: make(map[string]*models.Application),
		applied:      make(map[string]bool),
		pending:      make(map[string]*models.Application),
		maxPerRun:    10,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// ApplyResult captures the outcome of applying to a batch of jobs.
type ApplyResult struct {
	Applied  []models.Application
	Pending  []PendingJob // needs human review
	Skipped  []SkippedJob
	Blocked  []BlockedJob // blocked by guardrails
	Failed   []FailedJob
}

// PendingJob records a job queued for human review.
type PendingJob struct {
	Job        models.Job
	MatchScore float64
	Message    string
}

// SkippedJob records why a job was skipped.
type SkippedJob struct {
	Job    models.Job
	Reason string
}

// BlockedJob records why the guardrails blocked an application.
type BlockedJob struct {
	Job       models.Job
	Violation safety.Violation
}

// FailedJob records why an application failed.
type FailedJob struct {
	Job models.Job
	Err error
}

// ApplyToMatches takes match results and applies to top ones — with full
// safety checks at every step.
func (e *Engine) ApplyToMatches(ctx context.Context, matches []models.MatchResult, profile models.Profile, userID string) ApplyResult {
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

		// Run through guardrails.
		e.mu.Lock()
		alreadyApplied := e.applied[match.Job.ID]
		pendingCount := len(e.pending)
		e.mu.Unlock()

		violation := e.guardrails.CheckApplication(userID, match.Job, match.Score, alreadyApplied, pendingCount)
		if violation != nil {
			switch violation.Code {
			case safety.ViolationNeedsReview:
				// Queue for human review instead of auto-applying.
				app := &models.Application{
					ID:         fmt.Sprintf("pending_%s_%d", match.Job.ID, time.Now().UnixMilli()),
					JobID:      match.Job.ID,
					ProfileID:  profile.ID,
					Platform:   match.Job.Platform,
					Status:     models.StatusPending,
					MatchScore: match.Score,
				}
				e.mu.Lock()
				e.pending[app.ID] = app
				e.mu.Unlock()

				result.Pending = append(result.Pending, PendingJob{
					Job:        match.Job,
					MatchScore: match.Score,
					Message:    violation.Message,
				})
			default:
				result.Blocked = append(result.Blocked, BlockedJob{
					Job:       match.Job,
					Violation: *violation,
				})
			}
			continue
		}

		// Wait for rate limiter before hitting the ATS.
		if err := e.rateLimiter.Wait(ctx, match.Job.Platform); err != nil {
			result.Failed = append(result.Failed, FailedJob{
				Job: match.Job,
				Err: fmt.Errorf("rate limit wait cancelled: %w", err),
			})
			continue
		}

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

		// Record with guardrails and internal tracking.
		e.guardrails.RecordApplication(userID, match.Job.Company)
		e.mu.Lock()
		e.applied[match.Job.ID] = true
		e.applications[app.ID] = app
		e.mu.Unlock()

		result.Applied = append(result.Applied, *app)
		applied++

		// Human-like delay between applications.
		if applied < e.maxPerRun {
			delay := e.reqProtect.HumanDelay()
			select {
			case <-ctx.Done():
				return result
			case <-time.After(delay):
			}
		}
	}

	return result
}

// ConfirmPending approves a pending application and submits it.
func (e *Engine) ConfirmPending(ctx context.Context, pendingID string, profile models.Profile) (*models.Application, error) {
	e.mu.Lock()
	app, ok := e.pending[pendingID]
	if !ok {
		e.mu.Unlock()
		return nil, fmt.Errorf("pending application %s not found", pendingID)
	}
	delete(e.pending, pendingID)
	e.mu.Unlock()

	// Rate limit.
	if err := e.rateLimiter.Wait(ctx, app.Platform); err != nil {
		return nil, err
	}

	client, ok := e.registry.Get(app.Platform)
	if !ok {
		return nil, fmt.Errorf("no client for platform %s", app.Platform)
	}

	// We need the full job to apply. Get it from the client.
	job, err := client.GetJob(ctx, app.JobID)
	if err != nil {
		return nil, fmt.Errorf("fetch job details: %w", err)
	}

	submitted, err := client.Apply(ctx, *job, profile, nil)
	if err != nil {
		return nil, err
	}

	submitted.MatchScore = app.MatchScore
	e.guardrails.RecordApplication(app.ProfileID, job.Company)

	e.mu.Lock()
	e.applied[app.JobID] = true
	e.applications[submitted.ID] = submitted
	e.mu.Unlock()

	return submitted, nil
}

// RejectPending removes a pending application without submitting.
func (e *Engine) RejectPending(pendingID string) {
	e.mu.Lock()
	delete(e.pending, pendingID)
	e.mu.Unlock()
}

// PendingApplications returns all applications awaiting human review.
func (e *Engine) PendingApplications() []models.Application {
	e.mu.Lock()
	defer e.mu.Unlock()
	apps := make([]models.Application, 0, len(e.pending))
	for _, app := range e.pending {
		apps = append(apps, *app)
	}
	return apps
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

// DailyRemaining returns how many applications a user has left today.
func (e *Engine) DailyRemaining(userID string) int {
	return e.guardrails.DailyRemaining(userID)
}

// Stats returns summary statistics.
func (e *Engine) Stats() (total, applied, pending, failed int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	pending = len(e.pending)
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
