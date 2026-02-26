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
| `internal/types` | — | Not built yet |
| `internal/config` | — | Not built yet |
| `internal/store` | — | Not built yet — encryption logic needs thorough unit tests |
| `internal/proxy` | — | Not built yet — capturer logic is the highest-risk component |
| `internal/platforms/blinkit` | — | Not built yet — endpoints unverified (see PLATFORMS.md) |
| `internal/platforms/zepto` | — | Not built yet — endpoints unverified |
| `internal/platforms/instamart` | — | Not built yet — endpoints unverified |
| `internal/mcp` | — | Not built yet |
| `internal/refresh` | — | Not built yet |
| `tests/arch_test.go` | — | Not built yet — must be first test written |

---

## Known Gaps (update as found)

### Critical (fix before first production use)
- [ ] `store` — AES-GCM nonce reuse check: verify nonces are never reused across writes
- [ ] `platforms/*` — all endpoints marked PENDING CAPTURE in PLATFORMS.md
- [ ] `proxy/capturer` — session completion detection: define minimum required fields before emitting Done signal
- [ ] `mcp/handlers` — `place_order` confirmation: Claude must confirm with user before this tool is called

### Important (fix within first sprint)
- [ ] `refresh` — handle partial refresh failure (some apps succeed, some fail)
- [ ] `store` — automated daily backup with retention policy
- [ ] All platform clients — 429 rate limit backoff implementation
- [ ] `proxy` — capturer must handle apps that paginate address/payment responses across multiple requests

### Nice to Have
- [ ] Integration tests against mock platform APIs
- [ ] Prometheus metrics endpoint on MCP server
- [ ] `list_sessions` — surface CA cert expiry date alongside token expiry

---

## Coverage Targets

| Package | Current | Target |
|---|---|---|
| `internal/store` | 0% | 90% — encryption logic must be exhaustively tested |
| `internal/proxy/capturer` | 0% | 80% — session extraction logic is high risk |
| `internal/platforms/*` | 0% | 70% — mock HTTP responses for each endpoint |
| `internal/mcp/handlers` | 0% | 75% — happy path + all error cases |
| `internal/refresh` | 0% | 85% — token lifecycle coverage |
| `tests/arch_test.go` | 0% | 100% — must cover all layer rules |

---

## Fragile Areas (proceed with extra care)

**`proxy/capturer.go`** — The session capturer parses live HTTPS traffic. Platform apps change their API shapes with updates. Any app update can break capture. Monitor for changes.

**`platforms/*/client.go` (all three)** — Endpoints, request shapes, and device header requirements are reverse engineered. They are not documented by the platforms and will change without notice.

**Token refresh flow** — If a refresh_token expires and re-capture is needed, the system degrades silently until `list_sessions` is checked. Consider adding a proactive alert mechanism.

---

*Last updated: 2026-02-26 | Update this file whenever a new gap is found or a grade changes*
