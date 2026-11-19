# Troubleshooting

Where to look when something goes wrong: the debug log first, then the
troubleshooting section of the guide for the part that's failing.

- [The debug log](#the-debug-log)
- [Problems by topic](#problems-by-topic)

## The debug log

```sh
DK_EMAIL_DEBUG=1 dk-email
```

This appends to `$XDG_CACHE_HOME/dk-email/debug.log` (default
`~/.cache/dk-email/debug.log`).

- It records timings and metadata only, never passwords, tokens or message
  bodies.
- It also records IMAP drops and reconnects, each login step, and the
  `watch ...` lines for push, `new mail`, `push refresh` and
  `window focus` / `window blur`. See [New-mail
  notifications](notifications.md#push) for what the `watch` lines mean.
- It may contain folder names and email addresses from server errors, so
  check it before sharing.

## Problems by topic

- **Password or keychain problems:** [Passwords](passwords.md#troubleshooting).
- **`password_cmd` with gpg or pass asks for a passphrase in the wrong place:**
  [Passwords from GPG](gpg.md#troubleshooting).
- **OAuth sign-in or "refused the access token":** [Signing in with OAuth 2.0](oauth.md#troubleshooting).
- **A login test fails when adding an account:** [Accounts](accounts.md#when-the-login-test-fails).
- **No alert, no banner, or alerts only every couple of minutes:**
  [New-mail notifications](notifications.md#troubleshooting).
- **Attachments won't attach or open:** [Attachments](attachments.md#status-messages).
- **Search returns nothing or something unexpected:** [Searching](search.md#input-dk-email-doesnt-understand).
