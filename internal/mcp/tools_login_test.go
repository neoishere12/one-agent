package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/platforms/blinkit"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

func TestStartLoginBlinkitCompletes(t *testing.T) {
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "http://worker.local")
	stubBlinkitWorkerStatus(t, func(ctx context.Context) (*blinkit.BrowserWorkerStatus, error) {
		return &blinkit.BrowserWorkerStatus{
			OK:                 true,
			PageURL:            "https://blinkit.com/",
			Title:              "Blinkit",
			AccessTokenPresent: false,
			AuthKeyPresent:     false,
			ChallengeDetected:  false,
		}, nil
	})

	release := make(chan struct{})
	stubBlinkitBootstrap(t, func(ctx context.Context, st *store.Store) (*types.AppSession, error) {
		<-release
		sess := fixtureBlinkitSession(time.Now())
		if err := st.Set(context.Background(), types.PlatformBlinkit, sess); err != nil {
			return nil, err
		}
		return sess, nil
	})

	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})
	result, err := server.call(context.Background(), "start_login", mustRaw(t, map[string]any{
		"app":             "blinkit",
		"timeout_seconds": 30,
	}))
	if err != nil {
		t.Fatalf("start_login failed: %v", err)
	}
	start := result.(loginStatusOutput)
	if start.Status != loginStatusPending {
		t.Fatalf("expected pending start status, got %+v", start)
	}
	if strings.TrimSpace(start.LoginID) == "" {
		t.Fatalf("expected login_id, got %+v", start)
	}

	close(release)
	status := waitForLoginStatus(t, server, start.LoginID, loginStatusCompleted, 2*time.Second)
	if !status.Ready {
		t.Fatalf("expected ready=true after completion, got %+v", status)
	}
	if status.TokenExpiresAt.IsZero() {
		t.Fatalf("expected token_expires_at to be set, got %+v", status)
	}
}

func TestStartLoginUsesPortalURLWhenConfigured(t *testing.T) {
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "http://worker.local")
	t.Setenv("MCP_PUBLIC_BASE_URL", "https://agent.example.com")
	t.Setenv("BLINKIT_EXTERNAL_LOGIN_URL_TEMPLATE", "https://login.example.com/vnc.html?login_id={login_id_escaped}&target={target_url_escaped}")
	stubBlinkitWorkerStatus(t, func(ctx context.Context) (*blinkit.BrowserWorkerStatus, error) {
		return &blinkit.BrowserWorkerStatus{OK: true}, nil
	})

	release := make(chan struct{})
	stubBlinkitBootstrap(t, func(ctx context.Context, st *store.Store) (*types.AppSession, error) {
		<-release
		return fixtureBlinkitSession(time.Now()), nil
	})

	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})
	result, err := server.call(context.Background(), "start_login", mustRaw(t, map[string]any{"app": "blinkit"}))
	if err != nil {
		t.Fatalf("start_login failed: %v", err)
	}
	start := result.(loginStatusOutput)
	if !strings.HasPrefix(start.LoginURL, "https://agent.example.com/login/blinkit?login_id=") {
		t.Fatalf("expected portal login_url, got %q", start.LoginURL)
	}
	if !strings.Contains(start.Message, "Open login_url") {
		t.Fatalf("expected remote browser handoff message, got %q", start.Message)
	}
	close(release)
}

func TestStartLoginUsesDirectExternalURLWithoutPortal(t *testing.T) {
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "http://worker.local")
	t.Setenv("BLINKIT_EXTERNAL_LOGIN_URL_TEMPLATE", "https://login.example.com/vnc.html?login_id={login_id_escaped}&target={target_url_escaped}")
	stubBlinkitWorkerStatus(t, func(ctx context.Context) (*blinkit.BrowserWorkerStatus, error) {
		return &blinkit.BrowserWorkerStatus{OK: true}, nil
	})

	release := make(chan struct{})
	stubBlinkitBootstrap(t, func(ctx context.Context, st *store.Store) (*types.AppSession, error) {
		<-release
		return fixtureBlinkitSession(time.Now()), nil
	})

	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})
	result, err := server.call(context.Background(), "start_login", mustRaw(t, map[string]any{"app": "blinkit"}))
	if err != nil {
		t.Fatalf("start_login failed: %v", err)
	}
	start := result.(loginStatusOutput)
	if !strings.HasPrefix(start.LoginURL, "https://login.example.com/vnc.html?login_id=") {
		t.Fatalf("expected direct external login_url, got %q", start.LoginURL)
	}
	if !strings.Contains(start.LoginURL, "target=https%3A%2F%2Fblinkit.com%2F") {
		t.Fatalf("expected encoded Blinkit target URL, got %q", start.LoginURL)
	}
	close(release)
}

func TestStartLoginBlinkitFailureMarksHumanVerification(t *testing.T) {
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "http://worker.local")
	stubBlinkitWorkerStatus(t, func(ctx context.Context) (*blinkit.BrowserWorkerStatus, error) {
		return &blinkit.BrowserWorkerStatus{
			OK:                 true,
			PageURL:            "https://blinkit.com/",
			Title:              "Blinkit",
			AccessTokenPresent: false,
			AuthKeyPresent:     false,
			ChallengeDetected:  true,
		}, nil
	})
	stubBlinkitBootstrap(t, func(ctx context.Context, st *store.Store) (*types.AppSession, error) {
		return nil, fmt.Errorf("human_verification_required: challenge page")
	})

	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})
	result, err := server.call(context.Background(), "start_login", mustRaw(t, map[string]any{"app": "blinkit"}))
	if err != nil {
		t.Fatalf("start_login failed: %v", err)
	}
	start := result.(loginStatusOutput)
	status := waitForLoginStatus(t, server, start.LoginID, loginStatusFailed, 2*time.Second)
	if !status.NeedsHumanVerification {
		t.Fatalf("expected needs_human_verification=true, got %+v", status)
	}
}

func TestStartLoginReturnsExistingPendingFlow(t *testing.T) {
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "http://worker.local")
	statusCalls := 0
	stubBlinkitWorkerStatus(t, func(ctx context.Context) (*blinkit.BrowserWorkerStatus, error) {
		statusCalls++
		return &blinkit.BrowserWorkerStatus{
			OK:                 true,
			PageURL:            "https://blinkit.com/",
			Title:              "Blinkit",
			AccessTokenPresent: false,
			AuthKeyPresent:     false,
			ChallengeDetected:  false,
		}, nil
	})

	release := make(chan struct{})
	stubBlinkitBootstrap(t, func(ctx context.Context, st *store.Store) (*types.AppSession, error) {
		<-release
		sess := fixtureBlinkitSession(time.Now())
		if err := st.Set(context.Background(), types.PlatformBlinkit, sess); err != nil {
			return nil, err
		}
		return sess, nil
	})

	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})
	firstResult, err := server.call(context.Background(), "start_login", mustRaw(t, map[string]any{
		"app": "blinkit",
	}))
	if err != nil {
		t.Fatalf("first start_login failed: %v", err)
	}
	first := firstResult.(loginStatusOutput)
	secondResult, err := server.call(context.Background(), "start_login", mustRaw(t, map[string]any{
		"app": "blinkit",
	}))
	if err != nil {
		t.Fatalf("second start_login failed: %v", err)
	}
	second := secondResult.(loginStatusOutput)
	if first.LoginID != second.LoginID {
		t.Fatalf("expected same login_id for pending flow, first=%q second=%q", first.LoginID, second.LoginID)
	}
	if statusCalls != 1 {
		t.Fatalf("expected worker status check once, got %d", statusCalls)
	}
	close(release)
}

func TestLoginStatusUnknownID(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})
	_, err := server.call(context.Background(), "login_status", mustRaw(t, map[string]any{"login_id": "missing"}))
	if err == nil {
		t.Fatal("expected login_status to fail for unknown id")
	}
	if !strings.Contains(err.Error(), "login_id not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func waitForLoginStatus(
	t *testing.T,
	server *Server,
	loginID string,
	targetStatus string,
	timeout time.Duration,
) loginStatusOutput {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := loginStatusOutput{}
	for time.Now().Before(deadline) {
		result, err := server.call(context.Background(), "login_status", mustRaw(t, map[string]any{
			"login_id": loginID,
		}))
		if err != nil {
			t.Fatalf("login_status failed: %v", err)
		}
		last = result.(loginStatusOutput)
		if last.Status == targetStatus {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for login status %q, last=%+v", targetStatus, last)
	return last
}
