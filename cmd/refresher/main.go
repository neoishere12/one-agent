package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"one-agent/internal/config"
	"one-agent/internal/platforms"
	"one-agent/internal/platforms/blinkit"
	"one-agent/internal/platforms/instamart"
	"one-agent/internal/platforms/zepto"
	"one-agent/internal/refresh"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

func main() {
	if err := run(); err != nil {
		slog.Error("token refresher failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	st, err := store.New(cfg)
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer func() { _ = st.Close() }()

	results, err := refresh.RefreshAll(context.Background(), st, newPlatformClients(st))
	for app, result := range results {
		slog.Info("refresh result",
			"app", app,
			"success", result.Success,
			"new_expiry", result.NewExpiry,
			"err", result.Err,
		)
	}
	if err != nil {
		return fmt.Errorf("refresh all: %w", err)
	}
	return nil
}

func newPlatformClients(st *store.Store) map[types.Platform]platforms.Platform {
	return map[types.Platform]platforms.Platform{
		types.PlatformBlinkit:   blinkit.New(st),
		types.PlatformZepto:     zepto.New(st),
		types.PlatformInstamart: instamart.New(st),
	}
}
