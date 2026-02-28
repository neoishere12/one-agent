package proxyman

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"one-agent/internal/types"
)

func TestParseHARExtractsPayload(t *testing.T) {
	now := time.Date(2026, 2, 26, 20, 0, 0, 0, time.UTC)
	payload, err := ParseHAR(bytes.NewReader(mustHAR(t, []harEntry{
		harFixtureEntry("https://api.example.com/v2/addresses", []harHeader{
			{Name: "Authorization", Value: "Bearer access-1"},
			{Name: "X-Device-Id", Value: "dev-123"},
			{Name: "User-Agent", Value: "ProxymanTest/1.0"},
		}, map[string]any{
			"addresses": []any{map[string]any{"id": "addr-1", "label": "Home", "full_address": "Baner, Pune"}},
		}),
		harFixtureEntry("https://api.example.com/v2/payment-methods", nil, map[string]any{
			"payment_tokens": []any{map[string]any{"id": "pay-1", "label": "Visa 4242", "token": "tok-1", "type": "card"}},
		}),
		harFixtureEntry("https://api.example.com/v2/auth/refresh", nil, map[string]any{
			"access_token":  "access-2",
			"refresh_token": "refresh-2",
			"expires_in":    3600,
		}),
	})), ParseOptions{App: types.PlatformBlinkit, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("ParseHAR: %v", err)
	}
	if payload.App != "blinkit" {
		t.Fatalf("App: got %q", payload.App)
	}
	if payload.AccessToken != "access-2" {
		t.Fatalf("AccessToken: got %q", payload.AccessToken)
	}
	if payload.RefreshToken != "refresh-2" {
		t.Fatalf("RefreshToken: got %q", payload.RefreshToken)
	}
	if got, want := payload.TokenExpiresAt, now.Add(time.Hour); !got.Equal(want) {
		t.Fatalf("TokenExpiresAt: got %s want %s", got, want)
	}
	if payload.DeviceHeaders["x-device-id"] != "dev-123" {
		t.Fatalf("device headers missing x-device-id: %+v", payload.DeviceHeaders)
	}
	if len(payload.Addresses) != 1 || payload.Addresses[0].ID != "addr-1" {
		t.Fatalf("Addresses: got %+v", payload.Addresses)
	}
	if len(payload.Payments) != 1 || payload.Payments[0].Token != "tok-1" {
		t.Fatalf("Payments: got %+v", payload.Payments)
	}
}

func TestParseHARFallsBackToDefaultTTL(t *testing.T) {
	now := time.Date(2026, 2, 26, 20, 0, 0, 0, time.UTC)
	payload, err := ParseHAR(bytes.NewReader(mustHAR(t, []harEntry{
		harFixtureEntry("https://api.example.com/v2/auth/refresh", []harHeader{{Name: "Authorization", Value: "Bearer access-1"}}, map[string]any{
			"refresh_token": "refresh-1",
		}),
	})), ParseOptions{
		App:             types.PlatformZepto,
		Now:             func() time.Time { return now },
		DefaultTokenTTL: 2 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ParseHAR: %v", err)
	}
	if got, want := payload.TokenExpiresAt, now.Add(2*time.Hour); !got.Equal(want) {
		t.Fatalf("fallback expiry: got %s want %s", got, want)
	}
}

func TestParseHARErrorsWithoutRefreshToken(t *testing.T) {
	_, err := ParseHAR(bytes.NewReader(mustHAR(t, []harEntry{
		harFixtureEntry("https://api.example.com/v2/addresses", []harHeader{{Name: "Authorization", Value: "Bearer access-1"}}, map[string]any{"ok": true}),
	})), ParseOptions{App: types.PlatformInstamart})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseHARUsesBlinkitHeaderTokenFallback(t *testing.T) {
	now := time.Date(2026, 2, 26, 20, 0, 0, 0, time.UTC)
	payload, err := ParseHAR(bytes.NewReader(mustHAR(t, []harEntry{
		harFixtureEntry("https://api2.grofers.com/v1/aerobar", []harHeader{
			{Name: "access_token", Value: "access-header"},
			{Name: "auth_key", Value: "refresh-header"},
			{Name: "device_id", Value: "dev-123"},
			{Name: "User-Agent", Value: "blinkit/1.0"},
		}, map[string]any{"ok": true}),
	})), ParseOptions{
		App:             types.PlatformBlinkit,
		Now:             func() time.Time { return now },
		DefaultTokenTTL: 90 * time.Minute,
	})
	if err != nil {
		t.Fatalf("ParseHAR: %v", err)
	}
	if payload.AccessToken != "access-header" || payload.RefreshToken != "refresh-header" {
		t.Fatalf("header fallback tokens not parsed: access=%q refresh=%q", payload.AccessToken, payload.RefreshToken)
	}
	if got, want := payload.TokenExpiresAt, now.Add(90*time.Minute); !got.Equal(want) {
		t.Fatalf("TokenExpiresAt: got %s want %s", got, want)
	}
}

func TestParseHARWithDiagnostics(t *testing.T) {
	now := time.Date(2026, 2, 26, 20, 0, 0, 0, time.UTC)
	_, diag, err := ParseHARWithDiagnostics(bytes.NewReader(mustHAR(t, []harEntry{
		harFixtureEntry("https://api.example.com/v2/addresses", []harHeader{
			{Name: "Authorization", Value: "Bearer access-1"},
			{Name: "X-Device-Id", Value: "dev-123"},
			{Name: "User-Agent", Value: "ProxymanTest/1.0"},
		}, map[string]any{
			"addresses": []any{map[string]any{"id": "addr-1", "label": "Home", "full_address": "Baner, Pune"}},
		}),
		harFixtureEntry("https://api.example.com/v2/payment-methods", nil, map[string]any{
			"payment_tokens": []any{map[string]any{"id": "pay-1", "label": "Visa 4242", "token": "tok-1", "type": "card"}},
		}),
		harFixtureEntry("https://api.example.com/v2/auth/refresh", nil, map[string]any{
			"access_token":  "access-2",
			"refresh_token": "refresh-2",
			"expires_in":    3600,
		}),
	})), ParseOptions{App: types.PlatformBlinkit, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("ParseHARWithDiagnostics: %v", err)
	}
	if diag == nil {
		t.Fatal("diagnostics is nil")
	}
	assertParseDiagnostics(t, diag)
}

func TestParseHARExtractsAddressFromQueryHintPath(t *testing.T) {
	payload, diag, err := ParseHARWithDiagnostics(bytes.NewReader(mustHAR(t, []harEntry{
		harFixtureEntry(
			"https://api2.grofers.com/api/v1/config/primary?fetch_nearest_addresses=true",
			[]harHeader{
				{Name: "access_token", Value: "access-1"},
				{Name: "auth_key", Value: "refresh-1"},
			},
			map[string]any{
				"location": map[string]any{
					"nearest_address": map[string]any{
						"id":              207580381,
						"label":           "Home",
						"display_address": "Pune, Maharashtra",
					},
				},
			},
		),
	})), ParseOptions{App: types.PlatformBlinkit})
	if err != nil {
		t.Fatalf("ParseHARWithDiagnostics: %v", err)
	}
	if len(payload.Addresses) != 1 || payload.Addresses[0].ID != "207580381" {
		t.Fatalf("Addresses: got %+v", payload.Addresses)
	}
	if diag == nil || diag.AddressPathEntries != 1 {
		t.Fatalf("AddressPathEntries: got %+v", diag)
	}
}

func TestParseHARIgnoresNonAuthBodyTokens(t *testing.T) {
	now := time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
	payload, diag, err := ParseHARWithDiagnostics(bytes.NewReader(mustHAR(t, []harEntry{
		harFixtureEntry(
			"https://api2.grofers.com/v1/layout/search",
			[]harHeader{
				{Name: "access_token", Value: "access-header"},
				{Name: "auth_key", Value: "refresh-header"},
			},
			map[string]any{
				"access_token": "payment-scoped-token",
				"expires_at":   1730000000,
			},
		),
		harFixtureEntry(
			"https://api2.grofers.com/v3/payments/zomato/generate_access_token/",
			nil,
			map[string]any{
				"access_token": "payment-token-2",
				"expires_in":   300,
			},
		),
	})), ParseOptions{
		App:             types.PlatformBlinkit,
		Now:             func() time.Time { return now },
		DefaultTokenTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ParseHARWithDiagnostics: %v", err)
	}
	if payload.AccessToken != "access-header" {
		t.Fatalf("AccessToken should come from request header, got %q", payload.AccessToken)
	}
	if payload.RefreshToken != "refresh-header" {
		t.Fatalf("RefreshToken should come from request header fallback, got %q", payload.RefreshToken)
	}
	if got, want := payload.TokenExpiresAt, now.Add(24*time.Hour); !got.Equal(want) {
		t.Fatalf("TokenExpiresAt should use default TTL, got %s want %s", got, want)
	}
	if diag == nil {
		t.Fatal("diagnostics is nil")
	}
	if diag.AuthFieldResponses != 0 {
		t.Fatalf("AuthFieldResponses: got %d want 0", diag.AuthFieldResponses)
	}
}

func assertParseDiagnostics(t *testing.T, diag *ParseDiagnostics) {
	t.Helper()
	mustEqual(t, "TotalEntries", diag.TotalEntries, 3)
	mustEqual(t, "JSONResponses", diag.JSONResponses, 3)
	mustEqual(t, "AuthHeaderEntries", diag.AuthHeaderEntries, 1)
	mustEqual(t, "AuthFieldResponses", diag.AuthFieldResponses, 1)
	mustEqual(t, "AddressPathEntries", diag.AddressPathEntries, 1)
	mustEqual(t, "PaymentPathEntries", diag.PaymentPathEntries, 1)
	mustEqual(t, "DeviceHeadersCollected", diag.DeviceHeadersCollected, 2)
	mustEqual(t, "AddressesExtracted", diag.AddressesExtracted, 1)
	mustEqual(t, "PaymentsExtracted", diag.PaymentsExtracted, 1)
	top := diag.TopPaths(2)
	if len(top) != 2 {
		t.Fatalf("TopPaths: got %d entries", len(top))
	}
	if top[0] == "" || top[1] == "" {
		t.Fatalf("TopPaths contains empty values: %+v", top)
	}
}

func mustEqual(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %d want %d", name, got, want)
	}
}

func harFixtureEntry(url string, headers []harHeader, body any) harEntry {
	text, _ := json.Marshal(body)
	return harEntry{
		Request: harRequest{Method: "GET", URL: url, Headers: headers},
		Response: harResponse{Status: 200, Content: harContent{
			Text:     string(text),
			MimeType: "application/json",
		}},
	}
}

func mustHAR(t *testing.T, entries []harEntry) []byte {
	t.Helper()
	b, err := json.Marshal(harFile{Log: harLog{Entries: entries}})
	if err != nil {
		t.Fatalf("json.Marshal HAR: %v", err)
	}
	return b
}
