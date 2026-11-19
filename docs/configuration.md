# Configuration

Everything dk-email needs lives in one TOML file. You never have to edit it
by hand: the connect popup adds accounts for you. This guide lists every
key, its default and where to read more.

- [Where the config lives](#where-the-config-lives)
- [Start from the example](#start-from-the-example)
- [Example](#example)
- [`[ui]`](#ui)
- [`[notify]`](#notify)
- [`[colors]`](#colors)
- [`[[account]]`](#account)
- [Validation](#validation)

## Where the config lives

The config folder is `$XDG_CONFIG_HOME/dk-email/` (default
`~/.config/dk-email/`) on every OS, macOS included. The file is
`config.toml` inside it. The folder is `0700`, `config.toml` is `0600`, and
a `.gitignore` is written so the folder can be kept in a dotfiles repo
safely. The `.gitignore` lists only `config.toml` (plus `.gitignore`,
`keys.toml` and `themes/`) as tracked, so other files in the folder, such as
a certificate or an encrypted password file, stay out of git.

To use another file, run `dk-email --config path/to.toml`.

If the file doesn't exist, dk-email starts with the defaults and no
accounts, and the connect popup opens. See
[Adding an account](accounts.md#adding-an-account). Adding an account
appends an `[[account]]` block and keeps your comments and formatting.

## Start from the example

[`config.example.toml`](../config.example.toml) is a commented starter with
every key in it. Copy it into place and edit it:

```sh
mkdir -p ~/.config/dk-email
cp config.example.toml ~/.config/dk-email/config.toml
chmod 600 ~/.config/dk-email/config.toml
```

Or skip the file and let the connect popup (`A`) write your accounts. Tests
keep the example in step with the code: it must load without errors and
mention every key.

## Example

```toml
[ui]
default_layout   = "split"   # "split" | "list"
refresh_interval = "120s"

[notify]
bell    = true   # ring the bell on new INBOX mail (default true)
desktop = true   # send a desktop notification, OSC 9 (default true)
title   = true   # show the INBOX unread count in the window title (default true)

[[account]]
name         = "Personal"
email        = "me@gmail.com"
username     = "me@gmail.com"                               # optional, defaults to email
imap_host    = "imap.gmail.com"
imap_port    = 993                                          # optional, 993 for implicit, 143 for starttls
tls          = "implicit"                                   # "implicit" (default) | "starttls"
password_cmd = "op read op://Personal/Gmail/app-password"   # omit to use the keychain
# archive_folder = "[Gmail]/All Mail"                       # optional, overrides the detected archive folder
# tls_cert       = "~/.config/dk-email/bridge.pem"          # optional, trust this cert instead of the system roots
# smtp_host      = "smtp.gmail.com"                         # optional, enables sending; omit to disable sending
# smtp_port      = 465                                      # optional, 465 for implicit, 587 for starttls
# smtp_tls       = "implicit"                               # optional, "implicit" (default) | "starttls"
# from_name      = "Your Name"                              # optional, display name on outgoing mail
# aliases        = [{ email = "me@example.com", name = "Me" }]   # optional, other From addresses (needs smtp_host)
# save_sent      = true                                     # optional, save a copy of sent mail to Sent (default: false on Gmail and Microsoft 365/Outlook, true elsewhere)
# auth           = "oauth2"                                 # optional, sign in with OAuth 2.0 instead of a password (see oauth.md)
# oauth_provider = "microsoft"                              # with auth = "oauth2": "microsoft" or "google"
# oauth_client_id = "…"                                     # with auth = "oauth2": your app's client ID
```

## `[ui]`

| Key | Default | Meaning |
|---|---|---|
| `default_layout` | `"split"` | `"split"` (folders │ threads) or `"list"` (threads only). `v` toggles it while running. List is forced below 80 columns. |
| `refresh_interval` | `"120s"` | How often dk-email refreshes every account. A duration such as `"120s"` or `"2m"`; it must be positive. |

## `[notify]`

| Key | Default | Meaning |
|---|---|---|
| `bell` | `true` | Ring the bell on new INBOX mail. |
| `desktop` | `true` | Send a desktop notification (OSC 9). |
| `title` | `true` | Show the INBOX unread count in the window title. |

All keys are optional. See [New-mail notifications](notifications.md).

## `[colors]`

By default dk-email uses your terminal's 16-colour palette, so it follows
your terminal theme (Ghostty, iTerm2, etc.), light or dark. Change the
terminal theme and dk-email changes with it. The one exception is the text
on the selected row, which is a fixed black: many dark themes make palette
black a grey that barely shows on the accent.

To override a colour, add a `[colors]` table. Every key is optional:

```toml
[colors]
accent    = 5          # focus, selected row background (default: magenta)
on_accent = 16         # text on the accent colour (default: black, fixed)
muted     = "#808080"  # secondary text (default: the terminal's faint style)
border    = 8          # pane borders, separators (default: bright black)
unread    = 4          # unread markers and counts (default: blue)
star      = 3          # starred marker (default: yellow)
error     = 1          # errors (default: red)
hint      = 6          # the hint bar (default: cyan)
key       = 5          # keys in the help popup (default: magenta)
```

A value is an ANSI colour number (`0`–`255`, quoted or not) or a hex colour
(`"#rgb"` or `"#rrggbb"`). Numbers `0`–`15` follow your terminal theme;
higher numbers and hex colours are fixed. Unknown keys and invalid values
are errors.

## `[[account]]`

One `[[account]]` block per account. `name`, `imap_host` and `username` (or
`email`, which `username` falls back to) are required.

| Key | Default | Meaning |
|---|---|---|
| `name` | none, required | Sidebar label and the key of the account's keychain entry. Unique, ignoring case. |
| `email` | none | Your address. Also the default `username`. |
| `username` | `email` | The IMAP and SMTP login. |
| `imap_host` | none, required | Incoming mail server. |
| `imap_port` | 993 for `implicit`, 143 for `starttls` | Incoming mail port. |
| `tls` | `"implicit"` | `"implicit"` or `"starttls"`. dk-email never falls back to plaintext. |
| `password_cmd` | none (use the keychain) | Command that prints the password. See [Passwords](passwords.md). |
| `auth` | `"password"` | `"password"` or `"oauth2"`. See [Signing in with OAuth 2.0](oauth.md#configuration-reference). |
| `oauth_provider` | none | `"microsoft"` or `"google"`; needs `auth = "oauth2"`. See [OAuth](oauth.md#configuration-reference). |
| `oauth_client_id` | none | Your app's client ID; needs `auth = "oauth2"`. |
| `oauth_tenant` | `"common"` (Microsoft only) | Microsoft tenant ID or domain. |
| `oauth_client_secret_cmd` | none (use the keychain) | Google only. Command that prints the client secret. |
| `archive_folder` | the folder marked `\Archive`, else `\All` (Gmail's All Mail) | Where `e` archives to. If neither exists, archive is disabled for the account. |
| `tls_cert` | none | PEM file of certificates to trust instead of the system roots, for IMAP and SMTP. Accepts `~/`; a relative path is resolved from the config file's folder. See [Proton Mail](accounts.md#proton-mail). |
| `smtp_host` | none (sending off) | Outgoing mail server. Setting it turns sending on. See [Sending mail](sending.md). |
| `smtp_port` | 465 for `implicit`, 587 for `starttls` | Outgoing port. Needs `smtp_host`. |
| `smtp_tls` | `"implicit"` | `"implicit"` or `"starttls"`. Needs `smtp_host`. |
| `from_name` | none (address only) | Display name on outgoing mail. Needs `smtp_host`. |
| `aliases` | none | Other From addresses, `[{ email = "…", name = "…" }]`. Needs `smtp_host`. See [Sending from aliases](send-as.md). |
| `save_sent` | `false` on Gmail and Microsoft 365/Outlook, else `true` | Save a copy of sent mail to Sent over IMAP. Needs `smtp_host`. See [Sending mail](sending.md). |

Notes:

- `archive_folder` defaults to the folder marked `\Archive`, or else `\All`.
- SMTP reuses the account's IMAP login and `tls_cert`; there is no separate
  SMTP password.
- Passwords never go in this file; see [Passwords](passwords.md).

## Validation

dk-email checks the whole file when it starts and reports every problem
together, so you can fix them in one pass.

- Unknown keys are errors, so a typo like `pasword_cmd` can't silently fall
  back to the keychain.
- A `password` key is rejected, and so is `oauth_client_secret`: passwords
  and secrets never go in this file.
- `smtp_port`, `smtp_tls`, `from_name`, `aliases` and `save_sent` without
  `smtp_host` are errors.
- `refresh_interval` must be positive, and `default_layout` must be
  `"split"` or `"list"`.
- `oauth_*` keys without `auth = "oauth2"` are errors, as is combining them
  with `password_cmd`. See [OAuth](oauth.md#configuration-reference).
- Account names must be unique, and aliases must be plain, distinct
  addresses.
- Colours must be an ANSI number 0–255 or a hex colour.
