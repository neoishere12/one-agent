package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"one-agent/internal/platforms"
	"one-agent/internal/types"
)

func writeBootstrapHelperScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bootstrap-helper.sh")
	content := `#!/bin/sh
echo '{"ok":true,"session":{"access_token":"v2::boot-access","refresh_token":"v2::boot-refresh","token_expires_at":"2030-01-01T00:00:00Z","device_headers":{"app_client":"consumer_web","platform":"mobile_web","user-agent":"Mozilla/5.0"}}}'
`
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write bootstrap helper: %v", err)
	}
	return path
}

func TestBootstrapBlinkitWebSessionTool(t *testing.T) {
	helper := writeBootstrapHelperScript(t)
	t.Setenv("BLINKIT_BROWSER_NODE", "/bin/sh")
	t.Setenv("BLINKIT_BROWSER_HELPER", helper)
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "")
	t.Setenv("BLINKIT_BROWSER_BOOTSTRAP_TIMEOUT", "10s")

	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "bootstrap_blinkit_web_session", mustRaw(t, map[string]any{}))
	if err != nil {
		t.Fatalf("bootstrap_blinkit_web_session failed: %v", err)
	}
	out := result.(bootstrapBlinkitWebSessionOutput)
	if out.App != "blinkit" {
		t.Fatalf("app mismatch: %q", out.App)
	}
	if !out.TokenValid {
		t.Fatal("expected token_valid=true")
	}
	if out.DeviceHeaderCount == 0 {
		t.Fatal("expected non-zero device headers")
	}
}
