package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/types"
)

// TestCaptureSessionSuccess seeds a session with CapturedAt after the server's
// mocked "now" so the store-polling loop finds it on the first iteration.
func TestCaptureSessionSuccess(t *testing.T) {
	st := newTestStore(t)
	// fakeNow matches the value set by newServerForTests via withNow.
	fakeNow := time.Date(2026, 2, 26, 12, 0, 0, 0, time.UTC)
	session := defaultSession(types.PlatformBlinkit)
	session.CapturedAt = fakeNow.Add(time.Hour) // after fakeNow → detected as new
	seedSession(t, st, session)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	raw := mustRaw(t, captureSessionInput{App: "blinkit"})
	result, err := server.call(context.Background(), "capture_session", raw)
	if err != nil {
		t.Fatalf("capture_session failed: %v", err)
	}

	out := result.(captureSessionOutput)
	if out.App != "blinkit" {
		t.Errorf("App: got %q", out.App)
	}
	if out.AddressesFound != len(session.Addresses) {
		t.Errorf("AddressesFound: got %d", out.AddressesFound)
	}
	if out.PaymentMethodsFound != len(session.Payments) {
		t.Errorf("PaymentMethodsFound: got %d", out.PaymentMethodsFound)
	}
}

func TestListSessionsReturnsStoredData(t *testing.T) {
	st := newTestStore(t)
	seedSession(t, st, defaultSession(types.PlatformBlinkit))
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "list_sessions", nil)
	if err != nil {
		t.Fatalf("list_sessions failed: %v", err)
	}

	out := result.(listSessionsOutput)
	if len(out.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(out.Sessions))
	}
	if out.Sessions[0].App != "blinkit" {
		t.Errorf("App: got %q", out.Sessions[0].App)
	}
	if !out.Sessions[0].TokenValid {
		t.Error("TokenValid: expected true")
	}
}

func TestRefreshTokensOneApp(t *testing.T) {
	st := newTestStore(t)
	seedSession(t, st, defaultSession(types.PlatformBlinkit))
	expiry := time.Now().Add(24 * time.Hour).Round(time.Second)
	client := &mockPlatform{newAccess: "new-a", newRefresh: "new-r", newExpiry: expiry}
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: client,
	})

	app := "blinkit"
	raw := mustRaw(t, refreshTokensInput{App: &app})
	result, err := server.call(context.Background(), "refresh_tokens", raw)
	if err != nil {
		t.Fatalf("refresh_tokens failed: %v", err)
	}

	out := result.(refreshTokensOutput)
	if len(out.Refreshed) != 1 || out.Refreshed[0] != "blinkit" {
		t.Errorf("Refreshed: got %+v", out.Refreshed)
	}
	if out.Results["blinkit"].NewExpiry.IsZero() {
		t.Error("NewExpiry should be set")
	}
}

func TestRefreshTokensAllPartialFailure(t *testing.T) {
	st := newTestStore(t)
	seedSession(t, st, defaultSession(types.PlatformBlinkit))
	failed := &mockPlatform{refreshErr: errors.New("refresh failed")}
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: failed,
		types.PlatformZepto:   &mockPlatform{},
	})

	result, err := server.call(context.Background(), "refresh_tokens", mustRaw(t, refreshTokensInput{}))
	if err == nil {
		t.Fatal("expected refresh_tokens error for partial failure")
	}
	out := result.(refreshTokensOutput)
	if len(out.Failed) == 0 {
		t.Fatal("expected at least one failed app")
	}
}
