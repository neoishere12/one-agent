package mcp

import (
	"context"
	"sort"
	"strings"
	"sync"

	"one-agent/internal/types"
)

func (s *Server) handleSearchProduct(ctx context.Context, raw []byte) (any, *toolError) {
	var input searchProductInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Query) == "" {
		return nil, invalidParams("query is required", nil)
	}
	apps, parseErr := parseApps(input.Apps)
	if parseErr != nil {
		return nil, parseErr
	}
	results, failures := s.searchAcross(ctx, apps, input.Query, input.Latitude, input.Longitude)
	return searchProductOutput{Results: results, Errors: failures}, nil
}

func (s *Server) handleComparePrices(ctx context.Context, raw []byte) (any, *toolError) {
	var input comparePricesInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Query) == "" {
		return nil, invalidParams("query is required", nil)
	}
	apps := []types.Platform{types.PlatformBlinkit, types.PlatformZepto, types.PlatformInstamart}
	results, failures := s.searchAcross(ctx, apps, input.Query, input.Latitude, input.Longitude)
	ranked := rankResults(results)
	return comparePricesOutput{
		Query:        input.Query,
		Ranked:       ranked,
		SearchedApps: appStrings(apps),
		FailedApps:   failedApps(failures),
	}, nil
}

func (s *Server) searchAcross(
	ctx context.Context,
	apps []types.Platform,
	query string,
	lat, lng float64,
) ([]searchResult, map[string]string) {
	results := make([]searchResult, 0)
	failures := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, app := range apps {
		wg.Add(1)
		go func(app types.Platform) {
			defer wg.Done()
			s.searchOne(ctx, &mu, app, query, lat, lng, &results, failures)
		}(app)
	}

	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].Price < results[j].Price })
	return results, failures
}

func (s *Server) searchOne(
	ctx context.Context,
	mu *sync.Mutex,
	app types.Platform,
	query string,
	lat, lng float64,
	results *[]searchResult,
	failures map[string]string,
) {
	client, getErr := s.platform(app)
	if getErr != nil {
		recordFailure(mu, failures, app, getErr.Error())
		return
	}

	searchCtx, cancel := deadlineContext(ctx, s.searchLimit)
	defer cancel()
	products, err := client.Search(searchCtx, query, lat, lng)
	if err != nil {
		recordFailure(mu, failures, app, normalizeSearchError(app, err.Error()))
		return
	}

	mu.Lock()
	*results = append(*results, productsToSearchResults(app, products)...)
	mu.Unlock()
}

func parseApps(raw []string) ([]types.Platform, *toolError) {
	if len(raw) == 0 {
		return []types.Platform{
			types.PlatformBlinkit,
			types.PlatformZepto,
			types.PlatformInstamart,
		}, nil
	}
	out := make([]types.Platform, 0, len(raw))
	seen := make(map[types.Platform]struct{}, len(raw))
	for _, appRaw := range raw {
		app, err := parsePlatform(strings.ToLower(strings.TrimSpace(appRaw)))
		if err != nil {
			return nil, err
		}
		if _, exists := seen[app]; exists {
			continue
		}
		seen[app] = struct{}{}
		out = append(out, app)
	}
	return out, nil
}

func productsToSearchResults(app types.Platform, products []types.Product) []searchResult {
	out := make([]searchResult, 0, len(products))
	for _, product := range products {
		out = append(out, searchResult{
			App:         string(app),
			ProductID:   product.ID,
			Name:        product.Name,
			Price:       product.PriceRupees,
			MRP:         product.MRP,
			DeliveryFee: 0,
			ETAMinutes:  0,
			InStock:     product.InStock,
		})
	}
	return out
}

func rankResults(results []searchResult) []rankedPrice {
	ranked := make([]rankedPrice, 0, len(results))
	for i, result := range results {
		total := result.Price + result.DeliveryFee
		ranked = append(ranked, rankedPrice{
			Rank:        i + 1,
			App:         result.App,
			ProductID:   result.ProductID,
			Name:        result.Name,
			Price:       result.Price,
			DeliveryFee: result.DeliveryFee,
			Total:       total,
			ETAMinutes:  result.ETAMinutes,
		})
	}
	return ranked
}

func failedApps(failures map[string]string) []string {
	out := make([]string, 0, len(failures))
	for app := range failures {
		out = append(out, app)
	}
	sort.Strings(out)
	return out
}

func appStrings(apps []types.Platform) []string {
	out := make([]string, 0, len(apps))
	for _, app := range apps {
		out = append(out, string(app))
	}
	return out
}

func recordFailure(mu *sync.Mutex, failures map[string]string, app types.Platform, message string) {
	mu.Lock()
	failures[string(app)] = message
	mu.Unlock()
}

func normalizeSearchError(app types.Platform, message string) string {
	if app != types.PlatformBlinkit {
		return message
	}
	lower := strings.ToLower(strings.TrimSpace(message))
	if strings.Contains(lower, "needs_human_verification") || strings.Contains(lower, "human_verification_required") {
		return "needs_human_verification: run reverify_blinkit_session and complete verification/login in the Blinkit worker profile"
	}
	return message
}
