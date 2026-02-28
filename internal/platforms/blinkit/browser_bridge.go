package blinkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"one-agent/internal/types"
)

const (
	envBlinkitSearchMode      = "BLINKIT_SEARCH_MODE"
	envBlinkitBrowserHelper   = "BLINKIT_BROWSER_HELPER"
	envBlinkitBrowserNode     = "BLINKIT_BROWSER_NODE"
	envBlinkitBrowserTimeout  = "BLINKIT_BROWSER_TIMEOUT"
	envBlinkitBrowserWorker   = "BLINKIT_BROWSER_WORKER_URL"
	defaultBrowserHelperPath  = "scripts/blinkit-browser-search.mjs"
	defaultBrowserNodeBinary  = "node"
	defaultBrowserBridgeDelay = 45 * time.Second
)

type browserBridgeOutput struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error"`
	URL   string          `json:"url"`
	Raw   json.RawMessage `json:"raw"`
}

func browserModeEnabled() bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(envBlinkitSearchMode)))
	return mode == "browser"
}

func (c *Client) searchViaBrowserBridge(ctx context.Context, query string, lat, lng float64) ([]types.Product, error) {
	timeout := browserBridgeTimeout()
	bridgeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if workerURL := browserWorkerURL(); workerURL != "" {
		payload, err := queryBrowserWorker(bridgeCtx, workerURL, query, lat, lng)
		if err != nil {
			return nil, fmt.Errorf("browser worker request failed: %w", err)
		}
		return decodeBridgePayload(payload)
	}

	nodeBin := browserNodeBinary()
	args := browserBridgeArgs(query, lat, lng)
	cmd := exec.CommandContext(bridgeCtx, nodeBin, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	out := stdout.Bytes()
	diag := stderr.Bytes()
	if bridgeCtx.Err() == context.DeadlineExceeded {
		snippet := clipBridgeOutput(joinBridgeOutput(diag, out))
		if snippet == "" {
			return nil, fmt.Errorf("browser helper timed out after %s", timeout)
		}
		return nil, fmt.Errorf("browser helper timed out after %s: %s", timeout, snippet)
	}
	payload, decodeErr := decodeBrowserBridgeOutput(out)
	if decodeErr != nil {
		if runErr != nil {
			return nil, fmt.Errorf(
				"browser helper execution failed: %w: %s",
				runErr,
				clipBridgeOutput(joinBridgeOutput(diag, out)),
			)
		}
		if text := clipBridgeOutput(diag); text != "" {
			return nil, fmt.Errorf("%w: %s", decodeErr, text)
		}
		return nil, decodeErr
	}
	if runErr != nil && payload.OK {
		return nil, fmt.Errorf("browser helper execution failed: %w: %s", runErr, clipBridgeOutput(diag))
	}
	return decodeBridgePayloadWithDiag(payload, diag)
}

func browserBridgeArgs(query string, lat, lng float64) []string {
	args := []string{browserHelperPath(), "--query", query}
	if lat != 0 {
		args = append(args, "--lat", strconv.FormatFloat(lat, 'f', -1, 64))
	}
	if lng != 0 {
		args = append(args, "--lng", strconv.FormatFloat(lng, 'f', -1, 64))
	}
	return args
}

func browserHelperPath() string {
	helper := strings.TrimSpace(os.Getenv(envBlinkitBrowserHelper))
	if helper == "" {
		return defaultBrowserHelperPath
	}
	return helper
}

func browserNodeBinary() string {
	node := strings.TrimSpace(os.Getenv(envBlinkitBrowserNode))
	if node == "" {
		return defaultBrowserNodeBinary
	}
	return node
}

func browserBridgeTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(envBlinkitBrowserTimeout))
	if raw == "" {
		return defaultBrowserBridgeDelay
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultBrowserBridgeDelay
	}
	return d
}

func browserWorkerURL() string {
	return strings.TrimSpace(os.Getenv(envBlinkitBrowserWorker))
}

func decodeBrowserBridgeOutput(out []byte) (*browserBridgeOutput, error) {
	var payload browserBridgeOutput
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("invalid browser helper JSON: %w: %s", err, clipBridgeOutput(out))
	}
	if !payload.OK {
		return &payload, nil
	}
	if len(payload.Raw) == 0 {
		return nil, fmt.Errorf("browser helper returned empty payload")
	}
	return &payload, nil
}

func decodeBridgePayload(payload *browserBridgeOutput) ([]types.Product, error) {
	return decodeBridgePayloadWithDiag(payload, nil)
}

func decodeBridgePayloadWithDiag(payload *browserBridgeOutput, diag []byte) ([]types.Product, error) {
	if !payload.OK {
		if payload.Error == "" {
			payload.Error = "unknown browser helper error"
		}
		if text := clipBridgeOutput(diag); text != "" {
			return nil, fmt.Errorf("browser helper error: %s (%s)", payload.Error, text)
		}
		return nil, fmt.Errorf("browser helper error: %s", payload.Error)
	}
	return decodeBrowserProducts(payload.Raw)
}

func decodeBrowserProducts(raw json.RawMessage) ([]types.Product, error) {
	var result searchResp
	if err := json.Unmarshal(raw, &result); err == nil {
		products := result.toProducts()
		if len(products) > 0 {
			return products, nil
		}
	}
	products, err := decodeSearchFallback(raw)
	if err != nil {
		return nil, fmt.Errorf("decode browser search payload: %w", err)
	}
	for i := range products {
		products[i].Platform = types.PlatformBlinkit
	}
	return products, nil
}

func clipBridgeOutput(out []byte) string {
	const maxLen = 400
	text := strings.TrimSpace(string(out))
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen]
}

func joinBridgeOutput(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, '\n')
		}
		out = append(out, part...)
	}
	return out
}
