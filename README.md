# dk-email

A keyboard-driven terminal email client for triaging several IMAP accounts (Gmail included). It uses a Thunderbird-style layout of folders, threads and a reading pane, with lazygit-style single-key actions.

- Shows each account's folders with unread counts.
- Groups replies into threads, ordered by newest activity.
- Lets you mark read/unread, star, archive, delete (to Trash) and move mail without leaving the keyboard.
- Adapts to the terminal width. A split view and a list + popup view are available.
- Reads passwords from 1Password (`op read`), any other command, or the OS keychain. They are never stored in a plain-text file.

v0.x is a personal tool: it reads and triages mail but doesn't send it. Nothing is cached on disk.

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
- a password source: either a 1Password reference, or "enter once → keychain".

Gmail addresses pre-fill `imap.gmail.com:993`. dk-email tests the connection, then appends an `[[account]]` block to `config.toml`, keeping your comments and formatting intact. Press `A` at any time to connect another account.

The config folder lives at `$XDG_CONFIG_HOME/dk-email/` (default `~/.config/dk-email/`) on every OS, macOS included. The folder is `0700`, `config.toml` is `0600`, and a `.gitignore` is written so the folder can be kept in a dotfiles repo safely.

## Configuration

```toml
[ui]
default_layout   = "split"   # "split" | "list"
wide_breakpoint  = 120       # columns; at least 80
list_width       = 35        # percent of the width for the thread list in narrow mode; 10–90
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
```

- Unknown keys are errors, so a typo like `pasword_cmd` can't silently fall back to the keychain. A `password` key is rejected: passwords never go in this file.
- `archive_folder` defaults to the folder marked `\Archive`, or else `\All` (Gmail's All Mail). If neither exists, archive is disabled for that account.
- `tls_cert` accepts `~/`. A relative path is resolved from the config file's folder.

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

Gmail rejects your normal Google password over IMAP when 2-Step Verification is on. Create an **app password** at <https://myaccount.google.com/apppasswords>, then store it in 1Password or the keychain. Google Workspace accounts that don't allow app passwords aren't supported, because they need OAuth.

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

## Keys

| Key | Action | Key | Action |
|---|---|---|---|
| `j`/`k`, `↓`/`↑` | move | `tab` / `shift+tab` | next / previous pane |
| `h`/`l`, `←`/`→` | focus left / right pane | `enter` | open thread / folder |
| `space` | expand / collapse thread | `g` / `G` | top / bottom |
| `u` | toggle read / unread | `s` | toggle star |
| `e` | archive | `d` | delete (moves to Trash) |
| `m` | move to folder | `r` | refresh |
| `a` | account / folder picker | `A` | connect account |
| `v` | toggle split ↔ list + popup | `?` | key help |
| `esc` | close popup / back | `q` | close the open popup, otherwise quit |
| `ctrl+c` | quit, even with a popup open | | |

- Actions on a collapsed thread apply to every message in it. On an expanded row, or in the reader, they apply to the selected message. Opening a message marks it read.
- In the reader, `pgup`/`pgdn` (or `ctrl+u`/`ctrl+d`) scroll by a page, and `space` expands or collapses every message.
- In the `?` help popup, `/` filters keys by key or description.
- In the connect popup, `tab`/`↓` and `shift+tab`/`↑` move between fields, and `←`/`→` or `space` change a choice. `enter` submits and `esc` cancels.

Below 80 columns, list + popup mode is forced.

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
