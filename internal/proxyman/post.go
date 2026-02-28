package proxyman

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PostOptions configures the ingest POST request.
type PostOptions struct {
	URL    string
	Secret string
	Client *http.Client
}

// Summary is a redacted view of an extracted payload.
type Summary struct {
	App             string
	HasAccessToken  bool
	HasRefreshToken bool
	TokenExpiresAt  time.Time
	DeviceHeaders   int
	Addresses       int
	Payments        int
}

// Summarize returns a redacted payload summary safe for console output.
func Summarize(p *IngestPayload) Summary {
	return Summary{
		App:             p.App,
		HasAccessToken:  p.AccessToken != "",
		HasRefreshToken: p.RefreshToken != "",
		TokenExpiresAt:  p.TokenExpiresAt,
		DeviceHeaders:   len(p.DeviceHeaders),
		Addresses:       len(p.Addresses),
		Payments:        len(p.Payments),
	}
}

// PostIngest sends the extracted payload to POST /sessions/ingest.
func PostIngest(ctx context.Context, payload *IngestPayload, opts PostOptions) error {
	if payload == nil {
		return fmt.Errorf("payload is nil")
	}
	if strings.TrimSpace(opts.URL) == "" {
		return fmt.Errorf("ingest URL is required")
	}
	if strings.TrimSpace(opts.Secret) == "" {
		return fmt.Errorf("ingest secret is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ingest-Secret", opts.Secret)
	resp, err := httpClient(opts.Client).Do(req)
	if err != nil {
		return fmt.Errorf("post ingest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	msg := readResponseSnippet(resp.Body)
	return fmt.Errorf("ingest failed: status %d %s", resp.StatusCode, msg)
}

func httpClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func readResponseSnippet(r io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(r, 512))
	if err != nil {
		return "(unable to read response body)"
	}
	text := strings.TrimSpace(string(b))
	if text == "" {
		return "(empty response body)"
	}
	return text
}
