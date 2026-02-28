package proxyman

import (
	"strings"

	"one-agent/internal/types"
)

func consumeRequestHeaders(agg *aggregate, headers []harHeader, app types.Platform) bool {
	foundAuth := false
	for _, h := range headers {
		name := strings.TrimSpace(h.Name)
		value := strings.TrimSpace(h.Value)
		if name == "" || value == "" {
			continue
		}
		lower := strings.ToLower(name)
		if token, ok := bearerToken(value); ok && lower == "authorization" {
			agg.accessToken = token
			foundAuth = true
			continue
		}
		if consumeTokenHeader(agg, app, lower, value) {
			foundAuth = true
			continue
		}
	}
	for k, v := range collectDeviceHeaders(headers) {
		agg.deviceHeaders[k] = v
	}
	return foundAuth
}

func collectDeviceHeaders(headers []harHeader) map[string]string {
	out := make(map[string]string)
	for _, h := range headers {
		name := strings.TrimSpace(h.Name)
		value := strings.TrimSpace(h.Value)
		if name == "" || value == "" {
			continue
		}
		lower := strings.ToLower(name)
		if lower == "cookie" {
			if normalized := normalizeCookieHeader(value); normalized != "" {
				out["cookie"] = normalized
			}
			continue
		}
		if isBlockedDeviceHeader(lower) {
			continue
		}
		if shouldKeepDeviceHeader(lower) {
			out[lower] = value
		}
	}
	return out
}

func consumeTokenHeader(agg *aggregate, app types.Platform, name, value string) bool {
	switch name {
	case "access_token", "x-access-token":
		agg.accessToken = value
		return true
	case "refresh_token", "x-refresh-token":
		agg.refreshToken = value
		return true
	case "auth_key":
		// Blinkit commonly sends `auth_key` alongside `access_token` in request headers.
		// Use it as a refresh-token fallback when no refresh body is captured.
		if app == types.PlatformBlinkit && agg.refreshToken == "" {
			agg.refreshToken = value
			return true
		}
	}
	return false
}

func bearerToken(v string) (string, bool) {
	if len(v) < len("Bearer ") {
		return "", false
	}
	if !strings.EqualFold(v[:7], "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(v[7:])
	if token == "" {
		return "", false
	}
	return token, true
}

func shouldKeepDeviceHeader(name string) bool {
	if strings.HasPrefix(name, "x-") {
		return true
	}
	return isKnownDeviceHeader(name)
}

func isBlockedDeviceHeader(name string) bool {
	switch name {
	case "content-length", "connection", "accept-encoding", "host", "transfer-encoding":
		return true
	default:
		return false
	}
}

func isKnownDeviceHeader(name string) bool {
	switch name {
	case "user-agent", "accept-language", "accept", "content-type":
		return true
	case "origin", "referer":
		return true
	case "sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform":
		return true
	case "sec-fetch-dest", "sec-fetch-mode", "sec-fetch-site":
		return true
	case "device_id", "device_token", "device_os", "advertising_id":
		return true
	case "app_client", "app_version", "app_api_version", "version_name", "host_app":
		return true
	case "platform", "web_app_version":
		return true
	case "lat", "lon", "cur_lat", "cur_lon", "entry_source":
		return true
	case "screen_width", "screen_density":
		return true
	case "session_uuid", "req_key", "rn_bundle_version":
		return true
	case "memory-level", "cpu-level", "battery-level", "storage-level":
		return true
	case "is_low_power_mode", "is_accessibility_enabled":
		return true
	case "api_experiment", "qd_sdk_version", "qd_sdk_request":
		return true
	default:
		return false
	}
}

func normalizeCookieHeader(raw string) string {
	parts := strings.Split(raw, ";")
	pairs := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if key == "" || val == "" {
			continue
		}
		lowerKey := strings.ToLower(key)
		if isCookieAttribute(lowerKey) {
			continue
		}
		if _, exists := seen[lowerKey]; exists {
			continue
		}
		seen[lowerKey] = struct{}{}
		pairs = append(pairs, key+"="+val)
	}
	return strings.Join(pairs, "; ")
}

func isCookieAttribute(key string) bool {
	switch key {
	case "path", "domain", "expires", "max-age", "samesite", "secure", "httponly", "priority", "partitioned":
		return true
	default:
		return false
	}
}
