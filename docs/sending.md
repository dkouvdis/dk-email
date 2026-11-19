# Sending mail

This guide covers turning on sending for an account, writing and sending a
message, saving drafts, and what to do when a send fails.

- [Setting up sending](#setting-up-sending)
- [Writing a message](#writing-a-message)
- [Drafts](#drafts)
- [Reply, reply all and forward](#reply-reply-all-and-forward)
- [Setting up sending when you connect an account](#setting-up-sending-when-you-connect-an-account)
- [Troubleshooting](#troubleshooting)

## Setting up sending

Sending is opt-in per account: an account sends only when it has an
`smtp_host`. Without one, `r`, `R`, `f` and `c` show `Sending isn't set up for
<account>: add smtp_host to its config`, and everything else works as before.

```toml
[[account]]
name         = "Personal"
email        = "me@gmail.com"
imap_host    = "imap.gmail.com"
password_cmd = "op read op://Personal/Gmail/app-password"
smtp_host    = "smtp.gmail.com"    # enables sending
smtp_port    = 465                 # optional, 465 for implicit, 587 for starttls
smtp_tls     = "implicit"          # optional, "implicit" (default) | "starttls"
from_name    = "Your Name"         # optional, display name on outgoing mail; default: the address only
save_sent    = false               # optional, save a copy to Sent; default: false on Gmail and Microsoft 365/Outlook, true elsewhere
```

- **Credentials:** SMTP logs in with the account's `username` and the same
  password as IMAP (`password_cmd` or the keychain), or, for OAuth accounts,
  the same token with `XOAUTH2`. There's no separate SMTP password. For Gmail,
  the same app password works for IMAP and SMTP. See
  [Passwords](passwords.md).
- **TLS:** the connection is always encrypted, and dk-email never falls back
  to plaintext. With `smtp_tls = "starttls"`, STARTTLS is mandatory. `tls_cert`
  applies to SMTP too, so Proton Mail Bridge works for both.
- **`smtp_port`** defaults to 465 for `implicit` and 587 for `starttls`.
  `smtp_tls` defaults to `implicit`.
- **`save_sent`:** dk-email saves a copy of each sent message to the account's
  Sent folder over IMAP. Gmail, Exchange Online and Outlook.com already file
  mail sent through their SMTP servers in Sent, so the default is `false` for
  the IMAP hosts `imap.gmail.com`, `outlook.office365.com` and
  `imap-mail.outlook.com`, or the SMTP hosts `smtp.office365.com` and
  `smtp-mail.outlook.com`, to avoid duplicates. It is `true` everywhere else.
  Set `save_sent = false` for any other provider that files SMTP-sent mail in
  Sent itself. If the account has no Sent folder, the message is still sent and
  the status warns.
- **`aliases`:** other addresses this account can send as, such as a custom
  domain. Gmail needs each one verified under "Send mail as" first, or it
  silently rewrites From. An address with its own SMTP login is a separate
  account. See [Sending from aliases](send-as.md).
- `smtp_port`, `smtp_tls`, `from_name`, `aliases` and `save_sent` without
  `smtp_host` are config errors, and so is an `smtp_tls` other than `implicit`
  or `starttls`, or an `smtp_port` outside 1 to 65535.
- **Microsoft 365 and Outlook.com** don't accept passwords: they sign in with
  [OAuth 2.0](oauth.md), for sending too.

All keys are listed in [Configuration](configuration.md).

## Writing a message

- `r` replies, `R` replies to everyone, `f` forwards and `c` starts a new
  message from the account under the sidebar cursor (the undimmed one).
- **Target:** on a collapsed thread and in the reader, `r`, `R` and `f` use the
  thread's latest message. On an expanded row, they use the selected message.
  (In the reader, a thread opened from a message row targets that message.)
- The composer opens in a popup, with **To**, **Cc**, **Bcc**, **Subject** and
  the body as plain-text fields. Its title shows the action, the subject and
  the account's address. dk-email never hands the terminal to another program.
- Replies start in the body, above the quote. New messages and forwards start
  in `To`. With `aliases`, a **From** row above `To` picks the sending address,
  and replies start from the address the message was sent to. See
  [Sending from aliases](send-as.md).
- Addresses are separated by commas, as in
  `Alice <alice@example.com>, bob@example.com`.
- The body is sent exactly as typed. Long lines are only wrapped on screen, and
  tabs become 4 spaces.

| Key | Action |
|---|---|
| `ctrl+s` | send |
| `ctrl+o` | attach a file (or drag and drop files onto the terminal) |
| `tab` / `shift+tab` | next / previous field |
| `←` / `→`, `d` | on the Attach row, choose / remove an attachment |
| `enter` | in a header field, move to the next field (in the body, a new line) |
| `esc` | close at once if nothing changed, otherwise ask what to do |
| `ctrl+c` | quit, even while composing |

On the From row, `enter` opens the list of addresses (`↑`/`↓` and `enter` pick,
`esc` closes it), and `←`/`→` switch address without opening it.

Attaching files has its own guide: see
[Attachments](attachments.md#attaching-files).

- If you press `esc` after changing something, the popup's last row asks
  (`Save draft?`) and the hint bar lists the answers: `s` saves to Drafts, `d`
  discards, `e` keeps editing, and `esc` saves and closes. A draft is only
  discarded on purpose. If the account can't save drafts, only `d` and `e` are
  offered.
- While a draft is saving, `esc` cancels the save and leaves the composer open.
  If the server already stored the draft, a retry can leave a duplicate.
- Sending happens in the background (`Sending…`). When it works, the status
  line says `Sent to <first recipient>`, plus `(+n)` for the others. `q` types a
  `q` while you compose, so it never quits by accident.
- A bad address or a failed send shows in the composer's footer, with your text
  intact, and `ctrl+s` retries. See [Troubleshooting](#troubleshooting).
- If the message was delivered but saving the Sent copy, marking the original
  answered or removing the draft failed, the composer closes and the status bar
  says so. dk-email never retries then, so nothing is sent twice.

## Drafts

- Choosing **save** in the `esc` prompt stores the message in the account's
  Drafts folder, flagged `\Draft` and `\Seen`. Saving needs no recipients. The
  saved copy keeps `Bcc`, the threading headers of a reply and every
  attachment. The status line says `Draft saved`.
- Press `enter` on a message in the Drafts folder (or click it twice) to
  reopen it in the composer. Saving it again replaces the old copy, and sending
  it removes the draft.
- Removing a single message needs the IMAP **UIDPLUS** extension, which Gmail,
  Fastmail and most other servers have. Without it, the old copy is kept and
  the status says so, so you may see two copies in Drafts. dk-email never uses
  a plain `EXPUNGE`, which could remove other messages you flagged for
  deletion.
- A resumed reply still threads, but sending it doesn't mark the original
  answered.
- Drafts work for accounts that have `smtp_host`, like the other compose keys.

## Reply, reply all and forward

- **Reply** goes to the message's `Reply-To`, else its sender. If the message
  is from you, it goes to its original recipients. The subject gets `Re: `, and
  `In-Reply-To` and `References` are set so the reply threads correctly. Only
  the target message is quoted, under `On <date>, <sender> wrote:`, with `> `
  at the start of each line. After sending, the original is marked answered.
- **Reply all** also includes the original's To and Cc, minus your own
  addresses (aliases included), with duplicates removed.
- **Forward** starts with an empty `To` and the subject `Fwd: …`. The original
  goes below a `---------- Forwarded message ----------` block with its sender,
  date, subject and recipients. The original's attachments are listed on the
  Attach row with their original names and types, and you can remove any of
  them.
- Outgoing mail is plain text. Attach files with `ctrl+o` or by drag and drop;
  see [Attachments](attachments.md#attaching-files).
- `Bcc` recipients get the message, but the `Bcc` header is never written to a
  sent message. Only the saved draft keeps it.

## Setting up sending when you connect an account

The connect-account popup has optional **SMTP host**, **SMTP port** and **SMTP
TLS** fields. Gmail addresses pre-fill `smtp.gmail.com:465` with implicit TLS.
When an SMTP host is filled in, the popup tests the SMTP login (connect and
sign in, nothing is sent) before saving, and writes the `smtp_*` keys to the
new `[[account]]` block. Leave the host empty if the account shouldn't send.

Press `E` to edit or delete an existing account; see
[Editing an account](accounts.md#editing-an-account).

## Troubleshooting

| What you see | What it means |
|---|---|
| `Sending isn't set up for <account>: add smtp_host to its config` | The account has no `smtp_host`. Add it to the config, or edit the account with `E`. |
| `<account> isn't connected.` | The account hasn't connected yet. Press `ctrl+r` to retry failed accounts. |
| `Send failed: <reason>` in the composer footer | Nothing was sent. Your text is intact, so fix the cause (the address, the network, the login) and press `ctrl+s` again. A send gives up after 30 seconds. |
| `Sent, but couldn't save a copy to Sent: <reason>` in the status bar | The message was delivered. Only the copy in Sent failed. dk-email doesn't retry, so nothing goes out twice. |
| `Sent, but no Sent folder; copy not saved` | The message was delivered, but the account has no Sent folder to hold a copy. Set `save_sent = false` to stop the warning if your provider files sent mail itself. |
| `Sent, but couldn't mark the original answered: <reason>` or `…couldn't remove the draft: <reason>` | Delivered. Only the follow-up step failed, and it isn't retried. |
| `Save failed: <reason>` in the composer footer | The draft wasn't saved. Your text is intact. Try again, or discard. A save gives up after 15 seconds. |
| `Draft saved, but couldn't remove the old copy: server can't delete a single message (no UIDPLUS)` | The new draft is saved, and the old one stays in Drafts. Delete it by hand. |

If several follow-up steps fail, the status joins them in one sentence.

For logs, see [Troubleshooting](troubleshooting.md).
