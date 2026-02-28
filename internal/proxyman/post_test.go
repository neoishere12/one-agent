package proxyman

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPostIngestSendsSecretAndJSON(t *testing.T) {
	var seenSecret string
	var seenBody string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seenSecret = req.Header.Get("X-Ingest-Secret")
		b, _ := io.ReadAll(req.Body)
		seenBody = string(b)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
			Header:     make(http.Header),
		}, nil
	})}
	payload := &IngestPayload{
		App:            "blinkit",
		AccessToken:    "a",
		RefreshToken:   "r",
		TokenExpiresAt: time.Date(2026, 2, 27, 0, 0, 0, 0, time.UTC),
	}
	if err := PostIngest(context.Background(), payload, PostOptions{
		URL:    "http://example.test/sessions/ingest",
		Secret: "secret-1",
		Client: client,
	}); err != nil {
		t.Fatalf("PostIngest: %v", err)
	}
	if seenSecret != "secret-1" {
		t.Fatalf("X-Ingest-Secret: got %q", seenSecret)
	}
	if !strings.Contains(seenBody, `"app":"blinkit"`) {
		t.Fatalf("body missing app field: %s", seenBody)
	}
}

func TestPostIngestReturnsStatusError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader(`{"error":"unauthorized"}`)),
			Header:     make(http.Header),
		}, nil
	})}
	err := PostIngest(context.Background(), &IngestPayload{App: "blinkit"}, PostOptions{
		URL:    "http://example.test/sessions/ingest",
		Secret: "s",
		Client: client,
	})
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
