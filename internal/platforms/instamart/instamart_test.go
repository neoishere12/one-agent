package instamart_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"one-agent/internal/config"
	"one-agent/internal/platforms/instamart"
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
	sess := &types.AppSession{
		App:          types.PlatformInstamart,
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		DeviceHeaders: map[string]string{
			"x-device-id":   "dev-003",
			"x-app-version": "4.0.0",
		},
		CapturedAt: time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := s.Set(context.Background(), types.PlatformInstamart, sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}

func newMockServer(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/instamart/v2/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mustJSON(t, map[string]any{
			"products": []map[string]any{
				{"id": "i1", "name": "Amul Lassi", "brand": "Amul",
					"price": 29.0, "mrp": 30.0, "unit": "200ml", "in_stock": true},
			},
		})))
	})
	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := instamart.New(s, instamart.WithBaseURL(srv.URL))

	products, err := client.Search(context.Background(), "lassi", 18.52, 73.85)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if products[0].Platform != types.PlatformInstamart {
		t.Errorf("Platform: got %q", products[0].Platform)
	}
}

func TestAddToCart(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/instamart/v1/cart", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cart_id":"icart-001"}`))
	})
	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := instamart.New(s, instamart.WithBaseURL(srv.URL))

	cartID, err := client.AddToCart(context.Background(), "iprod-001", 1)
	if err != nil {
		t.Fatalf("AddToCart: %v", err)
	}
	if cartID != "icart-001" {
		t.Errorf("CartID: got %q", cartID)
	}
}

func TestCheckout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/instamart/v1/checkout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"checkout_id":"ico-001","delivery_fee":30,"eta_minutes":15}`))
	})
	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := instamart.New(s, instamart.WithBaseURL(srv.URL))

	result, err := client.Checkout(context.Background(), "icart-001", "iaddr-001")
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if result.CheckoutID != "ico-001" {
		t.Errorf("CheckoutID: got %q", result.CheckoutID)
	}
	if result.DeliveryFee != 30 {
		t.Errorf("DeliveryFee: got %d", result.DeliveryFee)
	}
}

func TestPay(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/instamart/v1/checkout/confirm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"order_id":"INST-001","status":"confirmed","eta_minutes":15,"total":59.0}`))
	})
	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := instamart.New(s, instamart.WithBaseURL(srv.URL))

	order, err := client.Pay(context.Background(), "ico-001", "ipay-token")
	if err != nil {
		t.Fatalf("Pay: %v", err)
	}
	if order.ID != "INST-001" {
		t.Errorf("OrderID: got %q", order.ID)
	}
	if order.Platform != types.PlatformInstamart {
		t.Errorf("Platform: got %q", order.Platform)
	}
}

func TestRefreshToken(t *testing.T) {
	expiry := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/v2/refresh", func(w http.ResponseWriter, r *http.Request) {
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
	client := instamart.New(s, instamart.WithBaseURL(srv.URL))

	newAccess, _, _, err := client.RefreshToken(context.Background(), "test-refresh-token")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if newAccess != "new-access" {
		t.Errorf("newAccess: got %q", newAccess)
	}
}

func TestOrderStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/instamart/v1/orders/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"out_for_delivery","eta_minutes":3}`))
	})
	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := instamart.New(s, instamart.WithBaseURL(srv.URL))

	status, eta, err := client.OrderStatus(context.Background(), "INST-001")
	if err != nil {
		t.Fatalf("OrderStatus: %v", err)
	}
	if status != "out_for_delivery" {
		t.Errorf("Status: got %q", status)
	}
	if eta != 3 {
		t.Errorf("ETA: got %d", eta)
	}
}
