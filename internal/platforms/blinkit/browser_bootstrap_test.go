package blinkit_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"one-agent/internal/platforms/blinkit"
	"one-agent/internal/types"
)

func writeExecutableScript(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helper.sh")
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}
	return path
}

func TestSearchBrowserModeWithoutStoredSession(t *testing.T) {
	helper := writeExecutableScript(t, `#!/bin/sh
echo '{"ok":true,"raw":{"products":[{"id":"p1","name":"Amul Lassi","brand":"Amul","price":28,"mrp":30,"unit":"200ml","store_id":"s1","in_stock":true}]}}'
`)
	t.Setenv("BLINKIT_SEARCH_MODE", "browser")
	t.Setenv("BLINKIT_BROWSER_NODE", "/bin/sh")
	t.Setenv("BLINKIT_BROWSER_HELPER", helper)
	t.Setenv("BLINKIT_BROWSER_TIMEOUT", "5s")
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "")

	s := newTestStore(t)
	client := blinkit.New(s)
	products, err := client.Search(context.Background(), "amul lassi", 0, 0)
	if err != nil {
		t.Fatalf("Search(browser,no-session): %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if products[0].Platform != types.PlatformBlinkit {
		t.Fatalf("platform mismatch: %q", products[0].Platform)
	}
}

func TestBootstrapWebSessionPersistsSession(t *testing.T) {
	helper := writeExecutableScript(t, `#!/bin/sh
echo '{"ok":true,"session":{"access_token":"v2::access","refresh_token":"v2::refresh","token_expires_at":"2030-01-01T00:00:00Z","device_headers":{"app_client":"consumer_web","platform":"mobile_web","user-agent":"Mozilla/5.0"},"addresses":[{"id":"207580381","label":"Home","full_address":"Pune, Maharashtra","lat":18.5204,"lng":73.8567,"is_default":true}]}}'
`)
	t.Setenv("BLINKIT_BROWSER_NODE", "/bin/sh")
	t.Setenv("BLINKIT_BROWSER_HELPER", helper)
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "")
	t.Setenv("BLINKIT_BROWSER_BOOTSTRAP_TIMEOUT", "10s")

	s := newTestStore(t)
	session, err := blinkit.BootstrapWebSession(context.Background(), s)
	if err != nil {
		t.Fatalf("BootstrapWebSession: %v", err)
	}
	if session.App != types.PlatformBlinkit {
		t.Fatalf("app mismatch: %q", session.App)
	}
	if session.AccessToken != "v2::access" {
		t.Fatalf("access token mismatch: %q", session.AccessToken)
	}
	if !session.ExpiresAt.After(time.Now()) {
		t.Fatalf("expiry should be in future: %s", session.ExpiresAt)
	}
	stored, err := s.Get(context.Background(), types.PlatformBlinkit)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if stored.AccessToken != "v2::access" {
		t.Fatalf("stored access token mismatch: %q", stored.AccessToken)
	}
	if len(stored.Addresses) != 1 || stored.Addresses[0].ID != "207580381" {
		t.Fatalf("stored addresses mismatch: %+v", stored.Addresses)
	}
}

func TestBootstrapWebSessionPreservesExistingMetadataWhenHelperReturnsAuthOnly(t *testing.T) {
	helper := writeExecutableScript(t, `#!/bin/sh
echo '{"ok":true,"session":{"access_token":"v2::access-new","refresh_token":"v2::refresh-new","token_expires_at":"2030-01-01T00:00:00Z","device_headers":{"app_client":"consumer_web","platform":"mobile_web","user-agent":"Mozilla/5.0"}}}'
`)
	t.Setenv("BLINKIT_BROWSER_NODE", "/bin/sh")
	t.Setenv("BLINKIT_BROWSER_HELPER", helper)
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "")
	t.Setenv("BLINKIT_BROWSER_BOOTSTRAP_TIMEOUT", "10s")

	s := newTestStore(t)
	if err := s.Set(context.Background(), types.PlatformBlinkit, &types.AppSession{
		App:          types.PlatformBlinkit,
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		Addresses: []types.Address{
			{ID: "addr-1", Label: "Home", Line1: "Baner, Pune", IsDefault: true},
		},
		Payments: []types.PaymentMethod{
			{ID: "pay-1", Label: "Visa 4242", Token: "tok-1", Type: "card", IsDefault: true},
		},
		CapturedAt: time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	session, err := blinkit.BootstrapWebSession(context.Background(), s)
	if err != nil {
		t.Fatalf("BootstrapWebSession: %v", err)
	}
	if len(session.Addresses) != 1 || session.Addresses[0].ID != "addr-1" {
		t.Fatalf("expected existing address preserved, got %+v", session.Addresses)
	}
	if len(session.Payments) != 1 || session.Payments[0].ID != "pay-1" {
		t.Fatalf("expected existing payment preserved, got %+v", session.Payments)
	}
}
