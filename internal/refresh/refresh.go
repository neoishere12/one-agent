// Package refresh provides explicit token refresh logic for all platform sessions.
// It is called by the systemd timer daemon (cmd/refresher) and available as a
// background routine in cmd/server.
//
// Layer rule: refresh may import types, config, store, platforms — never mcp or cmd.
package refresh

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

// Result holds the outcome of refreshing a single platform session.
type Result struct {
	App       types.Platform
	Success   bool
	NewExpiry time.Time
	Err       error
}

// RefreshOne refreshes the token for a single platform session.
// Reads current session from store, calls platform.RefreshToken, updates store.
// Returns an error if the session is not found or the refresh call fails.
func RefreshOne(ctx context.Context, s *store.Store, app types.Platform, client platforms.Platform) (Result, error) {
	sess, err := s.Get(ctx, app)
	if err != nil {
		return Result{App: app, Err: err}, fmt.Errorf("refresh: load session for %s: %w", app, err)
	}

	newAccess, newRefresh, expiresAt, err := client.RefreshToken(ctx, sess.RefreshToken)
	if err != nil {
		result := Result{App: app, Err: err}
		return result, fmt.Errorf("refresh: %s token refresh: %w", app, err)
	}

	sess.AccessToken = newAccess
	sess.RefreshToken = newRefresh
	sess.ExpiresAt = expiresAt
	sess.UpdatedAt = time.Now()

	if err := s.Set(ctx, app, sess); err != nil {
		result := Result{App: app, Err: err}
		return result, fmt.Errorf("refresh: persist %s session: %w", app, err)
	}

	slog.Info("refresh: token refreshed",
		"app", app,
		"new_expires_at", expiresAt.Format(time.RFC3339),
		// never log the token value itself (BELIEFS.md §2)
	)

	return Result{App: app, Success: true, NewExpiry: expiresAt}, nil
}

// RefreshAll refreshes tokens for every platform in clients.
// Failures are collected — successful platforms continue even if others fail.
// Returns a summary map and a combined error if any platform failed.
func RefreshAll(ctx context.Context, s *store.Store, clients map[types.Platform]platforms.Platform) (map[types.Platform]Result, error) {
	results := make(map[types.Platform]Result, len(clients))
	var firstErr error

	for app, client := range clients {
		result, err := RefreshOne(ctx, s, app, client)
		results[app] = result
		if err != nil {
			slog.Warn("refresh: platform failed", "app", app, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if firstErr != nil {
		failed := countFailed(results)
		return results, fmt.Errorf("refresh all: %d/%d platforms failed (first: %w)",
			failed, len(clients), firstErr)
	}

	slog.Info("refresh: all platforms refreshed", "count", len(clients))
	return results, nil
}

func countFailed(results map[types.Platform]Result) int {
	n := 0
	for _, r := range results {
		if !r.Success {
			n++
		}
	}
	return n
}
