package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/internal/applicant"
	"github.com/jamesrausch100/curly-chainsaw/internal/config"
	"github.com/jamesrausch100/curly-chainsaw/internal/discovery"
	"github.com/jamesrausch100/curly-chainsaw/internal/matcher"
	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Agent is the main orchestrator that ties discovery, matching, and
// application together into a single coherent loop.
type Agent struct {
	cfg       *config.Config
	discovery *discovery.Engine
	matcher   *matcher.Matcher
	applicant *applicant.Engine
	logger    Logger
}

// Logger is a simple logging interface.
type Logger interface {
	Info(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// defaultLogger prints to stdout.
type defaultLogger struct{}

func (l defaultLogger) Info(msg string, args ...interface{})  { fmt.Printf("[INFO]  "+msg+"\n", args...) }
func (l defaultLogger) Error(msg string, args ...interface{}) { fmt.Printf("[ERROR] "+msg+"\n", args...) }

// New creates a new agent.
func New(cfg *config.Config, disc *discovery.Engine, match *matcher.Matcher, app *applicant.Engine) *Agent {
	return &Agent{
		cfg:       cfg,
		discovery: disc,
		matcher:   match,
		applicant: app,
		logger:    defaultLogger{},
	}
}

// SetLogger overrides the default logger.
func (a *Agent) SetLogger(l Logger) {
	a.logger = l
}

// RunResult captures the outcome of a single agent run.
type RunResult struct {
	Discovered   int
	NewJobs      int
	Matched      int
	Applied      int
	Pending      int // awaiting human review
	Blocked      int // blocked by guardrails
	Skipped      int
	Failed       int
	TopMatches   []models.MatchResult
	Errors       []string
	Warnings     []string
	Duration     time.Duration
}

// Run executes a single discovery -> match -> apply cycle.
func (a *Agent) Run(ctx context.Context) RunResult {
	start := time.Now()
	result := RunResult{}

	// 1. Discover jobs.
	a.logger.Info("Discovering jobs across %d platforms...", len(a.cfg.Agent.SearchKeywords))

	query := models.SearchQuery{
		Keywords: a.cfg.Agent.SearchKeywords,
	}

	if a.cfg.Profile.RemoteOnly {
		remote := true
		query.Remote = &remote
	}

	discResult := a.discovery.Discover(ctx, query)
	result.Discovered = len(discResult.Jobs)
	result.NewJobs = discResult.NewCount

	for _, e := range discResult.Errors {
		result.Errors = append(result.Errors, e.Error())
		a.logger.Error("Platform error: %s", e.Error())
	}

	a.logger.Info("Found %d jobs (%d new) in %s", result.Discovered, result.NewJobs, discResult.Duration)

	if len(discResult.Jobs) == 0 {
		result.Duration = time.Since(start)
		return result
	}

	// 2. Match jobs against profile.
	a.logger.Info("Matching %d jobs against profile...", len(discResult.Jobs))

	matches := a.matcher.Match(discResult.Jobs, a.cfg.Profile)
	result.Matched = len(matches)

	// Keep top matches for reporting.
	topN := 20
	if len(matches) < topN {
		topN = len(matches)
	}
	result.TopMatches = matches[:topN]

	a.logger.Info("Found %d matches (top score: %.2f)", result.Matched, topScore(matches))

	// 3. Apply (if auto-apply is enabled) — with full safety guardrails.
	if a.cfg.Agent.AutoApply {
		remaining := a.applicant.DailyRemainingByPlatform(a.cfg.Profile.ID)
		a.logger.Info("Auto-applying (min score: %.0f%%)...", a.cfg.Agent.MinMatchScore*100)
		for platform, left := range remaining {
			a.logger.Info("  %s: %d applications remaining today", platform, left)
		}

		applyResult := a.applicant.ApplyToMatches(ctx, matches, a.cfg.Profile, a.cfg.Profile.ID)

		result.Applied = len(applyResult.Applied)
		result.Pending = len(applyResult.Pending)
		result.Blocked = len(applyResult.Blocked)
		result.Skipped = len(applyResult.Skipped)
		result.Failed = len(applyResult.Failed)

		for _, b := range applyResult.Blocked {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("[%s] %s at %s: %s", b.Violation.Code, b.Job.Title, b.Job.Company, b.Violation.Message))
		}

		for _, p := range applyResult.Pending {
			a.logger.Info("Pending review: %s at %s (%.0f%% match)", p.Job.Title, p.Job.Company, p.MatchScore*100)
		}

		for _, f := range applyResult.Failed {
			result.Errors = append(result.Errors, fmt.Sprintf("apply failed for %s at %s: %s", f.Job.Title, f.Job.Company, f.Err))
			a.logger.Error("Failed to apply: %s at %s — %s", f.Job.Title, f.Job.Company, f.Err)
		}

		a.logger.Info("Applied: %d | Pending: %d | Blocked: %d | Skipped: %d | Failed: %d",
			result.Applied, result.Pending, result.Blocked, result.Skipped, result.Failed)
	} else {
		a.logger.Info("Auto-apply disabled. Use --apply to enable.")
	}

	result.Duration = time.Since(start)
	a.logger.Info("Run complete in %s", result.Duration)

	return result
}

// Watch runs the agent in a continuous loop with a configurable interval.
func (a *Agent) Watch(ctx context.Context) error {
	interval := time.Duration(a.cfg.Agent.PollingIntervalS) * time.Second
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}

	a.logger.Info("Starting watch mode (interval: %s)", interval)

	for {
		result := a.Run(ctx)
		printRunSummary(result)

		select {
		case <-ctx.Done():
			a.logger.Info("Watch mode stopped.")
			return ctx.Err()
		case <-time.After(interval):
			// Next run.
		}
	}
}

func topScore(matches []models.MatchResult) float64 {
	if len(matches) == 0 {
		return 0
	}
	return matches[0].Score
}

func printRunSummary(r RunResult) {
	fmt.Println("\n========================================")
	fmt.Printf("  curly-chainsaw run complete\n")
	fmt.Printf("  Duration:   %s\n", r.Duration.Round(time.Millisecond))
	fmt.Printf("  Discovered: %d jobs (%d new)\n", r.Discovered, r.NewJobs)
	fmt.Printf("  Matched:    %d\n", r.Matched)
	fmt.Printf("  Applied:    %d\n", r.Applied)
	fmt.Printf("  Pending:    %d (awaiting review)\n", r.Pending)
	fmt.Printf("  Blocked:    %d (guardrails)\n", r.Blocked)
	fmt.Printf("  Skipped:    %d\n", r.Skipped)
	fmt.Printf("  Failed:     %d\n", r.Failed)
	fmt.Println("========================================")

	if len(r.TopMatches) > 0 {
		fmt.Println("\nTop Matches:")
		for i, m := range r.TopMatches {
			if i >= 10 {
				fmt.Printf("  ... and %d more\n", len(r.TopMatches)-10)
				break
			}
			marker := "  "
			if m.Score >= 0.8 {
				marker = "* "
			}
			fmt.Printf("  %s[%.0f%%] %s @ %s — %s\n", marker, m.Score*100, m.Job.Title, m.Job.Company, m.Reason)
		}
	}

	if len(r.Warnings) > 0 {
		fmt.Printf("\nGuardrails (%d):\n", len(r.Warnings))
		for _, w := range r.Warnings {
			fmt.Printf("  - %s\n", w)
		}
	}

	if len(r.Errors) > 0 {
		fmt.Printf("\nErrors (%d):\n", len(r.Errors))
		for _, e := range r.Errors {
			fmt.Printf("  - %s\n", e)
		}
	}
	fmt.Println()
}
