package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/types"
)

func TestIngestRejectsWrongSecret(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{}, WithIngestSecret("correct-secret"))

	req := httptest.NewRequest(http.MethodPost, "/sessions/ingest", bytes.NewReader(validIngestBody(t, types.PlatformBlinkit)))
	req.Header.Set("X-Ingest-Secret", "wrong-secret")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestIngestRejectsMissingSecret(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{}, WithIngestSecret("correct-secret"))

	req := httptest.NewRequest(http.MethodPost, "/sessions/ingest", bytes.NewReader(validIngestBody(t, types.PlatformBlinkit)))
	// no X-Ingest-Secret header
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestIngestRejectsBadJSON(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{}, WithIngestSecret("correct-secret"))

	req := httptest.NewRequest(http.MethodPost, "/sessions/ingest", bytes.NewReader([]byte("not json")))
	req.Header.Set("X-Ingest-Secret", "correct-secret")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestIngestRejectsUnknownApp(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{}, WithIngestSecret("correct-secret"))

	body := ingestSessionInput{App: "unknown", AccessToken: "a", RefreshToken: "r"}
	req := httptest.NewRequest(http.MethodPost, "/sessions/ingest", bytes.NewReader(mustRaw(t, body)))
	req.Header.Set("X-Ingest-Secret", "correct-secret")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIngestRejectsMissingTokenFields(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{}, WithIngestSecret("correct-secret"))

	body := ingestSessionInput{App: string(types.PlatformBlinkit)}
	req := httptest.NewRequest(http.MethodPost, "/sessions/ingest", bytes.NewReader(mustRaw(t, body)))
	req.Header.Set("X-Ingest-Secret", "correct-secret")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIngestRateLimited(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(
		st,
		map[types.Platform]platforms.Platform{},
		WithIngestSecret("correct-secret"),
		WithIngestRateLimit(1, time.Minute),
	)
	first := httptest.NewRequest(http.MethodPost, "/sessions/ingest", bytes.NewReader(validIngestBody(t, types.PlatformBlinkit)))
	first.Header.Set("X-Ingest-Secret", "correct-secret")
	first.RemoteAddr = "203.0.113.10:4000"
	w1 := httptest.NewRecorder()
	server.ServeHTTP(w1, first)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", w1.Code)
	}
	second := httptest.NewRequest(http.MethodPost, "/sessions/ingest", bytes.NewReader(validIngestBody(t, types.PlatformBlinkit)))
	second.Header.Set("X-Ingest-Secret", "correct-secret")
	second.RemoteAddr = "203.0.113.10:4001"
	w2 := httptest.NewRecorder()
	server.ServeHTTP(w2, second)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request expected 429, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestIngestSuccess(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{}, WithIngestSecret("correct-secret"))

	req := httptest.NewRequest(http.MethodPost, "/sessions/ingest", bytes.NewReader(validIngestBody(t, types.PlatformBlinkit)))
	req.Header.Set("X-Ingest-Secret", "correct-secret")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	session, err := st.Get(context.Background(), types.PlatformBlinkit)
	if err != nil {
		t.Fatalf("store.Get after ingest: %v", err)
	}
	if session.AccessToken != "test-access" {
		t.Errorf("AccessToken: got %q", session.AccessToken)
	}
	if len(session.Addresses) != 1 || session.Addresses[0].ID != "addr-1" {
		t.Errorf("Addresses: got %+v", session.Addresses)
	}
	if len(session.Payments) != 1 || session.Payments[0].ID != "pay-1" {
		t.Errorf("Payments: got %+v", session.Payments)
	}
}

func TestIngestPreservesExistingEntitiesWhenIncomingEmpty(t *testing.T) {
	st := newTestStore(t)
	existing := existingBlinkitSession()
	if err := st.Set(context.Background(), types.PlatformBlinkit, existing); err != nil {
		t.Fatalf("seed existing session: %v", err)
	}
	server := newServerForTests(st, map[types.Platform]platforms.Platform{}, WithIngestSecret("correct-secret"))
	body := ingestSessionInput{
		App:            string(types.PlatformBlinkit),
		AccessToken:    "new-access",
		RefreshToken:   "new-refresh",
		TokenExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	req := httptest.NewRequest(http.MethodPost, "/sessions/ingest", bytes.NewReader(mustRaw(t, body)))
	req.Header.Set("X-Ingest-Secret", "correct-secret")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	saved, err := st.Get(context.Background(), types.PlatformBlinkit)
	if err != nil {
		t.Fatalf("store.Get after ingest: %v", err)
	}
	if saved.AccessToken != "new-access" || saved.RefreshToken != "new-refresh" {
		t.Fatalf("tokens not updated: %+v", saved)
	}
	assertPreservedSessionData(t, saved)
}

func existingBlinkitSession() *types.AppSession {
	return &types.AppSession{
		App:          types.PlatformBlinkit,
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		DeviceHeaders: map[string]string{
			"device_id": "dev-1",
		},
		Addresses: []types.Address{
			{ID: "addr-1", Label: "Home", Line1: "Baner, Pune"},
		},
		Payments: []types.PaymentMethod{
			{ID: "pay-1", Label: "Visa 4242", Token: "tok-1", Type: "card"},
		},
		CapturedAt: time.Now().Add(-time.Hour),
		UpdatedAt:  time.Now().Add(-time.Hour),
	}
}

func assertPreservedSessionData(t *testing.T, saved *types.AppSession) {
	t.Helper()
	if len(saved.Addresses) != 1 || saved.Addresses[0].ID != "addr-1" {
		t.Fatalf("addresses not preserved: %+v", saved.Addresses)
	}
	if len(saved.Payments) != 1 || saved.Payments[0].ID != "pay-1" {
		t.Fatalf("payments not preserved: %+v", saved.Payments)
	}
	if saved.DeviceHeaders["device_id"] != "dev-1" {
		t.Fatalf("device headers not preserved: %+v", saved.DeviceHeaders)
	}
}

func validIngestBody(t *testing.T, app types.Platform) []byte {
	t.Helper()
	body := ingestSessionInput{
		App:            string(app),
		AccessToken:    "test-access",
		RefreshToken:   "test-refresh",
		TokenExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		DeviceHeaders:  map[string]string{"x-device-id": "dev-123"},
		Addresses:      []ingestAddress{{ID: "addr-1", Label: "Home", FullAddress: "Baner, Pune"}},
		Payments:       []ingestPayment{{ID: "pay-1", Label: "Visa •••• 4242", Token: "tok-xxx", Type: "card"}},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}
