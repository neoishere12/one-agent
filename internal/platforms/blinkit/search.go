package blinkit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/types"
)

const browserWorkerStatusProbeTimeout = 3 * time.Second

var (
	browserWorkerStatusQueryFn = queryBrowserWorkerStatus
	browserSearchBridgeFn      = func(c *Client, ctx context.Context, query string, lat, lng float64) ([]types.Product, error) {
		return c.searchViaBrowserBridge(ctx, query, lat, lng)
	}
)

// Search queries Blinkit for products matching query at the given coordinates.
func (c *Client) Search(ctx context.Context, query string, lat, lng float64) ([]types.Product, error) {
	if browserModeEnabled() {
		return c.searchViaBrowser(ctx, query, lat, lng)
	}

	sess, err := c.loadSession(ctx)
	if err != nil {
		return nil, err
	}
	if isWebSession(sess) {
		c.warmupWebSession(ctx, sess)
	}
	sess = withSearchLocation(sess, lat, lng)
	return c.searchViaAPI(ctx, query, sess)
}

func (c *Client) searchViaBrowser(ctx context.Context, query string, lat, lng float64) ([]types.Product, error) {
	if workerURL := browserWorkerURL(); workerURL != "" {
		probeCtx, cancel := context.WithTimeout(ctx, browserWorkerStatusProbeTimeout)
		status, err := browserWorkerStatusQueryFn(probeCtx, workerURL)
		cancel()
		if err == nil && status.ChallengeDetected {
			return nil, fmt.Errorf(
				"needs_human_verification: blinkit challenge detected in browser session; run reverify_blinkit_session",
			)
		}
	}

	products, err := browserSearchBridgeFn(c, ctx, query, lat, lng)
	if err != nil {
		return nil, fmt.Errorf("blinkit browser search: %w", err)
	}
	return products, nil
}

func (c *Client) searchViaAPI(ctx context.Context, query string, sess *types.AppSession) ([]types.Product, error) {
	resp, err := c.base.DoRequestWithBaseURL(
		ctx,
		http.MethodPost,
		c.requestBaseURL(sess),
		searchPath(query),
		map[string]any{},
		sess,
	)
	if err != nil {
		return nil, fmt.Errorf("blinkit search: %w", err)
	}
	defer resp.Body.Close()
	logSearchFailure(resp, sess)
	if err := platforms.CheckStatus(resp, "blinkit", "search"); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("blinkit search read: %w", err)
	}
	return decodeSearchProducts(body)
}

func logSearchFailure(resp *http.Response, sess *types.AppSession) {
	if resp.StatusCode < http.StatusBadRequest {
		return
	}
	host := ""
	path := ""
	if resp.Request != nil && resp.Request.URL != nil {
		host = resp.Request.URL.Host
		path = resp.Request.URL.Path
	}
	slog.Warn("blinkit search non-2xx",
		"status", resp.StatusCode,
		"host", host,
		"path", path,
		"app_client", sess.DeviceHeaders["app_client"],
	)
}

func decodeSearchProducts(body []byte) ([]types.Product, error) {
	var result searchResp
	if err := json.Unmarshal(body, &result); err == nil {
		products := result.toProducts()
		if len(products) > 0 {
			return products, nil
		}
	}
	products, err := decodeSearchFallback(body)
	if err != nil {
		return nil, fmt.Errorf("blinkit search decode: %w", err)
	}
	return products, nil
}
