package proxyman

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func walkJSON(v any, visitMap func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		visitMap(x)
		for _, child := range x {
			walkJSON(child, visitMap)
		}
	case []any:
		for _, child := range x {
			walkJSON(child, visitMap)
		}
	}
}

func getString(m map[string]any, keys ...string) string {
	for _, want := range keys {
		for key, raw := range m {
			if normKey(key) != normKey(want) {
				continue
			}
			s, ok := raw.(string)
			if ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func getFloat(m map[string]any, keys ...string) (float64, bool) {
	for _, want := range keys {
		for key, raw := range m {
			if normKey(key) != normKey(want) {
				continue
			}
			switch n := raw.(type) {
			case float64:
				return n, true
			case int:
				return float64(n), true
			case int64:
				return float64(n), true
			}
		}
	}
	return 0, false
}

func getAny(m map[string]any, keys ...string) (any, bool) {
	for _, want := range keys {
		for key, raw := range m {
			if normKey(key) == normKey(want) {
				return raw, true
			}
		}
	}
	return nil, false
}

func normKey(key string) string {
	lower := strings.ToLower(strings.TrimSpace(key))
	repl := strings.NewReplacer("_", "", "-", "", " ", "")
	return repl.Replace(lower)
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return formatFloatValue(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case fmt.Stringer:
		return strings.TrimSpace(x.String())
	default:
		return ""
	}
}

func formatFloatValue(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return ""
	}
	if math.Trunc(v) == v {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
