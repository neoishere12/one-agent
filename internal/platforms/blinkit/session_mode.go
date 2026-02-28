package blinkit

import "one-agent/internal/types"

func (c *Client) requestBaseURL(sess *types.AppSession) string {
	// Respect explicit overrides in tests or custom deployments.
	if c.base.BaseURL != DefaultBaseURL {
		return c.base.BaseURL
	}
	if isWebSession(sess) {
		return WebBaseURL
	}
	return DefaultBaseURL
}

func isWebSession(sess *types.AppSession) bool {
	if sess == nil {
		return false
	}
	for k, v := range sess.DeviceHeaders {
		if k == "app_client" && v == "consumer_web" {
			return true
		}
	}
	return false
}
