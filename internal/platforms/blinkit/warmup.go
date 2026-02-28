package blinkit

import (
	"context"
	"log/slog"
	"net/http"

	"one-agent/internal/types"
)

func (c *Client) warmupWebSession(ctx context.Context, sess *types.AppSession) {
	key := warmupSessionKey(sess)
	if c.shouldSkipWarmup(key) {
		return
	}
	base := c.requestBaseURL(sess)
	steps := []struct {
		method string
		path   string
		body   any
	}{
		{method: http.MethodGet, path: "/config/main"},
		{method: http.MethodGet, path: "/v1/consumerweb/eta"},
		{method: http.MethodPost, path: "/v1/layout/feed", body: map[string]any{}},
	}
	for _, step := range steps {
		resp, err := c.base.DoRequestWithBaseURL(ctx, step.method, base, step.path, step.body, sess)
		if err != nil {
			slog.Debug("blinkit web warmup request failed", "method", step.method, "path", step.path, "err", err)
			continue
		}
		resp.Body.Close()
	}
	c.markWarmed(key)
}

func warmupSessionKey(sess *types.AppSession) string {
	if sess == nil {
		return ""
	}
	return sess.AccessToken + "|" + sess.RefreshToken
}

func (c *Client) shouldSkipWarmup(key string) bool {
	c.warmupMu.Lock()
	defer c.warmupMu.Unlock()
	return key != "" && c.warmedSessionKey == key
}

func (c *Client) markWarmed(key string) {
	if key == "" {
		return
	}
	c.warmupMu.Lock()
	c.warmedSessionKey = key
	c.warmupMu.Unlock()
}
