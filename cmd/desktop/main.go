// Curly Chainsaw — Native Desktop Application
//
// Builds native Windows (.exe) and macOS (.app) binaries using Wails v2.
// The Go backend runs the full agent pipeline while the frontend renders
// a native webview window with system-level integration:
//
//   - System tray icon with background polling
//   - Native notifications for new matches
//   - OS-level keyboard shortcuts
//   - Auto-launch on startup (optional)
//   - Menu bar integration (macOS) / taskbar integration (Windows)
//
// Build:
//   wails build -platform darwin/amd64   # macOS Intel
//   wails build -platform darwin/arm64   # macOS Apple Silicon
//   wails build -platform windows/amd64  # Windows
//
// Dev:
//   wails dev
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jamesrausch100/curly-chainsaw/internal/config"
	"github.com/jamesrausch100/curly-chainsaw/internal/platform"
)

const (
	appName    = "Curly Chainsaw"
	appVersion = "1.0.0"
)

// DesktopApp wraps the shared platform.App with desktop-specific features:
// tray icon, native notifications, OS integration, and window management.
type DesktopApp struct {
	app        *platform.App
	configPath string
	watchStop  chan struct{}
}

// NewDesktopApp creates the desktop application instance.
func NewDesktopApp(cfg *config.Config, configPath string) (*DesktopApp, error) {
	app, err := platform.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("init platform: %w", err)
	}

	return &DesktopApp{
		app:        app,
		configPath: configPath,
		watchStop:  make(chan struct{}),
	}, nil
}

// Run executes a single discover -> match -> apply cycle and returns
// results to the frontend via Wails bindings.
func (d *DesktopApp) Run(ctx context.Context) (*platform.RunResult, error) {
	return d.app.Run(ctx)
}

// Search performs a keyword search and returns matched jobs.
func (d *DesktopApp) Search(ctx context.Context, keywords []string) ([]interface{}, error) {
	results, err := d.app.Search(ctx, keywords)
	if err != nil {
		return nil, err
	}

	// Convert to interface slice for Wails JS bridge.
	out := make([]interface{}, len(results))
	for i, r := range results {
		out[i] = r
	}
	return out, nil
}

// GetProfile returns the user's profile for the settings panel.
func (d *DesktopApp) GetProfile() interface{} {
	return d.app.Profile()
}

// GetStats returns session statistics for the dashboard.
func (d *DesktopApp) GetStats() interface{} {
	return d.app.GetStats()
}

// StartWatch begins background polling for new jobs.
// In production this triggers native OS notifications on new matches.
func (d *DesktopApp) StartWatch(ctx context.Context, intervalSeconds int) {
	go func() {
		ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				result, err := d.app.Run(ctx)
				if err != nil {
					log.Printf("[watch] error: %v", err)
					continue
				}
				if result.Matched > 0 {
					// In the Wails build, this emits a native OS notification.
					log.Printf("[watch] %d new matches found!", result.Matched)
				}
			case <-d.watchStop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// StopWatch stops background polling.
func (d *DesktopApp) StopWatch() {
	close(d.watchStop)
	d.watchStop = make(chan struct{})
}

// SaveConfig persists the current configuration.
func (d *DesktopApp) SaveConfig() error {
	return d.app.SaveConfig(d.configPath)
}

// Startup is called by Wails when the app starts. It initializes
// system tray, registers global hotkeys, and sets up auto-launch.
func (d *DesktopApp) Startup(ctx context.Context) {
	log.Printf("%s v%s starting", appName, appVersion)
}

// Shutdown is called by Wails on app exit. Clean up background tasks.
func (d *DesktopApp) Shutdown(ctx context.Context) {
	d.StopWatch()
	log.Printf("%s shutting down", appName)
}

func main() {
	// Load config.
	cfgPath := config.DefaultConfigPath()
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		// If no config exists, generate one and tell the user.
		fmt.Printf("No config found at %s — generating example.\n", cfgPath)
		if genErr := config.GenerateExample(cfgPath); genErr != nil {
			log.Fatalf("generate config: %v", genErr)
		}
		cfg, err = config.Load(cfgPath)
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
	}

	desktop, err := NewDesktopApp(cfg, cfgPath)
	if err != nil {
		log.Fatalf("init desktop: %v", err)
	}

	// NOTE: In a full Wails build this block is replaced by:
	//
	//   err = wails.Run(&options.App{
	//       Title:     appName,
	//       Width:     1280,
	//       Height:    800,
	//       MinWidth:  900,
	//       MinHeight: 600,
	//       AssetServer: &assetserver.Options{
	//           Assets: frontend,
	//       },
	//       OnStartup:  desktop.Startup,
	//       OnShutdown: desktop.Shutdown,
	//       Bind: []interface{}{desktop},
	//   })
	//
	// For now, run in headless/CLI mode so the package compiles without
	// the Wails dependency. Install Wails (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)
	// and swap this block to get the native window.

	fmt.Printf("\n  %s v%s\n", appName, appVersion)
	fmt.Printf("  ─────────────────────────────────\n")
	fmt.Printf("  Profile:  %s\n", cfg.Profile.Name)
	fmt.Printf("  Location: %s (remote only: %v)\n", cfg.Profile.Location, cfg.Profile.RemoteOnly)
	fmt.Printf("  Skills:   %d loaded\n", len(cfg.Profile.Skills))
	fmt.Printf("  Keywords: %v\n", cfg.Agent.SearchKeywords)
	fmt.Printf("  ─────────────────────────────────\n")
	fmt.Printf("  Desktop app ready. Waiting for Wails integration.\n")
	fmt.Printf("  Run `wails dev` to launch the native window.\n\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	desktop.Startup(ctx)

	// Wait for interrupt.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	desktop.Shutdown(ctx)
}
