# AGENTS.md

> This file is the **map**, not the encyclopedia.
> All detailed knowledge lives in `docs/`. Start here, go deeper there.

---

## What This Project Is

A silent quick commerce shopping agent. You call MCP tools via Claude. Orders are placed on Blinkit, Zepto, and Swiggy Instamart without ever opening an app. A Go binary runs 24/7 on a Linux VPS.

**One-line purpose:** Natural language → order placed → confirmation returned. Zero app interaction after setup.

---

## Repository Knowledge Base

| Document | What It Covers |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Domain map, package layering, dependency rules |
| [docs/DESIGN.md](docs/DESIGN.md) | HLD, component responsibilities, data flows |
| [docs/SETUP.md](docs/SETUP.md) | One-time VPS + Proxyman iPhone capture + HAR import setup guide |
| [docs/PLATFORMS.md](docs/PLATFORMS.md) | Per-platform API contracts, endpoint map, known quirks |
| [docs/TOKENS.md](docs/TOKENS.md) | Token lifecycle, refresh strategy, expiry handling |
| [docs/MCP_TOOLS.md](docs/MCP_TOOLS.md) | All exposed MCP tool specs, inputs, outputs, error contracts |
| [docs/QUALITY.md](docs/QUALITY.md) | Quality grades per domain, known gaps, coverage targets |
| [docs/BELIEFS.md](docs/BELIEFS.md) | Core agent-first operating principles for this repo |

---

## Layered Architecture (Enforce This)

Dependencies flow **strictly** in this direction. Never reverse.

```
types → config → store → platforms → mcp → cmd
```

- `types` — shared structs, no imports from this repo
- `config` — env/config loading, imports types only
- `store` — encrypted SQLite, imports types + config
- `platforms` — per-app API clients, imports store + types
- `mcp` — tool handlers + `/sessions/ingest` endpoint, imports platforms + store
- `cmd` — entrypoints only, imports mcp

Violations are caught by structural tests in `tests/arch_test.go`. **Do not bypass.**

---

## Naming Conventions (Enforced by Linter)

- Structs: `PascalCase` — e.g. `AppSession`, `PaymentMethod`
- MCP tools: `snake_case` — e.g. `place_order`, `capture_session`
- DB columns: `snake_case`
- Errors: always wrap with `fmt.Errorf("context: %w", err)`
- All loggers: structured (`slog`), never `fmt.Println`

---

## File Size Limits

- No file over **300 lines**. Split by responsibility if approaching limit.
- No function over **50 lines**. Extract helpers.

---

## Critical Constraints

- **Never** log token values, payment tokens, or device headers — even at DEBUG level
- **Never** store the master encryption key on disk — env var only
- **Never** call platform APIs without device headers — requests will be rejected
- **Always** validate token expiry before any API call — refresh if `expires_at < now + 30min`
- **Always** rate-limit outbound requests — max 1 req/sec per platform

---

## Running Locally

```bash
cp .env.example .env        # fill in STORE_MASTER_KEY
go run ./cmd/server         # starts MCP server on :8080
go run ./cmd/refresher      # runs token refresh once then exits
go run ./cmd/proxyman-watch -dir ./tmp-har -dry-run -diagnostics  # auto-parse local HAR drop folder
go test ./...               # runs all tests including arch tests
```

---

## When Something Breaks

1. Check `docs/QUALITY.md` for known gaps in the failing domain
2. Check `docs/PLATFORMS.md` for known API quirks
3. If a token 401 — run `refresh_tokens()` tool, check `docs/TOKENS.md`
4. If arch test fails — read `docs/ARCHITECTURE.md` before changing imports
5. If linter fails — error message contains fix instructions, read them

---

*Last verified: 2026-02-27 — automated HAR ingest loop (`proxyman-watch`) + importer app auto-detection | Maintained by agents + Nitin*
