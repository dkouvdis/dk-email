# Copilot Instructions for `dk-email`

This file mirrors the shared agent guidance in `AGENTS.md` so non-GitHub agents (for example Claude/Codex setups) can use the same conventions.

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
- `internal/ui` is a single root Bubble Tea model coordinating sidebar, thread list, popups, and reader.

Data/control flow:

1. Startup loads config and creates one connector per account.
2. UI `Init` schedules asynchronous `tea.Cmd` work (connect, fetch folders/summaries/messages, flag/move).
3. Background work returns typed messages (e.g., `accountMsg`, `summariesMsg`, `messageMsg`) to `Model.Update`.
4. UI never talks directly to IMAP internals; all network I/O is behind `mail.Mailbox`.
5. Sending goes through `mail.Sender` (nil when the account has no `smtp_host`), also inside a `tea.Cmd`. The editor runs via `tea.ExecProcess`, then a confirm popup (`y`/`e`/`d`) decides whether to send.

Supporting modules:

- `internal/thread`: pure threading logic (union-find over Message-ID / References with constrained subject fallback).
- `internal/render`: MIME parsing + HTML-to-markdown rendering (email cruft stripped first; junk text/plain parts fall back to HTML) + Glamour styling of HTML-derived bodies (plain text is only wrapped) + attachment extraction (it also exposes attachment `Parts` with bytes for forwarding).
- `internal/compose`: pure drafting logic: reply/reply-all/forward/new drafts, editor-file format and parse, and MIME build.
- `internal/send`: send orchestration implementing `mail.Sender`: forward attachment fetch, build the message once, SMTP, Sent `APPEND`, then `\Answered`.
- `internal/secret`: password resolution from `password_cmd` or OS keychain.
- `internal/config`: strict TOML load/validate/defaults and append-account persistence.

## Key repository-specific conventions

- Config is intentionally strict: unknown keys fail validation; `password` key is rejected; credentials must come from `password_cmd` or keychain.
- An account sends only when `smtp_host` is set; SMTP reuses the account's IMAP credentials and `tls_cert`.
- Outgoing mail is plain text. The Markdown seam is `compose.BodyPart`; a later HTML mode replaces it without touching reply, forward or transport code.
- Config updates preserve user formatting/comments: adding an account appends a TOML block instead of re-encoding the file.
- Bubble Tea pattern is consistent: perform I/O in `tea.Cmd`, return typed messages, keep `Update` as the state transition point.
- `internal/mail` stays provider-agnostic and standard-library-only; IMAP-specific behavior belongs in `internal/mail/imap`.
- IMAP client behavior is deliberate: one lazily opened connection per account, serialized calls, reconnect on dropped connection, and context-driven cancellation.
- Threading behavior is deliberate: prefer header-based linkage, use subject fallback only for reply-like messages without references.
- Releases are tag-driven SemVer (`v*`) via GoReleaser; versions are injected via ldflags (no hardcoded version constants).
- Commit messages follow Conventional Commits (`feat:`, `fix:`, etc.) because release notes are grouped/filtered from commit prefixes.
