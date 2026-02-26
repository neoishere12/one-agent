# MCP_TOOLS.md

> Complete spec for every MCP tool. Inputs, outputs, errors, and side effects.
> This is the contract between Claude and the Go server. Do not change tool signatures without updating this doc.

---

## Tool Registry

| Tool | Category | Side Effects |
|---|---|---|
| `capture_session` | Setup | Writes to store, starts/stops proxy |
| `list_sessions` | Introspection | None |
| `refresh_tokens` | Auth | Writes to store |
| `search_product` | Commerce | None |
| `compare_prices` | Commerce | None |
| `get_saved_addresses` | Commerce | None |
| `place_order` | Commerce | **Places real order, charges payment** |
| `get_order_status` | Commerce | None |

---

## `capture_session`

Starts MITM proxy on WireGuard interface. Waits for iPhone traffic. Stores captured session.

**Input:**
```json
{
  "app": "blinkit" | "zepto" | "instamart"
}
```

**Output (success):**
```json
{
  "app": "blinkit",
  "captured_at": "2026-02-26T10:00:00Z",
  "addresses_found": 2,
  "payment_methods_found": 1,
  "token_expires_at": "2026-03-05T10:00:00Z",
  "proxy_instruction": "Set iPhone HTTP proxy to 10.0.0.1:8888, then open Blinkit and browse for 30 seconds."
}
```

**Errors:**
- `ErrCaptureTimeout` — no traffic received within 5 minutes
- `ErrProxyBusy` — capture already in progress for another app
- `ErrWireGuardDown` — WireGuard interface not reachable

**Side effects:** Writes `AppSession` to store. Proxy shuts down after capture.

**Note:** Tool streams status updates during capture. Claude should surface the `proxy_instruction` to the user before waiting.

---

## `list_sessions`

Returns all captured sessions and their current status.

**Input:** none

**Output:**
```json
{
  "sessions": [
    {
      "app": "blinkit",
      "captured_at": "2026-02-26T10:00:00Z",
      "token_expires_at": "2026-03-05T10:00:00Z",
      "token_valid": true,
      "addresses": ["Home", "Office"],
      "payment_methods": ["Visa •••• 4242"]
    }
  ]
}
```

---

## `refresh_tokens`

Manually refreshes tokens for one or all apps.

**Input:**
```json
{
  "app": "blinkit" | "zepto" | "instamart" | null
}
```
Pass `null` to refresh all.

**Output:**
```json
{
  "refreshed": ["blinkit", "zepto"],
  "failed": [],
  "results": {
    "blinkit": { "success": true, "new_expiry": "2026-03-05T14:00:00Z" },
    "zepto": { "success": true, "new_expiry": "2026-03-05T14:00:00Z" }
  }
}
```

**Errors:**
- `ErrSessionNotFound` — no session exists for the given app
- `ErrRefreshFailed` — platform rejected refresh token (session expired, re-capture needed)

---

## `search_product`

Searches for a product on specified platforms in parallel.

**Input:**
```json
{
  "query": "amul lassi",
  "apps": ["blinkit", "zepto", "instamart"],
  "latitude": 18.5204,
  "longitude": 73.8567
}
```

**Output:**
```json
{
  "results": [
    {
      "app": "zepto",
      "product_id": "prod_abc123",
      "name": "Amul Lassi Sweet 200ml",
      "price": 28,
      "mrp": 30,
      "delivery_fee": 0,
      "eta_minutes": 12,
      "in_stock": true
    }
  ]
}
```

Results are returned unsorted. Use `compare_prices` for ranked results.

**Errors:**
- `ErrSessionNotFound` — session missing for one of the requested apps
- `ErrTokenExpired` — token expired and refresh failed (re-capture needed)
- Partial results are returned even if one platform fails — failed apps listed in `errors` field

---

## `compare_prices`

Searches all apps and returns results ranked by total cost (price + delivery fee).

**Input:**
```json
{
  "query": "amul lassi",
  "latitude": 18.5204,
  "longitude": 73.8567
}
```

**Output:**
```json
{
  "query": "amul lassi",
  "ranked": [
    {
      "rank": 1,
      "app": "zepto",
      "product_id": "prod_abc123",
      "name": "Amul Lassi Sweet 200ml",
      "price": 28,
      "delivery_fee": 0,
      "total": 28,
      "eta_minutes": 12
    },
    {
      "rank": 2,
      "app": "blinkit",
      "product_id": "prod_xyz789",
      "name": "Amul Lassi 200ml",
      "price": 30,
      "delivery_fee": 0,
      "total": 30,
      "eta_minutes": 9
    }
  ],
  "searched_apps": ["blinkit", "zepto", "instamart"],
  "failed_apps": []
}
```

---

## `get_saved_addresses`

Returns saved delivery addresses for a given app.

**Input:**
```json
{
  "app": "zepto"
}
```

**Output:**
```json
{
  "app": "zepto",
  "addresses": [
    {
      "id": "addr_home_001",
      "label": "Home",
      "full_address": "Flat 4B, Pimpri, Pune 411018"
    },
    {
      "id": "addr_office_002",
      "label": "Office",
      "full_address": "IT Park, Hinjawadi Phase 1, Pune 411057"
    }
  ]
}
```

---

## `place_order`

**⚠️ This tool places a real order and charges the payment method. Confirm with user before calling.**

**Input:**
```json
{
  "app": "zepto",
  "product_id": "prod_abc123",
  "address_id": "addr_home_001",
  "payment_token": "card_xxxx4242",
  "quantity": 1
}
```

**Output:**
```json
{
  "order_id": "ZPT-XYZ123",
  "app": "zepto",
  "product": "Amul Lassi Sweet 200ml",
  "total_charged": 28,
  "address": "Home — Flat 4B, Pimpri, Pune",
  "payment": "Visa •••• 4242",
  "eta_minutes": 12,
  "status": "confirmed",
  "placed_at": "2026-02-26T10:05:00Z"
}
```

**Errors:**
- `ErrOutOfStock` — product no longer available
- `ErrAddressNotFound` — address ID not valid for this platform
- `ErrPaymentFailed` — payment rejected by platform
- `ErrTokenExpired` — token expired, refresh and retry
- `ErrCheckoutFailed` — checkout step failed (cart issue)

**Payment is never retried on network error.** If `Pay()` fails with a network error, return the error immediately. Do not retry.

---

## `get_order_status`

**Input:**
```json
{
  "app": "zepto",
  "order_id": "ZPT-XYZ123"
}
```

**Output:**
```json
{
  "order_id": "ZPT-XYZ123",
  "status": "out_for_delivery",
  "eta_minutes": 5,
  "last_updated": "2026-02-26T10:10:00Z"
}
```

**Status values:** `confirmed` | `preparing` | `out_for_delivery` | `delivered` | `cancelled`

---

*See also: [DESIGN.md](DESIGN.md) for flow diagrams | [PLATFORMS.md](PLATFORMS.md) for raw API details*
