package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"one-agent/internal/proxyman"
	"one-agent/internal/types"
)

const defaultIngestURL = "http://127.0.0.1:8080/sessions/ingest"

type cliConfig struct {
	harPath         string
	app             types.Platform
	ingestURL       string
	ingestSecret    string
	dryRun          bool
	diagnostics     bool
	timeout         time.Duration
	tokenExpiresAt  time.Time
	defaultTokenTTL time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		return err
	}
	payload, diag, err := parseHARFile(cfg)
	if cfg.diagnostics && diag != nil {
		printDiagnostics(*diag)
	}
	if err != nil {
		return err
	}
	printSummary(proxyman.Summarize(payload), cfg.dryRun)
	if cfg.dryRun {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	if err := proxyman.PostIngest(ctx, payload, proxyman.PostOptions{
		URL:    cfg.ingestURL,
		Secret: cfg.ingestSecret,
	}); err != nil {
		return err
	}
	fmt.Println("ingest posted successfully")
	return nil
}

func parseFlags(args []string) (cliConfig, error) {
	fs := flag.NewFlagSet("proxyman-import", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})
	cfg := cliConfig{}
	var app string
	var expiresAt string
	fs.StringVar(&cfg.harPath, "har", "", "path to Proxyman HAR export")
	fs.StringVar(&app, "app", "", "target app: blinkit|zepto|instamart")
	fs.StringVar(&cfg.ingestURL, "ingest-url", defaultIngestURL, "POST /sessions/ingest URL")
	fs.StringVar(&cfg.ingestSecret, "ingest-secret", os.Getenv("INGEST_SECRET"), "X-Ingest-Secret header value (defaults to INGEST_SECRET env)")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "parse HAR and print a redacted summary without posting")
	fs.BoolVar(&cfg.diagnostics, "diagnostics", false, "print redacted parser diagnostics (counts + top paths)")
	fs.DurationVar(&cfg.timeout, "timeout", 15*time.Second, "HTTP POST timeout")
	fs.StringVar(&expiresAt, "token-expires-at", "", "override token expiry (RFC3339)")
	fs.DurationVar(&cfg.defaultTokenTTL, "default-token-ttl", 7*24*time.Hour, "fallback TTL if expiry is not present in HAR")
	if err := fs.Parse(args); err != nil {
		return cliConfig{}, usageError(err)
	}
	if err := validateRequired(&cfg, app, expiresAt); err != nil {
		return cliConfig{}, err
	}
	return cfg, nil
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func usageError(parseErr error) error {
	return fmt.Errorf(
		"%v\nusage: proxyman-import -har capture.har [-app blinkit] [-ingest-url %s] [-dry-run]",
		parseErr,
		defaultIngestURL,
	)
}

func validateRequired(cfg *cliConfig, app, expiresAt string) error {
	cfg.harPath = strings.TrimSpace(cfg.harPath)
	cfg.ingestURL = strings.TrimSpace(cfg.ingestURL)
	cfg.ingestSecret = strings.TrimSpace(cfg.ingestSecret)
	cfg.app = types.Platform(strings.ToLower(strings.TrimSpace(app)))
	if cfg.harPath == "" {
		return fmt.Errorf("-har is required")
	}
	if cfg.app != "" && !isSupportedApp(cfg.app) {
		return fmt.Errorf("-app must be one of: blinkit, zepto, instamart")
	}
	if !cfg.dryRun && cfg.ingestSecret == "" {
		return fmt.Errorf("-ingest-secret (or INGEST_SECRET env) is required unless -dry-run is used")
	}
	if expiresAt == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(expiresAt))
	if err != nil {
		return fmt.Errorf("invalid -token-expires-at (use RFC3339): %w", err)
	}
	cfg.tokenExpiresAt = t.UTC()
	return nil
}

func isSupportedApp(app types.Platform) bool {
	switch app {
	case types.PlatformBlinkit, types.PlatformZepto, types.PlatformInstamart:
		return true
	default:
		return false
	}
}

func parseHARFile(cfg cliConfig) (*proxyman.IngestPayload, *proxyman.ParseDiagnostics, error) {
	data, err := os.ReadFile(cfg.harPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open HAR: %w", err)
	}
	app, err := resolveApp(cfg, data)
	if err != nil {
		return nil, nil, err
	}
	payload, diag, err := proxyman.ParseHARWithDiagnostics(bytes.NewReader(data), proxyman.ParseOptions{
		App:             app,
		TokenExpiresAt:  cfg.tokenExpiresAt,
		DefaultTokenTTL: cfg.defaultTokenTTL,
	})
	if err != nil {
		return nil, diag, err
	}
	return payload, diag, nil
}

func resolveApp(cfg cliConfig, data []byte) (types.Platform, error) {
	if cfg.app != "" {
		return cfg.app, nil
	}
	app, err := proxyman.DetectApp(filepath.Base(cfg.harPath), bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("auto-detect app failed: %w (pass -app explicitly)", err)
	}
	return app, nil
}

func printSummary(s proxyman.Summary, dryRun bool) {
	mode := "summary"
	if dryRun {
		mode = "dry-run"
	}
	fmt.Printf(
		"proxyman-import %s\napp=%s has_access=%t has_refresh=%t expires_at=%s device_headers=%d addresses=%d payments=%d\n",
		mode,
		s.App,
		s.HasAccessToken,
		s.HasRefreshToken,
		s.TokenExpiresAt.Format(time.RFC3339),
		s.DeviceHeaders,
		s.Addresses,
		s.Payments,
	)
}

func printDiagnostics(d proxyman.ParseDiagnostics) {
	fmt.Printf(
		"diagnostics entries=%d json=%d auth_header_entries=%d auth_field_responses=%d address_path_entries=%d payment_path_entries=%d device_headers=%d addresses=%d payments=%d\n",
		d.TotalEntries,
		d.JSONResponses,
		d.AuthHeaderEntries,
		d.AuthFieldResponses,
		d.AddressPathEntries,
		d.PaymentPathEntries,
		d.DeviceHeadersCollected,
		d.AddressesExtracted,
		d.PaymentsExtracted,
	)
	top := d.TopPaths(10)
	if len(top) == 0 {
		return
	}
	fmt.Println("top_paths:")
	for _, line := range top {
		fmt.Printf("  %s\n", line)
	}
}
