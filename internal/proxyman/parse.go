package proxyman

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"one-agent/internal/types"
)

const defaultTokenTTL = 7 * 24 * time.Hour

// ParseHAR extracts an ingest payload from a Proxyman HAR export.
func ParseHAR(r io.Reader, opts ParseOptions) (*IngestPayload, error) {
	payload, _, err := ParseHARWithDiagnostics(r, opts)
	return payload, err
}

// ParseHARWithDiagnostics extracts an ingest payload and returns redacted parse diagnostics.
func ParseHARWithDiagnostics(r io.Reader, opts ParseOptions) (*IngestPayload, *ParseDiagnostics, error) {
	parsedOpts, err := normalizeOptions(opts)
	if err != nil {
		return nil, nil, err
	}
	var file harFile
	if err := json.NewDecoder(r).Decode(&file); err != nil {
		return nil, nil, fmt.Errorf("decode HAR: %w", err)
	}
	agg := newAggregate(parsedOpts)
	diag := newParseDiagnostics()
	for _, entry := range orderedEntries(file.Log.Entries) {
		processEntry(agg, entry, parsedOpts, diag)
	}
	diag.finalize(agg)
	payload, err := finalizePayload(agg, parsedOpts)
	return payload, diag, err
}

func normalizeOptions(opts ParseOptions) (ParseOptions, error) {
	if !isSupportedApp(opts.App) {
		return ParseOptions{}, fmt.Errorf("unsupported app: %q", opts.App)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.DefaultTokenTTL <= 0 {
		opts.DefaultTokenTTL = defaultTokenTTL
	}
	return opts, nil
}

func isSupportedApp(app types.Platform) bool {
	switch app {
	case types.PlatformBlinkit, types.PlatformZepto, types.PlatformInstamart:
		return true
	default:
		return false
	}
}

func newAggregate(opts ParseOptions) *aggregate {
	return &aggregate{
		expiresAt:            opts.TokenExpiresAt,
		deviceHeaders:        make(map[string]string),
		blinkitSearchHeaders: make(map[string]string),
		addresses:            make(map[string]IngestAddress),
		payments:             make(map[string]IngestPayment),
	}
}

func processEntry(agg *aggregate, entry harEntry, opts ParseOptions, diag *ParseDiagnostics) {
	u, err := url.Parse(entry.Request.URL)
	if err != nil {
		return
	}
	route := detectionRoute(u)
	if diag != nil {
		diag.notePath(u.Path)
		if isAddressPath(route) {
			diag.AddressPathEntries++
		}
		if isPaymentPath(route) {
			diag.PaymentPathEntries++
		}
	}
	if consumeRequestHeaders(agg, entry.Request.Headers, opts.App) && diag != nil {
		diag.AuthHeaderEntries++
	}
	if opts.App == types.PlatformBlinkit && isBlinkitSearchPath(route) {
		agg.blinkitSearchHeaders = collectDeviceHeaders(entry.Request.Headers)
	}
	body, err := decodeResponseContent(entry.Response.Content)
	if err != nil || len(body) == 0 {
		return
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return
	}
	if diag != nil {
		diag.JSONResponses++
	}
	if isAuthResponseRoute(route) {
		auth := findAuthFields(decoded, opts.Now())
		if hasAuthFields(auth) && diag != nil {
			diag.AuthFieldResponses++
		}
		mergeAuthFields(agg, auth)
	}
	if isAddressPath(route) {
		consumeAddresses(agg, decoded)
	}
	if isPaymentPath(route) {
		consumePayments(agg, decoded)
	}
}

func decodeResponseContent(content harContent) ([]byte, error) {
	text := strings.TrimSpace(content.Text)
	if text == "" {
		return nil, nil
	}
	if strings.EqualFold(content.Encoding, "base64") {
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, fmt.Errorf("decode base64 content: %w", err)
		}
		return decoded, nil
	}
	return []byte(text), nil
}

func finalizePayload(agg *aggregate, opts ParseOptions) (*IngestPayload, error) {
	if agg.accessToken == "" {
		return nil, fmt.Errorf("access token not found in HAR")
	}
	if agg.refreshToken == "" {
		return nil, fmt.Errorf("refresh token not found in HAR")
	}
	expiresAt := agg.expiresAt
	if expiresAt.IsZero() {
		expiresAt = opts.Now().Add(opts.DefaultTokenTTL)
	}
	deviceHeaders := cloneHeaders(agg.deviceHeaders)
	if opts.App == types.PlatformBlinkit && len(agg.blinkitSearchHeaders) > 0 {
		// Keep Blinkit search headers as one coherent snapshot to avoid mixed-session
		// fingerprints when HAR exports contain interleaved app sessions.
		deviceHeaders = cloneHeaders(agg.blinkitSearchHeaders)
	}
	payload := &IngestPayload{
		App:            string(opts.App),
		AccessToken:    agg.accessToken,
		RefreshToken:   agg.refreshToken,
		TokenExpiresAt: expiresAt.UTC(),
		DeviceHeaders:  deviceHeaders,
		Addresses:      sortedAddresses(agg.addresses),
		Payments:       sortedPayments(agg.payments),
	}
	return payload, nil
}

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sortedAddresses(in map[string]IngestAddress) []IngestAddress {
	keys := sortedKeys(in)
	out := make([]IngestAddress, 0, len(keys))
	for _, k := range keys {
		out = append(out, in[k])
	}
	return out
}

func sortedPayments(in map[string]IngestPayment) []IngestPayment {
	keys := sortedKeys(in)
	out := make([]IngestPayment, 0, len(keys))
	for _, k := range keys {
		out = append(out, in[k])
	}
	return out
}

func sortedKeys[T any](in map[string]T) []string {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
