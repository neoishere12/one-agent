package blinkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type browserWorkerRequest struct {
	Query          string   `json:"query"`
	Lat            *float64 `json:"lat,omitempty"`
	Lng            *float64 `json:"lng,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

type browserBootstrapWorkerRequest struct {
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

func queryBrowserWorker(
	ctx context.Context,
	baseURL string,
	query string,
	lat, lng float64,
) (*browserBridgeOutput, error) {
	out, err := postBrowserWorker(ctx, baseURL, "/search", newWorkerRequest(ctx, query, lat, lng))
	if err != nil {
		return nil, err
	}
	payload, err := decodeBrowserBridgeOutput(out)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func bootstrapBrowserWorker(
	ctx context.Context,
	baseURL string,
	timeoutSeconds int,
) (*browserBootstrapOutput, error) {
	if remaining := contextTimeoutSeconds(ctx); remaining > 0 && (timeoutSeconds <= 0 || remaining < timeoutSeconds) {
		timeoutSeconds = remaining
	}
	req := browserBootstrapWorkerRequest{TimeoutSeconds: timeoutSeconds}
	out, err := postBrowserWorker(ctx, baseURL, "/bootstrap", req)
	if err != nil {
		return nil, err
	}
	return decodeBrowserBootstrapOutput(out)
}

func postBrowserWorker(
	ctx context.Context,
	baseURL string,
	path string,
	payload any,
) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal worker payload: %w", err)
	}
	url := strings.TrimRight(baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create worker request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("worker http call: %w", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read worker response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("worker status %d: %s", resp.StatusCode, clipBridgeOutput(out))
	}
	return out, nil
}

func newWorkerRequest(ctx context.Context, query string, lat, lng float64) browserWorkerRequest {
	req := browserWorkerRequest{Query: query}
	if lat != 0 {
		req.Lat = &lat
	}
	if lng != 0 {
		req.Lng = &lng
	}
	req.TimeoutSeconds = contextTimeoutSeconds(ctx)
	return req
}

func contextTimeoutSeconds(ctx context.Context) int {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	remaining := deadline.Sub(time.Now())
	if remaining <= 0 {
		return 1
	}
	secs := int(remaining.Seconds())
	if remaining > time.Duration(secs)*time.Second {
		secs++
	}
	if secs <= 0 {
		return 1
	}
	return secs
}
