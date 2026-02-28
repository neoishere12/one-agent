# PLATFORMS.md

> Per-platform API contracts, known endpoints, device header requirements, and quirks.
> This doc is populated progressively as sessions are captured and APIs are reverse engineered.
> Mark endpoints as VERIFIED once confirmed from live traffic.

---

## Platform Interface (implemented by all)

```go
type Platform interface {
    Search(ctx, query, lat, lng) ([]types.Product, error)
    AddToCart(ctx, productID, qty) (cartID string, error)
    Checkout(ctx, cartID, addressID) (checkoutID string, deliveryFee int, etaMin int, error)
    Pay(ctx, checkoutID, paymentToken) (types.Order, error)
    RefreshToken(ctx, refreshToken) (newAccess, newRefresh string, expiresAt time.Time, error)
    OrderStatus(ctx, orderID) (status string, etaMin int, error)
}
```

---

## Blinkit

**Base URL:** `https://api2.grofers.com` (VERIFIED from live capture on 2026-02-27)

**Known Headers (capture and freeze):**
```
Authorization: Bearer <access_token>
access_token: <access_token>        # commonly used by Blinkit app requests
auth_key: <refresh_token-like key>  # commonly used by Blinkit app requests
x-device-id: <captured — freeze forever>
x-app-version: <captured — update on major app updates>
x-platform: ios
x-build-number: <captured>
user-agent: <captured iOS user agent — freeze>
x-session-id: <rotate — UUIDv4 per request>
x-request-id: <rotate — UUIDv4 per request>
Content-Type: application/json
```

**Endpoints (populate from capture):**

| Operation | Method | Path | Status |
|---|---|---|---|
| Search | POST | `/v1/layout/search?q=<query>&search_type=type_to_search` | VERIFIED |
| Add to cart | POST | `/v2/cart/items` | PENDING CAPTURE |
| Checkout init | POST | `/v2/checkout/init` | PENDING CAPTURE |
| Confirm payment | POST | `/v2/checkout/confirm` | PENDING CAPTURE |
| Token refresh | POST | `/v2/auth/refresh` | PENDING CAPTURE |
| Order status | GET | `/v2/orders/:id` | PENDING CAPTURE |
| Address list | GET | `/v2/addresses` | PENDING CAPTURE |
| Payment methods | GET | `/v2/payment-methods` | PENDING CAPTURE |

**Known Quirks:**
- Search response shape is nested layout/snippet JSON (not a flat `products` list); parser fallback may be needed.
- Some address hints appear in query params (for example `fetch_nearest_addresses=true`) while path may not contain `address`.

---

## Zepto

**Base URL:** `https://api.zepto.com` (verify from captured traffic)

**Known Headers:** Same pattern as Blinkit — capture and freeze.

**Endpoints (populate from capture):**

| Operation | Method | Path | Status |
|---|---|---|---|
| Search | POST | `/v3/search` | PENDING CAPTURE |
| Add to cart | POST | `/v2/cart/add` | PENDING CAPTURE |
| Checkout init | POST | `/v2/checkout/init` | PENDING CAPTURE |
| Confirm payment | POST | `/v2/checkout/confirm` | PENDING CAPTURE |
| Token refresh | POST | `/auth/refresh` | PENDING CAPTURE |
| Order status | GET | `/v2/orders/:id` | PENDING CAPTURE |
| Address list | GET | `/v2/user/addresses` | PENDING CAPTURE |
| Payment methods | GET | `/v2/user/payment-methods` | PENDING CAPTURE |

**Known Quirks:**
- None documented yet

---

## Swiggy Instamart

**Base URL:** `https://api.swiggy.com` (verify from captured traffic)

**Known Headers:** Same pattern — capture and freeze.

**Endpoints (populate from capture):**

| Operation | Method | Path | Status |
|---|---|---|---|
| Search | POST | `/instamart/v2/search` | PENDING CAPTURE |
| Add to cart | POST | `/instamart/v1/cart` | PENDING CAPTURE |
| Checkout init | POST | `/instamart/v1/checkout` | PENDING CAPTURE |
| Confirm payment | POST | `/instamart/v1/checkout/confirm` | PENDING CAPTURE |
| Token refresh | POST | `/auth/v2/refresh` | PENDING CAPTURE |
| Order status | GET | `/instamart/v1/orders/:id` | PENDING CAPTURE |
| Address list | GET | `/user/v1/addresses` | PENDING CAPTURE |
| Payment methods | GET | `/user/v1/payment-instruments` | PENDING CAPTURE |

**Known Quirks:**
- Swiggy uses a shared auth token across Swiggy Food and Instamart — capture during Instamart session specifically
- None other documented yet

---

## General Notes (apply to all platforms)

### Location Handling
All search and checkout APIs require latitude/longitude. These come from user's stored home address coordinates, not from device GPS. Extract and store lat/lng alongside address IDs during capture.

### Request Signing
Some apps sign requests with an HMAC derived from request body + timestamp + device secret. If captured requests have an `x-signature` header, the signing algorithm must be reverse engineered from the app binary. Document here if found.

### Rate Limiting
- Max 1 request/second per platform to avoid triggering bot detection
- `compare_prices` fires 3 parallel searches — each still respects its own 1 req/sec budget
- If a 429 is received, back off 5 seconds and retry once

### 401 vs 403
- `401` — token expired, refresh and retry
- `403` — device fingerprint rejected or account blocked. Do not retry. Re-capture needed.

---

## How to Update This Doc

When you capture a session for an app and confirm endpoints from live traffic:
1. Update the endpoint table with verified paths
2. Change status from `PENDING CAPTURE` to `VERIFIED`
3. Add any quirks observed in the traffic to the Quirks section
4. Commit with message: `docs: verify [app] endpoints from capture`

---

*See also: [TOKENS.md](TOKENS.md) for token handling | [DESIGN.md](DESIGN.md) for flow diagrams*
