package safety

import (
	"fmt"
	"sync"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Guardrails prevents users from getting flagged as spam applicants.
// It enforces daily caps, per-company cooldowns, match score floors,
// and requires human confirmation before submitting.
type Guardrails struct {
	cfg    GuardrailConfig
	mu     sync.Mutex
	daily  map[string]*dailyTracker  // user_id -> daily tracker
	perCo  map[string]time.Time      // "user_id:company" -> last apply time
}

// GuardrailConfig controls the safety thresholds.
type GuardrailConfig struct {
	// MaxApplyPerDay is the hard ceiling on applications per user per day.
	// Even the most aggressive real human rarely applies to more than 20/day.
	MaxApplyPerDay int `json:"max_apply_per_day"`

	// MaxApplyPerCompanyPerWeek prevents spamming the same company.
	MaxApplyPerCompanyPerWeek int `json:"max_apply_per_company_per_week"`

	// CompanyCooldownHours is the minimum hours between applications to the same company.
	CompanyCooldownHours int `json:"company_cooldown_hours"`

	// MinMatchScore is the floor — don't apply to jobs below this score.
	// Prevents scatter-shot applications that make you look desperate.
	MinMatchScore float64 `json:"min_match_score"`

	// RequireConfirmation forces human-in-the-loop approval before applying.
	// When true, applications go to a "pending review" state first.
	RequireConfirmation bool `json:"require_confirmation"`

	// MaxPendingReview is the max number of pending applications.
	// Prevents queuing up hundreds of unreviewed applications.
	MaxPendingReview int `json:"max_pending_review"`
}

// DefaultGuardrailConfig returns conservative defaults.
func DefaultGuardrailConfig() GuardrailConfig {
	return GuardrailConfig{
		MaxApplyPerDay:            15,
		MaxApplyPerCompanyPerWeek: 3,
		CompanyCooldownHours:      48,
		MinMatchScore:             0.6,
		RequireConfirmation:       true, // safe default: require human review
		MaxPendingReview:          25,
	}
}

type dailyTracker struct {
	count     int
	resetDate string // YYYY-MM-DD
}

// NewGuardrails creates a new guardrails instance.
func NewGuardrails(cfg GuardrailConfig) *Guardrails {
	return &Guardrails{
		cfg:   cfg,
		daily: make(map[string]*dailyTracker),
		perCo: make(map[string]time.Time),
	}
}

// Violation describes why an application was blocked.
type Violation struct {
	Code    ViolationCode
	Message string
}

func (v Violation) Error() string {
	return fmt.Sprintf("[%s] %s", v.Code, v.Message)
}

type ViolationCode string

const (
	ViolationDailyLimit     ViolationCode = "DAILY_LIMIT"
	ViolationCompanyCooldown ViolationCode = "COMPANY_COOLDOWN"
	ViolationLowScore       ViolationCode = "LOW_MATCH_SCORE"
	ViolationNeedsReview    ViolationCode = "NEEDS_HUMAN_REVIEW"
	ViolationPendingFull    ViolationCode = "PENDING_QUEUE_FULL"
	ViolationDuplicate      ViolationCode = "DUPLICATE_APPLICATION"
)

// CheckApplication validates whether an application should proceed.
// Returns nil if safe, or a Violation explaining why it was blocked.
func (g *Guardrails) CheckApplication(userID string, job models.Job, matchScore float64, alreadyApplied bool, pendingCount int) *Violation {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 1. Duplicate check — already applied to this exact job.
	if alreadyApplied {
		return &Violation{
			Code:    ViolationDuplicate,
			Message: fmt.Sprintf("Already applied to %q at %s", job.Title, job.Company),
		}
	}

	// 2. Match score floor — don't apply to weak matches.
	if matchScore < g.cfg.MinMatchScore {
		return &Violation{
			Code: ViolationLowScore,
			Message: fmt.Sprintf(
				"Match score %.0f%% is below minimum %.0f%%. Applying to weak matches hurts your profile with recruiters.",
				matchScore*100, g.cfg.MinMatchScore*100,
			),
		}
	}

	// 3. Daily limit — no one should be applying to 50 jobs a day.
	today := time.Now().Format("2006-01-02")
	tracker, ok := g.daily[userID]
	if !ok || tracker.resetDate != today {
		tracker = &dailyTracker{count: 0, resetDate: today}
		g.daily[userID] = tracker
	}

	if tracker.count >= g.cfg.MaxApplyPerDay {
		return &Violation{
			Code: ViolationDailyLimit,
			Message: fmt.Sprintf(
				"Daily application limit reached (%d/%d). Spreading applications across days looks more natural to recruiters.",
				tracker.count, g.cfg.MaxApplyPerDay,
			),
		}
	}

	// 4. Company cooldown — don't hit the same company too fast.
	companyKey := fmt.Sprintf("%s:%s", userID, job.Company)
	if lastApply, exists := g.perCo[companyKey]; exists {
		cooldown := time.Duration(g.cfg.CompanyCooldownHours) * time.Hour
		if time.Since(lastApply) < cooldown {
			remaining := cooldown - time.Since(lastApply)
			return &Violation{
				Code: ViolationCompanyCooldown,
				Message: fmt.Sprintf(
					"Applied to %s recently. Wait %s before applying again to avoid looking like spam.",
					job.Company, remaining.Round(time.Minute),
				),
			}
		}
	}

	// 5. Pending review queue — don't pile up unreviewed applications.
	if g.cfg.RequireConfirmation && pendingCount >= g.cfg.MaxPendingReview {
		return &Violation{
			Code:    ViolationPendingFull,
			Message: fmt.Sprintf("Review your %d pending applications before queuing more.", pendingCount),
		}
	}

	// 6. Human-in-the-loop check.
	if g.cfg.RequireConfirmation {
		return &Violation{
			Code:    ViolationNeedsReview,
			Message: fmt.Sprintf("Application to %q at %s needs your review before submitting.", job.Title, job.Company),
		}
	}

	return nil // all clear
}

// RecordApplication marks that a user applied to a job at a company.
// Call this AFTER a successful application.
func (g *Guardrails) RecordApplication(userID string, company string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Increment daily count.
	today := time.Now().Format("2006-01-02")
	tracker, ok := g.daily[userID]
	if !ok || tracker.resetDate != today {
		tracker = &dailyTracker{count: 0, resetDate: today}
		g.daily[userID] = tracker
	}
	tracker.count++

	// Record company cooldown.
	companyKey := fmt.Sprintf("%s:%s", userID, company)
	g.perCo[companyKey] = time.Now()
}

// DailyRemaining returns how many applications a user has left today.
func (g *Guardrails) DailyRemaining(userID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	tracker, ok := g.daily[userID]
	if !ok || tracker.resetDate != today {
		return g.cfg.MaxApplyPerDay
	}
	remaining := g.cfg.MaxApplyPerDay - tracker.count
	if remaining < 0 {
		return 0
	}
	return remaining
}
