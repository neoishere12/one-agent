# BELIEFS.md

> Core operating principles for this repository.
> These are not guidelines — they are invariants. Every agent and every human follows them.

---

## 1. The Layered Architecture Is Not Negotiable

Dependencies flow strictly: `types → config → store → platforms → mcp → cmd`

No exceptions. No "just this once." If you think you need to break the layer rule, you need a design change, not a shortcut. Structural tests enforce this. If arch_test fails, fix the design.

---

## 2. Never Log Sensitive Values

Token values, payment tokens, device fingerprints, and address IDs are never logged — not at DEBUG, not in error messages, not in comments.

If you need to debug auth, log the last 4 characters of a token or its expiry time. Never the value.

The linter scans for patterns that would leak token values. It will block the commit.

---

## 3. Payment Is Never Retried on Network Error

If `Pay()` returns a network error, return it to the caller immediately. Do not retry. The platform may have processed the payment already. Double-charging is a worse outcome than a failed order. Surface the error to Claude; let the user decide.

---

## 4. Docs Are Code

Documentation in `docs/` is the source of truth. It is version-controlled, cross-linked, and mechanically checked for stale cross-references. If you change a tool signature, interface, or flow, update the relevant doc in the same commit.

A doc that doesn't match the code is a bug.

---

## 5. Personal Use Constraints Are Permanent

This system is for personal use only. It will never:
- Serve multiple users
- Resell or publish platform API data
- Send more than 1 request/second to any platform
- Be deployed as a public service

These constraints are not configuration options. They are design assumptions that simplify the entire system. If the use case changes, the design must be reconsidered from scratch.

---

## 6. Error Messages Teach the Fix

When linters or structural tests fail, the error message must tell the agent exactly how to fix it. Generic errors ("import violation") are not acceptable. Specific errors ("types imports store — reverse dependency, move shared struct to types package") are required.

If you add a lint rule, write the fix instruction in the error message.

---

## 7. Setup Is One-Time; Runtime Is Zero-Touch

The MITM proxy, WireGuard tunnel, and CA cert installation happen once. They are not part of the normal operation loop. Any change that requires re-running setup steps is a breaking change and needs a migration note in SETUP.md.

---

## 8. The Store Is the Single Source of Truth for Sessions

Platform clients read session data from the store on every call. Nothing is cached in memory across requests. Token refreshes are reflected immediately on the next call. No stale session can cause a silent failure.

---

## 9. Fail Fast, Surface Clearly

If config is missing at startup — panic. Don't start with defaults that silently break later.

If a token is expired and refresh fails — return `ErrSessionExpired` with the app name. Don't fall back to an unauthenticated call.

If the store can't be read — return an error. Don't proceed with an empty session.

Claude should always receive a clear, actionable error. "Something went wrong" is not acceptable.

---

## 10. Quality Grades Are Honest

`docs/QUALITY.md` tracks the real state of the codebase — gaps, missing tests, known fragile areas. It is not a marketing document. If a domain has poor test coverage, it says so. Honest quality tracking is how problems get fixed rather than ignored.

---

*Last updated: 2026-02-26*
