# Implementation Plan: OAuth 2.0 (XOAUTH2)

Tasks, acceptance criteria, and verification steps: [oauth-todo.md](oauth-todo.md).

## Overview

Add first-class OAuth 2.0 (`XOAUTH2`) authentication for Microsoft 365 / Outlook.com and Google accounts alongside existing `password_cmd` (1Password / external CLI) and `keychain` password authentication. Users bring their own `oauth_client_id` (requiring no vendor Google CASA audit or Microsoft publisher verification), sign in from the built-in Connect/Edit popup via Loopback Authorization Code + PKCE (`127.0.0.1:0`) or Microsoft RFC 8628 Device Code, and store refresh/access token bundles in the OS keychain with automatic refresh across IMAP reconnects and SMTP sends.

## Architecture Decisions

- **Three credential modes (`oauth2`, `password_cmd`, `keychain`):** `popup.Connect` expands the credential source selector to `[1Password | Keychain | OAuth 2.0]` (plus `Keep current` when editing). `config.Account` adds `auth = "password" | "oauth2"`, `oauth_provider = "microsoft" | "google"`, `oauth_client_id`, `oauth_tenant` (default `"common"`), and `oauth_client_secret_cmd` (rejecting plaintext `oauth_client_secret`). Pairing `auth = "oauth2"` with `password_cmd` in `config.toml` also allows external token tools (`oama`, `mutt_oauth2.py`).
- **Namespaced OS keychain entries (`internal/secret`):** OAuth JSON token bundles (`{access_token, refresh_token, expiry}`) live under `oauth:<account>` and Google Desktop client secrets live under `oauth-client-secret:<account>`, keeping existing password entries separate and safe on rename/delete.
- **Stdlib OAuth flows + rotated refresh-token persistence (`internal/oauth`):** Implemented with Go's standard library (`net/http`, `crypto/sha256`, `encoding/json`) — zero new module dependencies. `TokenSource` serializes refreshes per account, refreshes within 2 minutes of expiry, immediately writes rotated Microsoft `refresh_token` values back to the keychain before returning, and scrubs tokens from errors.
- **Custom `XOAUTH2` `sasl.Client` + dynamic per-session auth seam:** `emersion/go-sasl` does not implement `XOAUTH2` (required by Exchange Online). A dedicated `XOAUTH2` client handles the 2-step base64 JSON error challenge (`+ <json>` → empty `\r\n` response) cleanly for `go-imap/v2` `Authenticate` and `go-smtp` `Auth`. `imap.Client.handshake()` and `smtp.Transport.session()` invoke a dynamic credential callback on every dial, reconnect, send, and verify instead of resolving a static password once at startup.
- **Microsoft 365 presets & `save_sent` default:** Outlook/M365 addresses auto-fill `outlook.office365.com:993` (`implicit`) and `smtp.office365.com:587` (`starttls`). `Account.SaveSentCopy()` defaults to `false` when `save_sent` is unset for Outlook/Office365 hosts as well as `imap.gmail.com` so Exchange Online does not create duplicate copies in `Sent Items`.

## Task List

### Phase 1: Foundation (Config, Keychain, Token Flows)
- [x] Task 1: OAuth account config schema and Exchange `save_sent` default
- [x] Task 2: Namespaced keychain storage for OAuth token bundles and client secrets
- [x] Task 3: OAuth 2.0 PKCE loopback, Device Code, and refreshing `TokenSource`

### Checkpoint: Foundation
- [ ] `go test -race ./internal/config ./internal/secret ./internal/oauth` passes; `go vet ./...` clean

### Phase 2: Transport (`XOAUTH2` SASL & Dynamic Reconnect Seam)
- [x] Task 4: `XOAUTH2` SASL client with 2-step error challenge handling
- [x] Task 5: Dynamic per-session credential resolution in IMAP, SMTP, and root wiring

### Checkpoint: Transport
- [ ] `go test -race ./internal/oauth ./internal/mail/... ./cmd/dk-email` passes; reconnect refreshes expired tokens

### Phase 3: Onboarding UI & Documentation
- [x] Task 6: `OAuth 2.0` option and provider presets in `popup.Connect`
- [x] Task 7: Connect/Edit OAuth sign-in, keychain rename/delete, and re-auth hints
- [x] Task 8: OAuth 2.0 and credential mode documentation

### Checkpoint: Complete
- [ ] `go test -race ./...`, `go vet ./...`, `gofmt -l .`, and `go build -o bin/dk-email ./cmd/dk-email` clean
- [ ] All acceptance criteria met; reviewed by `@architect`

## Dependency Graph

```
Task 1 (config) ────┬──► Task 3 (flows & TokenSource) ──┐
Task 2 (keychain) ──┘                                    ├──► Task 5 (dynamic IMAP/SMTP auth) ──┐
Task 4 (XOAUTH2) ────────────────────────────────────────┘                                      ├──► Task 7 (UI sign-in/edit) ──► Task 8 (docs)
Task 1 ────────────────────────────────────────────────────► Task 6 (popup.Connect OAuth UI) ──┘
```

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| `XOAUTH2` failure stalls `go-imap/v2` `Authenticate` if client aborts before empty `\r\n` | High | Task 4 returns `[]byte{}, nil` on the first challenge and fails after server `NO`/`535`; tested in Tasks 4–5. |
| Microsoft Entra ID rotates `refresh_token` on refresh; lost write invalidates login | High | Per-account mutex in `TokenSource` persists updated token JSON to keychain immediately before returning (Task 3). |
| `smtp.office365.com` rejects implicit TLS on port 465 | Med | Outlook/M365 preset sets `smtp_port = 587` and `smtp_tls = "starttls"` (Task 6). |
| Duplicate sent messages in Exchange `Sent Items` | Med | `SaveSentCopy()` defaults to `false` for Outlook/Office365 hosts when `save_sent` is unset (Task 1). |
| Personal Google Cloud project in `Testing` mode expires refresh tokens after 7 days | Med | Document `Internal` vs unverified `Published` vs App Passwords in `docs/accounts.md` (Task 8) and show `press E` re-auth hint on `invalid_grant` (Task 7). |

## Open Questions

None.
