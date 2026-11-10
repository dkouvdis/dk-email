# Spec: dk-email (v1)

Status: **Approved; v1 built, with `v0.1.0-beta.1` released on 2026-11-10.** Source of intent: [docs/intent/terminal-email-client.md](docs/intent/terminal-email-client.md).

## Objective

A keyboard-driven terminal email client for **triaging several IMAP accounts** (Gmail included), using a Thunderbird-style layout and lazygit-style single-key actions. v1 is a personal tool. Its code is structured so that it can go public, and gain sending, without a rewrite.

### User stories

1. On first run with no accounts, a **connect-account popup** opens. I enter a name, email, IMAP host/port and a password source, the app tests the connection, then saves the account.
2. I see the accounts and folders I'm working in, with **unread counts**.
3. I see a folder's mail as a list of **threads**: replies grouped together, newest activity first, expandable inline.
4. I read a whole conversation in a popup over the thread list.
5. Without leaving the keyboard, I can **mark read/unread, star/flag, archive, delete, and move** a thread or message.
6. The layout adapts to the terminal width, and I can switch between split and list + popup modes.
7. My passwords come from **1Password** (`op read`) or the **OS keychain**, never from a plain-text file.

### Layout

| Mode | When | Panes |
|---|---|---|
| **Narrow** (default) | split mode, width ≥ 80 cols | folders sidebar (24 cols) │ thread list (the rest). `a` focuses the sidebar |
| **List + popup** | toggled with `v`, or **forced automatically when width < 80 cols** | thread list at full width. The **account › folder** header sits at the top, and `a` opens the account/folder picker |

There's no reading pane. In both layouts, `enter` opens the conversation in a centered popup (~90% × 90%), and `esc` closes it. Changed on 2026-11-10 at your request: the earlier three-pane wide layout and the list │ reader narrow layout were replaced by the two above, and `wide_breakpoint` and `list_width` were removed. Until the first folder opens, the thread list says "Connecting…".

Every mode includes a bottom **hint bar** showing the keys for the focused pane, plus a status line for errors and progress (e.g. "Archiving…"). The hint bar is written lazygit style, description first and then the key (`Archive: e | Delete: d | … | Help: ? | Quit: q`), all in a soft blue (256-colour 110) so it reads easily on a dark background.

### Folder tree

The sidebar and the folder pickers show each account's folders as a tree, indented two spaces per level (added 2026-11-10 at your request):

- Special folders (Inbox, Drafts, Sent, Archive, All Mail, Trash, Junk) stay at the top level in that order, so Gmail's `[Gmail]/…` folders don't nest. Folders inside a special folder, such as `INBOX/Later`, nest under it.
- Other folders follow, nested by path and sorted by name at each level. A parent that isn't a folder itself (e.g. `Work` when only `Work/Clients` exists, or Gmail's `[Gmail]` above Starred and Important) is shown dimmed and can't be selected.
- The tree is always expanded. While a `/` filter is typed, matches are listed flat with their full path (`Work/Clients/Acme`).
- In the move picker (`m`), the open folder is shown dimmed rather than left out, so the tree keeps its shape.

### Thread list row

` ● *  Sender name            Subject (n)                     12:00` for an unread, starred thread of n messages, with one space of left padding. A collapsed thread shows its latest message. `space` expands it to show one indented row per message.

The selected thread row and the selected sidebar folder are drawn in white text on purple (256-colour 63, the same as the picker's highlight), not in reverse video. The unread dot and the yellow `*` star keep their colours on the selected row.

### Reader popup

Messages in the conversation are stacked oldest to newest. The latest (or first unread) message is expanded; earlier ones are collapsed to one line each. Each header shows From, To, Cc and Date. `text/plain` bodies are preferred; if only HTML exists, it's converted to text. Attachments are listed by name and size only.

## Assumptions

1. **`dk-email` is the official name** for the binary and config folder (confirmed). The config lives at `$XDG_CONFIG_HOME/dk-email/`, falling back to `~/.config/dk-email/` on **every OS, macOS included**.
2. **No plain-text `password` field in v1.** Passwords come only from `password_cmd` or the keychain.
3. **Threading happens per folder** (Thunderbird's default; confirmed): client-side, from `Message-ID` / `In-Reply-To` / `References`, falling back to normalized subject only for replies (`Re:`/`Fwd:`) that carry no references, so unrelated mails with a generic subject never merge. There's no cross-folder threading, so replies sitting in Sent won't appear inside Inbox threads.
4. **v1 caches nothing on disk.** Headers and bodies are kept in memory for the session. `~/.cache/dk-email/` is reserved for the future cache and holds only the debug log.
5. **Each folder loads its newest 200 messages**, and more are fetched when scrolling reaches the end.
6. **Refresh:** `r` refreshes manually, and the selected folder plus all folders' unread counts are polled every `refresh_interval` (default 120s; confirmed). No IMAP IDLE in v1.
7. **Delete moves mail to the Trash folder** without asking for confirmation, the way Gmail does. It's never a permanent expunge.
8. **Archive moves mail to the account's archive folder.** That's the folder with IMAP's special-use marker `\Archive`, or else `\All` (Gmail's All Mail), unless `archive_folder` overrides it in config. If none of these exist, archive is disabled for that account and a message explains why.
9. **Keybindings and themes are fixed in v1.** `keys.toml` and `themes/` are reserved names (and whitelisted in the config `.gitignore`) but aren't read yet.
10. **go-imap v2 is still in beta (v2.0.0-beta.8).** I'm accepting this because the v1 library is no longer maintained. The dependency is isolated behind the `mail` interface.
11. **Gmail needs an app password.** With 2-Step Verification (including passkeys), Gmail rejects the normal Google password over IMAP. Users create an app password at https://myaccount.google.com/apppasswords and point `password_cmd` at it. Login errors include this hint. **Google Workspace accounts** don't allow app passwords and need OAuth, so they aren't supported in v1.

## Tech Stack

| Concern | Choice | Version |
|---|---|---|
| Language | Go | 1.27 (`go.mod`: `go 1.27`) |
| TUI runtime | `charm.land/bubbletea/v2` | v2.1.x |
| Components | `charm.land/bubbles/v2` (list, viewport, textinput, help, key) | v2.2.x |
| Styling | `charm.land/lipgloss/v2` | v2.0.x |
| IMAP client | `github.com/emersion/go-imap/v2` (`imapclient`) | v2.0.0-beta.8 |
| IMAP test server | `github.com/emersion/go-imap/v2/imapserver/imapmemserver` | same |
| MIME parsing | `github.com/emersion/go-message` | v0.18.x |
| HTML → text | `github.com/k3a/html2text` | v1.5.x |
| Keychain | `github.com/zalando/go-keyring` | v0.2.x |
| Config | `github.com/BurntSushi/toml` | v1.6.x |
| TUI tests | `github.com/charmbracelet/x/exp/teatest/v2` | pseudo-version |
| Release | GoReleaser (v2 config, `version: 2`) via GitHub Actions | latest v2 |

Module path: `github.com/dkouvdis/dk-email`.

## Commands

```sh
# Build
go build -o bin/dk-email ./cmd/dk-email

# Run (dev)
go run ./cmd/dk-email
go run ./cmd/dk-email --config ./tmp/config.toml   # use an alternate config file

# Test
go test -race ./...
go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out

# Lint / format (output of gofmt -l must be empty)
go vet ./...
gofmt -l .

# Debug logging (writes ~/.cache/dk-email/debug.log)
DK_EMAIL_DEBUG=1 go run ./cmd/dk-email
```

CLI flags: `--config <path>`, `--version`.

Release commands (local runs need `goreleaser`, e.g. `brew install goreleaser`; CI does the real releases, see Versioning & Release):

```sh
goreleaser check                          # validate .goreleaser.yaml
goreleaser release --snapshot --clean     # local dry-run build into dist/, publishes nothing
git tag -a v0.1.0-beta.1 -m "v0.1.0-beta.1" && git push origin v0.1.0-beta.1   # triggers the release workflow
```

## Project Structure

```
cmd/dk-email/          → main: flags, wiring, tea.NewProgram
internal/config/       → paths (XDG), load/validate/append-account, writes config-dir .gitignore
internal/secret/       → password resolution: password_cmd runner, keychain get/set
internal/mail/         → provider-agnostic domain types + Mailbox interface (no IMAP imports)
internal/mail/imap/    → go-imap v2 implementation of mail.Mailbox
internal/thread/       → builds threads from message summaries (pure, no I/O)
internal/render/       → MIME → displayable text (plain preferred, HTML→text, attachment list)
internal/ui/           → root Bubble Tea model, layout calculation, keymap, styles
internal/ui/sidebar/   → folders pane
internal/ui/threadlist/→ thread list pane
internal/ui/reader/    → conversation view (pane + popup reuse the same model)
internal/ui/popup/     → picker, connect-account form, help, confirm
docs/intent/           → confirmed intent docs
.goreleaser.yaml       → build, archive, changelog, Homebrew cask config
.github/workflows/     → ci.yml (test/vet/gofmt on push & PR), release.yml (GoReleaser on v* tags)
testdata/ (per pkg)    → synthetic fixtures only, never real mail
```

### Core boundary (UI depends only on this)

```go
package mail

type Mailbox interface {
	Folders(ctx context.Context) ([]Folder, error)              // includes special-use role and unread count
	Summaries(ctx context.Context, folder string, before UID, limit int) ([]Summary, error) // newest first; ErrFolderChanged when paging after UIDVALIDITY changed
	Message(ctx context.Context, folder string, uid UID) (Message, error) // full parsed body
	SetFlag(ctx context.Context, folder string, uids []UID, f Flag, on bool) error // Seen, Flagged
	Move(ctx context.Context, from string, uids []UID, to string) error
	Close() error
}
```

`Summary` carries UID, From, To, Subject, Date, Flags, Message-ID, In-Reply-To and References, which is all that threading needs. **Sending is a separate, future `Sender` interface** (SMTP), so `Mailbox` never has to change to add reply/compose.

There's **one IMAP connection per account**, opened lazily and serialized behind a mutex. All network calls run inside `tea.Cmd`s with a context timeout, so the UI never blocks.

## Configuration

`~/.config/dk-email/config.toml` (directory `0700`, file `0600`):

```toml
[ui]
default_layout   = "split"   # "split" | "list"
refresh_interval = "120s"

[[account]]
name         = "Personal"
email        = "me@gmail.com"
username     = "me@gmail.com"
imap_host    = "imap.gmail.com"
imap_port    = 993
tls          = "implicit"                                   # "implicit" | "starttls"
password_cmd = "op read op://Personal/Gmail/app-password"   # omit to use the keychain
# archive_folder = "[Gmail]/All Mail"                       # optional override
# tls_cert       = "~/.config/dk-email/bridge.pem"          # optional: trust this cert instead of the system roots
```

- **Defaults and validation:**
  - `tls` defaults to `implicit`. `imap_port` defaults to 993 for `implicit` and 143 for `starttls`. `username` defaults to `email`.
  - Unknown keys are rejected, so a typo such as `pasword_cmd` can't silently fall back to the keychain. A `password` key is rejected with a hint to use `password_cmd`.
  - `[ui]` limits: `refresh_interval` positive.
  - All problems in a file are reported together.
  - The config dir and its `.gitignore` are only created for the default location, never next to a custom `--config` path.
  - `tls_cert` (optional) is a PEM file of certificates trusted for that account **instead of** the system roots. It's for local bridges with self-signed certificates, such as Proton Mail Bridge, whose 20-year certificate macOS rejects as "not standards compliant" even when trusted in Keychain. The host name and expiry are still verified. `~/` is expanded, and a relative path is resolved from the config file's folder. A missing file, or one with no PEM certificate, fails at load. Added 2026-11-09 with your approval.
- **Password resolution:** if `password_cmd` is set, run it with `sh -c` and use the **first line** of stdout with only the line ending removed. Spaces inside the password are kept, and extra lines (as `pass show` prints) are ignored. The command inherits the user's environment but gets no stdin, and on timeout (60s) its whole process group is killed. Errors include the command's stderr (control characters stripped, length capped) but never its stdout. If `op` isn't on the inherited `PATH`, use its absolute path. Otherwise read from the keychain (service `dk-email`, key = account `name`). If neither yields a password, the account is shown as "needs password", and pressing `A` opens the connect popup to supply one (stored in the keychain).
- **The connect-account popup appends** a new `[[account]]` block to the file as text, so the user's comments and formatting are preserved. It never re-encodes the whole file. When the address ends in `@gmail.com` / `@googlemail.com`, the popup pre-fills `imap.gmail.com:993 implicit`. Its password source is either a 1Password reference (stored as `password_cmd = "op read <ref>"`) or "enter once → keychain".
- **Config-dir `.gitignore`**, written on first run if missing:
  ```
  *
  !.gitignore
  !config.toml
  !keys.toml
  !themes/
  !themes/**
  ```

## Keybindings (v1, fixed)

| Key | Action | Key | Action |
|---|---|---|---|
| `j`/`k`, `↓`/`↑` | move | `tab` / `shift+tab` | cycle pane focus |
| `h`/`l`, `←`/`→` | focus left/right pane | `enter` | open thread / folder |
| `space` | expand/collapse thread | `g` / `G` | top / bottom |
| `u` | toggle read/unread | `s` | toggle star/flag |
| `e` | archive | `d` | delete (→ Trash) |
| `m` | move to folder (picker) | `r` | refresh |
| `a` | focus folders (narrow) / folder picker (list) | `A` | connect account |
| `v` | toggle split ↔ list + popup | `?` | full key help |
| `esc` | close popup / back | `q` | close the open popup, otherwise quit |
| `ctrl+c` | quit, even with a popup open | | |

Actions on a **collapsed thread** apply to every message in it. On an expanded row, or in the reader, they apply to the selected message. Opening a message marks it read.

The **`?` help popup** is sized to its content, groups keys into sections, and scrolls with `j`/`k`, `↑`/`↓`, `pgup`/`pgdn` and `g`/`G`. Inside it, `/` filters the keys by key or description.

**Mouse** (added 2026-11-10 at your request): clicking a folder in the sidebar opens it. Clicking a thread or message row selects it, and clicking the selected row again opens it. The wheel acts like `↑`/`↓` in the open popup, or else in the pane under the pointer, and never opens anything. Clicks do nothing while a popup is open.

In the folders sidebar and the folder pickers (`a` in list mode, `m` for move), **`/` filters folders** by name, ignoring case. Only matching folders show, under their account, and accounts with no match are hidden. While typing, every key goes to the filter (`q` included): `↑`/`↓` move between matches, `enter` opens the highlighted folder and clears the filter, and `esc` clears it. The picker keeps its size while filtering. Added 2026-11-10 at your request.

## Versioning & Release

- **Version scheme: [SemVer 2.0](https://semver.org).** Tags are `vMAJOR.MINOR.PATCH`, with optional pre-release suffixes: `v0.1.0-beta.1`, `v0.1.0-rc.1`, then `v0.1.0`.
  - **0.x** while pre-public: minor bumps may break config, and patch releases are fixes only.
  - This v1 spec ships as **`v0.1.0`**, with `-beta.N` tags cut along the way.
  - **`v1.0.0`** marks being ready for public use. After that, breaking config or CLI changes require a major bump.
- **Versions come only from git tags.** Nothing in the source holds a version number.
  - GoReleaser injects the version through linker flags: `-X main.version={{.Version}} -X main.commit={{.ShortCommit}} -X main.date={{.Date}}`.
  - Builds that aren't tagged report `dev`. For `go install …@vX.Y.Z` builds, `main` falls back to `debug.ReadBuildInfo()`.
  - `dk-email --version` prints, for example, `dk-email 0.1.0-beta.1 (abc1234, 2026-11-20)`.
- **Release pipeline** (`.github/workflows/release.yml`): pushing a `v*` tag triggers this sequence.
  1. Check out the repo with full history.
  2. Run `go test -race ./...`.
  3. Run `goreleaser release --clean`.
  4. GoReleaser builds with `CGO_ENABLED=0` for **darwin/arm64, darwin/amd64, linux/amd64 and linux/arm64**, and packs each into a `.tar.gz` with `checksums.txt`.
  5. It creates a **GitHub Release** with a changelog generated from commits.
  6. `release.prerelease: auto` marks `-beta`/`-rc` tags as pre-releases.
- **Commit messages follow [Conventional Commits](https://www.conventionalcommits.org)** (`feat:`, `fix:`, `docs:`, `chore:` …). GoReleaser's changelog groups by prefix (Features / Bug fixes / Other) and drops `docs:`, `test:` and `chore:` entries.
- **Homebrew:** a GoReleaser `homebrew_casks` entry publishes to the tap repo **`dkouvdis/homebrew-tap`**, so you can run `brew install dkouvdis/tap/dk-email`.
  - It uses `homebrew_casks` because the older `brews` section is deprecated as of GoReleaser v2.10.
  - `skip_upload: auto` means **pre-releases never reach the tap**; only stable tags update the cask.
  - The workflow pushes to the tap using the repo secret `HOMEBREW_TAP_GITHUB_TOKEN`: a fine-grained personal access token with contents write access to the tap repo only.
  - The binaries are unsigned, so macOS may quarantine them. The cask clears the quarantine attribute using the declarative `postflight_steps` (configured as `hooks.post.install_steps` in `.goreleaser.yaml`), and the README documents the manual fallback (`xattr -d com.apple.quarantine`). Apple code signing and notarization are out of scope until 1.0.
- **CI** (`.github/workflows/ci.yml`): runs `go test -race ./...`, `go vet ./...` and `gofmt -l .` on every push and pull request, plus `goreleaser check` once `.goreleaser.yaml` exists.
- **Prerequisites (one-time setup, done by you):**
  - Install GoReleaser locally (`brew install goreleaser`) for snapshot dry-runs. It's optional, since CI does the real releases.
  - Create the empty public repo `dkouvdis/homebrew-tap`.
  - Add the `HOMEBREW_TAP_GITHUB_TOKEN` secret to `dkouvdis/dk-email`.

## Code Style

`gofmt` everywhere. Errors are wrapped with context using `%w`. Interfaces are small and defined by the package that consumes them. No package-level mutable state. All I/O runs inside `tea.Cmd`s and comes back as typed messages:

```go
type summariesLoadedMsg struct {
	account, folder string
	summaries       []mail.Summary
}

type errMsg struct{ err error }

func loadSummaries(mb mail.Mailbox, account, folder string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		sums, err := mb.Summaries(ctx, folder, 0, 200)
		if err != nil {
			return errMsg{fmt.Errorf("load %s/%s: %w", account, folder, err)}
		}
		return summariesLoadedMsg{account: account, folder: folder, summaries: sums}
	}
}
```

Naming: identifiers are lowercase, short and descriptive. Bubble Tea message types end in `Msg`. Tests are table-driven and named `TestThing_Behaviour`.

## Testing Strategy

- **Framework:** the standard `testing` package. `go test -race ./...` must pass.
- **Unit tests (pure, fast):**
  - `thread`: grouping by Message-ID/References, out-of-order arrival, missing parents, subject fallback, sorting by newest activity.
  - `render`: plain/HTML/multipart/alternative bodies, charsets, attachment listing.
  - `config`: parse and validate, defaults, XDG path resolution, text-append preserves comments, `.gitignore` written.
  - `secret`: `password_cmd` success, failure and empty output (using a fake runner); keychain read/write through the `go-keyring` mock.
  - `ui`: layout widths per mode and at the 80-column breakpoint; key handling at the `Update` level (input key → state change plus the expected `mail` call on a fake `Mailbox`).
- **Integration tests:** `mail/imap` against an in-process `imapmemserver`, seeded with synthetic messages and special-use folders: list folders, page through summaries, set and clear flags, move, archive-folder resolution.
- **TUI smoke tests:** `teatest` golden output for narrow and list + popup at fixed sizes, run against a fake `Mailbox`.
- **Coverage target:** ≥ 80% for `thread`, `render`, `config`, `secret` and `mail/imap`. UI packages are covered by behaviour tests with no numeric target.
- **Fixtures:** synthetic `.eml` files in `testdata/` only. **Real mail is never committed.**
- **Manual check before calling v1 done:** run against a real Gmail account, using an app password from 1Password.

## Boundaries

- **Always:**
  - Run `go test -race ./...`, `go vet ./...` and `gofmt -l .` before committing.
  - Keep IMAP types out of `ui` and `ui` out of `mail`.
  - Make all network I/O asynchronous, with timeouts.
  - Treat the account's mail as the user's data: actions are reversible (move rather than expunge).
- **Ask first:**
  - Adding any dependency not listed above, or a linter or Makefile. (CI and GoReleaser are approved.)
  - Pushing any git tag or triggering a release.
  - Changing the config schema or file locations.
  - Writing to a real account during development.
  - Any Gmail-specific IMAP extension (`X-GM-*`).
- **Never:**
  - Store or log passwords, `password_cmd` output, or message bodies. The debug log records metadata only.
  - Commit real mail, cache files, `coverage.out`, `bin/`, `dist/`, or anything from `~/.cache/dk-email`.
  - Commit tokens. The tap token lives only in GitHub Actions secrets.
  - Hardcode a version string in source.
  - Permanently expunge mail in v1.
  - Re-encode the whole user config file.

The repo `.gitignore` will cover `bin/`, `dist/`, `coverage.out`, `*.db`, `debug.log`, `tmp/`, and any `*.eml` outside `testdata/`.

## Success Criteria

1. With an empty config dir, launching opens the connect-account popup. A valid Gmail app password, supplied via an `op read` reference, connects successfully. `config.toml` gains an appended `[[account]]` block, the config `.gitignore` exists, and no password appears in any file.
2. A wrong password or host shows an error **inside the popup** and saves nothing.
3. With two accounts configured, the narrow layout's sidebar lists both accounts' folders with correct unread counts. In list mode, `a` switches between them and the header shows `Account › Folder`.
4. Resizing across 80 columns switches between narrow and list + popup without restarting. Below 80 columns, list + popup is forced. `v` toggles list + popup at 80 columns and above.
5. The Inbox shows threads, with replies grouped. `space` expands a thread and `enter` shows the full conversation.
6. `u`, `s`, `e`, `d` and `m` each change state on the server, verified by reopening the folder after a restart. Delete lands in Trash and archive lands in All Mail on Gmail.
7. The UI stays responsive (keys still move the cursor) while a slow folder is loading.
8. `go test -race ./...` passes, coverage targets are met, and `gofmt -l .` is empty.
9. Pushing `v0.1.0-beta.1` produces a GitHub **pre-release** with four archives and `checksums.txt`, and does **not** touch the tap. Pushing `v0.1.0` produces a normal release and updates the cask in `dkouvdis/homebrew-tap`. After that, `brew install dkouvdis/tap/dk-email && dk-email --version` prints `dk-email 0.1.0 (…)`.

## Out of Scope (v1)

- OAuth
- Reply, compose and SMTP (only the `Sender` interface slot is reserved)
- Unified inbox
- Tags and labels
- Search
- Offline sync and an on-disk cache
- IMAP IDLE
- Opening or saving attachments
- Rich HTML rendering
- Custom keybindings and themes
- Mouse support
- Windows builds
- macOS code signing and notarization

## Resolved Decisions

1. **Name:** `dk-email` is official.
2. **Very narrow terminals (< 80 cols):** list + popup is forced automatically.
3. **Threading:** per folder for v1.
4. **Auto-refresh:** poll every 120s; no IMAP IDLE in v1.
5. **1Password CLI:** `op` is already installed and is used for real-account testing.
6. **Releases:** SemVer tags with pre-releases (`v0.1.0-beta.1`). Pushing a tag runs GoReleaser in GitHub Actions to create a GitHub Release. Stable releases also update the Homebrew tap.

## Open Questions

None.
