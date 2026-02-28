package blinkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"one-agent/internal/store"
	"one-agent/internal/types"
)

const (
	envBlinkitBrowserBootstrapTimeout = "BLINKIT_BROWSER_BOOTSTRAP_TIMEOUT"
	defaultBrowserBootstrapTimeout    = 5 * time.Minute
	defaultBrowserSessionTTL          = 7 * 24 * time.Hour
)

type browserBootstrapOutput struct {
	OK      bool                    `json:"ok"`
	Error   string                  `json:"error"`
	Session browserBootstrapSession `json:"session"`
}

type browserBootstrapSession struct {
	AccessToken   string            `json:"access_token"`
	RefreshToken  string            `json:"refresh_token"`
	TokenExpires  time.Time         `json:"token_expires_at"`
	DeviceHeaders map[string]string `json:"device_headers"`
}

// BootstrapWebSession captures a Blinkit web session using the browser helper
// and persists it to the encrypted store.
func BootstrapWebSession(ctx context.Context, st *store.Store) (*types.AppSession, error) {
	timeout := browserBootstrapTimeout()
	bootstrapCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload, err := runBrowserBootstrap(bootstrapCtx, timeout)
	if err != nil {
		return nil, err
	}
	session, err := normalizeBootstrapSession(payload)
	if err != nil {
		return nil, err
	}
	if err := st.Set(ctx, types.PlatformBlinkit, session); err != nil {
		return nil, fmt.Errorf("save blinkit session: %w", err)
	}
	return session, nil
}

func runBrowserBootstrap(ctx context.Context, timeout time.Duration) (*browserBootstrapOutput, error) {
	if workerURL := browserWorkerURL(); workerURL != "" {
		return bootstrapBrowserWorker(ctx, workerURL, int(timeout.Seconds()))
	}
	return bootstrapViaHelper(ctx, timeout)
}

func bootstrapViaHelper(ctx context.Context, timeout time.Duration) (*browserBootstrapOutput, error) {
	cmd := exec.CommandContext(ctx, browserNodeBinary(), browserHelperPath(), "--bootstrap")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			msg := clipBridgeOutput(joinBridgeOutput(stderr.Bytes(), stdout.Bytes()))
			if msg == "" {
				return nil, fmt.Errorf("browser bootstrap timed out after %s", timeout)
			}
			return nil, fmt.Errorf("browser bootstrap timed out after %s: %s", timeout, msg)
		}
		msg := clipBridgeOutput(joinBridgeOutput(stderr.Bytes(), stdout.Bytes()))
		if msg == "" {
			return nil, fmt.Errorf("browser bootstrap execution failed: %w", err)
		}
		return nil, fmt.Errorf("browser bootstrap execution failed: %w: %s", err, msg)
	}
	payload, err := decodeBrowserBootstrapOutput(stdout.Bytes())
	if err != nil {
		if msg := clipBridgeOutput(stderr.Bytes()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return payload, nil
}

func decodeBrowserBootstrapOutput(out []byte) (*browserBootstrapOutput, error) {
	var payload browserBootstrapOutput
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("invalid browser bootstrap JSON: %w: %s", err, clipBridgeOutput(out))
	}
	if !payload.OK {
		if strings.TrimSpace(payload.Error) == "" {
			payload.Error = "unknown browser bootstrap error"
		}
		return nil, fmt.Errorf("browser bootstrap failed: %s", payload.Error)
	}
	if strings.TrimSpace(payload.Session.AccessToken) == "" {
		return nil, fmt.Errorf("browser bootstrap missing access_token")
	}
	return &payload, nil
}

func normalizeBootstrapSession(payload *browserBootstrapOutput) (*types.AppSession, error) {
	if payload == nil {
		return nil, fmt.Errorf("browser bootstrap returned empty payload")
	}
	now := time.Now().UTC()
	access := strings.TrimSpace(payload.Session.AccessToken)
	if access == "" {
		return nil, fmt.Errorf("browser bootstrap missing access token")
	}
	refresh := strings.TrimSpace(payload.Session.RefreshToken)
	if refresh == "" {
		refresh = access
	}
	expires := payload.Session.TokenExpires.UTC()
	if expires.IsZero() || !expires.After(now) {
		expires = now.Add(defaultBrowserSessionTTL)
	}
	headers := cloneSessionHeaders(payload.Session.DeviceHeaders)
	if headers["app_client"] == "" {
		headers["app_client"] = "consumer_web"
	}
	if headers["platform"] == "" {
		headers["platform"] = "mobile_web"
	}
	return &types.AppSession{
		App:           types.PlatformBlinkit,
		AccessToken:   access,
		RefreshToken:  refresh,
		ExpiresAt:     expires,
		DeviceHeaders: headers,
		CapturedAt:    now,
		UpdatedAt:     now,
	}, nil
}

func cloneSessionHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if key := strings.TrimSpace(k); key != "" {
			out[key] = v
		}
	}
	return out
}

func browserBootstrapTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(envBlinkitBrowserBootstrapTimeout))
	if raw == "" {
		return defaultBrowserBootstrapTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultBrowserBootstrapTimeout
	}
	return d
}
