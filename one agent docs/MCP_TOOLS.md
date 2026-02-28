# MCP_TOOLS.md

> Complete spec for every MCP tool. Inputs, outputs, errors, and side effects.
> This is the contract between Claude and the Go server. Do not change tool signatures without updating this doc.

---

## Tool Registry

| Tool | Category | Side Effects |
|---|---|---|
| `capture_session` | Setup | None (polls store; external importer writes the session) |
| `bootstrap_blinkit_web_session` | Setup | Writes Blinkit web session to store |
| `list_sessions` | Introspection | None |
| `refresh_tokens` | Auth | Writes to store |
| `search_product` | Commerce | None |
| `compare_prices` | Commerce | None |
| `get_saved_addresses` | Commerce | None |
| `get_saved_payment_methods` | Commerce | None |
| `place_order` | Commerce | **Places real order, charges payment** |
| `get_order_status` | Commerce | None |

---

## MCP Protocol Methods

The server supports standards-style MCP JSON-RPC methods on `POST /mcp`:

- `initialize`
- `tools/list`
- `tools/call`
- `notifications/initialized`
- `initialized` (alias for compatibility)
- `ping`

Transport behavior:
- accepts both single JSON-RPC requests and JSON-RPC batch arrays
- returns HTTP `200` for JSON-RPC method errors (with `error` object in body)
- returns `202` for notification-only requests (no request IDs)
- supports `GET /mcp` probe and `OPTIONS /mcp` preflight
- also supports legacy SSE transport for compatibility: `GET /sse` + `POST /messages?sessionId=...`

`/rpc` remains available for backward-compatible direct method calls where the JSON-RPC `method` is the tool name itself (for example `"list_sessions"`).

`tools/call` should be preferred for MCP clients. Use:
```json
{
  "name": "compare_prices",
  "arguments": {
    "query": "amul lassi",
    "latitude": 18.5204,
    "longitude": 73.8567
  }
}
```

---

## `capture_session`

Polls the token store until a new session for the requested app is delivered via `POST /sessions/ingest` (typically by `cmd/proxyman-import` after parsing a Proxyman HAR export). Returns when the session is confirmed written, or times out after 5 minutes.

**Before calling:** Capture traffic in Proxyman on iPhone, export HAR, and ingest via `proxyman-import` (one-shot) or `proxyman-watch` (automated folder loop) so it POSTs the session to `/sessions/ingest`. Use `-dry-run -diagnostics` first when tuning a new HAR export shape.

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
  "token_expires_at": "2026-03-05T10:00:00Z"
}
```

**Errors:**
- `ErrCaptureTimeout` — no session received within 5 minutes (HAR not imported yet, wrong app selected, or ingest failed)

**Side effects:** None on the VPS. An external importer (for example `proxyman-import`) writes the session via `/sessions/ingest`.

---

## `bootstrap_blinkit_web_session`

Bootstraps a Blinkit **web** session directly from the Playwright browser helper and persists it into the encrypted store.

Use this when:
- Blinkit browser mode is enabled (`BLINKIT_SEARCH_MODE=browser`)
- you want to avoid HAR ingest just to initialize Blinkit search/list session state

**Input:**
```json
{
  "timeout_seconds": 300
}
```
`timeout_seconds` is optional (`0..900`). If omitted, server env defaults apply.

**Output (success):**
```json
{
  "app": "blinkit",
  "captured_at": "2026-02-28T11:30:00Z",
  "token_expires_at": "2026-03-07T11:30:00Z",
  "token_valid": true,
  "device_header_count": 3
}
```

**Errors:**
- Browser helper not installed or not reachable
- Login not completed in the browser profile within timeout
- Worker/helper timeout

**Side effects:** writes/overwrites Blinkit session in store.

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
`apps` is optional. If omitted, server defaults to all three apps.

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

### Blinkit 403 Fallback Mode

If Blinkit replay sessions repeatedly fail with `403` / `device fingerprint` errors, you can enable browser-backed Blinkit search:

- `BLINKIT_SEARCH_MODE=browser`
- `BLINKIT_BROWSER_HELPER=scripts/blinkit-browser-search.mjs`
- `BLINKIT_BROWSER_NODE=node`
- `BLINKIT_BROWSER_TIMEOUT=45s`
- `BLINKIT_BROWSER_PROFILE_DIR=.data/blinkit-browser-profile`
- `BLINKIT_BROWSER_HEADLESS=true` (set `false` for first-time login bootstrap)
- Optional proxy egress on VPS:
  - `BLINKIT_BROWSER_PROXY_SERVER=http://<proxy-host>:<port>`
  - `BLINKIT_BROWSER_PROXY_USERNAME=<username>`
  - `BLINKIT_BROWSER_PROXY_PASSWORD=<password>`

In this mode, Blinkit `search_product` uses Playwright and a persistent Chromium profile to obtain live `/v1/layout/search` payloads from `blinkit.com`.
In browser mode, Blinkit search can run even when no Blinkit API session was ingested.

To avoid browser relaunch on every search, run helper worker mode once:
- `node scripts/blinkit-browser-search.mjs --worker --worker-port 42199`
- set `BLINKIT_BROWSER_WORKER_URL=http://127.0.0.1:42199` on the server
- worker diagnostics:
  - `GET /health` -> worker liveness
  - `GET /status` -> token presence + challenge detection snapshot

**Errors:**
- `ErrSessionNotFound` — session missing for one of the requested apps
- `ErrTokenExpired` — token expired and refresh failed (re-capture needed)
- Partial results are returned even if one platform fails — failed apps listed in `errors` field
- Browser mode can return:
  - `human_verification_required: ...` when Blinkit anti-bot challenge is detected
  - successful results can come from DOM extraction fallback when direct network interception is blocked

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

## `get_saved_payment_methods`

Returns saved payment methods from the captured session and additional payment modes supported by `place_order`.

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
  "saved_methods": [
    {
      "id": "pay_card_001",
      "type": "card",
      "label": "Visa •••• 4242",
      "is_default": true
    }
  ],
  "extra_modes": [
    { "mode": "cod", "label": "Cash on Delivery" },
    { "mode": "upi_intent", "label": "UPI Intent" }
  ]
}
```

Use `payment_id` with `payment_mode: "saved"` in `place_order` when you want to avoid passing raw payment tokens from the UI layer.

---

## `place_order`

**⚠️ This tool places a real order and charges the payment method. Confirm with user before calling.**

**Input:**
```json
{
  "app": "zepto",
  "product_id": "prod_abc123",
  "address_id": "addr_home_001",
  "payment_mode": "saved",
  "payment_id": "pay_card_001",
  "payment_token": "card_xxxx4242",
  "quantity": 1,
  "confirm": true
}
```
`confirm` must be `true` or the handler rejects the request.

`payment_mode` values:
- `saved` (default) — requires either `payment_token` or `payment_id`
- `cod` — cash on delivery
- `upi_intent` — marks order flow as requiring user-side UPI intent authorization

**Output:**
```json
{
  "order_id": "ZPT-XYZ123",
  "app": "zepto",
  "product": "prod_abc123",
  "total_charged": 28,
  "address": "Home — Flat 4B, Pimpri, Pune",
  "payment": "Visa •••• 4242",
  "payment_mode": "saved",
  "upi_intent_required": false,
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
- invalid params — missing fields, invalid `payment_mode`, unknown `payment_id`, or `confirm != true`

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

---

## `POST /sessions/ingest` *(HTTP endpoint, not an MCP tool)*

Called by a capture importer (for example `cmd/proxyman-import` or `cmd/proxyman-watch`) after parsing a Proxyman export. Not exposed to Claude.

**Auth:** `X-Ingest-Secret: <shared secret>` header — must match `INGEST_SECRET` env var. Constant-time comparison. Returns 401 if missing or wrong.

**Transport:** Served by the same MCP HTTP server on `MCP_PORT` (plain HTTP in the current repo). If you need HTTPS, terminate TLS in front of the server (for example, reverse proxy/load balancer).

**Rate limiting:** Per-client rate limited on the server (fixed-window, by client IP / forwarded IP). Default is `10/min`, configurable via `INGEST_RATE_LIMIT_MAX` and `INGEST_RATE_LIMIT_WINDOW`. Exceeding the limit returns 429.

**Body:**
```json
{
  "app": "blinkit",
  "access_token": "...",
  "refresh_token": "...",
  "token_expires_at": "2026-03-05T10:00:00Z",
  "device_headers": {
    "x-device-id": "...",
    "x-app-version": "...",
    "user-agent": "...",
    "x-build-number": "...",
    "x-platform": "ios"
  },
  "address_ids": [
    { "id": "addr_home_001", "label": "Home", "full_address": "Flat 4B, Pimpri, Pune 411018" }
  ],
  "payment_tokens": [
    { "id": "pay_card_001", "label": "Visa •••• 4242", "token": "...", "type": "card" }
  ]
}
```

**Response (success):** `200 { "status": "ok" }`

**Errors:**
- `401` — missing or invalid `X-Ingest-Secret`
- `429` — rate limited (too many ingest attempts from one client in the window)
- `400` — malformed JSON
- `400` — invalid ingest payload after JSON decode (e.g. unknown app, missing token fields)
- `500` — store write failed

**Side effects:** Writes encrypted `AppSession` to store. `capture_session` MCP tool detects the new session on its next poll.

---

*See also: [DESIGN.md](DESIGN.md) for flow diagrams | [PLATFORMS.md](PLATFORMS.md) for raw API details*
