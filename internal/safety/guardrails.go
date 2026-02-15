package safety

import (
	"fmt"
	"sync"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Guardrails prevents users from getting flagged as spam applicants.
// It enforces per-platform daily caps, per-company cooldowns, match score
// floors, and requires human confirmation before submitting.
type Guardrails struct {
	cfg    GuardrailConfig
	mu     sync.Mutex
	daily  map[string]*dailyTracker // "user_id:platform" -> daily tracker
	perCo  map[string]time.Time     // "user_id:company" -> last apply time
}

// GuardrailConfig controls the safety thresholds.
type GuardrailConfig struct {
	// MaxApplyPerPlatformPerDay is the ceiling per ATS platform per day.
	// Each platform (Greenhouse, Ashby, Workday) tracks independently —
	// Greenhouse doesn't know about your Ashby apps and vice versa.
	MaxApplyPerPlatformPerDay int `json:"max_apply_per_platform_per_day"`

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
		MaxApplyPerPlatformPerDay: 11,  // per platform, not global
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
	ViolationPlatformDailyLimit ViolationCode = "PLATFORM_DAILY_LIMIT"
	ViolationCompanyCooldown    ViolationCode = "COMPANY_COOLDOWN"
	ViolationLowScore           ViolationCode = "LOW_MATCH_SCORE"
	ViolationNeedsReview        ViolationCode = "NEEDS_HUMAN_REVIEW"
	ViolationPendingFull        ViolationCode = "PENDING_QUEUE_FULL"
	ViolationDuplicate          ViolationCode = "DUPLICATE_APPLICATION"
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

	// 3. Per-platform daily limit — each ATS is independent.
	// Greenhouse has no idea what you did on Ashby, so we track separately.
	today := time.Now().Format("2006-01-02")
	platformKey := fmt.Sprintf("%s:%s", userID, job.Platform)
	tracker, ok := g.daily[platformKey]
	if !ok || tracker.resetDate != today {
		tracker = &dailyTracker{count: 0, resetDate: today}
		g.daily[platformKey] = tracker
	}

	if tracker.count >= g.cfg.MaxApplyPerPlatformPerDay {
		return &Violation{
			Code: ViolationPlatformDailyLimit,
			Message: fmt.Sprintf(
				"%s daily limit reached (%d/%d). Spreading applications across days looks more natural to recruiters.",
				job.Platform, tracker.count, g.cfg.MaxApplyPerPlatformPerDay,
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

// RecordApplication marks that a user applied to a job on a platform.
// Call this AFTER a successful application.
func (g *Guardrails) RecordApplication(userID string, company string, platform models.Platform) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Increment per-platform daily count.
	today := time.Now().Format("2006-01-02")
	platformKey := fmt.Sprintf("%s:%s", userID, platform)
	tracker, ok := g.daily[platformKey]
	if !ok || tracker.resetDate != today {
		tracker = &dailyTracker{count: 0, resetDate: today}
		g.daily[platformKey] = tracker
	}
	tracker.count++

	// Record company cooldown.
	companyKey := fmt.Sprintf("%s:%s", userID, company)
	g.perCo[companyKey] = time.Now()
}

// DailyRemainingByPlatform returns how many applications a user has left
// today on each platform.
func (g *Guardrails) DailyRemainingByPlatform(userID string) map[models.Platform]int {
	g.mu.Lock()
	defer g.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	platforms := []models.Platform{models.PlatformAshby, models.PlatformGreenhouse, models.PlatformWorkday}
	remaining := make(map[models.Platform]int, len(platforms))

	for _, p := range platforms {
		key := fmt.Sprintf("%s:%s", userID, p)
		tracker, ok := g.daily[key]
		if !ok || tracker.resetDate != today {
			remaining[p] = g.cfg.MaxApplyPerPlatformPerDay
		} else {
			r := g.cfg.MaxApplyPerPlatformPerDay - tracker.count
			if r < 0 {
				r = 0
			}
			remaining[p] = r
		}
	}

	return remaining
}
