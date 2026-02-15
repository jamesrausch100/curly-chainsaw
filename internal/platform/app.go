// Package platform provides a unified API bridge that powers the desktop app,
// web app, and mobile app. All three surfaces call through this layer so the
// UX is consistent regardless of where curly-chainsaw is running.
package platform

import (
	"context"
	"sync"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/internal/agent"
	"github.com/jamesrausch100/curly-chainsaw/internal/applicant"
	"github.com/jamesrausch100/curly-chainsaw/internal/ats"
	"github.com/jamesrausch100/curly-chainsaw/internal/config"
	"github.com/jamesrausch100/curly-chainsaw/internal/discovery"
	"github.com/jamesrausch100/curly-chainsaw/internal/matcher"
	"github.com/jamesrausch100/curly-chainsaw/internal/safety"
	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// App is the shared application core used by every platform surface
// (desktop, web, mobile). It holds all state and orchestration logic.
type App struct {
	mu        sync.RWMutex
	cfg       *config.Config
	registry  *ats.Registry
	discovery *discovery.Engine
	matcher   *matcher.Matcher
	applicant *applicant.Engine
	agent     *agent.Agent

	// Cached results from the last run.
	lastRun    *agent.RunResult
	lastRunAt  time.Time
	allMatches []models.MatchResult
}

// New creates a new platform App from the given configuration.
func New(cfg *config.Config) (*App, error) {
	registry := ats.NewRegistry()

	disc := discovery.New(registry)
	m := matcher.NewDefault()

	app := applicant.New(registry,
		applicant.WithGuardrailConfig(cfg.Safety),
		applicant.WithRateLimits(safety.DefaultLimits()),
	)

	ag := agent.New(cfg, disc, m, app)

	return &App{
		cfg:       cfg,
		registry:  registry,
		discovery: disc,
		matcher:   m,
		applicant: app,
		agent:     ag,
	}, nil
}

// Profile returns the user's current profile.
func (a *App) Profile() models.Profile {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.Profile
}

// UpdateProfile replaces the user's profile in the running config.
func (a *App) UpdateProfile(p models.Profile) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg.Profile = p
}

// SearchKeywords returns the configured search keywords.
func (a *App) SearchKeywords() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.Agent.SearchKeywords
}

// RunResult holds the results from a discovery+match+apply cycle.
type RunResult struct {
	Discovered int                 `json:"discovered"`
	Matched    int                 `json:"matched"`
	Applied    int                 `json:"applied"`
	Pending    int                 `json:"pending"`
	TopMatches []models.MatchResult `json:"top_matches"`
	Duration   time.Duration       `json:"duration"`
	Errors     []string            `json:"errors,omitempty"`
}

// Run executes a full discover -> match -> apply cycle.
func (a *App) Run(ctx context.Context) (*RunResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	result := a.agent.Run(ctx)

	a.lastRun = &result
	a.lastRunAt = time.Now()

	if result.TopMatches != nil {
		a.allMatches = append(a.allMatches, result.TopMatches...)
	}

	return &RunResult{
		Discovered: result.Discovered,
		Matched:    result.Matched,
		Applied:    result.Applied,
		Pending:    result.Pending,
		TopMatches: result.TopMatches,
		Duration:   result.Duration,
		Errors:     result.Errors,
	}, nil
}

// Search performs a one-off search without applying.
func (a *App) Search(ctx context.Context, keywords []string) ([]models.MatchResult, error) {
	a.mu.RLock()
	profile := a.cfg.Profile
	a.mu.RUnlock()

	query := models.SearchQuery{Keywords: keywords}
	disc := a.discovery.Discover(ctx, query)

	results := a.matcher.Match(disc.Jobs, profile)
	return results, nil
}

// Matches returns all match results accumulated across runs.
func (a *App) Matches() []models.MatchResult {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.allMatches
}

// LastRunTime returns when the last agent run completed.
func (a *App) LastRunTime() time.Time {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastRunAt
}

// Config returns the current configuration.
func (a *App) Config() *config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

// SaveConfig persists the current config to disk.
func (a *App) SaveConfig(path string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return config.Save(a.cfg, path)
}

// Stats returns summary statistics for the UI.
type Stats struct {
	TotalDiscovered int       `json:"total_discovered"`
	TotalMatched    int       `json:"total_matched"`
	TotalApplied    int       `json:"total_applied"`
	TotalPending    int       `json:"total_pending"`
	LastRunAt       time.Time `json:"last_run_at,omitempty"`
	Uptime          string    `json:"uptime"`
}

// GetStats returns current session statistics.
func (a *App) GetStats() Stats {
	a.mu.RLock()
	defer a.mu.RUnlock()

	s := Stats{
		TotalMatched: len(a.allMatches),
		LastRunAt:    a.lastRunAt,
	}

	if a.lastRun != nil {
		s.TotalDiscovered = a.lastRun.Discovered
		s.TotalApplied = a.lastRun.Applied
		s.TotalPending = a.lastRun.Pending
	}

	return s
}
