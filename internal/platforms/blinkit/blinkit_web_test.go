package blinkit_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"one-agent/internal/platforms/blinkit"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

func seedWebSession(t *testing.T, s *store.Store) {
	t.Helper()
	sess := &types.AppSession{
		App:          types.PlatformBlinkit,
		AccessToken:  "web-access-token",
		RefreshToken: "web-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		DeviceHeaders: map[string]string{
			"app_client":      "consumer_web",
			"platform":        "mobile_web",
			"web_app_version": "1008010016",
			"user-agent":      "Mozilla/5.0",
		},
		CapturedAt: time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := s.Set(context.Background(), types.PlatformBlinkit, sess); err != nil {
		t.Fatalf("seed web session: %v", err)
	}
}

func TestSearchWebSessionDoesNotInjectReqKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/layout/search", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("req_key"); got != "" {
			t.Fatalf("req_key should not be injected for web sessions, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"products":[]}`))
	})
	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedWebSession(t, s)
	client := blinkit.New(s, blinkit.WithBaseURL(srv.URL))
	if _, err := client.Search(context.Background(), "amul lassi", 18.52, 73.85); err != nil {
		t.Fatalf("Search(web): %v", err)
	}
}
