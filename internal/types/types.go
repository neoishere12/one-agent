// Package types defines shared data structures used across all layers.
// It has no imports from this repo and no business logic — structs only.
package types

import (
	"fmt"
	"time"
)

// Platform identifies a supported quick commerce platform.
type Platform string

const (
	PlatformBlinkit   Platform = "blinkit"
	PlatformZepto     Platform = "zepto"
	PlatformInstamart Platform = "instamart"
)

// AppSession holds a captured platform session: auth tokens, device headers,
// saved addresses and payment methods.
//
// Token fields are redacted from String() to prevent accidental log leaks.
type AppSession struct {
	App           Platform          `json:"app"`
	AccessToken   string            `json:"access_token"`
	RefreshToken  string            `json:"refresh_token"`
	ExpiresAt     time.Time         `json:"expires_at"`
	DeviceHeaders map[string]string `json:"device_headers"`
	Addresses     []Address         `json:"addresses"`
	Payments      []PaymentMethod   `json:"payments"`
	CapturedAt    time.Time         `json:"captured_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// String redacts tokens and device headers to prevent accidental logging.
func (s AppSession) String() string {
	return fmt.Sprintf(
		"AppSession{app:%s expires_at:%s updated_at:%s [tokens+headers redacted]}",
		s.App,
		s.ExpiresAt.Format(time.RFC3339),
		s.UpdatedAt.Format(time.RFC3339),
	)
}

// Product is a single search result from any platform.
type Product struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Brand       string   `json:"brand"`
	ImageURL    string   `json:"image_url"`
	PriceRupees float64  `json:"price_rupees"`
	MRP         float64  `json:"mrp"`
	Unit        string   `json:"unit"`     // e.g. "500g", "1L"
	StoreID     string   `json:"store_id"` // platform-specific store/dark-store ID
	Platform    Platform `json:"platform"`
	InStock     bool     `json:"in_stock"`
}

// Order is a placed order with current status and ETA.
type Order struct {
	ID          string      `json:"id"`
	Platform    Platform    `json:"platform"`
	Status      string      `json:"status"`
	ETAMinutes  int         `json:"eta_minutes"`
	Items       []OrderItem `json:"items"`
	TotalRupees float64     `json:"total_rupees"`
	PlacedAt    time.Time   `json:"placed_at"`
}

// OrderItem is one line in an Order.
type OrderItem struct {
	ProductID string  `json:"product_id"`
	Name      string  `json:"name"`
	Qty       int     `json:"qty"`
	Price     float64 `json:"price"`
}

// Address is a saved delivery address.
type Address struct {
	ID        string  `json:"id"`
	Label     string  `json:"label"`    // e.g. "Home", "Office"
	Line1     string  `json:"line1"`
	Line2     string  `json:"line2"`
	City      string  `json:"city"`
	PinCode   string  `json:"pin_code"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	IsDefault bool    `json:"is_default"`
}

// PaymentMethod is a saved UPI mandate or card token.
// Token is redacted from String() to prevent accidental logging.
type PaymentMethod struct {
	ID        string `json:"id"`
	Type      string `json:"type"`      // "upi" | "card"
	Label     string `json:"label"`     // e.g. "SBI UPI", "HDFC •••• 1234"
	Token     string `json:"token"`     // never log — payment instrument token
	IsDefault bool   `json:"is_default"`
}

// String redacts the payment token to prevent accidental logging.
func (p PaymentMethod) String() string {
	return fmt.Sprintf("PaymentMethod{id:%s type:%s label:%s token:[REDACTED]}", p.ID, p.Type, p.Label)
}
