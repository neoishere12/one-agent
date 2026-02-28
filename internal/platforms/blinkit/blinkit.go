// Package blinkit implements platforms.Platform for Blinkit.
//
// Endpoint paths are PENDING CAPTURE — all marked with that comment.
// Update from live traffic after running capture_session("blinkit") (PLATFORMS.md).
package blinkit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

const (
	DefaultBaseURL = "https://api2.grofers.com"
	WebBaseURL     = "https://blinkit.com"

	// Endpoint paths — partially validated from live capture on 2026-02-27.
	// Keep remaining endpoints marked PENDING CAPTURE until order flow is captured.
	pathSearch   = "/v1/layout/search"
	pathCart     = "/v2/cart/items"
	pathCheckout = "/v2/checkout/init"
	pathPay      = "/v2/checkout/confirm"
	pathRefresh  = "/v2/auth/refresh"
	pathOrder    = "/v2/orders/"
)

// Client implements platforms.Platform for Blinkit.
type Client struct {
	base             *platforms.BaseClient
	warmupMu         sync.Mutex
	warmedSessionKey string
}

// Option configures a Client.
type Option func(*platforms.BaseClient)

// WithBaseURL overrides the API base URL. Used in tests to point at httptest.Server.
func WithBaseURL(url string) Option {
	return func(b *platforms.BaseClient) { b.BaseURL = url }
}

// New creates a Blinkit Client backed by the given session store.
func New(s *store.Store, opts ...Option) *Client {
	b := platforms.NewBaseClient(s, types.PlatformBlinkit, DefaultBaseURL)
	for _, opt := range opts {
		opt(b)
	}
	return &Client{base: b}
}

// loadSession wraps BaseClient.LoadSession, binding this client's RefreshToken.
func (c *Client) loadSession(ctx context.Context) (*types.AppSession, error) {
	return c.base.LoadSession(ctx, c.RefreshToken)
}

// AddToCart adds a product to the Blinkit cart and returns the cart ID.
func (c *Client) AddToCart(ctx context.Context, productID string, qty int) (string, error) {
	sess, err := c.loadSession(ctx)
	if err != nil {
		return "", err
	}
	body := map[string]any{"product_id": productID, "quantity": qty}
	resp, err := c.base.DoRequestWithBaseURL(ctx, http.MethodPost, c.requestBaseURL(sess), pathCart, body, sess)
	if err != nil {
		return "", fmt.Errorf("blinkit add_to_cart: %w", err)
	}
	defer resp.Body.Close()
	if err := platforms.CheckStatus(resp, "blinkit", "add_to_cart"); err != nil {
		return "", err
	}
	var result struct {
		CartID string `json:"cart_id"` // TODO: verify field name from captured traffic
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("blinkit add_to_cart decode: %w", err)
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
	resp, err := c.base.DoRequestWithBaseURL(ctx, http.MethodPost, c.requestBaseURL(sess), pathCheckout, body, sess)
	if err != nil {
		return platforms.CheckoutResult{}, fmt.Errorf("blinkit checkout: %w", err)
	}
	defer resp.Body.Close()
	if err := platforms.CheckStatus(resp, "blinkit", "checkout"); err != nil {
		return platforms.CheckoutResult{}, fmt.Errorf("%w: %w", platforms.ErrCheckoutFailed, err)
	}
	var result checkoutResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return platforms.CheckoutResult{}, fmt.Errorf("blinkit checkout decode: %w", err)
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
		"payment_token": paymentToken, // sent over HTTPS, never logged
	}
	resp, err := c.base.DoRequestWithBaseURL(ctx, http.MethodPost, c.requestBaseURL(sess), pathPay, body, sess)
	if err != nil {
		// Do NOT retry — payment may already have been processed (BELIEFS.md §3)
		return types.Order{}, fmt.Errorf("blinkit pay: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusPaymentRequired {
		return types.Order{}, platforms.ErrPaymentFailed
	}
	if err := platforms.CheckStatus(resp, "blinkit", "pay"); err != nil {
		return types.Order{}, err
	}
	var result payResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return types.Order{}, fmt.Errorf("blinkit pay decode: %w", err)
	}
	return types.Order{
		ID:          result.OrderID,
		Platform:    types.PlatformBlinkit,
		Status:      result.Status,
		ETAMinutes:  result.ETAMinutes,
		TotalRupees: result.Total,
		PlacedAt:    time.Now(),
	}, nil
}

// RefreshToken exchanges a refresh token for new credentials.
// Reads device headers from the stored session without expiry check.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (string, string, time.Time, error) {
	sess, err := c.base.LoadSessionRaw(ctx)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("blinkit refresh: load session: %w", err)
	}
	body := map[string]string{"refresh_token": refreshToken} // over HTTPS, never logged
	resp, err := c.base.DoRequestWithBaseURL(ctx, http.MethodPost, c.requestBaseURL(sess), pathRefresh, body, sess)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("blinkit refresh: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", time.Time{}, fmt.Errorf("%w: status %d", platforms.ErrRefreshFailed, resp.StatusCode)
	}
	var result refreshResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", time.Time{}, fmt.Errorf("blinkit refresh decode: %w", err)
	}
	return result.AccessToken, result.RefreshToken, result.ExpiresAt, nil
}

// OrderStatus returns the current status and ETA for a Blinkit order.
func (c *Client) OrderStatus(ctx context.Context, orderID string) (string, int, error) {
	sess, err := c.loadSession(ctx)
	if err != nil {
		return "", 0, err
	}
	resp, err := c.base.DoRequestWithBaseURL(ctx, http.MethodGet, c.requestBaseURL(sess), pathOrder+orderID, nil, sess)
	if err != nil {
		return "", 0, fmt.Errorf("blinkit order_status: %w", err)
	}
	defer resp.Body.Close()
	if err := platforms.CheckStatus(resp, "blinkit", "order_status"); err != nil {
		return "", 0, err
	}
	var result orderStatusResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, fmt.Errorf("blinkit order_status decode: %w", err)
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
			StoreID: p.StoreID, Platform: types.PlatformBlinkit, InStock: p.InStock,
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
