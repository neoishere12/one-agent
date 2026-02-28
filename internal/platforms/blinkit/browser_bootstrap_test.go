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
echo '{"ok":true,"session":{"access_token":"v2::access","refresh_token":"v2::refresh","token_expires_at":"2030-01-01T00:00:00Z","device_headers":{"app_client":"consumer_web","platform":"mobile_web","user-agent":"Mozilla/5.0"}}}'
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
}
