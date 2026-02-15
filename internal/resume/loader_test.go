package resume

import (
	"testing"

	"github.com/jamesrausch100/curly-chainsaw/internal/config"
)

func TestLoadJamesProfile(t *testing.T) {
	cfg, err := config.Load("../../profiles/james_rausch.json")
	if err != nil {
		t.Fatalf("failed to load james config: %v", err)
	}

	if cfg.Profile.Name != "James Nicholas Rausch" {
		t.Fatalf("name: got %q", cfg.Profile.Name)
	}
	if cfg.Profile.Email != "JamesRausch100@gmail.com" {
		t.Fatalf("email: got %q", cfg.Profile.Email)
	}
	if cfg.Profile.Location != "Denver, Colorado" {
		t.Fatalf("location: got %q", cfg.Profile.Location)
	}
	if len(cfg.Profile.Skills) == 0 {
		t.Fatal("expected skills")
	}
	if len(cfg.Profile.Experience) != 4 {
		t.Fatalf("expected 4 experience entries, got %d", len(cfg.Profile.Experience))
	}
	if cfg.Profile.Experience[0].Company != "Market2Agent.com" {
		t.Fatalf("first company: got %q", cfg.Profile.Experience[0].Company)
	}
	if len(cfg.Profile.Education) != 1 {
		t.Fatalf("expected 1 education entry, got %d", len(cfg.Profile.Education))
	}
	if cfg.Profile.Preferences.MinSalary != 90000 {
		t.Fatalf("min salary: got %d", cfg.Profile.Preferences.MinSalary)
	}
	if len(cfg.Agent.SearchKeywords) == 0 {
		t.Fatal("expected search keywords")
	}
	if cfg.Safety.MaxApplyPerPlatformPerDay != 11 {
		t.Fatalf("safety limit: got %d", cfg.Safety.MaxApplyPerPlatformPerDay)
	}

	t.Logf("Profile loaded: %s (%s) — %d skills, %d experience, %d keywords",
		cfg.Profile.Name, cfg.Profile.Location,
		len(cfg.Profile.Skills), len(cfg.Profile.Experience),
		len(cfg.Agent.SearchKeywords))
}
