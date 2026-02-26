# DESIGN.md

> Component responsibilities and data flows. Read before adding features or changing interfaces.

---

## System Overview

Two distinct phases: **Setup** (one-time, iPhone needed) and **Runtime** (permanent, fully silent).

---

## Setup Phase

**Goal:** Capture real app sessions from iPhone traffic. Store encrypted. Never repeat.

```
iPhone (any network)
    │  WireGuard tunnel → VPS public IP
    ▼
VPS — MITM Proxy (internal/proxy, port 8888 on wg0 interface)
    │  TLS intercepted using installed root CA
    │  Parses requests + responses for each target app
    ▼
Session Capturer (internal/proxy/capturer.go)
    │  Extracts from traffic:
    │    - access_token (from Authorization header)
    │    - refresh_token (from auth response body)
    │    - device headers (x-device-id, x-app-version, user-agent, etc.)
    │    - address IDs (from address list response)
    │    - payment tokens (from payment methods response)
    │    - endpoint paths (inferred from observed requests)
    ▼
Token Store (internal/store)
    │  AES-256-GCM encrypts all fields
    │  Writes to data/sessions.db
    ▼
Proxy shuts down. WireGuard tunnel no longer needed.
```

**Trigger:** Claude calls `capture_session(app)` MCP tool.

**Completion signal:** Proxy emits on `Done` channel when `AppSession` is fully populated (all required fields captured). MCP tool returns success.

---

## Runtime Phase

**Goal:** Search, compare, order — all via Claude calling MCP tools.

### Search + Compare Flow

```
Claude → compare_prices("amul lassi")
    │
    ▼
mcp/handlers.go — CompareHandler
    │  reads stored sessions for all 3 platforms
    │  validates token expiry, refreshes if needed
    │  launches 3 goroutines in parallel
    ▼
platforms/blinkit.Search()  platforms/zepto.Search()  platforms/instamart.Search()
    │  each makes HTTP POST to platform search API
    │  injects frozen device headers + rotating request IDs
    │  returns []types.Product
    ▼
CompareHandler aggregates results
    │  sorts by (price + delivery_fee) ascending
    │  returns ranked list with platform, price, ETA, product_id
    ▼
Claude receives ranked response, presents to user
```

### Order Placement Flow

```
Claude → place_order(app, product_id, address_id, payment_token)
    │
    ▼
mcp/handlers.go — PlaceOrderHandler
    │  validates input, loads session
    │  validates token expiry
    ▼
Step 1: platforms/{app}.AddToCart(productID, qty=1)
    │  → returns cart_id
    ▼
Step 2: platforms/{app}.Checkout(cart_id, address_id)
    │  → returns checkout_id, delivery_fee, eta_minutes
    ▼
Step 3: platforms/{app}.Pay(checkout_id, payment_token)
    │  → returns types.Order{ID, Status, ETA}
    ▼
MCP tool returns: order_id, platform, total_price, eta
Claude: "Ordered Amul Lassi from Zepto ₹28, arriving in 12 min. Order #XYZ123"
```

### Token Refresh Flow

```
systemd timer (every 4 hours)
    ▼
cmd/refresher/main.go
    │  loads all sessions from store
    │  for each session:
    ▼
refresh/refresher.go — RefreshAll()
    │  calls platforms/{app}.RefreshToken(refresh_token)
    │  → returns new access_token, new refresh_token, new expiry
    │  updates store atomically
    ▼
Logs result (no token values in logs — see BELIEFS.md)
```

**Also triggered:** Pre-flight check in every MCP tool handler. If `expires_at < now + 30min`, refresh inline before proceeding.

---

## Component Interface Contracts

### MITM Proxy → Store

Proxy writes a fully populated `types.AppSession` to store. MCP tools read from store. Proxy and MCP tools never run concurrently for the same app — `capture_session` holds a mutex per app.

### MCP Tool → Platform Client

Every platform call goes through the `Platform` interface. Handlers never call platform HTTP directly. This allows platform clients to be swapped or mocked in tests without changing handler logic.

### Platform Client → Store

Platform clients read `AppSession` from store at the start of every method call. They do not cache sessions in memory. This ensures token refreshes are picked up immediately.

---

## Error Handling Strategy

| Error Type | Handler Behavior |
|---|---|
| Token expired (401) | Refresh inline, retry once. If still 401, return `ErrSessionExpired`. |
| Platform API 4xx (not 401) | Return immediately with platform error details. Do not retry. |
| Platform API 5xx | Retry up to 2 times with 500ms backoff. Then return error. |
| Store read failure | Return immediately. Never proceed with empty session. |
| Capture timeout (>5 min) | Shut down proxy, return `ErrCaptureTimeout`. |
| Network error during order | Do NOT retry pay step — risk of double charge. Return error. |

**Payment is never retried.** If `Pay()` returns a network error, surface it to Claude and let the user decide.

---

## Concurrency Model

- `compare_prices` fans out to N platforms via goroutines with `errgroup`
- Each platform call has a 10-second context timeout
- Token refresh uses a per-app mutex to prevent concurrent refresh races
- MITM proxy runs in its own goroutine, signals completion on a channel
- MCP server handles each tool call in its own goroutine

---

*See also: [ARCHITECTURE.md](ARCHITECTURE.md) for layering | [PLATFORMS.md](PLATFORMS.md) for API details | [TOKENS.md](TOKENS.md) for token lifecycle*
