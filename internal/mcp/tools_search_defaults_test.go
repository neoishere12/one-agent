package mcp

import (
	"context"
	"testing"

	"one-agent/internal/platforms"
	"one-agent/internal/types"
)

func TestSearchProductDefaultsAppsWhenOmitted(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: &mockPlatform{
			searchProducts: []types.Product{
				{ID: "p1", Name: "Lassi", PriceRupees: 28, Platform: types.PlatformBlinkit, InStock: true},
			},
		},
	})

	raw := mustRaw(t, map[string]any{
		"query":     "lassi",
		"latitude":  18.5,
		"longitude": 73.8,
	})
	result, err := server.call(context.Background(), "search_product", raw)
	if err != nil {
		t.Fatalf("search_product returned error: %v", err)
	}

	out := result.(searchProductOutput)
	if len(out.Results) == 0 {
		t.Fatal("expected at least one result")
	}
	if out.Results[0].App != "blinkit" {
		t.Fatalf("unexpected app: %q", out.Results[0].App)
	}
	if out.Errors["zepto"] == "" || out.Errors["instamart"] == "" {
		t.Fatal("expected missing-session failures for defaulted apps")
	}
}
