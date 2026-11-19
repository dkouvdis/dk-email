# Passwords

How dk-email finds the password for an account: from a command, such as
1Password, or from the OS keychain. For which login to pick for your
provider, see [Choosing a login](accounts.md#choosing-a-login).

> [!IMPORTANT]
> **No password or secret is ever written to `config.toml`.** A `password`
> or `oauth_client_secret` key is rejected. Secrets come from a command, the
> OS keychain, or an OAuth sign-in kept in the keychain. See
> [Accounts](accounts.md#choosing-a-login) for the options.

- [Lookup order](#lookup-order)
- [1Password](#1password)
- [GPG and pass](#gpg-and-pass)
- [Keychain](#keychain)
- [An account that needs a password](#an-account-that-needs-a-password)
- [OAuth accounts](#oauth-accounts)
- [Troubleshooting](#troubleshooting)

## Lookup order

dk-email looks for a password in this order:

1. **`password_cmd`**: run with `sh -c`, using the first line of its output.
   Anything after the first line is ignored. It times out after 60s. If
   `op` isn't on your `PATH` when dk-email starts, use its absolute path.
2. **The OS keychain**: used when `password_cmd` is omitted.

The command runs in your environment with no terminal to type into, so it
must not wait for keyboard input. A desktop prompt such as 1Password's
Touch ID approval works; dk-email waits up to 60 seconds for it.

Other `password_cmd` examples:

| Source | `password_cmd` |
|---|---|
| 1Password | `op read op://Personal/Mail/password` |
| pass / gopass ([setup](gpg.md)) | `pass show mail/work` |
| GPG-encrypted file ([setup](gpg.md)) | `gpg --quiet --decrypt ~/.config/dk-email/work.gpg` |
| macOS Keychain (an entry of your own) | `security find-generic-password -s mail-work -w` |
| Linux Secret Service (an entry of your own) | `secret-tool lookup service mail account work` |
| Bitwarden | `bw get password mail-work` |
| Environment variable | `printenv WORK_MAIL_PASSWORD` |

The two keychain rows read an entry you created yourself, under whatever
names you chose. Leave `password_cmd` out to use the entry dk-email manages
(see [Keychain](#keychain)).

## 1Password

```toml
password_cmd = "op read op://Personal/Gmail/app-password"
```

## GPG and pass

```toml
password_cmd = "gpg --quiet --decrypt ~/.config/dk-email/work.gpg"  # or "pass show mail/work"
```

gpg must ask for its passphrase in a graphical pinentry (such as
`pinentry-mac`), or have it cached. See [Passwords from GPG](gpg.md).

## Keychain

Omit `password_cmd`. dk-email reads the keychain entry whose **service** is
`dk-email` and whose **account** is the account's `name`. The name must
match exactly, including case and spaces.

```toml
[[account]]
name      = "Personal"
email     = "me@gmail.com"
imap_host = "imap.gmail.com"
# imap_port defaults to 993 (implicit TLS) or 143 (starttls); username defaults to email
```

There are two ways to store the password:

- **In the app:** press `A` and choose "enter once → keychain". For an
  existing account that has no password, `A` asks only for the password.
- **By hand on macOS.** `-w` comes last with no value, so `security`
  prompts for the password and it never lands in your shell history. `-U`
  replaces an existing entry.

  ```sh
  security add-generic-password -s dk-email -a "Personal" -U -w
  ```

To check what's stored:

```sh
security find-generic-password -s dk-email -a "Personal" -w
```

A password stored by dk-email itself prints as `go-keyring-base64:…`. That
is expected: the keychain library encodes it, and dk-email decodes it when
reading.

**Linux** uses the Secret Service (GNOME Keyring or KWallet), in the default
"login" collection. dk-email's entries carry the attributes `service` and
`username`, where `username` is the account's `name`:

```sh
secret-tool store --label "dk-email" service dk-email username "Personal"
```

## An account that needs a password

If neither `password_cmd` nor the keychain yields a password, the account
shows as needing a password, and `A` opens a prompt to store one in the
keychain.

## OAuth accounts

Accounts with `auth = "oauth2"` don't use a password: they sign in with
OAuth 2.0, and the sign-in is kept in the keychain. With `auth = "oauth2"`,
`password_cmd` instead prints an access token, for external token tools such
as `oama`. See [Signing in with OAuth 2.0](oauth.md).

## Troubleshooting

- `no password: set password_cmd or store one in the keychain` means no
  entry has service `dk-email` and account exactly equal to `name`.
- On macOS, dk-email reads the keychain through `/usr/bin/security`. Entries
  created with `security` or by dk-email are readable without a prompt. An
  entry created in Keychain Access may make macOS ask whether `security` can
  read it. Choose "Always Allow".
- A `password_cmd` that fails, is dismissed or times out shows its message on
  the account. Fix the cause and press `ctrl+r` to try again. See
  [When the login test fails](accounts.md#when-the-login-test-fails) and,
  for gpg, [Troubleshooting](gpg.md#troubleshooting).
