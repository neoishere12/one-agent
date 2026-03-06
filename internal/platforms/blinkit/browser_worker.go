package blinkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"one-agent/internal/types"
)

var ErrBrowserWorkerNotConfigured = errors.New("blinkit browser worker url not configured")

type browserWorkerRequest struct {
	Query          string   `json:"query"`
	Lat            *float64 `json:"lat,omitempty"`
	Lng            *float64 `json:"lng,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

type browserBootstrapWorkerRequest struct {
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

type browserMetadataOutput struct {
	OK        bool             `json:"ok"`
	Error     string           `json:"error"`
	Addresses []browserAddress `json:"addresses"`
	Payments  []browserPayment `json:"payments"`
}

type browserOrderWorkerRequest struct {
	ProductID      string `json:"product_id"`
	Quantity       int    `json:"quantity"`
	AddressID      string `json:"address_id"`
	PaymentToken   string `json:"payment_token"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type browserOrderOutput struct {
	OK    bool               `json:"ok"`
	Error string             `json:"error"`
	Order browserOrderResult `json:"order"`
}

type browserOrderResult struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	ETAMinutes  int     `json:"eta_minutes"`
	TotalRupees float64 `json:"total_rupees"`
}

// BrowserWorkerStatus is the worker-reported browser session state used for
// fast challenge/login diagnostics.
type BrowserWorkerStatus struct {
	OK                 bool   `json:"ok"`
	PageURL            string `json:"page_url"`
	Title              string `json:"title"`
	AccessTokenPresent bool   `json:"access_token_present"`
	AuthKeyPresent     bool   `json:"auth_key_present"`
	ChallengeDetected  bool   `json:"challenge_detected"`
}

// BrowserWorkerConfigured reports whether BLINKIT_BROWSER_WORKER_URL is set.
func BrowserWorkerConfigured() bool {
	return browserWorkerURL() != ""
}

// BrowserWorkerStatusSnapshot returns current worker-side session/challenge state.
func BrowserWorkerStatusSnapshot(ctx context.Context) (*BrowserWorkerStatus, error) {
	workerURL := browserWorkerURL()
	if workerURL == "" {
		return nil, ErrBrowserWorkerNotConfigured
	}
	return queryBrowserWorkerStatus(ctx, workerURL)
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

func browserWorkerMetadata(
	ctx context.Context,
	baseURL string,
) ([]types.Address, []types.PaymentMethod, error) {
	req := browserBootstrapWorkerRequest{TimeoutSeconds: contextTimeoutSeconds(ctx)}
	out, err := postBrowserWorker(ctx, baseURL, "/metadata", req)
	if err != nil {
		return nil, nil, err
	}
	var payload browserMetadataOutput
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, nil, fmt.Errorf("invalid worker metadata JSON: %w: %s", err, clipBridgeOutput(out))
	}
	if !payload.OK {
		return nil, nil, fmt.Errorf("browser metadata failed: %s", strings.TrimSpace(payload.Error))
	}
	return mapBootstrapAddresses(payload.Addresses), mapBootstrapPayments(payload.Payments), nil
}

func browserWorkerPlaceOrder(
	ctx context.Context,
	baseURL string,
	productID, addressID, paymentToken string,
	quantity int,
) (types.Order, error) {
	req := browserOrderWorkerRequest{
		ProductID:      productID,
		Quantity:       quantity,
		AddressID:      addressID,
		PaymentToken:   paymentToken,
		TimeoutSeconds: contextTimeoutSeconds(ctx),
	}
	out, err := postBrowserWorker(ctx, baseURL, "/order", req)
	if err != nil {
		return types.Order{}, err
	}
	var payload browserOrderOutput
	if err := json.Unmarshal(out, &payload); err != nil {
		return types.Order{}, fmt.Errorf("invalid worker order JSON: %w: %s", err, clipBridgeOutput(out))
	}
	if !payload.OK {
		return types.Order{}, fmt.Errorf("browser order failed: %s", strings.TrimSpace(payload.Error))
	}
	return types.Order{
		ID:          strings.TrimSpace(payload.Order.ID),
		Platform:    types.PlatformBlinkit,
		Status:      strings.TrimSpace(payload.Order.Status),
		ETAMinutes:  payload.Order.ETAMinutes,
		TotalRupees: payload.Order.TotalRupees,
		PlacedAt:    time.Now(),
	}, nil
}

func queryBrowserWorkerStatus(ctx context.Context, baseURL string) (*BrowserWorkerStatus, error) {
	out, err := getBrowserWorker(ctx, baseURL, "/status")
	if err != nil {
		return nil, err
	}
	var status BrowserWorkerStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return nil, fmt.Errorf("invalid worker status JSON: %w: %s", err, clipBridgeOutput(out))
	}
	if !status.OK {
		return nil, fmt.Errorf("worker status payload not ok: %s", clipBridgeOutput(out))
	}
	return &status, nil
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

func getBrowserWorker(
	ctx context.Context,
	baseURL string,
	path string,
) ([]byte, error) {
	url := strings.TrimRight(baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create worker request: %w", err)
	}
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
