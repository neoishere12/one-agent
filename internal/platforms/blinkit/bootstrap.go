package blinkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	Addresses     []browserAddress  `json:"addresses"`
	Payments      []browserPayment  `json:"payments"`
}

type browserAddress struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	FullAddress string  `json:"full_address"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	IsDefault   bool    `json:"is_default"`
}

type browserPayment struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Token     string `json:"token"`
	Type      string `json:"type"`
	IsDefault bool   `json:"is_default"`
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
	mergeBootstrapMetadata(ctx, st, session)
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
		Addresses:     mapBootstrapAddresses(payload.Session.Addresses),
		Payments:      mapBootstrapPayments(payload.Session.Payments),
		CapturedAt:    now,
		UpdatedAt:     now,
	}, nil
}

func mergeBootstrapMetadata(ctx context.Context, st *store.Store, session *types.AppSession) {
	if st == nil || session == nil {
		return
	}
	existing, err := st.Get(ctx, types.PlatformBlinkit)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return
		}
		return
	}
	if len(session.Addresses) == 0 && len(existing.Addresses) > 0 {
		session.Addresses = append([]types.Address(nil), existing.Addresses...)
	}
	if len(session.Payments) == 0 && len(existing.Payments) > 0 {
		session.Payments = append([]types.PaymentMethod(nil), existing.Payments...)
	}
}

func mapBootstrapAddresses(in []browserAddress) []types.Address {
	if len(in) == 0 {
		return nil
	}
	out := make([]types.Address, 0, len(in))
	for _, address := range in {
		id := strings.TrimSpace(address.ID)
		full := strings.TrimSpace(address.FullAddress)
		if id == "" || full == "" {
			continue
		}
		out = append(out, types.Address{
			ID:        id,
			Label:     strings.TrimSpace(address.Label),
			Line1:     full,
			Lat:       address.Lat,
			Lng:       address.Lng,
			IsDefault: address.IsDefault,
		})
	}
	return out
}

func mapBootstrapPayments(in []browserPayment) []types.PaymentMethod {
	if len(in) == 0 {
		return nil
	}
	out := make([]types.PaymentMethod, 0, len(in))
	for _, payment := range in {
		token := strings.TrimSpace(payment.Token)
		if token == "" {
			continue
		}
		id := strings.TrimSpace(payment.ID)
		if id == "" {
			id = token
		}
		out = append(out, types.PaymentMethod{
			ID:        id,
			Type:      strings.TrimSpace(payment.Type),
			Label:     strings.TrimSpace(payment.Label),
			Token:     token,
			IsDefault: payment.IsDefault,
		})
	}
	return out
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
