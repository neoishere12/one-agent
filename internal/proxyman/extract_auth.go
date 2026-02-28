package proxyman

import (
	"strconv"
	"strings"
	"time"
)

func consumeAuthFields(agg *aggregate, decoded any, now time.Time) {
	fields := findAuthFields(decoded, now)
	mergeAuthFields(agg, fields)
}

func findAuthFields(decoded any, now time.Time) authFields {
	var out authFields
	walkJSON(decoded, func(m map[string]any) {
		if token := getString(m, "access_token", "accessToken"); token != "" {
			out.AccessToken = token
		}
		if token := getString(m, "refresh_token", "refreshToken"); token != "" {
			out.RefreshToken = token
		}
		if t, ok := expiresAtFromMap(m, now); ok {
			out.ExpiresAt = t
		}
	})
	return out
}

func mergeAuthFields(agg *aggregate, fields authFields) {
	if fields.AccessToken != "" {
		agg.accessToken = fields.AccessToken
	}
	if fields.RefreshToken != "" {
		agg.refreshToken = fields.RefreshToken
	}
	if !fields.ExpiresAt.IsZero() {
		agg.expiresAt = fields.ExpiresAt.UTC()
	}
}

func hasAuthFields(fields authFields) bool {
	return fields.AccessToken != "" || fields.RefreshToken != "" || !fields.ExpiresAt.IsZero()
}

func expiresAtFromMap(m map[string]any, now time.Time) (time.Time, bool) {
	if raw, ok := getAny(m,
		"token_expires_at",
		"expires_at",
		"expiresAt",
		"access_token_expires_at",
		"accessTokenExpiresAt",
		"expiry",
		"valid_till",
		"validTill",
	); ok {
		if t, ok := parseAbsoluteExpiry(raw); ok {
			return t, true
		}
	}
	if raw, ok := getAny(m, "expires_in", "expiresIn", "access_expires_in", "accessExpiresIn"); ok {
		if secs, ok := parseSeconds(raw); ok && secs > 0 {
			return now.Add(time.Duration(secs) * time.Second), true
		}
	}
	return time.Time{}, false
}

func parseAbsoluteExpiry(raw any) (time.Time, bool) {
	switch v := raw.(type) {
	case string:
		return parseTimeString(v)
	case float64:
		return parseUnixNumber(v)
	default:
		return time.Time{}, false
	}
}

func parseTimeString(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return parseUnixInt(n)
	}
	return time.Time{}, false
}

func parseUnixNumber(n float64) (time.Time, bool) {
	return parseUnixInt(int64(n))
}

func parseUnixInt(n int64) (time.Time, bool) {
	if n <= 0 {
		return time.Time{}, false
	}
	if n > 1_000_000_000_000 {
		return time.UnixMilli(n), true
	}
	if n > 1_000_000_000 {
		return time.Unix(n, 0), true
	}
	return time.Time{}, false
}

func parseSeconds(raw any) (int64, bool) {
	switch v := raw.(type) {
	case float64:
		return int64(v), true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}
