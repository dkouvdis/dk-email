# dk-email

A keyboard-driven terminal email client for triaging several IMAP accounts (Gmail included). It shows a folders sidebar beside a thread list, opens conversations in a popup, and uses lazygit-style single-key actions.

- Shows each account's folders as a tree with unread counts (none on Trash). Special folders (Inbox, Sent, Trash…) stay at the top, and accounts other than the one you're in are dimmed.
- Groups replies into threads, ordered by newest activity.
- Lets you mark read/unread, star, archive, delete (to Trash) and move mail without leaving the keyboard.
- Has two layouts: narrow (folders │ threads, the default) and list (threads only), toggled with `v`. List is forced below 80 columns.
- Replies, reply-alls, forwards and new messages are written in a built-in composer and sent over SMTP. See [Sending mail](#sending-mail).
- Reads passwords from 1Password (`op read`), any other command, or the OS keychain. They are never stored in a plain-text file.

v0.x is a personal tool: it reads, triages and sends mail. Nothing is cached on disk.

## Install

### Homebrew (macOS, Linux)

```sh
brew install dkouvdis/tap/dk-email
```

The binaries aren't signed by Apple. The cask removes macOS's quarantine flag on install, but if macOS still says the binary "is damaged" or "can't be opened", clear the flag yourself:

```sh
xattr -d com.apple.quarantine "$(which dk-email)"
```

### Go

Requires Go 1.27 or later.

```sh
go install github.com/dkouvdis/dk-email/cmd/dk-email@latest
```

While the repository is private, set `GOPRIVATE=github.com/dkouvdis/dk-email` and make sure `git` can authenticate to GitHub.

### From source

```sh
go build -o bin/dk-email ./cmd/dk-email
```

## Usage

```sh
dk-email                       # uses $XDG_CONFIG_HOME/dk-email/config.toml (or ~/.config/dk-email/config.toml)
dk-email --config path/to.toml # use another config file
dk-email --version
```

On first run with no accounts, the **connect-account popup** opens. It asks for the following:

- a name, email and username;
- the IMAP host, port and TLS mode;
- a password source: either a 1Password reference, or "enter once → keychain";
- optionally, the SMTP host, port and TLS mode, which turn on [sending](#sending-mail).

Gmail addresses pre-fill `imap.gmail.com:993` and `smtp.gmail.com:465` (implicit TLS). dk-email tests the IMAP connection, and the SMTP login too when an SMTP host is filled in, then appends an `[[account]]` block to `config.toml`, keeping your comments and formatting intact. Press `A` at any time to connect another account.

The config folder lives at `$XDG_CONFIG_HOME/dk-email/` (default `~/.config/dk-email/`) on every OS, macOS included. The folder is `0700`, `config.toml` is `0600`, and a `.gitignore` is written so the folder can be kept in a dotfiles repo safely.

## Configuration

```toml
[ui]
default_layout   = "split"   # "split" | "list"
refresh_interval = "120s"

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
# save_sent      = true                                     # optional, save a copy of sent mail to Sent (default: false on Gmail, true elsewhere)
```

- Unknown keys are errors, so a typo like `pasword_cmd` can't silently fall back to the keychain. A `password` key is rejected: passwords never go in this file.
- `archive_folder` defaults to the folder marked `\Archive`, or else `\All` (Gmail's All Mail). If neither exists, archive is disabled for that account.
- `tls_cert` accepts `~/`. A relative path is resolved from the config file's folder. It applies to SMTP as well as IMAP.
- The `smtp_*`, `from_name` and `save_sent` keys are described under [Sending mail](#sending-mail).

### Colours

By default dk-email uses your terminal's 16-colour palette, so it follows your terminal theme (Ghostty, iTerm2, etc.), light or dark. Change the terminal theme and dk-email changes with it.

To override a colour, add a `[colors]` table. Every key is optional:

```toml
[colors]
accent    = 5          # focus, selected row background (default: magenta)
on_accent = 0          # text on the accent colour (default: black)
muted     = "#808080"  # secondary text (default: the terminal's faint style)
border    = 8          # pane borders, separators (default: bright black)
unread    = 4          # unread markers and counts (default: blue)
star      = 3          # starred marker (default: yellow)
error     = 1          # errors (default: red)
hint      = 6          # the hint bar (default: cyan)
key       = 5          # keys in the help popup (default: magenta)
```

A value is an ANSI colour number (`0`–`255`, quoted or not) or a hex colour (`"#rgb"` or `"#rrggbb"`). Numbers `0`–`15` follow your terminal theme; higher numbers and hex colours are fixed. Unknown keys and invalid values are errors.

### Passwords

dk-email looks for a password in this order:

1. **`password_cmd`**: run with `sh -c`, using the first line of its output. It times out after 60s. If `op` isn't on your `PATH` when dk-email starts, use its absolute path.
2. **The OS keychain**: used when `password_cmd` is omitted.

If neither yields a password, the account shows as needing a password, and `A` opens a prompt to store one in the keychain.

#### 1Password

```toml
password_cmd = "op read op://Personal/Gmail/app-password"
```

#### Keychain

Omit `password_cmd`. dk-email reads the keychain entry whose **service** is `dk-email` and whose **account** is the account's `name`. The name must match exactly, including case and spaces.

```toml
[[account]]
name      = "Personal"
email     = "me@gmail.com"
imap_host = "imap.gmail.com"
# imap_port defaults to 993 (implicit TLS) or 143 (starttls); username defaults to email
```

There are two ways to store the password:

- **In the app:** press `A` and choose "enter once → keychain". For an existing account that has no password, `A` asks only for the password.
- **By hand on macOS.** `-w` comes last with no value, so `security` prompts for the password and it never lands in your shell history. `-U` replaces an existing entry.

  ```sh
  security add-generic-password -s dk-email -a "Personal" -U -w
  ```

To check what's stored:

```sh
security find-generic-password -s dk-email -a "Personal" -w
```

A password stored by dk-email itself prints as `go-keyring-base64:…`. That is expected: the keychain library encodes it, and dk-email decodes it when reading.

**Linux** uses the Secret Service (GNOME Keyring or KWallet), in the default "login" collection:

```sh
secret-tool store --label "dk-email" service dk-email username "Personal"
```

**Troubleshooting:**

- `no password: set password_cmd or store one in the keychain` means no entry has service `dk-email` and account exactly equal to `name`.
- On macOS, dk-email reads the keychain through `/usr/bin/security`. Entries created with `security` or by dk-email are readable without a prompt. An entry created in Keychain Access may make macOS ask whether `security` can read it. Choose "Always Allow".

#### Gmail

Gmail rejects your normal Google password over IMAP when 2-Step Verification is on. Create an **app password** at <https://myaccount.google.com/apppasswords>, then store it in 1Password or the keychain. Google Workspace accounts that don't allow app passwords aren't supported, because they need OAuth. The same app password works for sending (see [Sending mail](#sending-mail)).

#### Proton Mail Bridge

```toml
[[account]]
name      = "Proton"
email     = "me@proton.me"
username  = "me@proton.me"                     # the username shown in Bridge's mailbox details
imap_host = "127.0.0.1"
imap_port = 1143
tls       = "starttls"
tls_cert  = "~/.config/dk-email/bridge.pem"
```

Export the certificate from Bridge: **Settings → Advanced settings → Export TLS certificates**. Then copy `cert.pem` to the `tls_cert` path.

Why this is needed: macOS rejects Bridge's 20-year self-signed certificate as "not standards compliant", even if you trust it in Keychain. `tls_cert` tells dk-email to trust that certificate instead of the system roots, for this account only. The host name and expiry are still checked.

The Bridge password (from Bridge's mailbox details, not your Proton password) goes in `password_cmd` or the keychain like any other.

## Sending mail

Sending is opt-in per account: an account sends only when it has an `smtp_host`. Without one, `r`, `R`, `f` and `c` show `Sending isn't set up for <account>: add smtp_host to its config`, and everything else works as before.

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
save_sent    = false               # optional, save a copy to Sent; default: false on Gmail, true elsewhere
```

- **Credentials:** SMTP logs in with the account's `username` and the same password as IMAP (`password_cmd` or the keychain). There's no separate SMTP password. For Gmail, the same app password works for IMAP and SMTP.
- **TLS:** the connection is always encrypted, and dk-email never falls back to plaintext. With `smtp_tls = "starttls"`, STARTTLS is mandatory. `tls_cert` applies to SMTP too, so Proton Mail Bridge works for both.
- **`smtp_port`** defaults to 465 for `implicit` and 587 for `starttls`. `smtp_tls` defaults to `implicit`.
- **`save_sent`:** dk-email saves a copy of each sent message to the account's Sent folder over IMAP. Gmail already files mail sent through its SMTP server in Sent, so the default is `false` for `imap.gmail.com` to avoid duplicates, and `true` everywhere else. Set `save_sent = false` for any other provider that files SMTP-sent mail in Sent itself. If the account has no Sent folder, the message is still sent and the status warns.
- `smtp_port`, `smtp_tls`, `from_name` and `save_sent` without `smtp_host` are config errors.
- **Microsoft 365 and Outlook.com** aren't supported yet. They no longer accept password logins and need OAuth, which dk-email doesn't have.

### Writing a message

- `r` replies, `R` replies to everyone, `f` forwards and `c` starts a new message from the current account.
- **Target:** on a collapsed thread and in the reader, `r`, `R` and `f` use the thread's latest message. On an expanded row, they use the selected message.
- The composer opens in a popup, with **To**, **Cc**, **Bcc**, **Subject** and the body as plain-text fields. Its title shows the action, the subject and the account's address. dk-email never hands the terminal to another program.
- Replies start in the body, above the quote. New messages and forwards start in `To`. `From` isn't editable: it's always the account's address (and `from_name`).
- Addresses are separated by commas, as in `Alice <alice@example.com>, bob@example.com`.
- The body is sent exactly as typed. Long lines are only wrapped on screen, and tabs become 4 spaces.

| Key | Action |
|---|---|
| `ctrl+s` | send |
| `tab` / `shift+tab` | next / previous field |
| `enter` | in a header field, move to the next field (in the body, a new line) |
| `esc` | close at once if nothing changed, otherwise ask what to do |
| `ctrl+c` | quit, even while composing |

- If you press `esc` after changing something, the popup's last row asks (`Save draft?`) and the hint bar lists the answers: `s` saves to Drafts, `d` discards, `e` keeps editing, and `esc` saves and closes. A draft is only discarded on purpose. If the account can't save drafts, only `d` and `e` are offered.
- Sending happens in the background (`Sending…`). A bad address or a failed send shows in the composer's footer, with your text intact, and `ctrl+s` retries. `q` types a `q` while you compose, so it never quits by accident.
- If the message was delivered but saving the Sent copy, marking the original answered or removing the draft failed, the composer closes and the status bar says so. dk-email never retries then, so nothing is sent twice.

### Drafts

- Choosing **save** in the `esc` prompt stores the message in the account's Drafts folder, flagged `\Draft` and `\Seen`. Saving needs no recipients. The saved copy keeps `Bcc`, the threading headers of a reply and a forward's attachments.
- Press `enter` on a message in the Drafts folder (or click to open it) to reopen it in the composer. Saving it again replaces the old copy, and sending it removes the draft.
- Removing a single message needs the IMAP **UIDPLUS** extension, which Gmail, Fastmail and most other servers have. Without it, the old copy is kept and the status says so, so you may see two copies in Drafts. dk-email never uses a plain `EXPUNGE`, which could remove other messages you flagged for deletion.
- A resumed reply still threads, but sending it doesn't mark the original answered.
- Drafts work for accounts that have `smtp_host`, like the other compose keys.

### Reply, reply all and forward

- **Reply** goes to the message's `Reply-To`, else its sender. If the message is from you, it goes to its original recipients. The subject gets `Re: `, and `In-Reply-To` and `References` are set so the reply threads correctly. Only the target message is quoted. After sending, the original is marked answered.
- **Reply all** also includes the original's To and Cc, minus your own address, with duplicates removed.
- **Forward** starts with an empty `To` and the subject `Fwd: …`. The original's attachments are re-attached with their original names and types. They aren't listed in the composer and can't be removed in this version.
- Outgoing mail is plain text.
- `Bcc` recipients get the message, but the `Bcc` header is never written to a sent message. Only the saved draft keeps it.

### Connect popup

The connect-account popup has optional **SMTP host**, **SMTP port** and **SMTP TLS** fields. Gmail addresses pre-fill `smtp.gmail.com:465` with implicit TLS. When an SMTP host is filled in, the popup tests the SMTP login (connect and sign in, nothing is sent) before saving, and writes the `smtp_*` keys to the new `[[account]]` block. Leave the host empty if the account shouldn't send. Existing accounts are edited by hand in `config.toml`.

## Keys

| Key | Action | Key | Action |
|---|---|---|---|
| `j`/`k`, `↓`/`↑` | move | `tab` / `shift+tab` | next / previous pane |
| `h`/`l`, `←`/`→` | focus left / right pane | `enter` | open thread / folder |
| `space` | expand / collapse thread | `g` / `G` | top / bottom |
| `u` | toggle read / unread | `s` | toggle star |
| `e` | archive | `d` | delete (moves to Trash) |
| `m` | move to folder | `ctrl+r` | refresh |
| `r` | reply | `R` | reply all |
| `f` | forward | `c` | new message |
| `a` | focus folders (narrow) / folder picker (list) | `A` | connect account |
| `/` | filter folders | `v` | toggle split ↔ list + popup |
| `?` | key help | `esc` | close popup / back |
| `q` | close the open popup, otherwise quit | `ctrl+c` | quit, even with a popup open |

- Actions on a collapsed thread apply to every message in it. On an expanded row, or in the reader, they apply to the selected message. Opening a message marks it read.
- `r`, `R` and `f` work on the thread list and in the reader. `c` works anywhere a folder is open. In the Drafts folder, `enter` reopens a draft in the composer. See [Sending mail](#sending-mail).
- `enter` opens the conversation in a popup, and `esc` closes it. In the reader, `pgup`/`pgdn` (or `ctrl+u`/`ctrl+d`) scroll by a page, and `space` expands or collapses every message.
- **Mouse:** clicking a folder opens it. Clicking a thread selects it, and clicking it again opens it. The scroll wheel moves the selection (or scrolls the open popup) but never opens anything. Links in a message are underlined; cmd+click (ctrl+click on Linux) opens them while the reader popup is open, in terminals that support hyperlinks. To select text in the terminal, hold shift (or option on macOS) while dragging.
- Every popup lists its keys in the bottom hint bar (`Close: esc`, `Send: ctrl+s`, …), not inside the popup, and the panes behind an open popup are dimmed.
- In the `?` help popup, `/` filters keys by key or description.
- In the folders sidebar and the folder pickers (`a` in list mode, `m` to move), `/` filters folders by name. While typing, every key goes to the filter: `↑`/`↓` move between matches, `enter` opens the highlighted folder, and `esc` clears the filter.
- In the connect popup, `tab`/`↓` and `shift+tab`/`↑` move between fields, and `←`/`→` or `space` change a choice. `enter` submits and `esc` cancels.

Below 80 columns, list mode is forced.

## Debugging

```sh
DK_EMAIL_DEBUG=1 dk-email
```

This appends to `$XDG_CACHE_HOME/dk-email/debug.log` (default `~/.cache/dk-email/debug.log`). The log records timings and metadata only, never passwords or message bodies.

## Development

```sh
go test -race ./...
go vet ./...
gofmt -l .          # must print nothing
```

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org). The release changelog is grouped by `feat:` and `fix:`, and drops `docs:`, `test:` and `chore:`. The spec lives in [SPEC.md](SPEC.md), and the task plan in [tasks/](tasks/).

## Releasing (maintainers)

Versions come only from git tags ([SemVer](https://semver.org)). Nothing in the source holds a version number. GoReleaser injects it at build time.

**One-time prerequisites:**

- The public tap repo `dkouvdis/homebrew-tap` exists.
- The `HOMEBREW_TAP_GITHUB_TOKEN` secret is set on `dkouvdis/dk-email`. It must be a fine-grained personal access token with **Contents: read and write** on the tap repo only.
- Fine-grained tokens expire. When it does, stable releases fail at the cask upload step. Create a new token and update the secret, then re-run the release workflow.
- Optional: `brew install goreleaser` for local dry runs.

**Dry run** (builds into `dist/` and publishes nothing):

```sh
goreleaser check
goreleaser release --snapshot --clean
```

**Release.** Pushing a `v*` tag runs `.github/workflows/release.yml`, which runs the tests, then builds darwin/linux × amd64/arm64 archives with checksums and creates the GitHub release.

```sh
git tag -a v0.1.0-beta.1 -m "v0.1.0-beta.1"
git push origin v0.1.0-beta.1
```

- Tags with `-beta.N` or `-rc.N` become GitHub pre-releases and **never** update the Homebrew tap.
- A stable tag such as `v0.1.0` also pushes the updated cask to `dkouvdis/homebrew-tap`.
- Homebrew installs download the release assets, so `brew install` only works once this repository is public.
