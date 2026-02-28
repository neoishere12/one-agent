package proxyman

import (
	"fmt"
	"sort"
	"strings"
)

// ParseDiagnostics provides redacted parser diagnostics for a HAR import attempt.
type ParseDiagnostics struct {
	TotalEntries           int
	JSONResponses          int
	AuthHeaderEntries      int
	AuthFieldResponses     int
	AddressPathEntries     int
	PaymentPathEntries     int
	DeviceHeadersCollected int
	AddressesExtracted     int
	PaymentsExtracted      int
	PathCounts             map[string]int
}

func newParseDiagnostics() *ParseDiagnostics {
	return &ParseDiagnostics{PathCounts: make(map[string]int)}
}

func (d *ParseDiagnostics) notePath(path string) {
	if d == nil {
		return
	}
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	d.TotalEntries++
	d.PathCounts[path]++
}

func (d *ParseDiagnostics) finalize(agg *aggregate) {
	if d == nil || agg == nil {
		return
	}
	d.DeviceHeadersCollected = len(agg.deviceHeaders)
	d.AddressesExtracted = len(agg.addresses)
	d.PaymentsExtracted = len(agg.payments)
}

// TopPaths returns the most-seen request paths in descending frequency.
func (d *ParseDiagnostics) TopPaths(limit int) []string {
	if d == nil || len(d.PathCounts) == 0 || limit <= 0 {
		return nil
	}
	type pair struct {
		path  string
		count int
	}
	pairs := make([]pair, 0, len(d.PathCounts))
	for path, count := range d.PathCounts {
		pairs = append(pairs, pair{path: path, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count == pairs[j].count {
			return pairs[i].path < pairs[j].path
		}
		return pairs[i].count > pairs[j].count
	})
	if len(pairs) > limit {
		pairs = pairs[:limit]
	}
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, fmt.Sprintf("%dx %s", p.count, p.path))
	}
	return out
}
