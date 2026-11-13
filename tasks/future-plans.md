# Future Plans

Ideas deliberately deferred from the current scope. Each entry notes why it was
deferred and what the current design must keep possible.

## Incoming

- Do not show mail count in Bin/Trash

## Outgoing

### High Priority

- Sending covers reply, reply-all, forward and new message in plain text,
written in dk-email's built-in composer, with drafts saved to and reopened
from the IMAP Drafts folder ([compose-plan.md](compose-plan.md)).

### Med

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



### Low Prority

- **Undo in the composer**: the Bubbles textarea has no undo history.
- **Hard wrapping / format=flowed**: the body is sent exactly as typed;
  wrapping long lines or sending `format=flowed` would read better in some
  clients.
- **Address autocomplete**: suggest recipients from recent mail.


## General

### High Priority

- Keeping app open overnight, loses authentication, need to reopen app etc
- Account without SMTP, have ability to edit account
- **Generic password sources**: support credential sources beyond
  `password_cmd` and the OS keychain, with finer control over how secrets are
  resolved. The rule against storing a plaintext `password` in config stays.
- **OAuth 2.0 (XOAUTH2) for Microsoft 365 and Outlook.com**: Exchange Online
  no longer accepts password (basic) auth for IMAP, and SMTP basic auth is off
  by default for existing tenants from the end of December 2026, so these
  accounts can't connect today. Needs an OAuth sign-in flow, token refresh and
  storage in the keychain, used by both IMAP and SMTP. Once supported, check
  whether Exchange already files SMTP-sent mail in Sent Items and set the
  `save_sent` default to match.
  Sources: [Deprecation of Basic authentication in Exchange Online](https://learn.microsoft.com/en-us/exchange/clients-and-mobile-in-exchange-online/deprecation-of-basic-authentication-exchange-online),
  [SMTP AUTH deprecation timeline](https://techcommunity.microsoft.com/blog/exchange/updated-exchange-online-smtp-auth-basic-authentication-deprecation-timeline/4489835).


### To be planned

- Signautre
- Edit Account
- Settings config