# Editing and deleting accounts

You can change or remove an account from inside dk-email, without editing
`config.toml` by hand. To connect a new one, see
[Adding an account](adding-accounts.md).

## Editing an account

Move the sidebar cursor to the account (any of its folders, or its header
if it failed to connect) and press `E`. The **Edit account** popup opens
with the account's current settings filled in:

- name, email and username
- IMAP host, port and TLS
- login: password source or OAuth 2.0
- SMTP host, port and TLS
- aliases

The keys are the same as in the connect popup: `tab`/`↓` and
`shift+tab`/`↑` move between fields, `←`/`→` or `space` change a choice,
`enter` saves and `esc` cancels.

### Login

The **Login** row starts on **Keep current**, which leaves the login as it
is: the keychain entry, your `password_cmd`, or the OAuth sign-in. Switch it
to change it:

- **1Password:** enter an `op://` reference. It's saved as
  `password_cmd = "op read …"`.
- **Keychain:** type the new password. It replaces the one in the keychain.
- **OAuth 2.0:** pick the provider and enter your app's client ID, then
  **Sign in & save** signs in in your browser. Use this to switch an
  account to OAuth, or to sign in again when the sign-in has expired. See
  [Signing in with OAuth 2.0](oauth.md).

If you rename an account that uses the keychain or OAuth, its password or
sign-in moves to the new name.

### Saving

Saving works like connecting a new account: dk-email first tests the IMAP
login (after the browser sign-in, for OAuth), and the SMTP login when an SMTP host is set. Only if that works does
it save the account and reconnect it, with no restart.

If the test fails, the error shows in the popup. Fix the field and press
`enter` to test again, or press `ctrl+s` to **save anyway**. Use that when
you know the settings are right but the test can't pass right now: you're
offline, the server is down, or your password manager is locked. The status
bar then says the account was saved without a working login. If it still
can't connect, the error tells you to press `E` to edit it again.

Save anyway only skips the login test. With **OAuth 2.0** chosen it asks you
to sign in first instead, because the sign-in is what gets saved. The same config checks always run,
so for example aliases without an SMTP host are still rejected.

### What's kept

dk-email rewrites the account's `[[account]]` block in place, so the order
of your accounts doesn't change and everything outside the block (other
accounts, `[ui]`, comments between blocks) is kept exactly.

- Keys the popup doesn't show, such as `tls_cert`, `save_sent`,
  `archive_folder` and `from_name`, are kept as they are.
- Alias names are kept for every alias email that stays in the list.
- Comments **inside** the account's block are lost.
- Clearing the SMTP host turns sending off, so the sending keys
  (`smtp_port`, `smtp_tls`, `from_name`, `aliases`, `save_sent`) are removed
  with it.

If `config.toml` was changed by hand since dk-email started, saving stops
with "config changed on disk" and nothing is written. Restart dk-email and
try again.

## Deleting an account

Deleting is only possible from the edit popup, so it can't happen by a
stray key press. Press `E`, move to the **Delete account** row after
Submit and press `enter`. The popup asks to confirm:

| Key | Action |
|---|---|
| `y` | delete the account (and its keychain password or OAuth sign-in, if it has one) |
| `k` | delete the account but keep its keychain password or OAuth sign-in |
| `n` / `esc` | cancel |

`k` is only offered for accounts that use the keychain or OAuth. Deleting removes
the account's block from `config.toml`, disconnects it and moves the
sidebar to the next account. If it was your last account, the connect
popup opens as on first run.

An account can't be deleted while it still has changes in flight, such as
a pending move or a message being sent. Wait for them to finish and try
again.
