package proxyman

import (
	"net/url"
	"strings"
)

func detectionRoute(u *url.URL) string {
	if u == nil {
		return ""
	}
	if strings.TrimSpace(u.RawQuery) == "" {
		return u.Path
	}
	return u.Path + "?" + u.RawQuery
}

func isAuthResponseRoute(route string) bool {
	lower := strings.ToLower(route)
	switch {
	case strings.Contains(lower, "/auth/"):
		return true
	case strings.Contains(lower, "/oauth/"):
		return true
	case strings.Contains(lower, "/refresh"):
		return true
	case strings.Contains(lower, "/login"):
		return true
	case strings.Contains(lower, "/session"):
		return true
	default:
		return false
	}
}

func isBlinkitSearchPath(route string) bool {
	lower := strings.ToLower(route)
	return strings.Contains(lower, "/v1/layout/search")
}
