package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/internal/agent"
	"github.com/jamesrausch100/curly-chainsaw/internal/applicant"
	"github.com/jamesrausch100/curly-chainsaw/internal/ats"
	"github.com/jamesrausch100/curly-chainsaw/internal/ats/ashby"
	"github.com/jamesrausch100/curly-chainsaw/internal/ats/greenhouse"
	"github.com/jamesrausch100/curly-chainsaw/internal/ats/workday"
	"github.com/jamesrausch100/curly-chainsaw/internal/config"
	"github.com/jamesrausch100/curly-chainsaw/internal/discovery"
	"github.com/jamesrausch100/curly-chainsaw/internal/matcher"
	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

const banner = `
                  _                _           _
  ___ _   _ _ __| |_   _      ___| |__   __ _(_)_ __  ___  __ ___      __
 / __| | | | '__| | | | |    / __| '_ \ / _' | | '_ \/ __|/ _' \ \ /\ / /
| (__| |_| | |  | | |_| |   | (__| | | | (_| | | | | \__ \ (_| |\ V  V /
 \___|\__,_|_|  |_|\__, |    \___|_| |_|\__,_|_|_| |_|___/\__,_| \_/\_/
                   |___/
        a job for you. a job for me. a job for everyone.
`

func main() {
	// Subcommands.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			cmdInit()
			return
		case "run":
			cmdRun(os.Args[2:])
			return
		case "watch":
			cmdWatch(os.Args[2:])
			return
		case "search":
			cmdSearch(os.Args[2:])
			return
		case "status":
			cmdStatus()
			return
		case "version":
			fmt.Println("curly-chainsaw v0.1.0")
			return
		case "help", "--help", "-h":
			printHelp()
			return
		}
	}

	fmt.Print(banner)
	printHelp()
}

func printHelp() {
	fmt.Println(`
Usage: curly <command> [options]

Commands:
  init      Generate an example config file to get started
  run       Run a single discover -> match -> apply cycle
  watch     Continuously watch for new jobs and apply
  search    Quick search across all platforms
  status    Show application statistics
  version   Print version
  help      Show this help

Options (for run/watch):
  --config    Path to config file (default: ~/.curly-chainsaw/config.json)
  --apply     Enable auto-apply (overrides config)
  --dry-run   Discover and match only, don't apply
  --keywords  Comma-separated search keywords (overrides config)

Get started:
  1. curly init
  2. Edit ~/.curly-chainsaw/config.json with your profile and companies
  3. curly run --dry-run
  4. curly run --apply
`)
}

func cmdInit() {
	path := config.DefaultConfigPath()
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("Config already exists at %s\n", path)
		fmt.Println("Delete it first if you want to regenerate.")
		os.Exit(1)
	}

	if err := config.GenerateExample(path); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating config: %s\n", err)
		os.Exit(1)
	}

	fmt.Printf("Example config generated at: %s\n", path)
	fmt.Println("Edit it with your profile, skills, and target companies.")
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	apply := fs.Bool("apply", false, "enable auto-apply")
	dryRun := fs.Bool("dry-run", false, "discover and match only")
	keywords := fs.String("keywords", "", "comma-separated search keywords")
	fs.Parse(args)

	cfg := loadConfig(*configPath)

	if *apply {
		cfg.Agent.AutoApply = true
	}
	if *dryRun {
		cfg.Agent.AutoApply = false
	}
	if *keywords != "" {
		cfg.Agent.SearchKeywords = strings.Split(*keywords, ",")
	}

	fmt.Print(banner)

	ag := buildAgent(cfg)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	ag.Run(ctx)
}

func cmdWatch(args []string) {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	apply := fs.Bool("apply", false, "enable auto-apply")
	keywords := fs.String("keywords", "", "comma-separated search keywords")
	interval := fs.Int("interval", 0, "polling interval in seconds")
	fs.Parse(args)

	cfg := loadConfig(*configPath)

	if *apply {
		cfg.Agent.AutoApply = true
	}
	if *keywords != "" {
		cfg.Agent.SearchKeywords = strings.Split(*keywords, ",")
	}
	if *interval > 0 {
		cfg.Agent.PollingIntervalS = *interval
	}

	fmt.Print(banner)
	fmt.Println("Starting watch mode... (Ctrl+C to stop)")

	ag := buildAgent(cfg)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := ag.Watch(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Watch error: %s\n", err)
		os.Exit(1)
	}
}

func cmdSearch(args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	keywords := fs.String("keywords", "", "comma-separated search keywords (required)")
	platform := fs.String("platform", "", "limit to platform: ashby, greenhouse, workday")
	fs.Parse(args)

	if *keywords == "" {
		fmt.Fprintln(os.Stderr, "Error: --keywords is required for search")
		os.Exit(1)
	}

	cfg := loadConfig(*configPath)
	cfg.Agent.SearchKeywords = strings.Split(*keywords, ",")
	cfg.Agent.AutoApply = false

	fmt.Printf("Searching for: %s\n\n", *keywords)

	registry := buildRegistry(cfg)

	disc := discovery.New(registry)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	query := buildSearchQuery(cfg, *platform)
	result := disc.Discover(ctx, query)

	if len(result.Jobs) == 0 {
		fmt.Println("No jobs found.")
		return
	}

	fmt.Printf("Found %d jobs:\n\n", len(result.Jobs))
	for i, j := range result.Jobs {
		remote := ""
		if j.Remote {
			remote = " [Remote]"
		}
		fmt.Printf("  %d. %s @ %s%s\n     %s | %s\n     %s\n\n",
			i+1, j.Title, j.Company, remote,
			j.Location, j.Platform, j.URL)
	}
}

func cmdStatus() {
	fmt.Println("Application status tracking coming soon.")
	fmt.Println("(Requires persistent storage — SQLite or JSON file)")
}

func loadConfig(path string) *config.Config {
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config from %s: %s\n", path, err)
		fmt.Fprintln(os.Stderr, "Run 'curly init' to create a config file.")
		os.Exit(1)
	}
	return cfg
}

func buildRegistry(cfg *config.Config) *ats.Registry {
	registry := ats.NewRegistry()

	if cfg.Platforms.Ashby.Enabled {
		registry.Register(ashby.New(
			ashby.WithBoardTokens(cfg.Platforms.Ashby.BoardTokens),
		))
	}

	if cfg.Platforms.Greenhouse.Enabled {
		opts := []greenhouse.Option{
			greenhouse.WithBoardTokens(cfg.Platforms.Greenhouse.BoardTokens),
		}
		if cfg.Platforms.Greenhouse.APIKey != "" {
			opts = append(opts, greenhouse.WithAPIKey(cfg.Platforms.Greenhouse.APIKey))
		}
		registry.Register(greenhouse.New(opts...))
	}

	if cfg.Platforms.Workday.Enabled {
		registry.Register(workday.New(
			workday.WithTenants(cfg.Platforms.Workday.Tenants),
		))
	}

	return registry
}

func buildAgent(cfg *config.Config) *agent.Agent {
	registry := buildRegistry(cfg)
	disc := discovery.New(registry)
	match := matcher.NewDefault()
	app := applicant.New(registry,
		applicant.WithMaxPerRun(cfg.Agent.MaxApplyPerRun),
	)

	return agent.New(cfg, disc, match, app)
}

func buildSearchQuery(cfg *config.Config, platformFilter string) models.SearchQuery {
	query := models.SearchQuery{
		Keywords: cfg.Agent.SearchKeywords,
	}

	if platformFilter != "" {
		query.Platforms = []models.Platform{models.Platform(platformFilter)}
	}

	if cfg.Profile.RemoteOnly {
		remote := true
		query.Remote = &remote
	}

	return query
}
