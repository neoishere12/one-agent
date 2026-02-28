package main

import (
	"fmt"
	"time"

	"one-agent/internal/proxyman"
)

func printSummary(s proxyman.Summary, dryRun bool) {
	mode := "summary"
	if dryRun {
		mode = "dry-run"
	}
	fmt.Printf(
		"proxyman-watch %s\napp=%s has_access=%t has_refresh=%t expires_at=%s device_headers=%d addresses=%d payments=%d\n",
		mode,
		s.App,
		s.HasAccessToken,
		s.HasRefreshToken,
		s.TokenExpiresAt.Format(time.RFC3339),
		s.DeviceHeaders,
		s.Addresses,
		s.Payments,
	)
}

func printDiagnostics(d proxyman.ParseDiagnostics) {
	fmt.Printf(
		"diagnostics entries=%d json=%d auth_header_entries=%d auth_field_responses=%d address_path_entries=%d payment_path_entries=%d device_headers=%d addresses=%d payments=%d\n",
		d.TotalEntries,
		d.JSONResponses,
		d.AuthHeaderEntries,
		d.AuthFieldResponses,
		d.AddressPathEntries,
		d.PaymentPathEntries,
		d.DeviceHeadersCollected,
		d.AddressesExtracted,
		d.PaymentsExtracted,
	)
	top := d.TopPaths(10)
	if len(top) == 0 {
		return
	}
	fmt.Println("top_paths:")
	for _, line := range top {
		fmt.Printf("  %s\n", line)
	}
}
