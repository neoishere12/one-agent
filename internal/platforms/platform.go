// Package platforms defines the Platform interface and shared types
// implemented by all quick commerce platform clients.
//
// Layer rule: platforms may import types, config, store — never mcp or cmd.
package platforms

import (
	"context"
	"time"

	"one-agent/internal/types"
)

// Platform is the contract every platform client must satisfy.
// Each method reads its session from the store on every call (BELIEFS.md §8).
type Platform interface {
	// Search returns products matching query at the given coordinates.
	Search(ctx context.Context, query string, lat, lng float64) ([]types.Product, error)

	// AddToCart adds a product to the cart and returns the cart ID.
	AddToCart(ctx context.Context, productID string, qty int) (string, error)

	// Checkout initiates checkout and returns checkout ID, delivery fee, and ETA.
	Checkout(ctx context.Context, cartID, addressID string) (CheckoutResult, error)

	// Pay confirms payment. Network errors are NEVER retried (BELIEFS.md §3).
	Pay(ctx context.Context, checkoutID, paymentToken string) (types.Order, error)

	// RefreshToken exchanges a refresh token for new credentials.
	RefreshToken(ctx context.Context, refreshToken string) (newAccess, newRefresh string, expiresAt time.Time, err error)

	// OrderStatus returns the current delivery status and ETA for an order.
	OrderStatus(ctx context.Context, orderID string) (status string, etaMinutes int, err error)
}

// CheckoutResult contains the output of a successful checkout initiation.
type CheckoutResult struct {
	CheckoutID  string
	DeliveryFee int // rupees
	ETAMinutes  int
}
