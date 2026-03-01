package blinkit

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"one-agent/internal/types"
)

func TestSearchBrowserModeFailsFastOnWorkerChallenge(t *testing.T) {
	t.Setenv("BLINKIT_SEARCH_MODE", "browser")
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "http://worker.local")

	oldStatusFn := browserWorkerStatusQueryFn
	oldBridgeFn := browserSearchBridgeFn
	t.Cleanup(func() {
		browserWorkerStatusQueryFn = oldStatusFn
		browserSearchBridgeFn = oldBridgeFn
	})

	browserWorkerStatusQueryFn = func(ctx context.Context, baseURL string) (*BrowserWorkerStatus, error) {
		return &BrowserWorkerStatus{
			OK:                 true,
			PageURL:            "https://blinkit.com/s/?q=amul",
			Title:              "blinkit | Error Page",
			AccessTokenPresent: false,
			AuthKeyPresent:     false,
			ChallengeDetected:  true,
		}, nil
	}

	var bridgeCalls int32
	browserSearchBridgeFn = func(c *Client, ctx context.Context, query string, lat, lng float64) ([]types.Product, error) {
		atomic.AddInt32(&bridgeCalls, 1)
		return nil, nil
	}

	c := &Client{}
	_, err := c.searchViaBrowser(context.Background(), "amul lassi", 18.6456, 73.8852)
	if err == nil {
		t.Fatal("expected challenge error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "needs_human_verification") {
		t.Fatalf("expected needs_human_verification error, got %v", err)
	}
	if got := atomic.LoadInt32(&bridgeCalls); got != 0 {
		t.Fatalf("expected browser bridge not called on challenge preflight, got %d", got)
	}
}
