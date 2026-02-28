package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"one-agent/internal/config"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

// testKey is a fixed 32-byte key used only in tests — never in production.
const testKey = "0000000000000000000000000000000000000000000000000000000000000001"

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()

	t.Setenv("STORE_MASTER_KEY", testKey)
	cfg := config.Load()
	cfg.DBPath = filepath.Join(dir, "test.db")

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleSession(app types.Platform) *types.AppSession {
	return &types.AppSession{
		App:          app,
		AccessToken:  "tok_access_abc123",
		RefreshToken: "tok_refresh_xyz789",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		DeviceHeaders: map[string]string{
			"X-Device-ID": "dev-001",
		},
		Addresses: []types.Address{
			{ID: "addr1", Label: "Home", Line1: "42 MG Road", City: "Bengaluru", PinCode: "560001", IsDefault: true},
		},
		Payments: []types.PaymentMethod{
			{ID: "pm1", Type: "upi", Label: "SBI UPI", Token: "upi_mandate_token", IsDefault: true},
		},
		CapturedAt: time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func TestSetAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	want := sampleSession(types.PlatformBlinkit)

	if err := s.Set(ctx, types.PlatformBlinkit, want); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := s.Get(ctx, types.PlatformBlinkit)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.App != want.App {
		t.Errorf("App: got %q, want %q", got.App, want.App)
	}
	if got.AccessToken != want.AccessToken {
		t.Errorf("AccessToken mismatch after roundtrip")
	}
	if got.RefreshToken != want.RefreshToken {
		t.Errorf("RefreshToken mismatch after roundtrip")
	}
	if len(got.Addresses) != 1 || got.Addresses[0].Label != "Home" {
		t.Errorf("Addresses not preserved: %+v", got.Addresses)
	}
	if len(got.Payments) != 1 || got.Payments[0].Token != "upi_mandate_token" {
		t.Errorf("Payments not preserved: %+v", got.Payments)
	}
}

func TestGetNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), types.PlatformZepto)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestSetOverwritesExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := sampleSession(types.PlatformZepto)
	first.AccessToken = "first_token"
	if err := s.Set(ctx, types.PlatformZepto, first); err != nil {
		t.Fatal(err)
	}

	second := sampleSession(types.PlatformZepto)
	second.AccessToken = "second_token"
	if err := s.Set(ctx, types.PlatformZepto, second); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, types.PlatformZepto)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "second_token" {
		t.Errorf("expected second_token, got %q", got.AccessToken)
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.Set(ctx, types.PlatformInstamart, sampleSession(types.PlatformInstamart)); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, types.PlatformInstamart); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := s.Get(ctx, types.PlatformInstamart)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got: %v", err)
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, p := range []types.Platform{types.PlatformBlinkit, types.PlatformZepto} {
		if err := s.Set(ctx, p, sampleSession(p)); err != nil {
			t.Fatal(err)
		}
	}

	apps, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(apps) != 2 {
		t.Errorf("expected 2 apps, got %d: %v", len(apps), apps)
	}
}

func TestBackup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.Set(ctx, types.PlatformBlinkit, sampleSession(types.PlatformBlinkit)); err != nil {
		t.Fatal(err)
	}

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := s.Backup(ctx, backupPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("backup file not created: %v", err)
	}
}
