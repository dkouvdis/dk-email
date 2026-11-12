# Implementation Plan: SMTP sending (v0.2.0)

Spec: [smtp-spec.md](smtp-spec.md). Task details, acceptance criteria and verification: [smtp-todo.md](smtp-todo.md). The v0.1.0 plan stays in [plan.md](plan.md) / [todo.md](todo.md).

## Overview

Add reply, reply-all, forward and new message to dk-email. Mail is written in `$EDITOR`, confirmed in a popup, delivered over SMTP, copied to Sent via IMAP `APPEND` (except on Gmail), and the replied-to message is marked `\Answered`. The work is ordered so that:

1. The two unknowns come first: the new `go-smtp` dependency, and suspending Bubble Tea for an external editor.
2. **New message (`c`)** is the first end-to-end slice, because it needs the whole pipeline (config → editor → compose → SMTP → Sent) without reply or forward rules.
3. Reply/reply-all and forward then add only their draft rules, plus `\Answered` and attachment copying.
4. Onboarding, docs and the release come last.

## Architecture Decisions

- **`mail.Sender` is the only new seam the UI sees.** `Mailbox` keeps its methods. IMAP extras (`Raw`, `Append`, `\Answered`, Sent lookup) are concrete `*imap.Client` methods, used by `internal/send` through its own small interface.
- **`internal/compose` is pure:** it builds drafts, formats and parses the editor file, and builds the MIME bytes. It has no I/O, so it's cheap to table-test and golden-test.
- **Bytes are built once.** The exact bytes sent over SMTP are the bytes appended to Sent.
- **Failures after delivery never retry.** `mail.ErrAfterDelivery` separates "sent with a warning" from "not sent".
- **The body builder is a seam** (`compose.BodyPart`), so Markdown → HTML can replace it later.
- **The editor runs through `tea.ExecProcess`.** The temp file is `0600` and is removed on every exit path.
- **Config changes are additive and append-only.** Existing configs load unchanged, and sending stays disabled until `smtp_host` is set.
- **Library APIs (go-smtp v0.25.x, Bubble Tea v2 `ExecProcess`, go-imap v2 `Append`) are checked against current docs before use.**

## Dependency Graph

```
S1 config keys ──────────────┐
S2 mail types + Reply-To ──┬─┼── S5 compose (new msg + MIME) ──┐
                           │ │                                  ├── S8 `c` end-to-end (wiring + confirm popup)
S3 smtp transport ─────────┼─┴── S7 send orchestration ────────┘          │
S6 imap Append/\Answered/Raw┘                                              ├── S9 reply / reply-all
S4 editor exec + ctrl+r ───────────────────────────────────────────────────┤
                                                                           └── S10 forward (+ attachments)
S1 + S3 ── S11 onboarding SMTP fields
S8–S11 ── S12 docs ── S13 live check + release
```

## Task List

The full details are in [smtp-todo.md](smtp-todo.md).

### Phase 1: Foundation and de-risking
- [x] S1: SMTP config keys, defaults, validation and append
- [x] S2: Mail types for sending, plus `Reply-To` parsing
- [x] S3: SMTP transport on go-smtp (memserver tests + live send-to-self)
- [x] S4: Editor hand-off: `c` opens `$EDITOR` and discards; refresh moves to `ctrl+r`

### Checkpoint 1: Foundation
- [x] Tests, vet and gofmt are clean. A live SMTP send to yourself works on Gmail. The editor round-trip works in a real terminal.

### Phase 2: First slice, new message
- [x] S5: Compose: new-message template, editor-file parse, plain-text MIME build
- [x] S6: IMAP `Append`, `\Answered` and `Raw`
- [x] S7: Send orchestration (`internal/send`)
- [x] S8: `c` end-to-end: wiring, confirm popup, send state, retry

### Checkpoint 2: New message
- [x] `c` sends to yourself on Gmail (one copy in Sent) and on a non-Gmail account (APPENDed copy in Sent). A network-off failure retries cleanly.

### Phase 3: Reply and forward
- [x] S9: Reply and reply-all (`r`, `R`) with threading and `\Answered`
- [x] S10: Forward (`f`) with original attachments

### Checkpoint 3: All actions
- [ ] Success criteria 1–8 from the spec pass manually, sending to yourself.

### Phase 4: Onboarding, docs, release
- [x] S11: Connect popup SMTP fields + SMTP login test
- [ ] S12: Docs: README, SPEC.md, AGENTS.md and the Copilot mirror
- [ ] S13: Live verification and `v0.2.0-beta.1` (tag on your approval)

### Checkpoint 4: v0.2.0
- [ ] All spec success criteria are met, and you approve the tag.

## Parallelization

- S1, S2, S3 and S4 are independent, and S6 depends only on S2.
- S5 needs S2. S7 needs S3, S5 and S6. S8 needs S4 and S7.
- S9 and S10 both touch `compose` and `ui`, so do them one after the other.
- S11 can run alongside Phase 3.

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| go-smtp API differs from memory, or it has TLS quirks with Proton Bridge's cert | Med | S3 goes first, with an in-process server and a live check. Reuse the IMAP `tls_cert` pool code. |
| `tea.ExecProcess` leaves the terminal in a bad state (alt screen, mouse mode) after the editor | High | S4 is a dedicated slice with a manual check in Ghostty/iTerm/Terminal. Re-enable mouse and alt screen on return. |
| Duplicate sends: SMTP succeeded but the connection dropped before the final reply | Med | Retry only on clearly pre-delivery failures. Document that an ambiguous `DATA` failure may already be delivered, and show "may have been sent" in the error text. |
| Duplicate Sent copies on providers that auto-save (e.g. Gmail) | Low | `save_sent` defaults to false for Gmail and can be overridden per account. |
| The temp draft file leaks message text to disk | Low | `0600` in the OS temp dir, removed on every path. A test checks it's gone after send, discard and quit. |
| Rebinding `r` → `ctrl+r` breaks muscle memory | Low | Called out in the release notes, the README and the `?` help. |
| Forwarding large attachments blows the 60s timeout | Low | Timeout covers SMTP only. The fetch has its own 30s timeout, and sizes show in the confirm popup. |

## Open Questions

None. See [smtp-spec.md § Open Questions](smtp-spec.md#open-questions) for the resolved ones.
