# Project Memory — one-agent

## What This Is
Silent quick commerce shopping agent for Blinkit, Zepto, Swiggy Instamart.
Go binary on Linux VPS → exposes MCP tools → places orders via Claude.
Session capture via Proxyman HAR export + importer CLI (`cmd/proxyman-import`) posting to `/sessions/ingest`.

## Repository Layout
- Docs: `/Users/nitinsinghmanhas/Desktop/one-agent/one agent docs/`
- Code root: `/Users/nitinsinghmanhas/Desktop/one-agent/`
- Module name: `one-agent`
- Go version: 1.24.0

## Layer Order (NEVER reverse)
```
types → config → store → platforms → mcp → cmd
```

## Completed Stages
- **Stage 1** (2026-02-26): types, config, store, tests/arch_test.go — all tests pass
- **Stage 2** (2026-02-26): platforms (blinkit/zepto/instamart) + refresh package + platform/refresh tests
- **Stage 3** (2026-02-26): proxy capturer package with completion gating, pagination merge, timeout/save behavior + proxy tests
- **Stage 4** (2026-02-26): MCP JSON-RPC handlers + cmd entrypoints (`server`, `refresher`) + MCP handler test suite
- **Stage 5** (2026-02-26): proxy runtime lifecycle (capture wait/timeout/busy), bridge ingestion endpoints, and proxy runtime tests
- **Stage 6** (2026-02-26): proxy transport adapter integration (`HTTPProxyTransport`) + end-to-end capture flow tests (HTTP path)
- **Stage 7** (2026-02-26): HTTPS CONNECT MITM transport path with dynamic CA-signed leaf certs + HTTPS capture flow tests
- **Architecture change** (2026-02-26): internal/proxy + wireguard REMOVED. Session capture moved to external ingest (`POST /sessions/ingest`) and `capture_session` polls store. New config: INGEST_SECRET.
- **Stage 8** (2026-02-26): /sessions/ingest tests (4 passing) + deploy/ systemd units (shopping-agent.service, token-refresher.service, token-refresher.timer)
- **Stage 9** (2026-02-26): Proxyman HAR importer (`internal/proxyman` + `cmd/proxyman-import`) + parser/uploader tests + docs migration from custom iOS app plan
- **Stage 10** (2026-02-26): `/sessions/ingest` hardening — per-client rate limiting (429) + validation error mapping (400) + ingest tests expanded
- **Stage 11** (2026-02-26): iPhone test-readiness — `proxyman-import -diagnostics` (redacted parse counts/top paths) + env-configurable ingest rate limits (`INGEST_RATE_LIMIT_MAX`, `INGEST_RATE_LIMIT_WINDOW`)
- **Live HAR tuning** (2026-02-27): Blinkit Proxyman capture uses `access_token` + `auth_key` request headers (not `Authorization` bearer). Importer now supports Blinkit header-token fallback for ingest.
- **Stage 12** (2026-02-27): standards-style MCP wrapper on `/mcp` (`initialize`, `tools/list`, `tools/call`) + new payment flow support (`get_saved_payment_methods`, `place_order.payment_mode`: `saved|cod|upi_intent`)
- **Stage 13** (2026-02-27): automated HAR ingest loop via `cmd/proxyman-watch` (watch dir → auto-parse/ingest/archive) + app auto-detection in importer (`DetectApp*`, `proxyman-import` no longer requires `-app` when detectable)
- **Stage 14** (2026-02-27): MCP transport compatibility hardening for remote connectors — JSON-RPC batch support, `GET /mcp` probe payload, `OPTIONS /mcp` preflight handling, `ping` method, and HTTP-level tests
- **Stage 15** (2026-02-27): legacy SSE transport compatibility — `GET /sse` session stream + `POST /messages?sessionId=...` dispatch path alongside existing `/mcp` JSON-RPC transport
- **Stage 16** (2026-02-27): Proxyman parser hardening — ignore `access_token/refresh_token/expiry` fields from non-auth routes to prevent payment-token contamination and false session-expired failures
- **Stage 17** (2026-02-27): Proxyman replay hardening for 403s — sort HAR entries by `startedDateTime` before extraction, keep latest device header values by time, and drop volatile transport headers (`host/connection/content-length/accept-encoding`), with cookie handling refined later in Stage 21
- **Stage 18** (2026-02-27): additional Blinkit 403 mitigation — override `lat/lon/cur_lat/cur_lon` from MCP `search_product` inputs for each Blinkit search request (early header-dropping strategy for `req_key/session_uuid` later refined)
- **Stage 19** (2026-02-27): MCP search stability — increased per-app search timeout from 10s to 20s to reduce transient `context deadline exceeded` failures on slower network/platform responses
- **Stage 20** (2026-02-27): Blinkit header strategy refinement — keep captured `session_uuid`, generate fallback `req_key` only when missing, keep auth field extraction limited to auth routes
- **Stage 21** (2026-02-27): Blinkit fingerprint coherence hardening — preserve captured `req_key`, normalize cookie header pairs from HAR, and prefer latest `/v1/layout/search` header snapshot to avoid mixed-session replay values
- **Stage 22** (2026-02-28): Blinkit web-session hardening — detect `app_client=consumer_web` sessions and route calls to `https://blinkit.com` (not `api2.grofers.com`), disable mobile-only `req_key/session_uuid` fallback injection for web sessions, and retain web replay headers (`platform`, `web_app_version`, `origin`, `referer`, `sec-*`) in HAR parser
- **Stage 23** (2026-02-28): Blinkit web anti-bot mitigation attempt — add HTTP cookie jar persistence in BaseClient and best-effort pre-search warmup calls (`/config/main`, `/v1/consumerweb/eta`, `/v1/layout/feed`) for `consumer_web` sessions
- **Stage 24** (2026-02-28): Blinkit browser fallback path — optional `BLINKIT_SEARCH_MODE=browser` uses Playwright helper (`scripts/blinkit-browser-search.mjs`) to fetch live Blinkit `/v1/layout/search` payloads from a persistent Chromium profile when replayed web sessions still hit fingerprint 403s
- **Stage 25** (2026-02-28): MCP search UX hardening — `search_product` now treats `apps` as optional (defaults to all platforms), avoiding invalid-params tool execution failures for natural-language prompts that omit app list
- **Stage 26** (2026-02-28): Browser helper resilience — added in-page `fetch` fallback for Blinkit search when network interception misses, and timeout errors now include helper output snippets for faster diagnosis
- **Stage 27** (2026-02-28): Browser bridge stderr/stdout split — Go bridge now parses helper JSON from stdout only and keeps stderr as diagnostics, preventing `invalid browser helper JSON` failures when `BLINKIT_BROWSER_DEBUG=true`
- **Stage 28** (2026-02-28): Persistent browser worker mode — helper now supports long-lived `--worker` HTTP server (`/health`, `POST /search`) and MCP bridge can reuse it via `BLINKIT_BROWSER_WORKER_URL`, eliminating per-search browser relaunches
- **Stage 29** (2026-02-28): VPS browser-worker robustness — Blinkit helper now detects challenge pages earlier, adds DOM product extraction fallback before in-page fetch, exposes worker `GET /status` diagnostics, and returns clearer `human_verification_required` paths during search/bootstrap
- **Stage 30** (2026-03-01): Blinkit helper rewritten — Playwright → puppeteer-extra + stealth + fingerprint injection (iOS Chrome mobile). Adds `BLINKIT_BROWSER_COOKIES_FILE` (inject iPhone cookies from Proxyman export) and `BLINKIT_CURL_IMPERSONATE` (direct API calls via curl-impersonate for correct TLS/JA3 fingerprint). `package.json` created at repo root. All env/CLI/output interfaces unchanged; Go bridge code unaffected.

## Key Files (Latest)
- `internal/types/types.go` — AppSession, Product, Order, Address, PaymentMethod, Platform
- `internal/config/config.go` — Load(), panics on missing STORE_MASTER_KEY; Stage 11 adds ingest rate-limit env parsing
- `internal/store/store.go` — AES-256-GCM SQLite; Get/Set/Delete/List/Backup
- `internal/store/store_test.go` — 6 passing unit tests
- `internal/platforms/base.go` — shared rate limiter + device header/auth request flow
- `internal/platforms/blinkit/blinkit.go` — Blinkit client implementation (same pattern used by Zepto/Instamart)
- `internal/platforms/blinkit/search.go` — Blinkit search orchestration (`browser` vs API path), non-2xx telemetry, payload decode
- `internal/platforms/blinkit/browser_bridge.go` — Stage 24 browser helper bridge (`BLINKIT_SEARCH_MODE=browser`) + helper output decoding
- `internal/platforms/blinkit/browser_worker.go` — Stage 28 local worker HTTP client (`BLINKIT_BROWSER_WORKER_URL`)
- `internal/refresh/refresh.go` — RefreshOne/RefreshAll with partial-failure continuation
- `internal/proxy/` — **DELETED** (entire package removed)
- `internal/mcp/tools_ingest.go` — POST /sessions/ingest handler (validateIngestSecret + writeIngestedSession)
- `internal/mcp/ingest_rate_limit.go` — Stage 10 per-client ingest rate limiter (fixed window; forwarded IP / remote addr keying)
- `internal/mcp/protocol.go` — Stage 12/14 MCP protocol wrapper (`initialize`, `tools/list`, `tools/call`, `ping`)
- `internal/mcp/tools_payments.go` — Stage 12 payment-mode helpers + saved-payment mapping/resolution
- `internal/mcp/sse.go` — Stage 15 SSE hub/session transport (`/sse`, `/messages`)
- `internal/proxyman/diagnostics.go` — Stage 11 redacted HAR parser diagnostics (counts + top paths)
- `internal/proxyman/detect.go` — Stage 13 app auto-detection (filename + HAR host/path hints)
- `internal/proxyman/*.go` — Stage 9/11 Proxyman HAR parser (tokens/headers/address/payment extraction) + diagnostics + ingest uploader
- `internal/mcp/server.go` — JSON-RPC dispatch + MCP method routing + batch handling + probe/preflight compatibility + ingest rate-limit option wiring
- `internal/mcp/tools_*.go` — handlers for session/auth/search/order tools
- `internal/mcp/tools_*_test.go` — Stage 4 tests for capture/list/refresh/search/compare/order/status
- `cmd/server/main.go` — MCP server entrypoint (no proxy, wires WithIngestSecret + WithIngestRateLimit; routes `/rpc`, `/mcp`, `/sse`, `/messages`)
- `cmd/refresher/main.go` — token refresh daemon entrypoint (single run)
- `cmd/proxyman-import/main.go` — Stage 9/11/13 CLI: parse Proxyman HAR, optional `-diagnostics`, auto app detection, POST `/sessions/ingest`
- `cmd/proxyman-watch/*.go` — Stage 13 CLI: directory watcher for automatic HAR ingest + processed/failed archiving
- `scripts/blinkit-browser-search.mjs` — Stage 24 Playwright helper for live Blinkit search payload capture from browser context
  (Stage 28 adds persistent `--worker` mode for background reuse)
- `tests/arch_test.go` — layer deps, file/fn size, no Println, no sensitive log keys
- `tests/arch_helpers_test.go` — helpers (split to stay under 300-line limit)

## Invariants (BELIEFS.md)
- STORE_MASTER_KEY: env var only, never disk, never logged
- INGEST_SECRET: env var only; X-Ingest-Secret header required on POST /sessions/ingest
- Token values: never logged (arch test scans for this)
- Payment Pay(): never retry on network error
- Rate limit: max 1 req/sec per platform
- File limit: 300 lines max, function limit: 50 lines max
- Store is single source of truth — no in-memory session cache

## Dependencies
- `modernc.org/sqlite v1.46.1` — pure Go SQLite (no CGo)
- stdlib only for types/config
