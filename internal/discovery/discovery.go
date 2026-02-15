package discovery

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/internal/ats"
	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Engine discovers and aggregates jobs from all registered ATS platforms.
type Engine struct {
	registry *ats.Registry
	seen     map[string]bool // dedup by fingerprint
	mu       sync.Mutex
}

// New creates a new discovery engine.
func New(registry *ats.Registry) *Engine {
	return &Engine{
		registry: registry,
		seen:     make(map[string]bool),
	}
}

// Result holds the outcome of a discovery run.
type Result struct {
	Jobs     []models.Job
	NewCount int
	Errors   []PlatformError
	Duration time.Duration
}

// PlatformError captures a per-platform failure without killing the whole run.
type PlatformError struct {
	Platform models.Platform
	Err      error
}

func (e PlatformError) Error() string {
	return fmt.Sprintf("%s: %s", e.Platform, e.Err)
}

// Discover fans out to all platforms concurrently, deduplicates results,
// and returns a sorted aggregated list.
func (e *Engine) Discover(ctx context.Context, query models.SearchQuery) Result {
	start := time.Now()

	clients := e.registry.All()

	// Filter to requested platforms if specified.
	if len(query.Platforms) > 0 {
		wanted := make(map[models.Platform]bool)
		for _, p := range query.Platforms {
			wanted[p] = true
		}
		var filtered []ats.Client
		for _, c := range clients {
			if wanted[c.Platform()] {
				filtered = append(filtered, c)
			}
		}
		clients = filtered
	}

	type platformResult struct {
		jobs []models.Job
		err  error
		plat models.Platform
	}

	ch := make(chan platformResult, len(clients))

	// Fan out — one goroutine per platform.
	for _, client := range clients {
		go func(c ats.Client) {
			jobs, err := c.SearchJobs(ctx, query)
			ch <- platformResult{jobs: jobs, err: err, plat: c.Platform()}
		}(client)
	}

	var allJobs []models.Job
	var errors []PlatformError

	for range clients {
		pr := <-ch
		if pr.err != nil {
			errors = append(errors, PlatformError{Platform: pr.plat, Err: pr.err})
			continue
		}
		allJobs = append(allJobs, pr.jobs...)
	}

	// Deduplicate.
	deduped := e.deduplicate(allJobs)

	// Sort by posted date descending (newest first).
	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].PostedAt.After(deduped[j].PostedAt)
	})

	newCount := 0
	for _, j := range deduped {
		fp := fingerprint(j)
		e.mu.Lock()
		if !e.seen[fp] {
			e.seen[fp] = true
			newCount++
		}
		e.mu.Unlock()
	}

	return Result{
		Jobs:     deduped,
		NewCount: newCount,
		Errors:   errors,
		Duration: time.Since(start),
	}
}

// deduplicate removes duplicate jobs based on a fingerprint of company+title+location.
func (e *Engine) deduplicate(jobs []models.Job) []models.Job {
	seen := make(map[string]bool)
	var unique []models.Job

	for _, j := range jobs {
		fp := fingerprint(j)
		if !seen[fp] {
			seen[fp] = true
			unique = append(unique, j)
		}
	}

	return unique
}

// fingerprint generates a dedup key for a job.
func fingerprint(j models.Job) string {
	raw := strings.ToLower(fmt.Sprintf("%s|%s|%s", j.Company, j.Title, j.Location))
	hash := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", hash[:8])
}

// ResetSeen clears the seen jobs cache (e.g., for a fresh run).
func (e *Engine) ResetSeen() {
	e.mu.Lock()
	e.seen = make(map[string]bool)
	e.mu.Unlock()
}
