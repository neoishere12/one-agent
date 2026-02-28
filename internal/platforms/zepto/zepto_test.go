package zepto_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"one-agent/internal/config"
	"one-agent/internal/platforms/zepto"
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
		App:          types.PlatformZepto,
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		DeviceHeaders: map[string]string{
			"x-device-id":   "dev-002",
			"x-app-version": "12.0.0",
		},
		CapturedAt: time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := s.Set(context.Background(), types.PlatformZepto, sess); err != nil {
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
	mux.HandleFunc("/v3/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mustJSON(t, map[string]any{
			"products": []map[string]any{
				{"id": "z1", "name": "Amul Lassi", "brand": "Amul",
					"price": 26.0, "mrp": 30.0, "unit": "200ml", "in_stock": true},
			},
		})))
	})
	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := zepto.New(s, zepto.WithBaseURL(srv.URL))

	products, err := client.Search(context.Background(), "lassi", 18.52, 73.85)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if products[0].Platform != types.PlatformZepto {
		t.Errorf("Platform: got %q", products[0].Platform)
	}
	if products[0].PriceRupees != 26.0 {
		t.Errorf("Price: got %v", products[0].PriceRupees)
	}
}

func TestAddToCart(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/cart/add", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cart_id":"zcart-001"}`))
	})
	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := zepto.New(s, zepto.WithBaseURL(srv.URL))

	cartID, err := client.AddToCart(context.Background(), "zprod-001", 2)
	if err != nil {
		t.Fatalf("AddToCart: %v", err)
	}
	if cartID != "zcart-001" {
		t.Errorf("CartID: got %q", cartID)
	}
}

func TestCheckout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/checkout/init", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"checkout_id":"zco-001","delivery_fee":0,"eta_minutes":10}`))
	})
	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := zepto.New(s, zepto.WithBaseURL(srv.URL))

	result, err := client.Checkout(context.Background(), "zcart-001", "zaddr-001")
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if result.CheckoutID != "zco-001" {
		t.Errorf("CheckoutID: got %q", result.CheckoutID)
	}
	if result.ETAMinutes != 10 {
		t.Errorf("ETAMinutes: got %d", result.ETAMinutes)
	}
}

func TestPay(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/checkout/confirm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"order_id":"ZPT-001","status":"confirmed","eta_minutes":10,"total":26.0}`))
	})
	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := zepto.New(s, zepto.WithBaseURL(srv.URL))

	order, err := client.Pay(context.Background(), "zco-001", "zpay-token")
	if err != nil {
		t.Fatalf("Pay: %v", err)
	}
	if order.ID != "ZPT-001" {
		t.Errorf("OrderID: got %q", order.ID)
	}
	if order.Platform != types.PlatformZepto {
		t.Errorf("Platform: got %q", order.Platform)
	}
}

func TestRefreshToken(t *testing.T) {
	expiry := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
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
	client := zepto.New(s, zepto.WithBaseURL(srv.URL))

	newAccess, _, expiresAt, err := client.RefreshToken(context.Background(), "test-refresh-token")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if newAccess != "new-access" {
		t.Errorf("newAccess: got %q", newAccess)
	}
	if !expiresAt.Equal(expiry) {
		t.Errorf("expiresAt: got %v, want %v", expiresAt, expiry)
	}
}

func TestOrderStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/orders/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"preparing","eta_minutes":8}`))
	})
	srv := newMockServer(t, mux)
	s := newTestStore(t)
	seedSession(t, s)
	client := zepto.New(s, zepto.WithBaseURL(srv.URL))

	status, eta, err := client.OrderStatus(context.Background(), "ZPT-001")
	if err != nil {
		t.Fatalf("OrderStatus: %v", err)
	}
	if status != "preparing" {
		t.Errorf("Status: got %q", status)
	}
	if eta != 8 {
		t.Errorf("ETA: got %d", eta)
	}
}
