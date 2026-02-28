# TOKENS.md

> Token lifecycle, refresh strategy, and expiry handling.
> Read before touching auth, store, or refresh logic.

---

## Token Types

| Token | Lifetime (typical) | Stored Where | Refreshable |
|---|---|---|---|
| `access_token` | 7 days | AES-encrypted store | Yes, via refresh_token |
| `refresh_token` | 30–90 days | AES-encrypted store | No — triggers re-capture if expired |
| `payment_token` | Permanent (card token) | AES-encrypted store | N/A |
| `device_id` | Permanent | AES-encrypted store | N/A — freeze forever |

---

## Refresh Strategy

### Automatic (systemd timer)
Token refresh daemon runs every 4 hours via systemd timer. Refreshes all stored sessions proactively. Logs success/failure without logging token values.

```
systemd timer (4h) → cmd/refresher → refresh.RefreshAll() → store.Set()
```

### Inline (pre-flight in MCP handlers)
Every MCP tool handler checks token expiry before making any platform call:

```go
if session.ExpiresAt.Before(time.Now().Add(30 * time.Minute)) {
    session, err = refresh.RefreshOne(ctx, app)
    if err != nil {
        return ErrTokenExpired
    }
}
```

### Manual (via MCP tool)
Claude can call `refresh_tokens(app)` at any time. Useful after long idle periods.

---

## Expiry Handling Decision Tree

```
Token expires_at check:
    │
    ├── expires_at > now + 30min → proceed normally
    │
    ├── expires_at < now + 30min → refresh inline
    │       │
    │       ├── refresh succeeds → update store, proceed
    │       │
    │       └── refresh fails (401) → refresh_token expired
    │               │
    │               └── return ErrSessionExpired
    │                   Claude: "Blinkit session expired. Re-run capture_session('blinkit')."
    │
    └── expires_at is zero (never set) → treat as expired, refresh
```

---

## Security Rules

These are non-negotiable. Enforced by linter regex scans on every commit.

- **Never log access_token, refresh_token, or payment_token values** — not even at DEBUG level
- **Never include token values in error messages**
- **Never write raw tokens to disk** — always encrypt via store before persisting
- **Never pass tokens through environment variables at runtime** — only master key is in env
- **Never cache decrypted tokens in memory longer than one request** — read from store per call

---

## Re-capture Triggers

Re-capture (`capture_session`) is needed when:
- Refresh token expires (typically 30–90 days of inactivity)
- Platform forces logout (password change, suspicious activity)
- App major version update changes auth flow
- `list_sessions` shows `token_valid: false`

Re-capture does NOT require rebuilding the VPS. Re-open Proxyman on iPhone, capture the target app again, export HAR, then ingest via `proxyman-import` (one-shot) or `proxyman-watch` (automated folder loop).

---

## Encryption Details

- Algorithm: AES-256-GCM
- Key source: `STORE_MASTER_KEY` env var (32-byte hex)
- Nonce: random 12 bytes, prepended to ciphertext per field
- Each field encrypted independently (not the whole row)
- Master key never stored on disk — only in systemd environment file with `0600` permissions

---

*See also: [ARCHITECTURE.md](ARCHITECTURE.md) for store layer | [SETUP.md](SETUP.md) for initial capture*
