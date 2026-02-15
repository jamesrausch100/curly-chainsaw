package platform

import (
	"testing"

	"github.com/jamesrausch100/curly-chainsaw/internal/config"
	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{
		Profile: models.Profile{
			Name:       "Test User",
			Email:      "test@example.com",
			Location:   "Denver, CO",
			RemoteOnly: true,
			Skills:     []string{"Go", "Python"},
			Preferences: models.JobPreferences{
				Titles:    []string{"Software Engineer"},
				MinSalary: 100000,
			},
		},
		Agent: config.AgentConfig{
			MinMatchScore:  0.5,
			MaxApplyPerRun: 5,
			SearchKeywords: []string{"remote software engineer"},
		},
	}

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Profile should be accessible.
	p := app.Profile()
	if p.Name != "Test User" {
		t.Fatalf("profile name: got %q", p.Name)
	}
	if !p.RemoteOnly {
		t.Fatal("expected remote_only to be true")
	}

	// Keywords should be accessible.
	kw := app.SearchKeywords()
	if len(kw) != 1 || kw[0] != "remote software engineer" {
		t.Fatalf("keywords: got %v", kw)
	}

	// Stats should start at zero.
	stats := app.GetStats()
	if stats.TotalDiscovered != 0 || stats.TotalMatched != 0 || stats.TotalApplied != 0 {
		t.Fatalf("expected zeroed stats, got %+v", stats)
	}
}

func TestUpdateProfile(t *testing.T) {
	cfg := &config.Config{
		Profile: models.Profile{Name: "Old Name"},
	}

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	app.UpdateProfile(models.Profile{Name: "New Name", RemoteOnly: true})

	p := app.Profile()
	if p.Name != "New Name" {
		t.Fatalf("profile name after update: got %q", p.Name)
	}
	if !p.RemoteOnly {
		t.Fatal("expected remote_only after update")
	}
}

func TestMatches_EmptyInitially(t *testing.T) {
	cfg := &config.Config{}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	m := app.Matches()
	if len(m) != 0 {
		t.Fatalf("expected empty matches, got %d", len(m))
	}
}
