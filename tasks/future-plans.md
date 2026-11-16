# Future Plans

Ideas deliberately deferred from the current scope, and known bugs. Each
entry notes why it matters and, where it applies, what the current design
must keep possible.

## To be refined
- using email alias

## Bugs

- **Authentication lost overnight**: with the app left open overnight, the
  account stops working and the app has to be restarted. The IMAP client is
  meant to reconnect on a dropped connection, so find out why that path fails
  (expired session, stale TLS connection, a context that never cancels).
  Plan together with IDLE below: both are about long-lived connections.

## High Priority

- **Search**: server-side IMAP `SEARCH`, plus Gmail's `X-GM-RAW` for its
  search syntax. Needs a search method on `mail.Mailbox` (keeping
  `internal/mail` provider-agnostic), the IMAP implementation and a results
  view in the thread list.
- **Save and open attachments**: the reader only lists attachments. Add
  saving one to disk and opening it with the system's default app.
- **Undo for delete, archive and move**: `d` deletes without confirmation by
  design, so offer a short undo (e.g. `z` within a few seconds). The
  optimistic updates in `internal/ui/actions.go` already keep what's needed to
  reverse a move.
- **OAuth 2.0 (XOAUTH2) for Microsoft 365 and Outlook.com**: Exchange Online
  no longer accepts password (basic) auth for IMAP, and SMTP basic auth is off
  by default for existing tenants from the end of December 2026, so these
  accounts can't connect today. Needs an OAuth sign-in flow, token refresh and
  storage in the keychain, used by both IMAP and SMTP. Once supported, check
  whether Exchange already files SMTP-sent mail in Sent Items and set the
  `save_sent` default to match.
  Sources: [Deprecation of Basic authentication in Exchange Online](https://learn.microsoft.com/en-us/exchange/clients-and-mobile-in-exchange-online/deprecation-of-basic-authentication-exchange-online),
  [SMTP AUTH deprecation timeline](https://techcommunity.microsoft.com/blog/exchange/updated-exchange-online-smtp-auth-basic-authentication-deprecation-timeline/4489835).
- **Send-as aliases**: send from own-domain addresses through an existing
  account. Aliases (`{email, name}`) are listed in config or entered when
  connecting an account; the composer gets a From field, and replies start
  from the address the original was sent to. See
  [docs/ideas/send-as-aliases.md](../docs/ideas/send-as-aliases.md).
- **Edit accounts**: change an existing account from the app, including adding
  `smtp_host` to an account that can't send yet. Config updates must keep
  preserving the user's formatting and comments.

## Medium Priority

### Incoming

- **New mail push and notifications**: use IMAP `IDLE` instead of polling on
  `refresh_interval`, and announce new mail with a terminal bell or desktop
  notification. Shares the reconnect work with the overnight bug above.
- **Multi-select and bulk actions**: mark several threads, then archive,
  delete, move or mark them read at once. Includes "mark folder as read".
- **Scrollbar in the reader**.

### Outgoing

- **Markdown → HTML sending**: write Markdown and send
  `multipart/alternative` (plain text + HTML) so links like `[here](https://…)`
  render as clickable text. Body building stays behind a seam so this can be
  added without a rewrite.
- **Attach local files**: add files from disk to replies and new messages
  (e.g. a file picker in the composer). Forwarding the original message's
  attachments already works.
- **Mark the original answered for resumed replies**: a reply saved to Drafts
  and sent later keeps its threading headers, but the original isn't flagged
  `\Answered` because the link to it isn't stored with the draft.
- **Signature**: a per-account signature added to new messages, replies and
  forwards, shared by the account's aliases.

### General

- **Generic password sources**: support credential sources beyond
  `password_cmd` and the OS keychain, with finer control over how secrets are
  resolved. The rule against storing a plaintext `password` in config stays.
- **Settings in the app**: change UI settings without editing the config file
  by hand.

## Low Priority

- **Undo in the composer**: the Bubbles textarea has no undo history.
- **Hard wrapping / format=flowed**: the body is sent exactly as typed;
  wrapping long lines or sending `format=flowed` would read better in some
  clients.
- **Address autocomplete**: suggest recipients from recent mail.
- **Open in browser**: open HTML-heavy mail that renders poorly as Markdown in
  the system browser.
- **Local cache**: keep folders and messages on disk for faster startup and
  reading offline. A large change; defer until the rest is settled.
