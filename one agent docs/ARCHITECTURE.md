# ARCHITECTURE.md

> Source of truth for structural decisions. Read before touching imports or adding packages.

---

## Domain Map

```
cmd/
├── server/        # MCP server + MITM proxy entrypoint
└── refresher/     # Token refresh daemon (run by systemd timer)

internal/
├── types/         # Shared structs — no internal imports
├── config/        # Env + config loading
├── store/         # AES-256-GCM encrypted SQLite token store
├── proxy/         # MITM proxy lifecycle + traffic capturer
├── wireguard/     # WireGuard config helpers (setup phase only)
├── platforms/     # Per-platform API clients
│   ├── blinkit/
│   ├── zepto/
│   └── instamart/
├── mcp/           # MCP protocol + tool handlers
└── refresh/       # Token refresh logic

deploy/            # systemd service + timer units
certs/             # Root CA cert + key (never commit ca.key)
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
| `proxy` | types, config, store | platforms, mcp, cmd |
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
- `PROXY_PORT` — default 8888
- `WIREGUARD_INTERFACE` — default wg0
- `TOKEN_REFRESH_INTERVAL` — default 4h

### `internal/store`
Single SQLite file at `data/sessions.db`. All values AES-256-GCM encrypted at rest. Master key from `config.STORE_MASTER_KEY`.

Operations: `Get(app)`, `Set(app, session)`, `Delete(app)`, `List()`, `Backup()`

### `internal/proxy`
MITM proxy using `github.com/elazarl/goproxy`. Binds to WireGuard interface during setup. Generates root CA on first run. Captures `AppSession` from live traffic. Shuts down after successful capture.

### `internal/platforms/*`
One package per platform. Each implements the `Platform` interface:

```go
type Platform interface {
    Search(ctx context.Context, query string, lat, lng float64) ([]types.Product, error)
    AddToCart(ctx context.Context, productID string, qty int) (string, error)
    Checkout(ctx context.Context, cartID, addressID string) (string, error)
    Pay(ctx context.Context, checkoutID, paymentToken string) (types.Order, error)
    RefreshToken(ctx context.Context, refreshToken string) (string, string, error)
    OrderStatus(ctx context.Context, orderID string) (string, error)
}
```

Each platform client reads its session from `store` on every call and validates token expiry before sending requests.

### `internal/mcp`
Implements the MCP protocol. Exposes tools as JSON-RPC handlers. Each handler validates inputs, calls platform clients, and returns structured responses. See `docs/MCP_TOOLS.md` for full tool specs.

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

### Why WireGuard for setup?
iPhone sits behind carrier NAT and changes IP constantly. WireGuard gives the iPhone a stable, predictable IP (10.0.0.2) that the MITM proxy can trust. Only needed during session capture — disabled afterward.

### Why not store tokens in iOS Keychain?
The Go binary runs on VPS, not on iPhone. Tokens are captured once and must persist server-side. AES-256-GCM encrypted SQLite with env-var master key is the equivalent security boundary.

---

*See also: [DESIGN.md](DESIGN.md) for component flows | [BELIEFS.md](BELIEFS.md) for operating principles*
