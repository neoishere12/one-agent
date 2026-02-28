package platforms

import "errors"

// Sentinel errors returned by platform clients.
// Each message includes a fix instruction (BELIEFS.md §6).
var (
	// ErrSessionExpired means the refresh token is dead — re-capture required.
	ErrSessionExpired = errors.New("session expired — run capture_session() to re-capture")

	// ErrDeviceRejected means the platform returned 403 — device fingerprint rejected.
	// Attempting further calls will keep failing. Re-capture is required.
	ErrDeviceRejected = errors.New("device fingerprint rejected (403) — run capture_session() to re-capture")

	// ErrOutOfStock means the product is no longer available at this platform.
	ErrOutOfStock = errors.New("product out of stock — search again for alternatives")

	// ErrAddressNotFound means the address ID is not recognised by this platform.
	ErrAddressNotFound = errors.New("address not found — call get_saved_addresses() for valid IDs")

	// ErrPaymentFailed means the platform rejected the payment instrument.
	ErrPaymentFailed = errors.New("payment rejected by platform — check payment method")

	// ErrCheckoutFailed means checkout initiation failed.
	ErrCheckoutFailed = errors.New("checkout failed — verify cart contents and address ID")

	// ErrRefreshFailed means the refresh token was rejected by the platform.
	// The refresh_token has likely expired — re-capture is needed.
	ErrRefreshFailed = errors.New("token refresh failed — refresh_token expired, run capture_session()")
)
