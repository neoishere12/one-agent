# DESIGN.md

> Component responsibilities and data flows. Read before adding features or changing interfaces.

---

## System Overview

Two distinct phases: **Setup** (one-time, iPhone needed) and **Runtime** (permanent, fully silent).

---

## Setup Phase

**Goal:** Capture real app sessions from iPhone traffic. Store encrypted. Never repeat.

```
iPhone (any network — cellular or Wi-Fi)
    │  Proxyman iOS app capture + local VPN interception enabled
    │  HTTPS decrypted using trusted Proxyman CA certificate
    ▼
Proxyman (manual capture session)
    │  User opens target app and browses ~30s
    │  Requests/responses captured on-device
    │  Export capture as HAR file
    ▼
Mac / workstation running repo
    │  cmd/proxyman-import (one-shot) OR cmd/proxyman-watch (automated folder loop) parses HAR
    │  Extracts:
    │    - access_token (Authorization header or auth response body)
    │    - refresh_token + token_expires_at (auth response body)
    │    - device headers (x-*, user-agent, accept-language)
    │    - addresses / payment tokens (best-effort JSON heuristics)
    ▼
POST http://<vps-ip>:8080/sessions/ingest
    │  Header: X-Ingest-Secret: <shared secret>
    │  Body: { app, access_token, refresh_token, device_headers,
    │           address_ids, payment_tokens, token_expires_at }
    ▼
VPS — /sessions/ingest handler (internal/mcp)
    │  Validates X-Ingest-Secret
    │  Builds AppSession from body
    ▼
Token Store (internal/store)
    │  AES-256-GCM encrypts all fields
    │  Writes to data/sessions.db
    ▼
Proxyman capture can be turned off. VPS runs silently forever after.
```

**Trigger:** User starts a Proxyman capture on iPhone, browses the target app, exports HAR.  
Ingest path is either:
- one-shot: run `proxyman-import` per HAR
- automated: keep `proxyman-watch -dir ...` running and drop HAR files into that directory

**Claude's role during capture:** `capture_session(app)` still polls the store until a new session appears for the requested app (or times out after 5 minutes). Claude can confirm success after the HAR is imported.

**Completion signal:** importer/watcher receives `200 {"status":"ok"}` from `/sessions/ingest`. MCP tool returns success when store write is confirmed.

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
Claude → get_saved_addresses(app) + get_saved_payment_methods(app)
    │
    ▼
User selects address + payment mode (saved/cod/upi_intent)
    │
    ▼
Claude → place_order(app, product_id, address_id, payment_mode, payment_id/payment_token)
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
Step 3: platforms/{app}.Pay(checkout_id, payment_token_for_mode)
    │  → returns types.Order{ID, Status, ETA}
    ▼
MCP tool returns: order_id, platform, payment_mode, total_price, eta
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

### Proxyman HAR Importer → VPS Ingest Endpoint

`cmd/proxyman-import` (one-shot) and `cmd/proxyman-watch` (folder automation) parse Proxyman HAR exports and POST normalized session JSON to `POST /sessions/ingest` on the MCP server (plain HTTP on `MCP_PORT` in the current repo; use external TLS termination/reverse proxy if you require HTTPS). The ingest handler validates `X-Ingest-Secret`, constructs an `AppSession`, and writes it to store. `capture_session` MCP tool polls the store (short-sleep loop, 5-minute timeout) and returns when the new session appears.

### MCP Tool → Platform Client

Every platform call goes through the `Platform` interface. Handlers never call platform HTTP directly. This allows platform clients to be swapped or mocked in tests without changing handler logic.

MCP clients should use `POST /mcp` with `initialize`, `tools/list`, `tools/call` (and optional `ping`).  
`GET /mcp` and `OPTIONS /mcp` are also supported for connector probes/preflight.  
`POST /rpc` direct tool-name methods are still available for backward compatibility.

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
| Capture timeout (>5 min) | `capture_session` returns `ErrCaptureTimeout`. No server-side cleanup needed. |
| Network error during order | Do NOT retry pay step — risk of double charge. Return error. |

**Payment is never retried.** If `Pay()` returns a network error, surface it to Claude and let the user decide.

---

## Concurrency Model

- `compare_prices` fans out to N platforms via goroutines with `errgroup`
- Each platform call has a 10-second context timeout
- Token refresh uses a per-app mutex to prevent concurrent refresh races
- `capture_session` polls store in a loop (no goroutine or channel needed — HAR importer drives completion)
- MCP server handles each tool call in its own goroutine

---

*Last updated: 2026-02-27 — Stage 13: automated HAR ingest loop + app auto-detection*

*See also: [ARCHITECTURE.md](ARCHITECTURE.md) for layering | [PLATFORMS.md](PLATFORMS.md) for API details | [TOKENS.md](TOKENS.md) for token lifecycle*
