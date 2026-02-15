package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jamesrausch100/curly-chainsaw/internal/ats"
	"github.com/jamesrausch100/curly-chainsaw/internal/ats/ashby"
	"github.com/jamesrausch100/curly-chainsaw/internal/ats/greenhouse"
	"github.com/jamesrausch100/curly-chainsaw/internal/ats/workday"
	"github.com/jamesrausch100/curly-chainsaw/internal/config"
	"github.com/jamesrausch100/curly-chainsaw/internal/server"
	"github.com/jamesrausch100/curly-chainsaw/internal/storage"
)

func main() {
	addr := flag.String("addr", ":8080", "server listen address")
	configPath := flag.String("config", config.DefaultConfigPath(), "config file path")
	redisAddr := flag.String("redis", "localhost:6379", "redis address")
	redisPassword := flag.String("redis-password", "", "redis password")
	redisDB := flag.Int("redis-db", 0, "redis database number")
	flag.Parse()

	fmt.Println(`
                  _                _           _
  ___ _   _ _ __| |_   _      ___| |__   __ _(_)_ __  ___  __ ___      __
 / __| | | | '__| | | | |    / __| '_ \ / _' | | '_ \/ __|/ _' \ \ /\ / /
| (__| |_| | |  | | |_| |   | (__| | | | (_| | | | | \__ \ (_| |\ V  V /
 \___|\__,_|_|  |_|\__, |    \___|_| |_|\__,_|_|_| |_|___/\__,_| \_/\_/
                   |___/
        a job for you. a job for me. a job for everyone.
`)

	// Load config.
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config (%s), using defaults\n", err)
		defaultCfg := config.DefaultConfig()
		cfg = &defaultCfg
	}

	// Connect to Redis.
	store := storage.New(*redisAddr, *redisPassword, *redisDB)
	fmt.Printf("Connecting to Redis at %s...\n", *redisAddr)

	// Build ATS registry.
	registry := ats.NewRegistry()

	if cfg.Platforms.Ashby.Enabled {
		registry.Register(ashby.New(
			ashby.WithBoardTokens(cfg.Platforms.Ashby.BoardTokens),
		))
		fmt.Printf("  Ashby: %d boards\n", len(cfg.Platforms.Ashby.BoardTokens))
	}

	if cfg.Platforms.Greenhouse.Enabled {
		opts := []greenhouse.Option{
			greenhouse.WithBoardTokens(cfg.Platforms.Greenhouse.BoardTokens),
		}
		if cfg.Platforms.Greenhouse.APIKey != "" {
			opts = append(opts, greenhouse.WithAPIKey(cfg.Platforms.Greenhouse.APIKey))
		}
		registry.Register(greenhouse.New(opts...))
		fmt.Printf("  Greenhouse: %d boards\n", len(cfg.Platforms.Greenhouse.BoardTokens))
	}

	if cfg.Platforms.Workday.Enabled {
		registry.Register(workday.New(
			workday.WithTenants(cfg.Platforms.Workday.Tenants),
		))
		fmt.Printf("  Workday: %d tenants\n", len(cfg.Platforms.Workday.Tenants))
	}

	fmt.Printf("\nPlatforms registered: %d\n", len(registry.Platforms()))

	// Start server.
	srv := server.New(store, registry, cfg)
	fmt.Printf("Dashboard: http://localhost%s\n\n", *addr)

	if err := srv.Start(*addr); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %s\n", err)
		os.Exit(1)
	}
}
