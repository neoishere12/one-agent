package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"one-agent/internal/config"
	"one-agent/internal/platforms"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

const testKey = "0000000000000000000000000000000000000000000000000000000000000001"

type mockPlatform struct {
	searchProducts []types.Product
	searchErr      error
	addCartID      string
	addErr         error
	checkoutResult platforms.CheckoutResult
	checkoutErr    error
	payOrder       types.Order
	payErr         error
	lastPayToken   string
	newAccess      string
	newRefresh     string
	newExpiry      time.Time
	refreshErr     error
	status         string
	eta            int
	statusErr      error
}

type mockBrowserPlatform struct {
	mockPlatform
	browserOrder        types.Order
	browserOrderErr     error
	browserOrderCalls   int
	hydratedSession     *types.AppSession
	hydratedSessionErr  error
	hydratedSessionCall int
}

func (m *mockPlatform) Search(_ context.Context, _ string, _, _ float64) ([]types.Product, error) {
	return m.searchProducts, m.searchErr
}

func (m *mockPlatform) AddToCart(_ context.Context, _ string, _ int) (string, error) {
	return m.addCartID, m.addErr
}

func (m *mockPlatform) Checkout(_ context.Context, _, _ string) (platforms.CheckoutResult, error) {
	return m.checkoutResult, m.checkoutErr
}

func (m *mockPlatform) Pay(_ context.Context, _ string, paymentToken string) (types.Order, error) {
	m.lastPayToken = paymentToken
	return m.payOrder, m.payErr
}

func (m *mockPlatform) RefreshToken(_ context.Context, _ string) (string, string, time.Time, error) {
	return m.newAccess, m.newRefresh, m.newExpiry, m.refreshErr
}

func (m *mockPlatform) OrderStatus(_ context.Context, _ string) (string, int, error) {
	return m.status, m.eta, m.statusErr
}

func (m *mockBrowserPlatform) PlaceOrderViaBrowser(_ context.Context, _ string, _ string, _ string, _ int) (types.Order, error) {
	m.browserOrderCalls++
	return m.browserOrder, m.browserOrderErr
}

func (m *mockBrowserPlatform) HydrateBrowserSessionMetadata(_ context.Context) (*types.AppSession, error) {
	m.hydratedSessionCall++
	return m.hydratedSession, m.hydratedSessionErr
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("STORE_MASTER_KEY", testKey)
	cfg := config.Load()
	cfg.DBPath = filepath.Join(t.TempDir(), "mcp.db")
	st, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func defaultSession(app types.Platform) *types.AppSession {
	return &types.AppSession{
		App:          app,
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		DeviceHeaders: map[string]string{
			"x-device-id": "d-1",
		},
		Addresses: []types.Address{
			{
				ID: "addr-home", Label: "Home", Line1: "Baner Road", City: "Pune", PinCode: "411045",
			},
		},
		Payments: []types.PaymentMethod{
			{
				ID: "pay-card", Label: "Visa **** 4242", Token: "token-4242", Type: "card",
			},
		},
		CapturedAt: time.Now().Add(-time.Hour),
		UpdatedAt:  time.Now().Add(-time.Hour),
	}
}

func seedSession(t *testing.T, st *store.Store, session *types.AppSession) {
	t.Helper()
	if err := st.Set(context.Background(), session.App, session); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func newServerForTests(st *store.Store, clients map[types.Platform]platforms.Platform, opts ...Option) *Server {
	base := []Option{withNow(func() time.Time { return time.Date(2026, 2, 26, 12, 0, 0, 0, time.UTC) })}
	return New(st, clients, append(base, opts...)...)
}

func mustRaw(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}
