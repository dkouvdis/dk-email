# OAuth 2.0 (XOAUTH2): Tasks

Plan: [oauth-plan.md](oauth-plan.md).

---

## Task 1: OAuth account config schema and Exchange `save_sent` default

**Description:** Extend `config.Account` to support `auth = "password" | "oauth2"`, `oauth_provider = "microsoft" | "google"`, `oauth_client_id`, `oauth_tenant` (defaulting to `"common"` for Microsoft), and `oauth_client_secret_cmd`, while preserving comment-safe `AppendAccount` / `ReplaceAccount` serialization and defaulting `SaveSentCopy()` to `false` on Microsoft 365 / Outlook hosts.

**Acceptance criteria:**
- [x] `config.Load` accepts `auth = "oauth2"` with `oauth_provider` (`"microsoft"` or `"google"`) + `oauth_client_id` (or `auth = "oauth2"` paired with `password_cmd`), rejects plaintext `oauth_client_secret` and unknown keys, and leaves existing password configs unchanged.
- [x] `AppendAccount` and `ReplaceAccount` round-trip OAuth fields without losing surrounding comments or re-encoding unrelated `[[account]]` blocks.
- [x] `Account.SaveSentCopy()` defaults to `false` when `save_sent` is unset for `outlook.office365.com`, `imap-mail.outlook.com`, `smtp.office365.com`, and `smtp-mail.outlook.com` (in addition to `imap.gmail.com`), and respects explicit `save_sent = true|false`.

**Verification:**
- [x] Tests pass: `go test -race ./internal/config`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: Existing configs without `auth` load unchanged.

**Dependencies:** None

**Files likely touched:**
- `internal/config/config.go`
- `internal/config/append.go`
- `internal/config/config_test.go`
- `internal/config/append_test.go`
- `internal/config/edit_test.go`

**Estimated scope:** Medium: 3-5 files

---

## Task 2: Namespaced keychain storage for OAuth token bundles and client secrets

**Description:** Add keychain helpers in `internal/secret` to store, load, and delete JSON OAuth token bundles (`oauth:<account>`) and Google client secrets (`oauth-client-secret:<account>`) without colliding with existing password keychain entries, plus `oauth_client_secret_cmd` execution.

**Acceptance criteria:**
- [x] `secret` provides typed load/save/delete helpers for OAuth token bundles (`access_token`, `refresh_token`, `expiry`) and OAuth client secrets under distinct account-scoped keys, plus `ResolveClientSecret` supporting `oauth_client_secret_cmd` (60s timeout) or keychain fallback.
- [x] Deleting a missing OAuth keychain entry succeeds idempotently, matching `secret.Delete`.
- [x] Existing password keychain entries (`Resolve`/`SetPassword`/`DeletePassword`) remain byte-for-byte compatible.

**Verification:**
- [x] Tests pass: `go test -race ./internal/secret`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: None (unit-tested with mock keyring backend).

**Dependencies:** None

**Files likely touched:**
- `internal/secret/secret.go`
- `internal/secret/secret_test.go`

**Estimated scope:** Small: 1-2 files

---

## Task 3: OAuth 2.0 PKCE loopback, Device Code, and refreshing `TokenSource`

**Description:** Create stdlib-only `internal/oauth` implementing Microsoft Entra v2.0 and Google provider endpoints, interactive Loopback Authorization Code + PKCE (`127.0.0.1:0`) and Microsoft RFC 8628 Device Code sign-in flows, and a mutex-serialized `TokenSource` that refreshes expiring tokens and persists rotated refresh tokens.

**Acceptance criteria:**
- [x] Loopback PKCE S256 (`127.0.0.1:0`) and Microsoft RFC 8628 Device Code flows exchange authorization grants against `httptest` OAuth servers, validate `state`, and abort cleanly on context cancellation.
- [x] `TokenSource.Token(ctx)` returns cached valid tokens, refreshes when within 2 minutes of expiry, immediately persists rotated `refresh_token` responses via the store callback, and delegates to an external command callback when `password_cmd` is configured with `auth = "oauth2"`.
- [x] Error strings scrub `access_token`, `refresh_token`, and `client_secret` values, and wrap `invalid_grant` with a sentinel/detectable error for re-authentication hints.

**Verification:**
- [x] Tests pass: `go test -race ./internal/oauth`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: Confirm test error strings redact all token/secret substrings.

**Dependencies:** Task 1, Task 2

**Files likely touched:**
- `internal/oauth/provider.go`
- `internal/oauth/flow.go`
- `internal/oauth/token.go`
- `internal/oauth/oauth_test.go`

**Estimated scope:** Medium: 3-5 files

---

## Checkpoint: After Tasks 1-3 (Foundation)
- [ ] All tests pass: `go test -race ./internal/config ./internal/secret ./internal/oauth`
- [ ] Application builds cleanly: `go vet ./... && gofmt -l . && go build -o bin/dk-email ./cmd/dk-email`

---

## Task 4: `XOAUTH2` SASL client with 2-step error challenge handling

**Description:** Implement a `sasl.Client` for the `XOAUTH2` mechanism used by Exchange Online and Gmail on both IMAP and SMTP, properly completing the 2-step base64 JSON error challenge required by `go-imap/v2` `Authenticate` and `go-smtp` `Auth`.

**Acceptance criteria:**
- [ ] `Start()` returns mechanism `"XOAUTH2"` and initial response `"user=" + email + "\x01auth=Bearer " + token + "\x01\x01"`.
- [ ] On a server error challenge, `Next(challenge)` records the decoded JSON status/error without leaking the bearer token and returns `[]byte{}, nil` once so the transport sends the required empty `\r\n` response before reading tagged `NO` / `535`.
- [ ] Unexpected subsequent challenges fail cleanly with an error instead of looping.

**Verification:**
- [ ] Tests pass: `go test -race ./internal/oauth -run TestXOAuth2`
- [ ] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [ ] Manual check: None (protocol exchange verified in Task 5 transport tests).

**Dependencies:** None

**Files likely touched:**
- `internal/oauth/xoauth2.go`
- `internal/oauth/xoauth2_test.go`

**Estimated scope:** Small: 1-2 files

---

## Task 5: Dynamic per-session credential resolution in IMAP, SMTP, and root wiring

**Description:** Support dynamic per-connection credential callbacks in `imap.Client.handshake()` and `smtp.Transport.session()`, enabling transparent OAuth access-token refresh across IMAP reconnects and subsequent SMTP sends while preserving `LOGIN` / `PLAIN` for password accounts.

**Acceptance criteria:**
- [ ] `imap.Client.handshake()` resolves fresh credentials on initial dial and on automatic reconnect, calling `conn.Login` for password accounts and `conn.Authenticate(xoauth2)` for OAuth accounts (surfacing any recorded `XOAUTH2` challenge detail alongside the server's `NO` error).
- [ ] `smtp.Transport.session()` resolves fresh credentials on every `Send` and `Verify`, calling `sasl.NewPlainClient` for password accounts and `XOAUTH2` for OAuth accounts.
- [ ] `cmd/dk-email/main.go` wires `oauth.TokenSource` for `auth = "oauth2"` accounts and preserves existing `secret.Resolve` behavior for `password_cmd` and `keychain` accounts.

**Verification:**
- [ ] Tests pass: `go test -race ./internal/mail/... ./cmd/dk-email`
- [ ] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [ ] Manual check: Existing password/1Password test paths continue passing unchanged.

**Dependencies:** Task 3, Task 4

**Files likely touched:**
- `internal/mail/imap/client.go`
- `internal/mail/imap/client_test.go`
- `internal/mail/smtp/smtp.go`
- `internal/mail/smtp/smtp_test.go`
- `cmd/dk-email/main.go`

**Estimated scope:** Medium: 3-5 files

---

## Checkpoint: After Tasks 4-5 (Transport)
- [ ] All tests pass: `go test -race ./internal/oauth ./internal/mail/... ./cmd/dk-email`
- [ ] Reconnect after token expiry acquires a refreshed bearer token cleanly
- [ ] `go vet ./...` and `gofmt -l .` clean

---

## Task 6: `OAuth 2.0` option and provider presets in `popup.Connect`

**Description:** Expand `popup.Connect` so users can choose between `[1Password | Keychain | OAuth 2.0]` (plus `Keep current` in edit mode), auto-fill Microsoft 365 / Outlook and Gmail server settings from the email address, and enter BYO `oauth_client_id` fields (`[Microsoft | Google]`, `Tenant`, or `Client Secret`).

**Acceptance criteria:**
- [ ] Credential source selector cycles `[1Password | Keychain | OAuth 2.0]` on new accounts and `[Keep current | 1Password | Keychain | OAuth 2.0]` when editing, showing only the fields relevant to the chosen source and OAuth provider.
- [ ] Typing an `@outlook.com` / `@hotmail.com` / `@live.com` / `@office365.com` address (or selecting `Microsoft` OAuth when hosts haven't been manually edited) prefills `outlook.office365.com:993` (`implicit`) and `smtp.office365.com:587` (`starttls`); Gmail addresses prefill `imap.gmail.com:993` and `smtp.gmail.com:465`.
- [ ] While OAuth sign-in is pending, the popup displays the sign-in status / fallback URL (or Microsoft device code) and `esc` cancels the in-flight attempt cleanly.

**Verification:**
- [ ] Tests pass: `go test -race ./internal/ui/popup`
- [ ] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [ ] Manual check: Verify `popup.Connect` renders cleanly at 80x24 across all three credential sources.

**Dependencies:** Task 1

**Files likely touched:**
- `internal/ui/popup/connect.go`
- `internal/ui/popup/connect_test.go`

**Estimated scope:** Small: 1-2 files

---

## Task 7: Connect/Edit OAuth sign-in, keychain rename/delete, and re-auth hints

**Description:** Wire `popup.Connect` OAuth submissions in `internal/ui` and `cmd/dk-email` `Onboarding` to run browser PKCE (with Microsoft Device Code fallback when browser open fails or is requested), verify IMAP + SMTP `XOAUTH2` login, persist tokens + config atomically, migrate/delete OAuth keychain entries on account edit/remove, and point users to `E` when refresh fails with `invalid_grant`.

**Acceptance criteria:**
- [ ] Submitting a new or edited OAuth account runs sign-in, verifies IMAP + SMTP `XOAUTH2`, stores the token bundle (and Google client secret, if provided) in the keychain, writes `config.toml`, and connects the account in place without restart (`esc` cancels in-flight auth).
- [ ] Renaming an OAuth account moves its keychain token/secret entries before deleting old keys; deleting an account removes OAuth keychain entries when credential deletion is confirmed; editing with `Keep current` preserves existing OAuth tokens unless re-authenticated.
- [ ] Expired/revoked refresh tokens (`invalid_grant`) surface an actionable status message instructing the user to press `E` to edit/re-authenticate the account.

**Verification:**
- [ ] Tests pass: `go test -race ./internal/ui/... ./cmd/dk-email`
- [ ] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [ ] Manual check: Verify UI tests cover OAuth add, edit (`Keep current` & re-auth), rename, cancel, and `invalid_grant` status hint.

**Dependencies:** Task 5, Task 6

**Files likely touched:**
- `internal/ui/connect.go`
- `internal/ui/edit.go`
- `internal/ui/edit_test.go`
- `cmd/dk-email/main.go`

**Estimated scope:** Medium: 3-5 files

---

## Task 8: OAuth 2.0 and credential mode documentation

**Description:** Document all three credential modes (`oauth2`, `password_cmd`, and `keychain`) and step-by-step BYO `oauth_client_id` registration for Microsoft Entra ID and Google Cloud in `docs/accounts.md`, `README.md`, and `SPEC.md`.

**Acceptance criteria:**
- [ ] `docs/accounts.md` covers step-by-step BYO `client_id` setup for Microsoft Entra ID (public client, `http://localhost`, scopes, no client secret) and Google Cloud Desktop OAuth (`Internal` vs `Testing` 7-day refresh expiry vs unverified `Published`, plus App Password alternative), alongside `1Password`/`password_cmd` and external `oama` token commands.
- [ ] New `docs/oauth.md` is a complete, friendly OAuth guide: how sign-in works (browser PKCE, Microsoft device code), step-by-step Microsoft and Google app registration, every `oauth_*` config key with examples, external token commands (`oama`), where tokens are stored and how refresh works, re-authenticating with `E`, and troubleshooting (common AADSTS / Google errors, `invalid_grant`, 7-day Testing expiry). `docs/accounts.md` and `README.md` link to it.
- [ ] `README.md` and `SPEC.md` document `auth = "oauth2"`, `XOAUTH2` support on IMAP/SMTP, and Microsoft 365 `save_sent` default behavior.

**Verification:**
- [ ] Tests pass: `go test -race ./...`
- [ ] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [ ] Manual check: Read rendered markdown docs for accuracy and completeness.

**Dependencies:** Task 7

**Files likely touched:**
- `docs/oauth.md`
- `docs/accounts.md`
- `README.md`
- `SPEC.md`

**Estimated scope:** Medium: 3-5 files

---

## Checkpoint: Complete (After Tasks 6-8)
- [ ] Full suite passes: `go test -race ./...`
- [ ] Lint/format/build clean: `go vet ./... && gofmt -l . && go build -o bin/dk-email ./cmd/dk-email`
- [ ] `@architect` diff review passed
