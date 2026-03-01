package mcp

import "time"

type captureSessionInput struct {
	App string `json:"app"`
}

type bootstrapBlinkitWebSessionInput struct {
	TimeoutSeconds int `json:"timeout_seconds"`
}

type blinkitWorkerStatusInput struct {
	TimeoutSeconds int `json:"timeout_seconds"`
}

type blinkitWorkerStatusOutput struct {
	WorkerConfigured       bool   `json:"worker_configured"`
	WorkerURL              string `json:"worker_url,omitempty"`
	WorkerReachable        bool   `json:"worker_reachable"`
	PageURL                string `json:"page_url,omitempty"`
	Title                  string `json:"title,omitempty"`
	AccessTokenPresent     bool   `json:"access_token_present"`
	AuthKeyPresent         bool   `json:"auth_key_present"`
	ChallengeDetected      bool   `json:"challenge_detected"`
	NeedsHumanVerification bool   `json:"needs_human_verification"`
	Message                string `json:"message,omitempty"`
}

type reverifyBlinkitSessionInput struct {
	TimeoutSeconds int `json:"timeout_seconds"`
}

type reverifyBlinkitSessionOutput struct {
	App                    string                    `json:"app"`
	Ready                  bool                      `json:"ready"`
	NeedsHumanVerification bool                      `json:"needs_human_verification"`
	Message                string                    `json:"message"`
	CapturedAt             time.Time                 `json:"captured_at,omitempty"`
	TokenExpiresAt         time.Time                 `json:"token_expires_at,omitempty"`
	Worker                 blinkitWorkerStatusOutput `json:"worker"`
}

type bootstrapBlinkitWebSessionOutput struct {
	App               string    `json:"app"`
	CapturedAt        time.Time `json:"captured_at"`
	TokenExpiresAt    time.Time `json:"token_expires_at"`
	TokenValid        bool      `json:"token_valid"`
	DeviceHeaderCount int       `json:"device_header_count"`
}

type captureSessionOutput struct {
	App                 string    `json:"app"`
	CapturedAt          time.Time `json:"captured_at"`
	AddressesFound      int       `json:"addresses_found"`
	PaymentMethodsFound int       `json:"payment_methods_found"`
	TokenExpiresAt      time.Time `json:"token_expires_at"`
}

// ingestSessionInput is the body accepted by POST /sessions/ingest.
// Posted by the iOS Network Extension after a successful capture.
type ingestSessionInput struct {
	App            string            `json:"app"`
	AccessToken    string            `json:"access_token"`
	RefreshToken   string            `json:"refresh_token"`
	TokenExpiresAt time.Time         `json:"token_expires_at"`
	DeviceHeaders  map[string]string `json:"device_headers"`
	Addresses      []ingestAddress   `json:"address_ids"`
	Payments       []ingestPayment   `json:"payment_tokens"`
}

type ingestAddress struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	FullAddress string `json:"full_address"`
}

type ingestPayment struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Token string `json:"token"`
	Type  string `json:"type"`
}

type listSessionsOutput struct {
	Sessions []sessionSummary `json:"sessions"`
}

type sessionSummary struct {
	App            string    `json:"app"`
	CapturedAt     time.Time `json:"captured_at"`
	TokenExpiresAt time.Time `json:"token_expires_at"`
	TokenValid     bool      `json:"token_valid"`
	Addresses      []string  `json:"addresses"`
	PaymentMethods []string  `json:"payment_methods"`
}

type refreshTokensInput struct {
	App *string `json:"app"`
}

type refreshTokensOutput struct {
	Refreshed []string                 `json:"refreshed"`
	Failed    []string                 `json:"failed"`
	Results   map[string]refreshResult `json:"results"`
}

type refreshResult struct {
	Success   bool      `json:"success"`
	NewExpiry time.Time `json:"new_expiry,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type searchProductInput struct {
	Query     string   `json:"query"`
	Apps      []string `json:"apps"`
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
}

type searchProductOutput struct {
	Results []searchResult    `json:"results"`
	Errors  map[string]string `json:"errors,omitempty"`
}

type searchResult struct {
	App         string  `json:"app"`
	ProductID   string  `json:"product_id"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	MRP         float64 `json:"mrp"`
	DeliveryFee float64 `json:"delivery_fee"`
	ETAMinutes  int     `json:"eta_minutes"`
	InStock     bool    `json:"in_stock"`
}

type comparePricesInput struct {
	Query     string  `json:"query"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type comparePricesOutput struct {
	Query        string        `json:"query"`
	Ranked       []rankedPrice `json:"ranked"`
	SearchedApps []string      `json:"searched_apps"`
	FailedApps   []string      `json:"failed_apps"`
}

type rankedPrice struct {
	Rank        int     `json:"rank"`
	App         string  `json:"app"`
	ProductID   string  `json:"product_id"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	DeliveryFee float64 `json:"delivery_fee"`
	Total       float64 `json:"total"`
	ETAMinutes  int     `json:"eta_minutes"`
}

type getSavedAddressesInput struct {
	App string `json:"app"`
}

type getSavedAddressesOutput struct {
	App       string          `json:"app"`
	Addresses []addressResult `json:"addresses"`
}

type addressResult struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	FullAddress string `json:"full_address"`
}

type getSavedPaymentMethodsInput struct {
	App string `json:"app"`
}

type getSavedPaymentMethodsOutput struct {
	App          string                `json:"app"`
	SavedMethods []paymentMethodResult `json:"saved_methods"`
	ExtraModes   []paymentModeOption   `json:"extra_modes"`
}

type paymentMethodResult struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Label     string `json:"label"`
	IsDefault bool   `json:"is_default"`
}

type paymentModeOption struct {
	Mode  string `json:"mode"`
	Label string `json:"label"`
}

type placeOrderInput struct {
	App          string `json:"app"`
	ProductID    string `json:"product_id"`
	AddressID    string `json:"address_id"`
	PaymentID    string `json:"payment_id"`
	PaymentToken string `json:"payment_token"`
	PaymentMode  string `json:"payment_mode"`
	Quantity     int    `json:"quantity"`
	Confirm      bool   `json:"confirm"`
}

type placeOrderOutput struct {
	OrderID           string    `json:"order_id"`
	App               string    `json:"app"`
	Product           string    `json:"product"`
	TotalCharged      float64   `json:"total_charged"`
	Address           string    `json:"address"`
	Payment           string    `json:"payment"`
	PaymentMode       string    `json:"payment_mode"`
	UPIIntentRequired bool      `json:"upi_intent_required,omitempty"`
	NextAction        string    `json:"next_action,omitempty"`
	ETAMinutes        int       `json:"eta_minutes"`
	Status            string    `json:"status"`
	PlacedAt          time.Time `json:"placed_at"`
}

type getOrderStatusInput struct {
	App     string `json:"app"`
	OrderID string `json:"order_id"`
}

type getOrderStatusOutput struct {
	OrderID     string    `json:"order_id"`
	Status      string    `json:"status"`
	ETAMinutes  int       `json:"eta_minutes"`
	LastUpdated time.Time `json:"last_updated"`
}
