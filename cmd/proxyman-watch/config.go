package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"one-agent/internal/types"
)

const defaultIngestURL = "http://127.0.0.1:8080/sessions/ingest"

type cliConfig struct {
	watchDir        string
	processedDir    string
	failedDir       string
	app             types.Platform
	ingestURL       string
	ingestSecret    string
	dryRun          bool
	diagnostics     bool
	once            bool
	pollInterval    time.Duration
	settleDuration  time.Duration
	timeout         time.Duration
	tokenExpiresAt  time.Time
	defaultTokenTTL time.Duration
}

func parseFlags(args []string) (cliConfig, error) {
	fs := flag.NewFlagSet("proxyman-watch", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})
	cfg := cliConfig{}
	var app string
	var expiresAt string
	fs.StringVar(&cfg.watchDir, "dir", "", "directory to watch for .har files")
	fs.StringVar(&cfg.processedDir, "processed-dir", "", "destination directory for successfully processed files")
	fs.StringVar(&cfg.failedDir, "failed-dir", "", "destination directory for failed files")
	fs.StringVar(&app, "app", "", "override app detection: blinkit|zepto|instamart")
	fs.StringVar(&cfg.ingestURL, "ingest-url", defaultIngestURL, "POST /sessions/ingest URL")
	fs.StringVar(&cfg.ingestSecret, "ingest-secret", os.Getenv("INGEST_SECRET"), "X-Ingest-Secret header value (defaults to INGEST_SECRET env)")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "parse files and print summary without posting")
	fs.BoolVar(&cfg.diagnostics, "diagnostics", false, "print redacted parser diagnostics")
	fs.BoolVar(&cfg.once, "once", false, "scan directory once and exit")
	fs.DurationVar(&cfg.pollInterval, "poll-interval", 3*time.Second, "scan interval when not using -once")
	fs.DurationVar(&cfg.settleDuration, "settle", 2*time.Second, "minimum file age before processing")
	fs.DurationVar(&cfg.timeout, "timeout", 15*time.Second, "HTTP POST timeout")
	fs.StringVar(&expiresAt, "token-expires-at", "", "override token expiry (RFC3339)")
	fs.DurationVar(&cfg.defaultTokenTTL, "default-token-ttl", 7*24*time.Hour, "fallback TTL if expiry is not present in HAR")
	if err := fs.Parse(args); err != nil {
		return cliConfig{}, usageError(err)
	}
	return normalizeConfig(cfg, app, expiresAt)
}

func usageError(parseErr error) error {
	return fmt.Errorf(
		"%v\nusage: proxyman-watch -dir /path/to/har-drop [-ingest-url %s] [-dry-run]",
		parseErr,
		defaultIngestURL,
	)
}

func normalizeConfig(cfg cliConfig, app, expiresAt string) (cliConfig, error) {
	cfg.watchDir = strings.TrimSpace(cfg.watchDir)
	cfg.ingestURL = strings.TrimSpace(cfg.ingestURL)
	cfg.ingestSecret = strings.TrimSpace(cfg.ingestSecret)
	cfg.app = types.Platform(strings.ToLower(strings.TrimSpace(app)))
	if err := validateConfig(&cfg); err != nil {
		return cliConfig{}, err
	}
	if err := parseExpiry(&cfg, expiresAt); err != nil {
		return cliConfig{}, err
	}
	setDefaultDirs(&cfg)
	return cfg, nil
}

func validateConfig(cfg *cliConfig) error {
	if cfg.watchDir == "" {
		return fmt.Errorf("-dir is required")
	}
	if cfg.app != "" && !isSupportedApp(cfg.app) {
		return fmt.Errorf("-app must be one of: blinkit, zepto, instamart")
	}
	if !cfg.dryRun && cfg.ingestSecret == "" {
		return fmt.Errorf("-ingest-secret (or INGEST_SECRET env) is required unless -dry-run is used")
	}
	if cfg.pollInterval <= 0 {
		return fmt.Errorf("-poll-interval must be > 0")
	}
	if cfg.settleDuration < 0 {
		return fmt.Errorf("-settle must be >= 0")
	}
	return nil
}

func parseExpiry(cfg *cliConfig, expiresAt string) error {
	if strings.TrimSpace(expiresAt) == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(expiresAt))
	if err != nil {
		return fmt.Errorf("invalid -token-expires-at (use RFC3339): %w", err)
	}
	cfg.tokenExpiresAt = t.UTC()
	return nil
}

func setDefaultDirs(cfg *cliConfig) {
	if cfg.processedDir == "" {
		cfg.processedDir = filepath.Join(cfg.watchDir, "processed")
	}
	if cfg.failedDir == "" {
		cfg.failedDir = filepath.Join(cfg.watchDir, "failed")
	}
}

func isSupportedApp(app types.Platform) bool {
	switch app {
	case types.PlatformBlinkit, types.PlatformZepto, types.PlatformInstamart:
		return true
	default:
		return false
	}
}
