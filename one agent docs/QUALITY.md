# QUALITY.md

> Honest quality grades per domain. Known gaps. Coverage targets.
> Updated as the codebase evolves. Stale grades are worse than no grades.

---

## Grading Scale

| Grade | Meaning |
|---|---|
| A | Well-tested, documented, stable. Low risk to modify. |
| B | Adequate coverage. Some edge cases untested. Medium risk. |
| C | Minimal tests. Fragile areas known. High risk to modify without tests. |
| D | No meaningful tests. Known bugs. Do not modify without adding tests first. |
| — | Not yet built. |

---

## Domain Quality Grades

| Domain | Grade | Notes |
|---|---|---|
| `internal/types` | B | Built (Stage 1). Structs + Platform constants. `String()` redacts tokens/headers. No logic to unit-test. No gaps identified. |
| `internal/config` | C | Built (Stage 1). `Load()` + `mustMasterKey()` panic correctly. No unit tests yet — panic path not covered. |
| `internal/store` | B | Built (Stage 1). 6 unit tests passing: Set/Get roundtrip, overwrite, delete, list, not-found, backup. Missing: wrong-key decryption failure, tampered ciphertext, concurrent write safety. |
| `internal/proxy` | — | **Deleted.** Session capture now comes from Proxyman HAR imports (`cmd/proxyman-import`) into the VPS ingest endpoint in `internal/mcp`. |
| `internal/platforms` (base) | B | Built (Stage 2). BaseClient: rate limiter, device headers, session loading with inline refresh, CheckStatus. No unit tests for BaseClient directly — covered via platform client tests. |
| `internal/platforms/blinkit` | C+ | Built (Stage 2 + Stage 18/20/21/22/23/24/28 mitigation + browser bootstrap). All 6 Platform methods implemented + tests. Search overrides `lat/lon/cur_lat/cur_lon` request headers from MCP inputs. For web-captured sessions (`app_client=consumer_web`), Blinkit targets `https://blinkit.com`, skips mobile-only `req_key/session_uuid` fallback headers, and performs a best-effort warmup sequence (`/config/main`, `/v1/consumerweb/eta`, `/v1/layout/feed`) before search. Base HTTP client now persists cookies via cookie jar. Stage 24 adds optional browser-backed search (`BLINKIT_SEARCH_MODE=browser`) via Playwright bridge for persistent anti-bot/fingerprint failures; Stage 28 adds persistent worker reuse via `BLINKIT_BROWSER_WORKER_URL` to avoid per-search browser relaunch overhead. Browser mode search now runs without requiring a pre-ingested session, and `bootstrap_blinkit_web_session` can persist a Blinkit web session directly from the browser profile. For mobile-captured sessions, captured fingerprint headers are preserved and fallbacks apply only when absent. Endpoints are still partially reverse engineered; order/checkout payload contracts remain capture-dependent. |
| `internal/platforms/zepto` | C | Built (Stage 2). Same as blinkit — 6 methods + 6 tests. Endpoints PENDING CAPTURE. |
| `internal/platforms/instamart` | C | Built (Stage 2). Same pattern — 6 methods + 6 tests. Endpoints PENDING CAPTURE. Shared-auth quirk documented. |
| `internal/mcp` | B+ | Built. MCP protocol wrapper (`initialize`, `tools/list`, `tools/call`, `ping`) on `/mcp` + legacy `/rpc` direct mode. Transport compatibility includes batch JSON-RPC support, `GET /mcp` probe payload, `OPTIONS /mcp` preflight, HTTP 200 JSON-RPC error responses for method failures, and legacy SSE fallback (`GET /sse` + `POST /messages?sessionId=...`). Handlers cover session/search/order flows including `get_saved_payment_methods` and `place_order.payment_mode` (`saved|cod|upi_intent`). `/sessions/ingest` has constant-time secret validation and per-client rate limiting (429). |
| `internal/proxyman` | B+ | Built (Stage 9/11/13 + live-capture tuning + Stage 16/17/21/22 hardening). HAR parser + uploader now powers both one-shot import (`cmd/proxyman-import`) and folder automation (`cmd/proxyman-watch`). App auto-detection (filename + HAR host/path hints) added. `-diagnostics` prints redacted parser coverage (counts + top paths). Blinkit custom header token fallback (`access_token` + `auth_key`) included. Parser ignores auth fields from non-auth routes, orders entries by `startedDateTime`, normalizes malformed `Cookie` request headers into cookie-pairs only, preserves Blinkit `req_key`, prefers the latest `/v1/layout/search` header snapshot to avoid mixed-session fingerprint combinations, and retains key Blinkit-web headers (`platform`, `web_app_version`, `origin`, `referer`, `sec-*`) needed for web replay fidelity. Main risk: platform anti-bot behavior and export-shape drift. |
| `internal/refresh` | B | Built (Stage 2). RefreshOne + RefreshAll with partial-failure handling. 5 tests passing including partial-failure scenario. |
| `tests/arch_test.go` | A | Built (Stage 1). Covers: layer deps, cross-platform imports, file size (300L), function size (50L), no `fmt.Println` in internal, no sensitive log keys. Self-enforcing — caught its own 361-line violation during build. |

---

## Known Gaps (update as found)

### Critical (fix before first production use)
- [x] `store` — AES-GCM nonce reuse: resolved — `crypto/rand.Read` per write; collision probability 2^-96
- [ ] `platforms/*` — all endpoints marked PENDING CAPTURE in PLATFORMS.md
- [x] `mcp/handlers` — `place_order` enforces explicit confirmation (`confirm=true`)
- [x] `mcp/protocol` — standards-style MCP wrapper implemented (`initialize`, `tools/list`, `tools/call`)
- [x] `mcp` — `capture_session` rewritten: polls store every 2s, 5-minute timeout, no proxy lifecycle
- [x] `mcp` — `POST /sessions/ingest` built: constant-time `X-Ingest-Secret` validation, writes `AppSession` to store
- [ ] Proxyman live capture validation — verify Stage 13 automated HAR ingest flow on a real iPhone export for all 3 apps

### Important (fix within first sprint)
- [x] `refresh` — partial refresh failure handling is implemented and tested
- [ ] `store` — automated daily backup with retention policy
- [ ] All platform clients — 429 rate limit backoff implementation
- [x] `mcp/ingest` — constant-time `crypto/subtle` compare + per-client rate limiting implemented and tested
- [ ] `proxyman` — expand parser heuristics after first real captures (address/payment field variations per app)

### Nice to Have
- [ ] Integration tests against mock platform APIs
- [ ] Prometheus metrics endpoint on MCP server
- [ ] `list_sessions` — surface CA cert expiry date alongside token expiry
- [x] `proxyman-import` — redacted extraction diagnostics added (`-diagnostics`: counts + top paths) for HAR tuning

---

## Coverage Targets

| Package | Current | Target |
|---|---|---|
| `internal/store` | ~70% | 90% — missing: wrong-key, tampered ciphertext, concurrent writes |
| `internal/config` | 0% | 60% — add panic path tests with subprocess (`os.Exit` pattern) |
| `internal/proxy` | — | Deleted — no longer applicable |
| `internal/platforms/blinkit` | ~60% | 80% — endpoints will need re-testing after CAPTURE; add 401/403 response tests |
| `internal/platforms/zepto` | ~60% | 80% — same as blinkit |
| `internal/platforms/instamart` | ~60% | 80% — same as blinkit |
| `internal/mcp/handlers` | ~92% | 94% — MCP wrapper tests + HTTP-level contract tests added (`GET/OPTIONS`, batch requests, JSON-RPC method-error response behavior). Remaining gap: pagination cursor support coverage and streamable response semantics under long-running calls. |
| `internal/proxyman` | ~80% | 90% — add real HAR fixtures (base64 bodies, nested payment schemas, alternate expiry fields) |
| `internal/refresh` | ~80% | 85% — missing: store write failure after successful refresh |
| `tests/arch_test.go` | 100% | 100% — all layer rules enforced |

---

## Fragile Areas (proceed with extra care)

**`internal/proxyman` HAR extractor heuristics** — The importer infers tokens/addresses/payments from exported JSON using generic heuristics. Platform API shape changes or Proxyman export format differences can break extraction. Validate against real captures when app versions change.

**`platforms/*/client.go` (all three)** — Endpoints, request shapes, and device header requirements are reverse engineered. They are not documented by the platforms and will change without notice.

**Token refresh flow** — If a refresh_token expires and re-capture is needed, the system degrades silently until `list_sessions` is checked. Consider adding a proactive alert mechanism.

---

*Last updated: 2026-02-28 — Blinkit browser-backed search fallback (Playwright bridge) + docs/env updates | Update this file whenever a new gap is found or a grade changes*
