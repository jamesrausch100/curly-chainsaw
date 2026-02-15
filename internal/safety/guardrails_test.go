package safety

import (
	"fmt"
	"testing"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

func newTestJob(platform models.Platform, company, title string) models.Job {
	return models.Job{
		ID:       "job_" + title,
		Platform: platform,
		Company:  company,
		Title:    title,
	}
}

func TestGuardrails_BlocksDuplicates(t *testing.T) {
	g := NewGuardrails(DefaultGuardrailConfig())
	job := newTestJob(models.PlatformAshby, "Acme", "Backend Engineer")

	v := g.CheckApplication("user1", job, 0.9, true, 0)
	if v == nil {
		t.Fatal("expected duplicate violation")
	}
	if v.Code != ViolationDuplicate {
		t.Fatalf("expected DUPLICATE_APPLICATION, got %s", v.Code)
	}
}

func TestGuardrails_BlocksLowScore(t *testing.T) {
	g := NewGuardrails(DefaultGuardrailConfig()) // min score 0.6
	job := newTestJob(models.PlatformGreenhouse, "Acme", "Janitor")

	v := g.CheckApplication("user1", job, 0.3, false, 0)
	if v == nil {
		t.Fatal("expected low score violation")
	}
	if v.Code != ViolationLowScore {
		t.Fatalf("expected LOW_MATCH_SCORE, got %s", v.Code)
	}
}

func TestGuardrails_PerPlatformDailyLimit(t *testing.T) {
	cfg := DefaultGuardrailConfig()
	cfg.MaxApplyPerPlatformPerDay = 3
	cfg.RequireConfirmation = false // disable human review for this test
	g := NewGuardrails(cfg)

	ashbyJob := newTestJob(models.PlatformAshby, "AshbyCorp", "SWE")
	ghJob := newTestJob(models.PlatformGreenhouse, "GreenCorp", "SWE")

	// Apply 3 times on Ashby (different companies to avoid cooldown).
	for i := 0; i < 3; i++ {
		g.RecordApplication("user1", fmt.Sprintf("AshbyCo%d", i), models.PlatformAshby)
	}

	// 4th Ashby should be blocked.
	v := g.CheckApplication("user1", ashbyJob, 0.9, false, 0)
	if v == nil {
		t.Fatal("expected platform daily limit violation for Ashby")
	}
	if v.Code != ViolationPlatformDailyLimit {
		t.Fatalf("expected PLATFORM_DAILY_LIMIT, got %s", v.Code)
	}

	// Greenhouse should still be wide open — independent counter.
	v = g.CheckApplication("user1", ghJob, 0.9, false, 0)
	if v != nil {
		t.Fatalf("greenhouse should not be blocked, got: %s", v.Code)
	}
}

func TestGuardrails_CompanyCooldown(t *testing.T) {
	cfg := DefaultGuardrailConfig()
	cfg.CompanyCooldownHours = 24
	cfg.RequireConfirmation = false
	g := NewGuardrails(cfg)

	// Apply to Acme on Ashby.
	g.RecordApplication("user1", "Acme", models.PlatformAshby)

	// Now try to apply to Acme on Greenhouse — same company, different platform.
	job := newTestJob(models.PlatformGreenhouse, "Acme", "Frontend Eng")
	v := g.CheckApplication("user1", job, 0.9, false, 0)
	if v == nil {
		t.Fatal("expected company cooldown violation")
	}
	if v.Code != ViolationCompanyCooldown {
		t.Fatalf("expected COMPANY_COOLDOWN, got %s", v.Code)
	}
}

func TestGuardrails_DifferentCompaniesNoConflict(t *testing.T) {
	cfg := DefaultGuardrailConfig()
	cfg.CompanyCooldownHours = 48
	cfg.RequireConfirmation = false
	g := NewGuardrails(cfg)

	g.RecordApplication("user1", "Acme", models.PlatformAshby)

	// Different company should be fine.
	job := newTestJob(models.PlatformAshby, "Globex", "SWE")
	v := g.CheckApplication("user1", job, 0.9, false, 0)
	if v != nil {
		t.Fatalf("different company should not be blocked, got: %s", v.Code)
	}
}

func TestGuardrails_RequiresHumanReview(t *testing.T) {
	cfg := DefaultGuardrailConfig()
	cfg.RequireConfirmation = true
	g := NewGuardrails(cfg)

	job := newTestJob(models.PlatformAshby, "Acme", "SWE")
	v := g.CheckApplication("user1", job, 0.9, false, 0)
	if v == nil {
		t.Fatal("expected human review violation")
	}
	if v.Code != ViolationNeedsReview {
		t.Fatalf("expected NEEDS_HUMAN_REVIEW, got %s", v.Code)
	}
}

func TestGuardrails_PendingQueueFull(t *testing.T) {
	cfg := DefaultGuardrailConfig()
	cfg.RequireConfirmation = true
	cfg.MaxPendingReview = 5
	g := NewGuardrails(cfg)

	job := newTestJob(models.PlatformAshby, "Acme", "SWE")

	// With pendingCount at max, should get PENDING_QUEUE_FULL (not NEEDS_REVIEW).
	v := g.CheckApplication("user1", job, 0.9, false, 5)
	if v == nil {
		t.Fatal("expected pending full violation")
	}
	if v.Code != ViolationPendingFull {
		t.Fatalf("expected PENDING_QUEUE_FULL, got %s", v.Code)
	}
}

func TestGuardrails_AllClear(t *testing.T) {
	cfg := DefaultGuardrailConfig()
	cfg.RequireConfirmation = false
	g := NewGuardrails(cfg)

	job := newTestJob(models.PlatformAshby, "Acme", "SWE")
	v := g.CheckApplication("user1", job, 0.9, false, 0)
	if v != nil {
		t.Fatalf("expected all clear, got: %s", v.Code)
	}
}

func TestGuardrails_DailyRemainingByPlatform(t *testing.T) {
	cfg := DefaultGuardrailConfig()
	cfg.MaxApplyPerPlatformPerDay = 11
	g := NewGuardrails(cfg)

	remaining := g.DailyRemainingByPlatform("user1")

	// All platforms should start at 11.
	for _, p := range []models.Platform{models.PlatformAshby, models.PlatformGreenhouse, models.PlatformWorkday} {
		if remaining[p] != 11 {
			t.Fatalf("expected 11 remaining for %s, got %d", p, remaining[p])
		}
	}

	// Use 4 on Ashby.
	for i := 0; i < 4; i++ {
		g.RecordApplication("user1", "SomeCo", models.PlatformAshby)
	}

	remaining = g.DailyRemainingByPlatform("user1")
	if remaining[models.PlatformAshby] != 7 {
		t.Fatalf("expected 7 remaining for Ashby, got %d", remaining[models.PlatformAshby])
	}
	if remaining[models.PlatformGreenhouse] != 11 {
		t.Fatalf("expected 11 remaining for Greenhouse, got %d", remaining[models.PlatformGreenhouse])
	}
	if remaining[models.PlatformWorkday] != 11 {
		t.Fatalf("expected 11 remaining for Workday, got %d", remaining[models.PlatformWorkday])
	}
}

func TestGuardrails_DifferentUsersIndependent(t *testing.T) {
	cfg := DefaultGuardrailConfig()
	cfg.MaxApplyPerPlatformPerDay = 2
	cfg.RequireConfirmation = false
	g := NewGuardrails(cfg)

	// User1 burns their Ashby quota.
	g.RecordApplication("user1", "A", models.PlatformAshby)
	g.RecordApplication("user1", "B", models.PlatformAshby)

	job := newTestJob(models.PlatformAshby, "C", "SWE")

	// User1 blocked.
	v := g.CheckApplication("user1", job, 0.9, false, 0)
	if v == nil || v.Code != ViolationPlatformDailyLimit {
		t.Fatal("user1 should be blocked on Ashby")
	}

	// User2 should be fine — separate counters.
	v = g.CheckApplication("user2", job, 0.9, false, 0)
	if v != nil {
		t.Fatalf("user2 should not be blocked, got: %s", v.Code)
	}
}

func TestGuardrails_ViolationError(t *testing.T) {
	v := Violation{Code: ViolationLowScore, Message: "score too low"}
	expected := "[LOW_MATCH_SCORE] score too low"
	if v.Error() != expected {
		t.Fatalf("expected %q, got %q", expected, v.Error())
	}
}
