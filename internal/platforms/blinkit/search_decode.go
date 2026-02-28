package blinkit

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"one-agent/internal/types"
)

func searchPath(query string) string {
	return fmt.Sprintf("%s?q=%s&search_type=type_to_search&", pathSearch, url.QueryEscape(strings.TrimSpace(query)))
}

func decodeSearchFallback(data []byte) ([]types.Product, error) {
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	out := make([]types.Product, 0)
	walkJSON(decoded, func(m map[string]any) {
		id := mapString(m, "product_id", "id")
		name := mapString(m, "name", "title")
		price, ok := mapFloat(m, "price")
		if id == "" || name == "" || !ok {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		mrp, _ := mapFloat(m, "mrp")
		state := strings.ToLower(mapString(m, "state"))
		out = append(out, types.Product{
			ID:          id,
			Name:        name,
			Brand:       mapString(m, "brand"),
			PriceRupees: price,
			MRP:         mrp,
			Platform:    types.PlatformBlinkit,
			InStock:     state != "out_of_stock",
		})
	})
	return out, nil
}

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

func mapString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := m[key]
		if !ok {
			continue
		}
		if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func mapFloat(m map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		raw, ok := m[key]
		if !ok {
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
	return 0, false
}
