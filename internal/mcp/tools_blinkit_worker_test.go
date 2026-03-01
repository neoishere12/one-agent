package mcp

import (
	"context"
	"testing"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/platforms/blinkit"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

func stubBlinkitWorkerStatus(
	t *testing.T,
	fn func(context.Context) (*blinkit.BrowserWorkerStatus, error),
) {
	t.Helper()
	old := blinkitWorkerStatusSnapshotFn
	blinkitWorkerStatusSnapshotFn = fn
	t.Cleanup(func() { blinkitWorkerStatusSnapshotFn = old })
}

func stubBlinkitBootstrap(
	t *testing.T,
	fn func(context.Context, *store.Store) (*types.AppSession, error),
) {
	t.Helper()
	old := blinkitBootstrapWebSessionFn
	blinkitBootstrapWebSessionFn = fn
	t.Cleanup(func() { blinkitBootstrapWebSessionFn = old })
}

func fixtureBlinkitSession(now time.Time) *types.AppSession {
	return &types.AppSession{
		App:          types.PlatformBlinkit,
		AccessToken:  "v2::abc",
		RefreshToken: "v2::ref",
		ExpiresAt:    time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC),
		DeviceHeaders: map[string]string{
			"app_client": "consumer_web",
			"platform":   "mobile_web",
			"user-agent": "Mozilla/5.0",
		},
		CapturedAt: now.UTC(),
		UpdatedAt:  now.UTC(),
	}
}

func TestBlinkitWorkerStatusNotConfigured(t *testing.T) {
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "")
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "blinkit_worker_status", mustRaw(t, map[string]any{}))
	if err != nil {
		t.Fatalf("blinkit_worker_status failed: %v", err)
	}
	out := result.(blinkitWorkerStatusOutput)
	if out.WorkerConfigured {
		t.Fatal("expected worker_configured=false")
	}
	if out.WorkerReachable {
		t.Fatal("expected worker_reachable=false")
	}
}

func TestBlinkitWorkerStatusChallengeDetected(t *testing.T) {
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "http://worker.local")
	stubBlinkitWorkerStatus(t, func(ctx context.Context) (*blinkit.BrowserWorkerStatus, error) {
		return &blinkit.BrowserWorkerStatus{
			OK:                 true,
			PageURL:            "https://blinkit.com/s/?q=amul",
			Title:              "blinkit | Error Page",
			AccessTokenPresent: false,
			AuthKeyPresent:     false,
			ChallengeDetected:  true,
		}, nil
	})

	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "blinkit_worker_status", mustRaw(t, map[string]any{"timeout_seconds": 3}))
	if err != nil {
		t.Fatalf("blinkit_worker_status failed: %v", err)
	}
	out := result.(blinkitWorkerStatusOutput)
	if !out.WorkerConfigured || !out.WorkerReachable {
		t.Fatalf("expected configured+reachable, got %+v", out)
	}
	if !out.ChallengeDetected || !out.NeedsHumanVerification {
		t.Fatalf("expected challenge + human verification, got %+v", out)
	}
}

func TestReverifyBlinkitSessionChallengeReturnsGuidance(t *testing.T) {
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "http://worker.local")
	stubBlinkitWorkerStatus(t, func(ctx context.Context) (*blinkit.BrowserWorkerStatus, error) {
		return &blinkit.BrowserWorkerStatus{
			OK:                 true,
			PageURL:            "https://blinkit.com/",
			Title:              "blinkit | Error Page",
			AccessTokenPresent: false,
			AuthKeyPresent:     false,
			ChallengeDetected:  true,
		}, nil
	})

	bootstrapCalls := 0
	stubBlinkitBootstrap(t, func(ctx context.Context, st *store.Store) (*types.AppSession, error) {
		bootstrapCalls++
		return nil, nil
	})

	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "reverify_blinkit_session", mustRaw(t, map[string]any{"timeout_seconds": 60}))
	if err != nil {
		t.Fatalf("reverify_blinkit_session failed: %v", err)
	}
	out := result.(reverifyBlinkitSessionOutput)
	if out.Ready {
		t.Fatalf("expected ready=false, got %+v", out)
	}
	if !out.NeedsHumanVerification {
		t.Fatalf("expected needs_human_verification=true, got %+v", out)
	}
	if bootstrapCalls != 0 {
		t.Fatalf("expected bootstrap not called, got %d", bootstrapCalls)
	}
}

func TestReverifyBlinkitSessionSuccessPersistsSession(t *testing.T) {
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "http://worker.local")
	stubBlinkitWorkerStatus(t, func(ctx context.Context) (*blinkit.BrowserWorkerStatus, error) {
		return &blinkit.BrowserWorkerStatus{
			OK:                 true,
			PageURL:            "https://blinkit.com/",
			Title:              "Blinkit",
			AccessTokenPresent: true,
			AuthKeyPresent:     true,
			ChallengeDetected:  false,
		}, nil
	})

	now := time.Now()
	stubBlinkitBootstrap(t, func(ctx context.Context, st *store.Store) (*types.AppSession, error) {
		sess := fixtureBlinkitSession(now)
		if err := st.Set(context.Background(), types.PlatformBlinkit, sess); err != nil {
			return nil, err
		}
		return sess, nil
	})

	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "reverify_blinkit_session", mustRaw(t, map[string]any{"timeout_seconds": 60}))
	if err != nil {
		t.Fatalf("reverify_blinkit_session failed: %v", err)
	}
	out := result.(reverifyBlinkitSessionOutput)
	if !out.Ready {
		t.Fatalf("expected ready=true, got %+v", out)
	}
	if out.TokenExpiresAt.IsZero() || !out.TokenExpiresAt.After(time.Now().Add(24*time.Hour)) {
		t.Fatalf("expected future expiry, got %s", out.TokenExpiresAt)
	}
	stored, getErr := st.Get(context.Background(), types.PlatformBlinkit)
	if getErr != nil {
		t.Fatalf("store.Get blinkit: %v", getErr)
	}
	if stored.AccessToken != "v2::abc" {
		t.Fatalf("expected access token persisted, got %q", stored.AccessToken)
	}
}
