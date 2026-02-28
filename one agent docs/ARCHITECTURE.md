# ARCHITECTURE.md

> Source of truth for structural decisions. Read before touching imports or adding packages.

---

## Domain Map

```
cmd/
├── server/          # MCP server entrypoint
├── refresher/       # Token refresh daemon (run by systemd timer)
├── proxyman-import/ # One-shot Proxyman HAR → /sessions/ingest importer CLI
└── proxyman-watch/  # Directory watcher: auto-ingest HAR exports as they appear

internal/
├── types/         # Shared structs — no internal imports
├── config/        # Env + config loading
├── store/         # AES-256-GCM encrypted SQLite token store
├── platforms/     # Per-platform API clients
│   ├── blinkit/
│   ├── zepto/
│   └── instamart/
├── mcp/           # MCP protocol + tool handlers + /sessions/ingest endpoint
├── proxyman/      # Proxyman HAR parser + ingest payload builder/uploader
└── refresh/       # Token refresh logic

deploy/            # systemd service + timer units
  └── hostinger/   # Hostinger-focused bootstrap + systemd units (server + browser worker)
data/              # sessions.db (gitignored)
docs/              # This directory — source of truth
tests/             # Structural + integration tests
```

---

## Dependency Layer Rules

```
types → config → store → platforms → mcp → cmd
```

| Layer | Can Import | Cannot Import |
|---|---|---|
| `types` | stdlib only | anything in this repo |
| `config` | types, stdlib | store, platforms, mcp, cmd |
| `store` | types, config, stdlib | platforms, mcp, cmd |
| `platforms/*` | types, config, store | mcp, cmd, other platforms |
| `mcp` | types, config, store, platforms, refresh | cmd |
| `refresh` | types, config, store, platforms | mcp, cmd |
| `cmd/*` | anything | — |

**Enforcement:** `tests/arch_test.go` parses import graphs and fails CI on violations.

---

## Package Responsibilities

### `internal/types`
All shared data structs. No business logic. No imports from this repo.

Key types:
- `AppSession` — captured session with tokens, headers, addresses, payments
- `Product` — search result from any platform
- `Order` — placed order with ID, ETA, status
- `Address` — saved delivery address
- `PaymentMethod` — saved card or UPI mandate token

### `internal/config`
Loads from environment variables at startup. Validates required fields. Panics on missing critical config (fail fast).

Key config:
- `STORE_MASTER_KEY` — 32-byte hex, AES-256 master key
- `MCP_PORT` — default 8080
- `INGEST_SECRET` — shared secret for `X-Ingest-Secret` header; rejects POST /sessions/ingest without it
- `TOKEN_REFRESH_INTERVAL` — default 4h
- `INGEST_RATE_LIMIT_MAX` — default 10 requests per window (set `<=0` to disable)
- `INGEST_RATE_LIMIT_WINDOW` — default `1m` fixed window for `/sessions/ingest`

### `internal/store`
Single SQLite file at `data/sessions.db`. All values AES-256-GCM encrypted at rest. Master key from `config.STORE_MASTER_KEY`.

Operations: `Get(app)`, `Set(app, session)`, `Delete(app)`, `List()`, `Backup()`

### `internal/platforms/*`
One package per platform. Each implements the `Platform` interface defined in `internal/platforms/platform.go`:

```go
type Platform interface {
    Search(ctx context.Context, query string, lat, lng float64) ([]types.Product, error)
    AddToCart(ctx context.Context, productID string, qty int) (string, error)
    Checkout(ctx context.Context, cartID, addressID string) (CheckoutResult, error)
    Pay(ctx context.Context, checkoutID, paymentToken string) (types.Order, error)
    RefreshToken(ctx context.Context, refreshToken string) (newAccess, newRefresh string, expiresAt time.Time, err error)
    OrderStatus(ctx context.Context, orderID string) (status string, etaMinutes int, err error)
}

type CheckoutResult struct {
    CheckoutID  string
    DeliveryFee int // rupees
    ETAMinutes  int
}
```

Shared infrastructure lives in `internal/platforms/base.go` (`BaseClient`: rate limiter, device header injection, session loading with inline refresh). Each platform client embeds `*BaseClient` via a named field.

Each platform client reads its session from `store` on every call and validates token expiry before sending requests.

### `internal/mcp`
Implements the MCP protocol. Exposes tools as JSON-RPC handlers. Each handler validates inputs, calls platform clients, and returns structured responses. See `docs/MCP_TOOLS.md` for full tool specs.

Transport endpoints:
- `POST /mcp` — standards-style MCP JSON-RPC flow (`initialize`, `tools/list`, `tools/call`, `ping`) with single and batch request support
- `GET /mcp` — lightweight probe payload (`{"status":"ok","transport":{"type":"jsonrpc-http"}}`) for connector reachability checks
- `OPTIONS /mcp` — CORS/preflight response for remote connector probes
- `GET /sse` + `POST /messages?sessionId=...` — legacy SSE compatibility transport for older MCP clients
- `POST /rpc` — legacy direct JSON-RPC method calls where method name is the tool name
- `GET /health` — health check

Also registers `POST /sessions/ingest` on the same HTTP server. This endpoint:
- Validates `X-Ingest-Secret` header against `config.INGEST_SECRET` — rejects 401 without it
- Applies per-client rate limiting (returns 429 when exceeded; default `10/min`, configurable via env)
- Accepts `{ app, access_token, refresh_token, token_expires_at, device_headers, address_ids, payment_tokens }`
- Encrypts and writes `AppSession` to store
- Used by `cmd/proxyman-import` and `cmd/proxyman-watch` after parsing Proxyman HAR exports

### `internal/proxyman`
Parses Proxyman HAR exports (manual iPhone capture) and extracts:
- `Authorization: Bearer ...` or Blinkit `access_token` request header → `access_token`
- auth response body fields → `refresh_token` + token expiry (or TTL fallback)
- Blinkit `auth_key` request header → `refresh_token` fallback when no auth response body is present in the HAR
- request headers (`x-*`, `user-agent`, `accept-language`) → `device_headers`
- address/payment response bodies (best-effort heuristics) → `address_ids`, `payment_tokens`

Also provides:
- app detection helpers (`DetectApp*`) using file name + HAR host/path hints
- an HTTP uploader used by `cmd/proxyman-import` and `cmd/proxyman-watch` to POST extracted payloads to `/sessions/ingest` with `X-Ingest-Secret`
`ParseHARWithDiagnostics` and `proxyman-import -diagnostics` provide redacted parser diagnostics (counts + top paths) for tuning real Proxyman exports without exposing token values.

### `internal/refresh`
Calls `Platform.RefreshToken()` for each stored session. Updates store with new tokens. Called by `cmd/refresher` and also available as a background goroutine in `cmd/server`.

---

## Structural Tests

`tests/arch_test.go` enforces:
- Dependency layer rules (import graph parsing)
- File size limits (max 300 lines)
- Function size limits (max 50 lines, heuristic)
- Naming conventions (struct, function, variable naming)
- No raw `fmt.Println` in non-cmd packages
- No token values in log statements (regex scan)

Run with: `go test ./tests/...`

---

## Key Design Decisions

### Why Go?
Single binary, easy cross-compilation for VPS deployment, strong concurrency primitives for parallel platform calls, excellent stdlib for HTTP and crypto.

### Why SQLite over Postgres/Redis?
Zero infrastructure dependency. Single file. Sufficient for personal use with one user. Easy to backup. AES-256 encryption handled at application layer.

### Why Proxyman HAR import instead of a custom iOS app?
It is much faster to get to a working capture flow. Proxyman already handles iPhone HTTPS interception, certificate trust, and traffic export. We only need small ingestion CLIs to parse HAR and POST a normalized payload to `/sessions/ingest`. The tradeoff is that iPhone capture/export is still manual, but ingestion can now be automated with `cmd/proxyman-watch`.

### Why not store tokens in iOS Keychain?
The Go binary runs on VPS, not on iPhone. Tokens are captured once and must persist server-side. AES-256-GCM encrypted SQLite with env-var master key is the equivalent security boundary.

---

---

*Last updated: 2026-02-27 — Stage 13 complete; automated HAR ingest loop via `cmd/proxyman-watch` + app auto-detection in importer*

*See also: [DESIGN.md](DESIGN.md) for component flows | [BELIEFS.md](BELIEFS.md) for operating principles*
