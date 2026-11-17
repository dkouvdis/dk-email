# Agent Instructions for `dk-email`

These instructions are provider-agnostic and intended for any coding agent (Copilot, Claude, Codex, etc.).

## Build, test, lint, and release checks

Use the same commands CI runs:

```sh
# build
go build -o bin/dk-email ./cmd/dk-email

# full test suite
go test -race ./...

# run one package
go test -race ./internal/ui

# run a single test
go test -race ./internal/ui -run '^TestModel_ToggleView$'

# lint/format checks
go vet ./...
gofmt -l .

# release config validation
goreleaser check
```

## High-level architecture

`cmd/dk-email` is composition/root wiring: it parses flags, loads config, builds secret/mail connectors (plus a `mail.Sender` per account when `smtp_host` is set), and starts Bubble Tea.

Core boundaries:

- `internal/mail` defines provider-agnostic domain types plus the `mail.Mailbox` interface. UI code depends on this interface only.
- `internal/mail/imap` is the concrete IMAP adapter (`go-imap/v2`) implementing `mail.Mailbox`.
- `internal/mail/smtp` is the concrete SMTP transport (`go-smtp`): implicit TLS or STARTTLS, AUTH PLAIN, and `Verify` for the onboarding login test.
- `internal/ui` is a single root Bubble Tea model coordinating sidebar, thread list, popups (including the `popup.Compose` composer), and reader. It never hands the terminal to another program (no editor hand-off).

Data/control flow:

1. Startup loads config and creates one connector per account.
2. UI `Init` schedules asynchronous `tea.Cmd` work (connect, fetch folders/summaries/messages, flag/move).
3. Background work returns typed messages (e.g., `accountMsg`, `summariesMsg`, `messageMsg`) to `Model.Update`.
4. UI never talks directly to IMAP internals; all network I/O is behind `mail.Mailbox`.
5. Sending goes through `mail.Sender` (nil when the account has no `smtp_host`), also inside a `tea.Cmd`. The built-in `popup.Compose` composer edits the draft in place; `ctrl+s` sends it, and `esc` offers save/discard when it changed. Saving and reopening drafts goes through `mail.Drafter`, which `send.Sender` also implements.

Supporting modules:

- `internal/thread`: pure threading logic (union-find over Message-ID / References with constrained subject fallback).
- `internal/render`: MIME parsing + HTML-to-markdown rendering (email cruft stripped first; junk text/plain parts fall back to HTML) + Glamour styling of HTML-derived bodies (plain text is only wrapped) + attachment extraction (it also exposes attachment `Parts` with bytes for forwarding).
- `internal/compose`: pure drafting logic: reply/reply-all/forward/new drafts, composer fields (`Fields`/`FieldsOf`), MIME build (`Build` never writes Bcc; `BuildDraft` does), and `FromRaw` to reopen a saved draft.
- `internal/send`: orchestration implementing `mail.Sender` (forward attachment fetch, build once, SMTP, Sent `APPEND`, `\Answered`, remove the replaced draft) and `mail.Drafter` (`SaveDraft`, `OpenDraft`).
- `internal/secret`: password resolution from `password_cmd` or OS keychain, plus OAuth token bundles (`oauth:<account>`) and client secrets (`oauth-client-secret:<account>`) in the keychain.
- `internal/oauth`: stdlib-only OAuth 2.0 for Microsoft and Google: loopback PKCE and Microsoft device code sign-in, and a serialized `TokenSource` that refreshes near expiry and saves rotated refresh tokens. Errors never carry tokens, codes or secrets.
- `internal/config`: strict TOML load/validate/defaults and append-account persistence.

## Key repository-specific conventions

- Config is intentionally strict: unknown keys fail validation; `password` key is rejected; credentials must come from `password_cmd` or keychain.
- An account sends only when `smtp_host` is set; SMTP reuses the account's IMAP credentials and `tls_cert`.
- OAuth accounts (`auth = "oauth2"`) get their login from a `mail.Credentials` callback on every IMAP dial/reconnect and SMTP send/verify, and log in with `XOAUTH2`; password accounts resolve their password once per connect. The UI never sees a token: sign-in returns an opaque `ui.OAuthSession`, and the keychain is always written before `config.toml`. User guide: `docs/oauth.md`.
- Outgoing mail is plain text. The Markdown seam is `compose.BodyPart`; a later HTML mode replaces it without touching reply, forward or transport code.
- Saving and removing drafts is IMAP-side, via `DraftsFolder` and `Delete` on `imap.Client`; `Delete` needs UIDPLUS and never falls back to a plain `EXPUNGE` (without it the old draft is kept and the status says so).
- Config updates preserve user formatting/comments: adding an account appends a TOML block instead of re-encoding the file.
- Bubble Tea pattern is consistent: perform I/O in `tea.Cmd`, return typed messages, keep `Update` as the state transition point.
- `internal/mail` stays provider-agnostic and standard-library-only; IMAP-specific behavior belongs in `internal/mail/imap`.
- IMAP client behavior is deliberate: one lazily opened connection per account, serialized calls, reconnect on dropped connection, and context-driven cancellation.
- Threading behavior is deliberate: prefer header-based linkage, use subject fallback only for reply-like messages without references.
- Releases are tag-driven SemVer (`v*`) via GoReleaser; versions are injected via ldflags (no hardcoded version constants).
- Commit messages follow Conventional Commits (`feat:`, `fix:`, etc.) because release notes are grouped/filtered from commit prefixes.
