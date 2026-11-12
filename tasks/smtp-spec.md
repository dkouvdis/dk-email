# Spec: Sending mail over SMTP (v0.2.0)

Status: **Approved 2026-11-12.** Base spec: [SPEC.md](../SPEC.md). Plan: [smtp-plan.md](smtp-plan.md). Tasks: [smtp-todo.md](smtp-todo.md). Deferred ideas: [future-plans.md](future-plans.md).

## Objective

Let me answer mail without leaving dk-email: **reply, reply-all, forward and new message**, written in my own `$EDITOR`, sent over SMTP from any of my IMAP accounts (Gmail and non-Gmail alike). Replies thread correctly, the original is marked answered, a copy lands in Sent, and a failed send never loses what I wrote.

### Confirmed intent (2026-11-12)

- **Outcome:** reply, reply-all, forward and new message, sent over SMTP.
- **User:** me, triaging several IMAP accounts in one keyboard-driven client.
- **Why now:** v0.x is read-only; switching clients to answer mail is the gap.
- **Behaviour:** compose in `$EDITOR` with headers at the top; a reply targets the **latest message** and quotes **only that message**; outgoing mail is **plain text**.
- **Success:** correct threading headers, `\Answered` on the original, a copy in Sent (APPEND where the server doesn't save it), forwards carry the original attachments, failed sends never lose text.
- **Constraint:** provider-agnostic, behind the reserved `Sender` interface; `Mailbox` keeps its methods; same credential rules as IMAP; leave room for Markdown → HTML later.
- **Out of scope:** see [Out of Scope](#out-of-scope) and [future-plans.md](future-plans.md).

### User stories

1. On a thread, I press `r`. My editor opens with `To`, `Cc`, `Bcc` and `Subject` pre-filled and the latest message quoted. I write, save and quit, confirm with `y`, and the reply is sent.
2. `R` does the same but replies to everyone on the latest message, minus my own address.
3. `f` forwards the latest message, including its attachments, to whoever I type into `To`.
4. `c` opens a blank message from the current account.
5. If I quit the editor without changing anything, nothing is sent and nothing is asked.
6. If sending fails (network, auth, rejected recipient), I see the error and can retry, re-edit or discard. My text is never silently lost.
7. After sending, the message is in the account's Sent folder (once, not twice on Gmail), and the original shows as answered in other clients.

## Behaviour

### Keys

| Key | Action | Notes |
|---|---|---|
| `r` | reply | **Was refresh**; refresh moves to `ctrl+r` |
| `R` | reply-all | Shift pairs with `r`, like `A` with `a` |
| `f` | forward | |
| `c` | compose a new message | Uses the current account |
| `ctrl+r` | refresh | Replaces `r` |

- `r`, `R` and `f` work on the thread list and in the reader popup. `c` works anywhere a folder is open.
- **Target message:** on a collapsed thread and in the reader, the target is the thread's **latest message**. On an expanded row, it's the selected message, matching how the other actions (`u`, `s`, `e`…) resolve their target.
- The hint bar and `?` help gain a **Compose** section (`r`, `R`, `f`, `c`). The refresh hint shows `ctrl+r`.
- If the account has no `smtp_host`, these keys don't open the editor. The status line says: `Sending isn't set up for <account>: add smtp_host to its config`.

### Compose flow

1. dk-email builds a draft (see the reply, forward and new message rules below), writes it to a temp file, and suspends the TUI with `tea.ExecProcess` to run the editor.
2. Editor: `$VISUAL`, else `$EDITOR`, else `vi`. It runs as `sh -c '<editor> "$1"' -- <file>`, so values such as `code --wait` work.
3. When the editor exits, dk-email reads the file back and deletes it.
   - **Unchanged** (byte-identical to the template), or **empty**: the draft is discarded and the status says `Draft discarded`.
   - Otherwise the file is parsed. Header or address errors show in the confirm popup.
4. **Confirm popup** shows From, To, Cc, Bcc, Subject, the attachment names and sizes (for forwards), and the first lines of the body. It also shows any parse error.
   - `y` sends (disabled while there's a parse error or no recipient).
   - `e` reopens the editor with the current text.
   - `d` discards.
   - `esc` and `q` do **nothing** here. Discarding is always explicit.
5. **Sending** runs in a `tea.Cmd`. The popup closes, the status shows `Sending…`, and the UI stays responsive.
   - Success: `Sent to <first recipient> (+n)`.
   - Delivery failure: the confirm popup reopens with the same draft and the error, so `y` retries.
   - Delivered, but a follow-up step failed (Sent copy, `\Answered`): the status warns, e.g. `Sent, but couldn't save a copy to Sent: …`. **No retry is offered**, so the message is never sent twice.
6. `q` won't quit while a draft is open or a send is in flight. The status says `Finish or discard the draft first`, or `Sending…`. `ctrl+c` still quits immediately.

### Editor file format

```
To: Alice <alice@example.com>
Cc:
Bcc:
Subject: Re: Lunch on Friday

On Tue, 10 Nov 2026 at 12:00, Alice <alice@example.com> wrote:
> Shall we try the new place?
```

- Header lines run until the first blank line. Only `To`, `Cc`, `Bcc` and `Subject` are read. Unknown headers are a parse error, so a typo like `Too:` can't drop recipients.
- Addresses are comma-separated and parsed with `net/mail.ParseAddressList`. At least one recipient is required across To, Cc and Bcc.
- `From` is **not** in the file. It's always the account's identity, and the confirm popup shows it.
- The temp file is `os.CreateTemp("", "dk-email-*.eml")` with mode `0600`. The `.eml` suffix makes editors pick mail syntax. It's removed after reading, on discard, and on quit. This is the only place a message body touches disk, and only for the editor's lifetime.

### Reply (`r`)

- **To:** the target's `Reply-To` if present, else its `From`. If the target is **from me** (my own sent message), reply to its original `To` instead.
- **Subject:** `Re: ` + the original subject, unless it already starts with `Re:` (case-insensitive).
- **Threading:** `In-Reply-To` is the target's `Message-ID`. `References` is the target's `References` plus its `Message-ID`, deduplicated, with order kept.
- **Body:** a blank line for my text, then the attribution line `On <date>, <from> wrote:`, then the target's body with every line prefixed by `> ` (empty lines become `>`).
  - Only the target message is quoted, never earlier messages in the thread.
  - The quoted text is the body the reader shows (`mail.Message.Body`), so HTML-only mail is quoted as its Markdown rendering.
- **After send:** `\Answered` is set on the target.

### Reply-all (`R`)

As reply, plus:

- **To:** the reply recipient(s) plus the target's `To`.
- **Cc:** the target's `Cc`.
- My account's `email` is removed from To and Cc (case-insensitive), and addresses are deduplicated across To and Cc. If removing myself leaves To empty, the first Cc moves to To.

### Forward (`f`)

- **To:** empty. **Subject:** `Fwd: ` + the original subject, unless it already starts with `Fwd:` or `Fw:`.
- **Body:** a blank line for my text, then:
  ```
  ---------- Forwarded message ----------
  From: …
  Date: …
  Subject: …
  To: …
  Cc: …          (only if present)

  <original body, unquoted>
  ```
- **Attachments:** every attachment of the target is re-attached with its original filename and content type. The confirm popup lists them. They can't be removed in v1 (see [future-plans.md](future-plans.md)).
- No `In-Reply-To`/`References`, and no `\Answered`.

### New message (`c`)

- An empty `To`, `Cc`, `Bcc` and `Subject`, and an empty body.
- **Account:** the account of the open folder, or the sidebar's current account in narrow mode.

### Outgoing message format

- RFC 5322, with `MIME-Version: 1.0`, `Date` (local time zone), and a fresh `Message-ID` of `<random 128-bit hex>@<domain of the From address>`.
- `From: <from_name> <email>`, or just `email` when `from_name` is unset. Encoded words (RFC 2047) are used for non-ASCII names and subjects.
- **Body:** `text/plain; charset=utf-8`, quoted-printable, with lines left as written (no `format=flowed`).
- **With attachments:** `multipart/mixed`, with the text part first and then each attachment as base64 with `Content-Disposition: attachment; filename=…`.
- `Bcc` recipients get the message but the `Bcc` header is **not** written, neither in what's sent nor in the Sent copy.
- **Markdown seam:** building the body goes through one function, `compose.BodyPart(draft) → MIME part`. A later Markdown mode replaces it with a `multipart/alternative` (plain + HTML) without touching the reply, forward or transport code.

### Delivery and Sent

`Sender.Send` performs these steps in order:

1. **Forward only:** fetch the original's raw message over IMAP and extract its attachment parts (bytes + content type + filename).
2. Build the RFC 5322 bytes once. The same bytes are sent and saved to Sent.
3. **SMTP:** connect, authenticate, send.
   - Envelope sender is the account `email`.
   - Recipients are the deduplicated To + Cc + Bcc.
   - Timeout is 60s for the whole exchange, cancelled through the context.
   - **A failure here means nothing was sent.** Return the error; the draft is kept.
4. **Save to Sent** if `save_sent` is on: IMAP `APPEND` the bytes to the account's Sent-role folder with `\Seen`. If the account has no Sent folder, this step is a warning, not an error.
5. **Reply only:** set `\Answered` on the target message.

Failures in steps 4–5 are returned wrapped in `mail.ErrAfterDelivery`. The UI treats that as "sent, with a warning" and never retries.

## Configuration

New optional keys on `[[account]]`. All are additive, and existing configs keep working, with sending disabled until `smtp_host` is set.

```toml
[[account]]
name         = "Personal"
email        = "me@gmail.com"
imap_host    = "imap.gmail.com"
password_cmd = "op read op://Personal/Gmail/app-password"

smtp_host = "smtp.gmail.com"     # enables sending; omit to keep the account read-only
smtp_port = 465                  # default: 465 for implicit, 587 for starttls
smtp_tls  = "implicit"           # "implicit" | "starttls"; default "implicit"
from_name = "Dimitri Kouvdis"    # display name on outgoing mail; default: none (address only)
save_sent = false                # APPEND a copy to Sent; default false for imap.gmail.com, true otherwise
```

- **Credentials:** SMTP uses the account's `username` and the password from `password_cmd` or the keychain, the same as IMAP, resolved once per session. There's no separate SMTP password key, and the `password` key stays rejected.
- **Auth:** SASL `PLAIN` over TLS only. A plaintext connection is never used, and STARTTLS is mandatory when `smtp_tls = "starttls"`.
- **`tls_cert`** applies to SMTP too, so Proton Mail Bridge's self-signed certificate works for both protocols.
- **Validation** joins the existing all-errors-at-once report:
  - `smtp_tls` must be `implicit` or `starttls`.
  - `smtp_port` must be between 1 and 65535.
  - `smtp_port`, `smtp_tls`, `from_name` and `save_sent` without `smtp_host` are an error, e.g. `smtp_port is set but smtp_host is empty`.
- **`save_sent` default:** Gmail (`imap_host = imap.gmail.com`) already files SMTP-submitted mail in Sent, so it defaults to `false` there to avoid duplicates. It's `true` everywhere else. Set it explicitly for any other provider that already files SMTP-sent mail in Sent. Microsoft 365 and Outlook.com can't connect without OAuth (see [future-plans.md](future-plans.md)), so they have no default yet.
- **Onboarding:** the connect-account popup gains an optional **SMTP host** field, plus port and TLS.
  - For `@gmail.com` and `@googlemail.com` it's pre-filled with `smtp.gmail.com:465 implicit`.
  - When filled, the popup also tests SMTP login before saving.
  - The appended `[[account]]` block includes the `smtp_*` keys.
  - Existing accounts are edited by hand. The config is never re-encoded.

## Architecture

```
internal/mail/          + Draft, Attachment data, MessageRef, Sender, FlagAnswered, Message.ReplyTo, ErrAfterDelivery
internal/compose/       NEW pure: reply/reply-all/forward/new drafts, editor-file format+parse, MIME build
internal/mail/smtp/     NEW go-smtp transport: dial (implicit/STARTTLS, tls_cert), AUTH PLAIN, send raw bytes
internal/mail/imap/     + Raw(folder, uid), Append(folder, raw, flags), \Answered in SetFlag (concrete *Client methods)
internal/send/          NEW orchestration: implements mail.Sender using compose + a transport + a small IMAP store interface
internal/render/        + parse Reply-To; expose attachment parts (bytes) for forwarding
internal/config/        + smtp_host/smtp_port/smtp_tls/from_name/save_sent, defaults, validation, append
internal/ui/            + keys, editor exec, confirm popup, send state; refresh → ctrl+r
cmd/dk-email/           connector returns a Mailbox and an optional Sender per account
```

### Core types (in `mail`, standard library only)

```go
// MessageRef points at a stored message.
type MessageRef struct {
	Folder string
	UID    UID
}

// Draft is an outgoing message as the user wrote it. Addresses are already
// parsed; Body is plain text.
type Draft struct {
	From          Address
	To, Cc, Bcc   []Address
	Subject       string
	Body          string
	InReplyTo     string      // Message-ID being replied to, or ""
	References    []string
	ReplyTo       *MessageRef // marked \Answered after delivery
	ForwardOf     *MessageRef // attachments are copied from here
}

// Sender delivers drafts for one account. Send returns an error wrapping
// ErrAfterDelivery when the message was delivered but saving to Sent or
// marking the original answered failed; the caller must not retry then.
type Sender interface {
	Send(ctx context.Context, d Draft) error
}
```

- **`Mailbox` keeps its methods**, as SPEC.md promised.
  - The IMAP extras `Send` needs (`Raw`, `Append`, Sent-folder lookup, `\Answered`) live on `*imap.Client`.
  - `internal/send` reaches them through its own small interface, so it's testable with a fake and the UI never sees them.
- **`FlagAnswered`** joins `FlagSeen`/`FlagFlagged`. It's only set by `send`, and the UI doesn't toggle it.
- **Wiring:** `ui.Connector` returns `ui.Conn{Mailbox mail.Mailbox; Sender mail.Sender}`. `Sender` is nil when `smtp_host` is unset. One password resolution feeds both.
- **Library:** `github.com/emersion/go-smtp` (v0.25.x; same author and ecosystem as go-imap v2 and go-message). **New dependency, approved 2026-11-12.** Its API is checked against current docs before use.

## Testing Strategy

- **`compose` (pure, ≥ 90% coverage):** table tests for:
  - recipients: Reply-To, reply to my own message, reply-all self-removal and dedupe, Cc-to-To promotion
  - subject prefixes (`Re:`/`RE:`/`Fwd:`/`Fw:`) and References chains
  - quoting (empty lines, CRLF) and the forward header block
  - editor-file parse errors: unknown header, bad address, no recipients
  - MIME output: golden `.eml` files in `testdata/`, encoded words, no `Bcc` header, attachments
- **`mail/smtp` (≥ 80%):** against an in-process `go-smtp` server with a self-signed cert, covering implicit TLS, STARTTLS, auth failure, rejected recipient, and context cancellation.
- **`mail/imap`:** `Raw`, `Append` (lands in Sent with `\Seen`) and `\Answered`, against `imapmemserver`.
- **`send`:** with a fake transport and a fake store:
  - step order
  - forward attachment copy
  - `save_sent` on/off
  - no Sent folder (warning)
  - Append/flag failures wrap `ErrAfterDelivery`
  - an SMTP failure skips steps 4–5
- **`config`:** new keys, defaults (`save_sent` per host, ports per TLS mode), validation messages, and the append output.
- **`ui`:** `Update`-level tests with a fake `Sender` and a stub editor command:
  - each key builds the right draft
  - unchanged file → discarded
  - confirm-popup keys: `esc` doesn't discard
  - retry after failure
  - `ErrAfterDelivery` → warning with no retry
  - `q` blocked during send
  - the not-configured message
  - `ctrl+r` refreshes
- **Live check (manual, before release):** reply, reply-all, forward with an attachment, and new message, on Gmail and on one non-Gmail account (e.g. Proton Bridge or Fastmail). Each lands in Sent exactly once, threads in the recipient's client, and shows as answered.

## Boundaries

Everything in [SPEC.md § Boundaries](../SPEC.md#boundaries) still applies, plus:

- **Always:** build the message bytes once and send and save those same bytes. Treat any failure after delivery as a warning, never as a retry.
- **Ask first:** sending to any real address other than your own during development (live tests send to yourself only).
- **Never:**
  - Log message bodies, recipients' addresses or the SMTP password. The debug log records account, step, duration, byte size and recipient count.
  - Fall back to a plaintext SMTP connection.
  - Leave a draft temp file behind on a normal exit.

## Success Criteria

1. On a thread in Inbox, `r` → write → save → `y` delivers a reply. The recipient's client threads it under the original, the original shows as answered, and the reply appears in Sent **once** on Gmail and once on a non-Gmail account.
2. `R` on a message with To and Cc sends to all of them except me, with no duplicates.
3. `f` on a message with a PDF delivers the PDF intact (same bytes) to the new recipient.
4. `c` sends a new message from the current account.
5. Quitting the editor without changes discards silently, with no popup and nothing sent.
6. With the network off, sending shows the error in the confirm popup. Turning the network back on and pressing `y` delivers it, exactly once.
7. An account without `smtp_host` shows the "Sending isn't set up" status for `r`/`R`/`f`/`c`, and everything else works as before.
8. `ctrl+r` refreshes. `r` replies.
9. `go test -race ./...`, `go vet ./...` and `gofmt -l .` pass, and coverage targets are met.

## Out of Scope

Tracked in [future-plans.md](future-plans.md):

- Markdown → HTML sending (`multipart/alternative`); v1 is plain text through a replaceable body seam.
- Attaching local files.
- Saving drafts to the IMAP Drafts folder; v1 keeps failed drafts in memory only.

Also not in this version:

- Multiple identities or aliases per account, and choosing `From`.
- Signatures.
- Removing forwarded attachments before sending.
- Quoting the whole thread, or inline (bottom-posted) reply editing helpers.
- OAuth / XOAUTH2 for SMTP and IMAP, needed for Microsoft 365 and Outlook.com (tracked in [future-plans.md](future-plans.md)).
- Showing an "answered" marker in the thread list.
- An outbox, queued or offline sending, or scheduled send.
- Cross-folder threading (replies in Sent still won't appear inside Inbox threads; see SPEC.md Assumption 3).

## Decisions

1. **Keys:** `r` reply, `R` reply-all, `f` forward, `c` compose. Refresh moves from `r` to `ctrl+r`.
2. **SMTP library:** `github.com/emersion/go-smtp`.
3. **After the editor closes:** confirm popup (`y` send · `e` edit · `d` discard), never auto-send.
4. **Plain text only;** the body builder is the seam for Markdown later.
5. **Sent copies via IMAP APPEND,** skipped by default on Gmail (`save_sent`).
6. **Target message:** like Gmail, the latest in the thread by default, and the selected message on an expanded row.
7. **Version:** ships as `v0.2.0` (new feature, 0.x minor bump), with `-beta.N` tags along the way. Tags need your approval.

## Open Questions

Resolved:

- **Reply target:** like Gmail. The latest message by default (collapsed thread, reader), and the selected message on an expanded row, the way Gmail's `n`/`p` focus a message for `r`/`a`/`f`.
- **`save_sent` default for Microsoft 365 / Outlook.com:** doesn't apply yet, because those accounts need OAuth to connect at all. It's deferred to [future-plans.md](future-plans.md).

None open.
