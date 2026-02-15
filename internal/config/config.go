package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jamesrausch100/curly-chainsaw/internal/ats/workday"
	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Config holds all application configuration.
type Config struct {
	Profile    models.Profile   `json:"profile"`
	Platforms  PlatformConfigs  `json:"platforms"`
	Agent      AgentConfig      `json:"agent"`
}

// PlatformConfigs holds per-platform configuration.
type PlatformConfigs struct {
	Ashby      AshbyConfig      `json:"ashby"`
	Greenhouse GreenhouseConfig `json:"greenhouse"`
	Workday    WorkdayConfig    `json:"workday"`
}

// AshbyConfig configures the Ashby integration.
type AshbyConfig struct {
	Enabled     bool     `json:"enabled"`
	BoardTokens []string `json:"board_tokens"` // company board slugs
}

// GreenhouseConfig configures the Greenhouse integration.
type GreenhouseConfig struct {
	Enabled     bool     `json:"enabled"`
	BoardTokens []string `json:"board_tokens"`
	APIKey      string   `json:"api_key,omitempty"`
}

// WorkdayConfig configures the Workday integration.
type WorkdayConfig struct {
	Enabled bool              `json:"enabled"`
	Tenants []workday.Tenant  `json:"tenants"`
}

// AgentConfig controls the agent behavior.
type AgentConfig struct {
	MinMatchScore    float64  `json:"min_match_score"`    // minimum score to auto-apply (0.0-1.0)
	MaxApplyPerRun   int      `json:"max_apply_per_run"`  // max applications per agent run
	SearchKeywords   []string `json:"search_keywords"`    // default search terms
	AutoApply        bool     `json:"auto_apply"`         // whether to auto-apply or just discover
	PollingIntervalS int      `json:"polling_interval_s"` // seconds between discovery runs (watch mode)
}

// DefaultConfig returns a sensible starting configuration.
func DefaultConfig() Config {
	return Config{
		Agent: AgentConfig{
			MinMatchScore:    0.5,
			MaxApplyPerRun:   10,
			AutoApply:        false, // safe default: discover only
			PollingIntervalS: 300,   // 5 minutes
		},
	}
}

// Load reads configuration from a JSON file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

// Save writes the configuration to a JSON file.
func Save(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// ConfigDir returns the default configuration directory.
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".curly"
	}
	return filepath.Join(home, ".curly-chainsaw")
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

// GenerateExample creates an example config file to get users started.
func GenerateExample(path string) error {
	cfg := Config{
		Profile: models.Profile{
			Name:     "Your Name",
			Email:    "you@example.com",
			Phone:    "+1-555-0100",
			Location: "San Francisco, CA",
			Skills:   []string{"Go", "Python", "Kubernetes", "AWS"},
			Preferences: models.JobPreferences{
				Titles:    []string{"Software Engineer", "Backend Engineer", "Platform Engineer"},
				MinSalary: 150000,
			},
			Links: map[string]string{
				"linkedin": "https://linkedin.com/in/yourprofile",
				"github":   "https://github.com/yourhandle",
			},
		},
		Platforms: PlatformConfigs{
			Ashby: AshbyConfig{
				Enabled:     true,
				BoardTokens: []string{"example-company"},
			},
			Greenhouse: GreenhouseConfig{
				Enabled:     true,
				BoardTokens: []string{"examplecorp"},
			},
			Workday: WorkdayConfig{
				Enabled: true,
				Tenants: []workday.Tenant{
					{
						Company:  "Example Corp",
						Tenant:   "examplecorp",
						Instance: "wd5",
						Site:     "ExampleCorp_Careers",
					},
				},
			},
		},
		Agent: AgentConfig{
			MinMatchScore:    0.5,
			MaxApplyPerRun:   10,
			SearchKeywords:   []string{"software engineer", "backend", "platform"},
			AutoApply:        false,
			PollingIntervalS: 300,
		},
	}

	return Save(&cfg, path)
}
