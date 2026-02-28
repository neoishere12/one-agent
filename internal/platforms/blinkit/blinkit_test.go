package blinkit_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"one-agent/internal/config"
	"one-agent/internal/platforms"
	"one-agent/internal/platforms/blinkit"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

const testKey = "0000000000000000000000000000000000000000000000000000000000000001"

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("STORE_MASTER_KEY", testKey)
	cfg := config.Load()
	cfg.DBPath = filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedSession(t *testing.T, s *store.Store) {
	t.Helper()
	// Ensure unit tests run in API mode even if host env is set to browser mode.
	t.Setenv("BLINKIT_SEARCH_MODE", "")
	sess := &types.AppSession{
		App:          types.PlatformBlinkit,
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		DeviceHeaders: map[string]string{
			"x-device-id":   "dev-001",
			"x-app-version": "18.0.0",
		},
		CapturedAt: time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := s.Set(context.Background(), types.PlatformBlinkit, sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

// newMockServer returns an httptest.Server routing paths to JSON handlers.
func newMockServer(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}

func assertSearchHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("lat"); got != "18.52" {
		t.Fatalf("lat: got %q", got)
	}
	if got := r.Header.Get("lon"); got != "73.85" {
		t.Fatalf("lon: got %q", got)
	}
	if got := r.Header.Get("cur_lat"); got != "18.52" {
		t.Fatalf("cur_lat: got %q", got)
	}
	if got := r.Header.Get("cur_lon"); got != "73.85" {
		t.Fatalf("cur_lon: got %q", got)
	}
}

func TestSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/layout/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		assertSearchHeaders(t, r)
		if r.Header.Get("Authorization") == "" && r.Header.Get("access_token") == "" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mustJSON(t, map[string]any{
			"products": []map[string]any{
				{"id": "p1", "name": "Amul Lassi", "brand": "Amul", "price": 28.0,
					"mrp": 30.0, "unit": "200ml", "store_id": "s1", "in_stock": true},
			},
		})))
	})

	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := blinkit.New(s, blinkit.WithBaseURL(srv.URL))

	products, err := client.Search(context.Background(), "lassi", 18.52, 73.85)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if products[0].Name != "Amul Lassi" {
		t.Errorf("Name: got %q", products[0].Name)
	}
	if products[0].Platform != types.PlatformBlinkit {
		t.Errorf("Platform: got %q", products[0].Platform)
	}
}

func TestAddToCart(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/cart/items", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cart_id":"cart-abc"}`))
	})

	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := blinkit.New(s, blinkit.WithBaseURL(srv.URL))

	cartID, err := client.AddToCart(context.Background(), "prod-001", 1)
	if err != nil {
		t.Fatalf("AddToCart: %v", err)
	}
	if cartID != "cart-abc" {
		t.Errorf("CartID: got %q, want %q", cartID, "cart-abc")
	}
}

func TestCheckout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/checkout/init", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"checkout_id":"co-001","delivery_fee":25,"eta_minutes":12}`))
	})

	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := blinkit.New(s, blinkit.WithBaseURL(srv.URL))

	result, err := client.Checkout(context.Background(), "cart-abc", "addr-001")
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if result.CheckoutID != "co-001" {
		t.Errorf("CheckoutID: got %q", result.CheckoutID)
	}
	if result.DeliveryFee != 25 {
		t.Errorf("DeliveryFee: got %d", result.DeliveryFee)
	}
}

func TestPay(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/checkout/confirm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"order_id":"BLK-001","status":"confirmed","eta_minutes":10,"total":53.0}`))
	})

	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := blinkit.New(s, blinkit.WithBaseURL(srv.URL))

	order, err := client.Pay(context.Background(), "co-001", "pay-token")
	if err != nil {
		t.Fatalf("Pay: %v", err)
	}
	if order.ID != "BLK-001" {
		t.Errorf("OrderID: got %q", order.ID)
	}
	if order.Platform != types.PlatformBlinkit {
		t.Errorf("Platform: got %q", order.Platform)
	}
}

func TestPayNetworkError(t *testing.T) {
	// Close the server immediately — simulates a network error during Pay.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // closed before request

	s := newTestStore(t)
	seedSession(t, s)
	client := blinkit.New(s, blinkit.WithBaseURL(srv.URL))

	_, err := client.Pay(context.Background(), "co-001", "pay-token")
	if err == nil {
		t.Error("expected error on network failure, got nil")
	}
	// Confirm the error is not wrapped as ErrPaymentFailed (it's a network error)
	if strings.Contains(err.Error(), platforms.ErrPaymentFailed.Error()) {
		t.Errorf("network error misclassified as payment failure: %v", err)
	}
}

func TestRefreshToken(t *testing.T) {
	expiry := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mustJSON(t, map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_at":    expiry.Format(time.RFC3339),
		})))
	})

	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := blinkit.New(s, blinkit.WithBaseURL(srv.URL))

	newAccess, newRefresh, expiresAt, err := client.RefreshToken(context.Background(), "test-refresh-token")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if newAccess != "new-access" {
		t.Errorf("newAccess: got %q", newAccess)
	}
	if newRefresh != "new-refresh" {
		t.Errorf("newRefresh: got %q", newRefresh)
	}
	if !expiresAt.Equal(expiry) {
		t.Errorf("expiresAt: got %v, want %v", expiresAt, expiry)
	}
}

func TestOrderStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/orders/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"out_for_delivery","eta_minutes":5}`))
	})

	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := blinkit.New(s, blinkit.WithBaseURL(srv.URL))

	status, eta, err := client.OrderStatus(context.Background(), "BLK-001")
	if err != nil {
		t.Fatalf("OrderStatus: %v", err)
	}
	if status != "out_for_delivery" {
		t.Errorf("Status: got %q", status)
	}
	if eta != 5 {
		t.Errorf("ETA: got %d", eta)
	}
}
