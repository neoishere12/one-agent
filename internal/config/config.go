// Package config loads runtime configuration from environment variables.
// It panics on startup if required values are missing or invalid (fail-fast per BELIEFS.md §9).
package config

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

const (
	defaultMCPPort               = 8080
	defaultTokenRefreshInterval  = 4 * time.Hour
	defaultDBPath                = "data/sessions.db"
	defaultIngestRateLimitMax    = 10
	defaultIngestRateLimitWindow = time.Minute
)

// Config holds all runtime configuration for the agent.
// Never log StoreMasterKey or IngestSecret — they are secrets.
type Config struct {
	StoreMasterKey        []byte        // 32 bytes decoded from STORE_MASTER_KEY hex
	IngestSecret          string        // X-Ingest-Secret shared with iOS app
	MCPPort               int           // default 8080
	TokenRefreshInterval  time.Duration // default 4h
	DBPath                string        // default "data/sessions.db"
	IngestRateLimitMax    int           // default 10 per window; <=0 disables limiter
	IngestRateLimitWindow time.Duration // default 1m; <=0 disables limiter
}

// Load reads environment variables and returns a validated Config.
// Panics if STORE_MASTER_KEY is absent or not a valid 32-byte hex string.
func Load() *Config {
	key := mustMasterKey()

	cfg := &Config{
		StoreMasterKey:        key,
		IngestSecret:          strEnv("INGEST_SECRET", ""),
		MCPPort:               intEnv("MCP_PORT", defaultMCPPort),
		TokenRefreshInterval:  durationEnv("TOKEN_REFRESH_INTERVAL", defaultTokenRefreshInterval),
		DBPath:                strEnv("DB_PATH", defaultDBPath),
		IngestRateLimitMax:    intEnv("INGEST_RATE_LIMIT_MAX", defaultIngestRateLimitMax),
		IngestRateLimitWindow: durationEnv("INGEST_RATE_LIMIT_WINDOW", defaultIngestRateLimitWindow),
	}

	if cfg.IngestSecret == "" {
		slog.Warn("INGEST_SECRET not set — POST /sessions/ingest will reject all requests")
	}

	slog.Info("config loaded",
		"mcp_port", cfg.MCPPort,
		"token_refresh_interval", cfg.TokenRefreshInterval,
		"db_path", cfg.DBPath,
		"ingest_rate_limit_max", cfg.IngestRateLimitMax,
		"ingest_rate_limit_window", cfg.IngestRateLimitWindow,
		// StoreMasterKey and IngestSecret intentionally omitted from log
	)

	return cfg
}

// mustMasterKey reads STORE_MASTER_KEY and panics if it is missing or invalid.
// Required: exactly 64 hex characters (32 bytes) for AES-256.
func mustMasterKey() []byte {
	raw := os.Getenv("STORE_MASTER_KEY")
	if raw == "" {
		panic("STORE_MASTER_KEY is not set — generate one with: openssl rand -hex 32")
	}

	key, err := hex.DecodeString(raw)
	if err != nil {
		panic(fmt.Sprintf("STORE_MASTER_KEY is not valid hex: %v", err))
	}

	if len(key) != 32 {
		panic(fmt.Sprintf(
			"STORE_MASTER_KEY must be 32 bytes (64 hex chars), got %d bytes — regenerate with: openssl rand -hex 32",
			len(key),
		))
	}

	return key
}

// strEnv returns the env var value or the default.
func strEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// intEnv parses an integer env var, logging a warning and returning the default on parse error.
func intEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid int env var — using default", "key", key, "value", v, "default", def)
		return def
	}
	return n
}

// durationEnv parses a duration env var, logging a warning and returning the default on parse error.
func durationEnv(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("invalid duration env var — using default", "key", key, "value", v, "default", def)
		return def
	}
	return d
}
