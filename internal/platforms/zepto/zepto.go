// Package zepto implements platforms.Platform for Zepto.
//
// Endpoint paths are PENDING CAPTURE — all marked with that comment.
// Update from live traffic after running capture_session("zepto") (PLATFORMS.md).
package zepto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

const (
	DefaultBaseURL = "https://api.zepto.com" // PENDING CAPTURE: verify from live traffic

	// Endpoint paths — PENDING CAPTURE: verify all from live traffic (PLATFORMS.md)
	pathSearch   = "/v3/search"
	pathCart     = "/v2/cart/add"
	pathCheckout = "/v2/checkout/init"
	pathPay      = "/v2/checkout/confirm"
	pathRefresh  = "/auth/refresh"
	pathOrder    = "/v2/orders/"
)

// Client implements platforms.Platform for Zepto.
type Client struct {
	base *platforms.BaseClient
}

// Option configures a Client.
type Option func(*platforms.BaseClient)

// WithBaseURL overrides the API base URL. Used in tests to point at httptest.Server.
func WithBaseURL(url string) Option {
	return func(b *platforms.BaseClient) { b.BaseURL = url }
}

// New creates a Zepto Client backed by the given session store.
func New(s *store.Store, opts ...Option) *Client {
	b := platforms.NewBaseClient(s, types.PlatformZepto, DefaultBaseURL)
	for _, opt := range opts {
		opt(b)
	}
	return &Client{base: b}
}

func (c *Client) loadSession(ctx context.Context) (*types.AppSession, error) {
	return c.base.LoadSession(ctx, c.RefreshToken)
}

// Search queries Zepto for products matching query at the given coordinates.
func (c *Client) Search(ctx context.Context, query string, lat, lng float64) ([]types.Product, error) {
	sess, err := c.loadSession(ctx)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"q": query, "lat": lat, "lng": lng}
	resp, err := c.base.DoRequest(ctx, http.MethodPost, pathSearch, body, sess)
	if err != nil {
		return nil, fmt.Errorf("zepto search: %w", err)
	}
	defer resp.Body.Close()
	if err := platforms.CheckStatus(resp, "zepto", "search"); err != nil {
		return nil, err
	}
	var result searchResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("zepto search decode: %w", err)
	}
	return result.toProducts(), nil
}

// AddToCart adds a product to the Zepto cart and returns the cart ID.
func (c *Client) AddToCart(ctx context.Context, productID string, qty int) (string, error) {
	sess, err := c.loadSession(ctx)
	if err != nil {
		return "", err
	}
	body := map[string]any{"product_id": productID, "quantity": qty}
	resp, err := c.base.DoRequest(ctx, http.MethodPost, pathCart, body, sess)
	if err != nil {
		return "", fmt.Errorf("zepto add_to_cart: %w", err)
	}
	defer resp.Body.Close()
	if err := platforms.CheckStatus(resp, "zepto", "add_to_cart"); err != nil {
		return "", err
	}
	var result struct {
		CartID string `json:"cart_id"` // TODO: verify field name from captured traffic
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("zepto add_to_cart decode: %w", err)
	}
	return result.CartID, nil
}

// Checkout initiates checkout for a cart and returns checkout details.
func (c *Client) Checkout(ctx context.Context, cartID, addressID string) (platforms.CheckoutResult, error) {
	sess, err := c.loadSession(ctx)
	if err != nil {
		return platforms.CheckoutResult{}, err
	}
	body := map[string]any{"cart_id": cartID, "address_id": addressID}
	resp, err := c.base.DoRequest(ctx, http.MethodPost, pathCheckout, body, sess)
	if err != nil {
		return platforms.CheckoutResult{}, fmt.Errorf("zepto checkout: %w", err)
	}
	defer resp.Body.Close()
	if err := platforms.CheckStatus(resp, "zepto", "checkout"); err != nil {
		return platforms.CheckoutResult{}, fmt.Errorf("%w: %w", platforms.ErrCheckoutFailed, err)
	}
	var result checkoutResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return platforms.CheckoutResult{}, fmt.Errorf("zepto checkout decode: %w", err)
	}
	return platforms.CheckoutResult{
		CheckoutID:  result.CheckoutID,
		DeliveryFee: result.DeliveryFee,
		ETAMinutes:  result.ETAMinutes,
	}, nil
}

// Pay confirms payment for a checkout.
// Network errors are returned immediately and never retried (BELIEFS.md §3).
func (c *Client) Pay(ctx context.Context, checkoutID, paymentToken string) (types.Order, error) {
	sess, err := c.loadSession(ctx)
	if err != nil {
		return types.Order{}, err
	}
	body := map[string]any{
		"checkout_id":   checkoutID,
		"payment_token": paymentToken, // over HTTPS, never logged
	}
	resp, err := c.base.DoRequest(ctx, http.MethodPost, pathPay, body, sess)
	if err != nil {
		// Do NOT retry — payment may already have been processed (BELIEFS.md §3)
		return types.Order{}, fmt.Errorf("zepto pay: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusPaymentRequired {
		return types.Order{}, platforms.ErrPaymentFailed
	}
	if err := platforms.CheckStatus(resp, "zepto", "pay"); err != nil {
		return types.Order{}, err
	}
	var result payResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return types.Order{}, fmt.Errorf("zepto pay decode: %w", err)
	}
	return types.Order{
		ID:          result.OrderID,
		Platform:    types.PlatformZepto,
		Status:      result.Status,
		ETAMinutes:  result.ETAMinutes,
		TotalRupees: result.Total,
		PlacedAt:    time.Now(),
	}, nil
}

// RefreshToken exchanges a refresh token for new credentials.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (string, string, time.Time, error) {
	sess, err := c.base.LoadSessionRaw(ctx)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("zepto refresh: load session: %w", err)
	}
	body := map[string]string{"refresh_token": refreshToken} // over HTTPS, never logged
	resp, err := c.base.DoRequest(ctx, http.MethodPost, pathRefresh, body, sess)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("zepto refresh: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", time.Time{}, fmt.Errorf("%w: status %d", platforms.ErrRefreshFailed, resp.StatusCode)
	}
	var result refreshResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", time.Time{}, fmt.Errorf("zepto refresh decode: %w", err)
	}
	return result.AccessToken, result.RefreshToken, result.ExpiresAt, nil
}

// OrderStatus returns the current status and ETA for a Zepto order.
func (c *Client) OrderStatus(ctx context.Context, orderID string) (string, int, error) {
	sess, err := c.loadSession(ctx)
	if err != nil {
		return "", 0, err
	}
	resp, err := c.base.DoRequest(ctx, http.MethodGet, pathOrder+orderID, nil, sess)
	if err != nil {
		return "", 0, fmt.Errorf("zepto order_status: %w", err)
	}
	defer resp.Body.Close()
	if err := platforms.CheckStatus(resp, "zepto", "order_status"); err != nil {
		return "", 0, err
	}
	var result orderStatusResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, fmt.Errorf("zepto order_status decode: %w", err)
	}
	return result.Status, result.ETAMinutes, nil
}

// --- response types ---
// Field names are placeholders — TODO: verify all from captured traffic (PLATFORMS.md).

type searchResp struct {
	Products []struct {
		ID      string  `json:"id"`
		Name    string  `json:"name"`
		Brand   string  `json:"brand"`
		Image   string  `json:"image_url"`
		Price   float64 `json:"price"`
		MRP     float64 `json:"mrp"`
		Unit    string  `json:"unit"`
		StoreID string  `json:"store_id"`
		InStock bool    `json:"in_stock"`
	} `json:"products"`
}

func (r searchResp) toProducts() []types.Product {
	out := make([]types.Product, 0, len(r.Products))
	for _, p := range r.Products {
		out = append(out, types.Product{
			ID: p.ID, Name: p.Name, Brand: p.Brand, ImageURL: p.Image,
			PriceRupees: p.Price, MRP: p.MRP, Unit: p.Unit,
			StoreID: p.StoreID, Platform: types.PlatformZepto, InStock: p.InStock,
		})
	}
	return out
}

type checkoutResp struct {
	CheckoutID  string `json:"checkout_id"`  // TODO: verify field name
	DeliveryFee int    `json:"delivery_fee"` // TODO: verify field name
	ETAMinutes  int    `json:"eta_minutes"`  // TODO: verify field name
}

type payResp struct {
	OrderID    string  `json:"order_id"`    // TODO: verify field name
	Status     string  `json:"status"`      // TODO: verify field name
	ETAMinutes int     `json:"eta_minutes"` // TODO: verify field name
	Total      float64 `json:"total"`       // TODO: verify field name
}

type refreshResp struct {
	AccessToken  string    `json:"access_token"`  // TODO: verify field name
	RefreshToken string    `json:"refresh_token"` // TODO: verify field name
	ExpiresAt    time.Time `json:"expires_at"`    // TODO: verify field name
}

type orderStatusResp struct {
	Status     string `json:"status"`      // TODO: verify field name
	ETAMinutes int    `json:"eta_minutes"` // TODO: verify field name
}
