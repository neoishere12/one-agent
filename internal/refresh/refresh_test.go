package refresh_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"one-agent/internal/config"
	"one-agent/internal/platforms"
	"one-agent/internal/refresh"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

const testKey = "0000000000000000000000000000000000000000000000000000000000000001"

// mockPlatform is a stub that satisfies platforms.Platform for refresh tests.
type mockPlatform struct {
	newAccess  string
	newRefresh string
	expiresAt  time.Time
	err        error
}

func (m *mockPlatform) RefreshToken(_ context.Context, _ string) (string, string, time.Time, error) {
	return m.newAccess, m.newRefresh, m.expiresAt, m.err
}

// Stub remaining Platform methods — not exercised by refresh tests.
func (m *mockPlatform) Search(_ context.Context, _ string, _, _ float64) ([]types.Product, error) {
	return nil, nil
}
func (m *mockPlatform) AddToCart(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}
func (m *mockPlatform) Checkout(_ context.Context, _, _ string) (platforms.CheckoutResult, error) {
	return platforms.CheckoutResult{}, nil
}
func (m *mockPlatform) Pay(_ context.Context, _, _ string) (types.Order, error) {
	return types.Order{}, nil
}
func (m *mockPlatform) OrderStatus(_ context.Context, _ string) (string, int, error) {
	return "", 0, nil
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("STORE_MASTER_KEY", testKey)
	cfg := config.Load()
	cfg.DBPath = filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedSession(t *testing.T, s *store.Store, app types.Platform) {
	t.Helper()
	sess := &types.AppSession{
		App:          app,
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		CapturedAt:   time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := s.Set(context.Background(), app, sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func TestRefreshOne_Success(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedSession(t, s, types.PlatformBlinkit)

	newExpiry := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second)
	mock := &mockPlatform{
		newAccess:  "new-access",
		newRefresh: "new-refresh",
		expiresAt:  newExpiry,
	}

	result, err := refresh.RefreshOne(ctx, s, types.PlatformBlinkit, mock)
	if err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	if !result.NewExpiry.Equal(newExpiry) {
		t.Errorf("NewExpiry: got %v, want %v", result.NewExpiry, newExpiry)
	}

	// Verify store was updated with new tokens
	sess, err := s.Get(ctx, types.PlatformBlinkit)
	if err != nil {
		t.Fatalf("Get after refresh: %v", err)
	}
	if sess.AccessToken != "new-access" {
		t.Errorf("AccessToken not updated: got %q", sess.AccessToken)
	}
	if sess.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken not updated: got %q", sess.RefreshToken)
	}
	if !sess.ExpiresAt.Equal(newExpiry) {
		t.Errorf("ExpiresAt not updated: got %v", sess.ExpiresAt)
	}
}

func TestRefreshOne_PlatformError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedSession(t, s, types.PlatformZepto)

	mock := &mockPlatform{err: platforms.ErrRefreshFailed}

	result, err := refresh.RefreshOne(ctx, s, types.PlatformZepto, mock)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result.Success {
		t.Error("expected Success=false on error")
	}
	if !errors.Is(err, platforms.ErrRefreshFailed) {
		t.Errorf("expected ErrRefreshFailed in chain, got: %v", err)
	}
}

func TestRefreshOne_SessionNotFound(t *testing.T) {
	s := newTestStore(t)
	mock := &mockPlatform{}

	_, err := refresh.RefreshOne(context.Background(), s, types.PlatformInstamart, mock)
	if err == nil {
		t.Fatal("expected error for missing session, got nil")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound in chain, got: %v", err)
	}
}

func TestRefreshAll_AllSucceed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, app := range []types.Platform{types.PlatformBlinkit, types.PlatformZepto} {
		seedSession(t, s, app)
	}

	expiry := time.Now().Add(7 * 24 * time.Hour)
	clients := map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: &mockPlatform{newAccess: "ba", newRefresh: "br", expiresAt: expiry},
		types.PlatformZepto:   &mockPlatform{newAccess: "za", newRefresh: "zr", expiresAt: expiry},
	}

	results, err := refresh.RefreshAll(ctx, s, clients)
	if err != nil {
		t.Fatalf("RefreshAll: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	for app, r := range results {
		if !r.Success {
			t.Errorf("app %s: expected Success=true", app)
		}
	}
}

func TestRefreshAll_PartialFailure(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	seedSession(t, s, types.PlatformBlinkit)
	// Zepto has no session — will fail

	expiry := time.Now().Add(7 * 24 * time.Hour)
	clients := map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: &mockPlatform{newAccess: "ba", newRefresh: "br", expiresAt: expiry},
		types.PlatformZepto:   &mockPlatform{},
	}

	results, err := refresh.RefreshAll(ctx, s, clients)
	if err == nil {
		t.Fatal("expected error for partial failure, got nil")
	}
	// Blinkit should succeed despite Zepto failing
	if !results[types.PlatformBlinkit].Success {
		t.Error("blinkit should succeed even when zepto fails")
	}
	if results[types.PlatformZepto].Success {
		t.Error("zepto should not succeed with missing session")
	}
}
