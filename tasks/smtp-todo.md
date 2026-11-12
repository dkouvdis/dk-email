# Tasks: SMTP sending (v0.2.0)

Plan: [smtp-plan.md](smtp-plan.md) · Spec: [smtp-spec.md](smtp-spec.md)

Every task also meets the standing bar before it counts as done:

- `go test -race ./...`, `go vet ./...` and `gofmt -l .` all pass, the last with empty output.
- Commits follow Conventional Commits, with no `Co-authored-by` trailer.
- No secrets, real mail or hardcoded version strings are committed.
- No real address other than your own receives mail during development.

---

## Phase 1: Foundation and de-risking

### S1: SMTP config keys, defaults, validation and append

**Description:** Add the optional `smtp_host`, `smtp_port`, `smtp_tls`, `from_name` and `save_sent` keys to `config.Account`.

- Defaults: `smtp_tls` is `implicit`. The port is 465 for implicit and 587 for STARTTLS. `save_sent` is false for `imap.gmail.com` and true otherwise; use `*bool` so an explicit value wins.
- Validation follows the spec, joined into the existing error report.
- `AppendAccount` writes the new keys when set.
- Existing configs load unchanged.

**Acceptance criteria:**

- [x] A config with no `smtp_*` keys loads exactly as before, with sending disabled (`acct.SMTPHost == ""`).
- [x] The defaults and every validation message in the spec's Configuration section are covered by table tests, including `smtp_port is set but smtp_host is empty`.
- [x] `AppendAccount` output includes the `smtp_*`, `from_name` and `save_sent` lines only when set, and re-loads to an equal `Account`.

**Verification:**

- [x] `go test -race ./internal/config/...` passes with ≥ 80% coverage.

**Dependencies:** None

**Files likely touched:** `internal/config/config.go`, `internal/config/append.go`, `internal/config/config_test.go`, `internal/config/append_test.go`

**Estimated scope:** S

---

### S2: Mail types for sending, plus `Reply-To` parsing

**Description:** Add the provider-agnostic sending types to `internal/mail`, using only the standard library:

- `MessageRef`, `Draft`, `Sender`
- `FlagAnswered` (with `String`)
- `ErrAfterDelivery`
- `Message.ReplyTo []Address`

`render.Parse` fills `ReplyTo`. `mailtest` gains a fake `Sender` that records drafts and returns scripted errors.

**Acceptance criteria:**

- [x] `mail` still imports only the standard library, and `Mailbox` is unchanged.
- [x] `render.Parse` returns `ReplyTo` for a fixture with a `Reply-To` header, and nil without one.
- [x] `mailtest.Sender` records each `Draft` and can return a scripted error per call.

**Verification:**

- [x] `go test -race ./internal/mail/... ./internal/render/...` passes.
- [x] `go list -f '{{join .Imports "\n"}}' ./internal/mail` lists only standard-library packages.

**Dependencies:** None

**Files likely touched:** `internal/mail/mail.go`, `internal/mail/mail_test.go`, `internal/mail/mailtest/fake.go`, `internal/render/render.go`, `internal/render/render_test.go`

**Estimated scope:** M

---

### S3: SMTP transport on go-smtp

**Description:** Create `internal/mail/smtp` with a transport that sends raw RFC 5322 bytes:

- `Dial` uses implicit TLS or STARTTLS from `config.Account`, honouring `tls_cert` by sharing the IMAP cert-pool code.
- Authenticate with SASL PLAIN (username + password), send `MAIL FROM`/`RCPT TO`/`DATA`, then quit.
- Context cancellation and the 60s timeout close the connection.
- Plaintext is never allowed.
- Add `github.com/emersion/go-smtp` (approved). Check its v0.25.x API against current docs first.

**Acceptance criteria:**

- [x] Against an in-process go-smtp server with a self-signed cert, these pass: implicit TLS send, STARTTLS send, auth failure, rejected recipient (error names the address), and context cancellation.
- [x] A server that doesn't offer STARTTLS on `smtp_tls = "starttls"` fails rather than sending in plaintext.
- [x] A live, opt-in test (`DK_EMAIL_LIVE=1`, same pattern as `imap/live_test.go`) sends a one-line message from your Gmail account **to itself**.

**Verification:**

- [x] `go test -race ./internal/mail/smtp/...` passes with ≥ 80% coverage.
- [x] Manual: the live test delivers to your own inbox.

**Dependencies:** None (uses `config.Account`; the S1 fields can be stubbed in tests until S1 lands)

**Files likely touched:** `internal/mail/smtp/smtp.go`, `internal/mail/smtp/smtp_test.go`, `internal/mail/smtp/live_test.go`, `go.mod`, `go.sum`

**Estimated scope:** M

---

### S4: Editor hand-off (`c` opens `$EDITOR` and discards) and refresh moves to `ctrl+r`

**Description:** Build the UI plumbing for the editor:

- Write a template to a `0600` temp file (`dk-email-*.eml`).
- Run `$VISUAL`/`$EDITOR`/`vi` through `tea.ExecProcess` with `sh -c '<editor> "$1"' --`.
- On return, read the file and delete it.
- For now, `c` opens an empty template and always reports `Draft discarded`; S8 adds sending.

Also rebind refresh from `r` to `ctrl+r`, and add `r`/`R`/`f`/`c` to `KeyMap`, unbound to actions except `c`. Keep the editor command injectable for tests.

**Acceptance criteria:**

- [x] With a stub editor command, `c` produces an `ExecProcess` cmd. The return message carries the file contents, and the temp file no longer exists afterwards, on success, on editor error, and on quit.
- [x] `ctrl+r` refreshes, and `r` no longer does. The help popup and hint bar show `ctrl+r`.
- [x] Manual: in a real terminal, `c` opens your editor. On quitting it the TUI redraws correctly, with alt screen, mouse behaviour and layout intact.

**Verification:**

- [x] `go test -race ./internal/ui/...` passes.
- [x] Manual check above in your usual terminal.

**Dependencies:** None

**Files likely touched:** `internal/ui/keys.go`, `internal/ui/model.go`, `internal/ui/editor.go` (new), `internal/ui/editor_test.go` (new), `internal/ui/refresh_test.go`

**Estimated scope:** M

---

### ✅ Checkpoint 1: Foundation

- [x] Tests, vet and gofmt are clean.
- [x] A live SMTP send to yourself works on Gmail.
- [x] The editor round-trip works in a real terminal.
- [x] You review before Phase 2.

---

## Phase 2: First slice, new message

### S5: Compose: new-message template, editor-file parse, plain-text MIME build

**Description:** Create the pure `internal/compose` package with three pieces:

- `New(from)` builds the empty draft.
- `Format(draft) → file text` and `Parse(text, draft) → draft, error` handle the editor file. Only `To`/`Cc`/`Bcc`/`Subject` are read, an unknown header is an error, and at least one recipient is required.
- `Build(draft, attachments, now, msgID) → []byte` writes RFC 5322:
  - `Date`, `Message-ID`, `From` with `from_name`, encoded words, and no `Bcc` header
  - `text/plain; charset=utf-8` quoted-printable, routed through the `BodyPart` seam
  - `multipart/mixed` when attachments are present

**Acceptance criteria:**

- [x] `Format` → `Parse` round-trips. Unknown headers, bad addresses and no recipients each return a distinct, readable error.
- [x] Golden `.eml` fixtures in `testdata/` cover: ASCII, non-ASCII subject and name, a long line, Bcc omitted, and two attachments.
- [x] `go-message` re-parses every built message with the same headers and body.

**Verification:**

- [x] `go test -race ./internal/compose/...` passes with ≥ 90% coverage.

**Dependencies:** S2

**Files likely touched:** `internal/compose/compose.go`, `internal/compose/editorfile.go`, `internal/compose/mime.go`, `internal/compose/*_test.go`, `internal/compose/testdata/*.eml`

**Estimated scope:** M

---

### S6: IMAP `Append`, `\Answered` and `Raw`

**Description:** Add three concrete methods on `*imap.Client`, not on `Mailbox`:

- `Append(ctx, folder, raw, flags)`
- `Raw(ctx, folder, uid) ([]byte, error)`, using `BODY.PEEK[]` so it never sets `\Seen`
- `SentFolder(ctx) (string, bool)`, using the existing role detection

`SetFlag` also maps `mail.FlagAnswered` to `\Answered`. All of them use the existing `do` serialization and reconnect rules. Check go-imap v2's `Append` API against current docs first.

**Acceptance criteria:**

- [x] Against `imapmemserver`, `Append` to the Sent-role folder stores the exact bytes with `\Seen`, and a later `Summaries` lists it.
- [x] `Raw` returns the exact stored bytes and leaves `\Seen` unchanged.
- [x] `SetFlag(…, FlagAnswered, true)` sets `\Answered`. `SentFolder` returns false when no Sent-role folder exists.

**Verification:**

- [x] `go test -race ./internal/mail/imap/...` passes, and coverage stays ≥ 80%.

**Dependencies:** S2

**Files likely touched:** `internal/mail/imap/actions.go`, `internal/mail/imap/message.go`, `internal/mail/imap/folders.go`, `internal/mail/imap/actions_test.go`, `internal/mail/imap/message_test.go`

**Estimated scope:** M

---

### S7: Send orchestration (`internal/send`)

**Description:** Implement `mail.Sender` in `internal/send`, using a `Transport` (S3) and a small `Store` interface that `*imap.Client` satisfies (`Raw`, `Append`, `SentFolder`, `SetFlag`).

- Steps run in the spec's order: forward fetch → build once → SMTP → APPEND if `save_sent` → `\Answered` if replying.
- Failures in steps 4–5 wrap `mail.ErrAfterDelivery`. A missing Sent folder is a warning that also wraps it.
- The debug log records step timings only, with no addresses or bodies.

**Acceptance criteria:**

- [x] With fakes: the call order is correct, the bytes appended equal the bytes sent, `save_sent=false` skips APPEND, and an SMTP failure skips APPEND and flag and does **not** wrap `ErrAfterDelivery`.
- [x] An APPEND failure or a flag failure returns an error that `errors.Is(err, mail.ErrAfterDelivery)` matches.
- [x] Envelope recipients are To + Cc + Bcc, deduplicated, and the envelope sender is the account `email`.

**Verification:**

- [x] `go test -race ./internal/send/...` passes with ≥ 90% coverage.

**Dependencies:** S3, S5, S6

**Files likely touched:** `internal/send/send.go`, `internal/send/send_test.go`

**Estimated scope:** S

---

### S8: `c` end-to-end: wiring, confirm popup, send state, retry

**Description:** Connect everything for new messages.

- **Wiring:** `ui.Connector` returns `ui.Conn{Mailbox, Sender}`. `cmd/dk-email` builds a `send.Sender` when `smtp_host` is set, from one password resolution.
- **Confirm popup** (`internal/ui/popup`):
  - Shows From/To/Cc/Bcc/Subject, attachments and a body preview, plus any parse error.
  - Keys: `y` send, `e` edit, `d` discard. `esc` and `q` don't close it.
- **Model:**
  - Tracks the draft and send state, and blocks `q` while a draft or send is pending. `ctrl+r` still refreshes and `ctrl+c` still quits.
  - Shows the status messages from the spec. Reopens the popup with the error on delivery failure. Shows a warning, with no retry, on `ErrAfterDelivery`.
  - Shows the "Sending isn't set up" message for accounts without a Sender.

**Acceptance criteria:**

- [x] `Update`-level tests with `mailtest.Sender` and a stub editor cover: an unchanged file is discarded, `y` sends the parsed draft, `e` reopens the editor with the current text, `d` discards, `esc` keeps the popup, a failure then retry sends exactly once more, `ErrAfterDelivery` shows the warning and offers no retry, `q` is blocked while sending, and the not-configured message appears.
- [x] `cmd/dk-email` connector tests: no `smtp_host` gives a nil Sender, and with one, password resolution happens once.
- [x] Manual: `c` → write → `y` delivers to yourself from Gmail (one copy in Sent) and from a non-Gmail account (an APPENDed copy in Sent).

**Verification:**

- [x] `go test -race ./internal/ui/... ./cmd/dk-email/...` passes.
- [x] Manual check above.

**Dependencies:** S1, S4, S7

**Files likely touched:** `internal/ui/accounts.go`, `internal/ui/compose.go` (new), `internal/ui/compose_test.go` (new), `internal/ui/popup/confirm.go` (new), `cmd/dk-email/main.go`

**Estimated scope:** L. Split the confirm popup into its own commit if the diff grows past ~5 files.

---

### ✅ Checkpoint 2: New message

- [x] `c` sends to yourself on Gmail (one copy in Sent) and on a non-Gmail account (an APPENDed copy in Sent).
- [x] With the network off, the send fails into the confirm popup. Turning it back on and pressing `y` delivers exactly once.
- [x] No `dk-email-*.eml` files are left in `$TMPDIR` afterwards.
- [x] You review before Phase 3.

---

## Phase 3: Reply and forward

### S9: Reply and reply-all (`r`, `R`) with threading and `\Answered`

**Description:**

- **compose:** `Reply(msg, me)` and `ReplyAll(msg, me)` follow the spec's rules: `Reply-To`/`From`, reply to my own message, self-removal, dedupe, Cc-to-To promotion, `Re:` subject, `In-Reply-To`/`References`, attribution line and `> ` quoting of the target only. Set `Draft.ReplyTo` so `send` marks `\Answered`.
- **ui:** `r`/`R` resolve the target message (latest in a collapsed thread or the reader, the selected one on an expanded row) and fetch it via `Mailbox.Message` if it isn't loaded. Then the S8 flow runs.

**Acceptance criteria:**

- [ ] `compose` table tests cover every recipient, subject, References and quoting rule in the spec.
- [ ] UI tests: `r` on a collapsed thread drafts a reply to the latest message, `R` includes To and Cc minus me, and on an expanded row the selected message is the target.
- [ ] Manual: a reply to yourself threads under the original in Gmail's web UI, and the original shows as answered in another client.

**Verification:**

- [ ] `go test -race ./internal/compose/... ./internal/ui/...` passes.
- [ ] Manual check above.

**Dependencies:** S8

**Files likely touched:** `internal/compose/reply.go`, `internal/compose/reply_test.go`, `internal/ui/compose.go`, `internal/ui/compose_test.go`, `internal/ui/keys.go`

**Estimated scope:** M

---

### S10: Forward (`f`) with original attachments

**Description:**

- **render:** add `Parts(raw) ([]Part, error)`, returning each attachment's bytes, content type and filename, using the same attachment rules as `Parse`.
- **compose:** `Forward(msg)` sets an empty To, a `Fwd:` subject, and the forwarded-message header block plus the unquoted body. Set `Draft.ForwardOf`.
- **send:** step 1 fetches `Raw` and passes `Parts` to `Build`.
- **ui:** `f`. The confirm popup lists the attachment names and sizes.

**Acceptance criteria:**

- [ ] `render.Parts` returns byte-identical attachment contents for fixtures with base64, quoted-printable and an RFC 2231 filename.
- [ ] `send` with `ForwardOf` set fetches raw once and builds a `multipart/mixed` message carrying every attachment. A failed fetch is a pre-delivery error, so nothing is sent.
- [ ] Manual: forwarding a message with a PDF to yourself delivers the same bytes (`shasum` matches).

**Verification:**

- [ ] `go test -race ./internal/render/... ./internal/compose/... ./internal/send/... ./internal/ui/...` passes.
- [ ] Manual check above.

**Dependencies:** S9 (shares compose/ui files; logically only S8)

**Files likely touched:** `internal/render/parts.go`, `internal/render/render_test.go`, `internal/compose/forward.go`, `internal/send/send.go`, `internal/ui/compose.go`

**Estimated scope:** M

---

### ✅ Checkpoint 3: All actions

- [ ] Spec success criteria 1–8 pass manually, sending to yourself only.
- [ ] Coverage targets are met: `compose` ≥ 90%, `send` ≥ 90%, `mail/smtp` ≥ 80%, `mail/imap` ≥ 80%.
- [ ] You review before Phase 4.

---

## Phase 4: Onboarding, docs, release

### S11: Connect popup SMTP fields + SMTP login test

**Description:** The connect-account popup gains optional **SMTP host**, **port** and **TLS** fields.

- For `@gmail.com` and `@googlemail.com`, they're pre-filled with `smtp.gmail.com:465 implicit`.
- When the host is filled, the popup tests the SMTP login (connect + auth, no send) after IMAP succeeds, and shows errors inside the popup.
- The appended block includes the `smtp_*` keys. Leaving the host blank keeps today's behaviour.

**Acceptance criteria:**

- [ ] A Gmail address pre-fills the SMTP fields. A blank SMTP host saves the account with no `smtp_*` keys.
- [ ] A bad SMTP password shows the error in the popup and saves nothing. A valid one appends the account with `smtp_host`/`smtp_port`/`smtp_tls`.
- [ ] The transport gains a `Check(ctx)` (connect + auth only), tested against the in-process server.

**Verification:**

- [ ] `go test -race ./internal/ui/... ./internal/mail/smtp/... ./cmd/dk-email/...` passes.

**Dependencies:** S1, S3

**Files likely touched:** `internal/ui/popup/connect.go`, `internal/ui/popup/connect_test.go`, `internal/ui/connect.go`, `cmd/dk-email/main.go`, `internal/mail/smtp/smtp.go`

**Estimated scope:** M

---

### S12: Docs: README, SPEC.md, AGENTS.md and the Copilot mirror

**Description:** Update the user and agent docs for sending:

- **README:**
  - Drop "doesn't send".
  - Add a "Sending mail" section: config keys, Gmail app password reuse, `save_sent`, and `$EDITOR`.
  - Update the keys table: `r`, `R`, `f`, `c`, `ctrl+r`.
- **SPEC.md:**
  - Keybindings: `r` → `ctrl+r`, plus the new keys.
  - Remove "Reply, compose and SMTP" from Out of Scope.
  - Link `tasks/smtp-spec.md` from the Core boundary.
- **AGENTS.md** and `.github/copilot-instructions.md` (mirror): add `compose`, `send` and `mail/smtp` to the architecture section.

**Acceptance criteria:**

- [ ] The README key table and config example match the code: every `KeyMap` binding is listed, and every new config key is shown.
- [ ] AGENTS.md and copilot-instructions.md stay identical in body.
- [ ] No doc still says refresh is `r`.

**Verification:**

- [ ] `grep -rn '\`r\`.*refresh' README.md SPEC.md AGENTS.md .github/` returns nothing.

**Dependencies:** S8–S11

**Files likely touched:** `README.md`, `SPEC.md`, `AGENTS.md`, `.github/copilot-instructions.md`

**Estimated scope:** S

---

### S13: Live verification and `v0.2.0-beta.1`

**Description:** Run the full spec success-criteria list against Gmail and one non-Gmail account, sending to yourself only. Fix anything found. Then, **with your approval**, tag `v0.2.0-beta.1`. The release notes call out the `r` → `ctrl+r` rebind.

**Acceptance criteria:**

- [ ] Spec success criteria 1–9 are all checked.
- [ ] You approve and push the `v0.2.0-beta.1` tag, and the GitHub pre-release has four archives and `checksums.txt`.

**Verification:**

- [ ] `goreleaser check` passes. The release workflow is green.

**Dependencies:** S12

**Files likely touched:** none expected (fixes only)

**Estimated scope:** S

---

### ✅ Checkpoint 4: v0.2.0

- [ ] All spec success criteria are met.
- [ ] You approve tagging `v0.2.0-rc.1`, then `v0.2.0`.
