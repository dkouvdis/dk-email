# Tasks: dk-email v0.1.0

Plan: [plan.md](plan.md) · Spec: [SPEC.md](../SPEC.md)

Every task also meets the standing bar before it counts as done:
- `go test -race ./...`, `go vet ./...` and `gofmt -l .` all pass, the last with empty output.
- Commits follow Conventional Commits.
- No secrets, real mail or hardcoded version strings are committed.

---

## Phase 1: Foundation and de-risking

### T1: Scaffold the module with a `--version` flag, `.gitignore`, and CI

**Description:** Create the Go module `github.com/dkouvdis/dk-email` (`go 1.27`) and a `cmd/dk-email` entry point.
- Flags: `--version` and `--config`.
- Version variables `version`, `commit` and `date` are set by linker flags. Untagged builds report `dev`, and `go install` builds fall back to `debug.ReadBuildInfo()`.
- Add the repo `.gitignore` and a CI workflow.

**Acceptance criteria:**
- [x] `dk-email --version` prints `dk-email dev (<commit>[-dirty], <date>)` for an untagged build, taken from the git info Go embeds, or `dk-email dev (unknown, unknown)` when there's none. Building with `-ldflags "-X main.version=0.1.0-beta.1 …"` prints the injected values.
- [x] `.gitignore` covers `bin/`, `dist/`, `coverage.out`, `*.db`, `debug.log`, `tmp/`, and any `*.eml` outside `testdata/`.
- [x] `ci.yml` runs test, vet and gofmt on push and pull request.

**Verification:**
- [x] `go test -race ./...` (the version formatting test) passes.
- [x] `go build -o bin/dk-email ./cmd/dk-email && ./bin/dk-email --version` prints the dev version.
- [x] `git check-ignore bin/x dist/x coverage.out tmp/a.eml` lists all four paths, and `git check-ignore internal/render/testdata/a.eml` lists nothing.

**Dependencies:** None
**Files:** `go.mod`, `cmd/dk-email/main.go`, `cmd/dk-email/main_test.go`, `cmd/dk-email/version.go`, `cmd/dk-email/version_test.go`, `.gitignore`, `.github/workflows/ci.yml`
**Scope:** M

### T2: Config loading, paths, and the config-folder `.gitignore`

**Description:** Add an `internal/config` package that:
- resolves the config directory to `$XDG_CONFIG_HOME/dk-email` or `~/.config/dk-email` on every OS
- creates the directory with mode `0700` and writes the whitelist `.gitignore` if it's missing
- loads `config.toml`, applying the `[ui]` defaults (`split`, 120, 35, 120s), and validates each account
- honours the `--config` override

**Acceptance criteria:**
- [x] A missing file yields an empty config with defaults and no error, and invalid TOML returns an error that includes the line.
- [x] Validation rejects duplicate account names, a missing host or user, and an unknown `tls` value. A `password` key is rejected with a hint to use `password_cmd`.
- [x] The `.gitignore` is created once, never overwritten, and has the exact content from the spec.

**Verification:**
- [x] `go test -race ./internal/config/...` passes with at least 80% coverage, using table tests with `t.Setenv("XDG_CONFIG_HOME", t.TempDir())`.

**Dependencies:** T1
**Files:** `internal/config/config.go`, `internal/config/paths.go`, `internal/config/gitignore.go`, `internal/config/*_test.go`, `internal/config/testdata/full.toml`, `cmd/dk-email/main.go`, `cmd/dk-email/main_test.go`
**Scope:** M

### T3: Password resolution (`password_cmd` and keychain)

**Description:** Add an `internal/secret` package with `Resolve(ctx, account) (string, error)`.
- If `password_cmd` is set, run it with `sh -c` using the inherited environment and a 60s context, and use the first line of its stdout.
- Otherwise read the keychain entry for service `dk-email` under the account name.
- Also provide `Store(account, pw)` to save a password to the keychain.
- The command runner is injectable so tests can fake it.

**Acceptance criteria:**
- [x] A command that fails, prints nothing, or times out returns a typed error that includes stderr but **never the password**.
- [x] A missing keychain entry returns the error `ErrNoPassword`, which the UI later shows as "needs password".
- [x] No log or error string ever contains the password, and a test asserts this.

**Verification:**
- [x] `go test -race ./internal/secret/...` passes with at least 80% coverage, using `keyring.MockInit()` and a fake runner.

**Dependencies:** T1
**Files:** `internal/secret/secret.go`, `internal/secret/runner_unix.go`, `internal/secret/runner_other.go`, `internal/secret/secret_test.go`, `internal/secret/runner_unix_test.go`
**Scope:** S

### T4: Mail domain types, the `Mailbox` interface, and a fake

**Description:** Add an `internal/mail` package with:
- the types `Folder` (name, delimiter, role, unread, total), `UID`, `Flag`, `Summary` (headers plus the Message-ID, In-Reply-To and References fields) and `Message` (parts, attachments)
- the `Mailbox` interface from the spec
- `internal/mail/mailtest.Fake`: an in-memory `Mailbox` with configurable delay and errors, for UI tests

**Acceptance criteria:**
- [x] The `mail` package imports no IMAP or UI packages, and a test checks this using `go list -deps`.
- [x] `Fake` supports every interface method, including `Move` and `SetFlag` state changes, and can inject an error for each method.

**Verification:**
- [x] `go test -race ./internal/mail/...` passes.

**Dependencies:** T1
**Files:** `internal/mail/mail.go`, `internal/mail/mail_test.go`, `internal/mail/mailtest/fake.go`, `internal/mail/mailtest/fake_test.go`
**Scope:** S

### T5: IMAP connect + `Folders`

**Description:** Add an `internal/mail/imap` package with `Dial(ctx, account, password)`.
- It supports implicit TLS and STARTTLS, logs in, and closes cleanly.
- `Folders` lists mailboxes along with their special-use roles (Inbox, Sent, Drafts, Trash, Archive, All, Junk) and gets each folder's unread and total counts with `STATUS`.
- One connection is shared per account behind a mutex.
- Also add a read-only live test, behind the build tag `live`, that resolves the password with `secret` and lists the folders.

**Acceptance criteria:**
- [ ] The memserver integration test logs in, lists seeded folders with the correct roles and counts, and rejects a bad password with a clear error.
- [ ] Every call respects context cancellation and timeouts.
- [ ] The live test against your Gmail lists `INBOX`, `[Gmail]/All Mail` (role `All`), `[Gmail]/Trash` (role `Trash`) and the others.

**Verification:**
- [ ] `go test -race ./internal/mail/imap/...` passes.
- [ ] `DK_EMAIL_LIVE_ACCOUNT=Personal go test -tags live -run TestLiveFolders ./internal/mail/imap/ -v` passes. You run this manually.

**Dependencies:** T2, T3, T4
**Files:** `internal/mail/imap/client.go`, `internal/mail/imap/folders.go`, `internal/mail/imap/client_test.go`, `internal/mail/imap/live_test.go`
**Scope:** M

### ✅ Checkpoint 1: Foundation
- [ ] All tests pass and CI is green.
- [ ] The live check lists real Gmail folders through `op read`. This proves the go-imap v2 beta works with Gmail.
- [ ] You review before Phase 2.

---

## Phase 2: Read-only client → `v0.1.0-beta.1`

### T6: Threading package

**Description:** Add an `internal/thread` package with `Build([]mail.Summary) []Thread`.
- Group messages into threads using the Message-ID, In-Reply-To and References headers.
- Messages with no usable references fall back to a normalized subject (strip `Re:`, `Fwd:` and `[list]` prefixes).
- Sort threads by their newest message; messages within a thread are ordered oldest first.

**Acceptance criteria:**
- [ ] Handles out-of-order arrival, missing parents, cycles, duplicate Message-IDs, and messages with no Message-ID.
- [ ] Subject fallback never merges two unrelated messages that share a generic subject such as "Hello" when both have Message-IDs but no references.
- [ ] Building 5,000 summaries takes under 50ms, measured with a benchmark.

**Verification:**
- [ ] `go test -race ./internal/thread/...` passes with at least 80% coverage.
- [ ] `go test -bench . ./internal/thread/` passes.

**Dependencies:** T4
**Files:** `internal/thread/thread.go`, `internal/thread/subject.go`, `internal/thread/thread_test.go`
**Scope:** S

### T7: IMAP `Summaries` with UID paging

**Description:** Implement `Summaries(ctx, folder, before, limit)`.
- `SELECT` (read-only) the folder, find the newest `limit` UIDs below `before` (with `before = 0` meaning the newest overall), and fetch the envelope, flags and `References` header for each.
- Track UIDVALIDITY so a change triggers a reload.

**Acceptance criteria:**
- [ ] Paging through 450 seeded messages returns 200 + 200 + 50, newest first, with no duplicates or gaps.
- [ ] Summary fields (From, To, Subject, Date, Flags and all three threading IDs) match the seeded data, including encoded-word subjects.
- [ ] Selecting a folder doesn't change any flags (no implicit `\Seen`).

**Verification:**
- [ ] `go test -race ./internal/mail/imap/...` passes.
- [ ] The live test `TestLiveSummaries` shows the 200 newest INBOX subjects.

**Dependencies:** T5
**Files:** `internal/mail/imap/summaries.go`, `internal/mail/imap/summaries_test.go`, `internal/mail/imap/live_test.go`
**Scope:** S

### T8: UI shell (layout engine, hint bar, status line, help, debug log)

**Description:** Add an `internal/ui` package containing:
- the root Bubble Tea v2 model and the pure `Layout(w, h, mode)` function. Wide is ≥ 120 columns with a 24-column sidebar and a 35% list; narrow is under 120 with a 35/65 split; below 80 columns list + popup is forced.
- focus cycling, the keymap from the spec (`bubbles/key`), a context-sensitive hint bar, a status line, the `?` help popup, quitting, and resize handling
- debug logging to `~/.cache/dk-email/debug.log` when `DK_EMAIL_DEBUG=1` (metadata only)

`main` wires the config and accounts into the model, with placeholder panes for now.

**Acceptance criteria:**
- [ ] Table tests for `Layout` cover widths 60, 79, 80, 119, 120 and 200 in both modes.
- [ ] Resizing across the breakpoints switches the layout live, and `v` has no effect below 80 columns.
- [ ] teatest golden files exist for the wide, narrow and list layouts at fixed sizes.

**Verification:**
- [ ] `go test -race ./internal/ui/...` passes.
- [ ] Manual: `go run ./cmd/dk-email`, resize the terminal, and press `?`, `tab` and `q`.

**Dependencies:** T2, T4
**Files:** `internal/ui/model.go`, `internal/ui/layout.go`, `internal/ui/keys.go`, `internal/ui/styles.go`, `internal/ui/layout_test.go`, `internal/ui/popup/help.go`
**Scope:** M (6 files; the help popup is small)

### T9: Sidebar with folder selection (wide mode)

**Description:** Add an `internal/ui/sidebar` package that groups folders under each account header.
- Show unread counts and order folders by role first (Inbox, Drafts, Sent, Archive, Trash, Junk), then alphabetically.
- When the app starts, connect each account asynchronously and show a spinner per account while it connects. An account with an error shows the reason.
- `enter` selects a folder and sends the event that triggers loading its summaries.

**Acceptance criteria:**
- [ ] With two fake accounts, both appear with their unread counts. A failing account shows its error without blocking the other.
- [ ] Keys continue to work while an account is connecting (a fake with a delay proves this).

**Verification:**
- [ ] `go test -race ./internal/ui/...` passes, including update-level tests with `mailtest.Fake`.

**Dependencies:** T8, T5
**Files:** `internal/ui/sidebar/sidebar.go`, `internal/ui/sidebar/sidebar_test.go`, `internal/ui/model.go`, `internal/ui/accounts.go`
**Scope:** M

### T10: Thread list (expand/collapse, navigation, paging)

**Description:** Add an `internal/ui/threadlist` package that renders thread rows in the format the spec describes: unread dot, star, sender, subject with message count, and date.
- Supports `j`/`k`, `g`/`G` and `space` to expand or collapse a thread.
- Reaching the end of the list fetches the next page, and all loaded messages are re-threaded together.

**Acceptance criteria:**
- [ ] Rows are truncated to the pane width and dates are formatted relative to now (time today, otherwise the date).
- [ ] Expanding shows one indented row per message. Collapsing keeps the cursor on the thread.
- [ ] Paging adds older threads without moving the cursor, and a thread that spans a page boundary is merged.

**Verification:**
- [ ] `go test -race ./internal/ui/...` passes.
- [ ] teatest golden output for the list exists.

**Dependencies:** T9, T6, T7
**Files:** `internal/ui/threadlist/threadlist.go`, `internal/ui/threadlist/row.go`, `internal/ui/threadlist/threadlist_test.go`, `internal/ui/model.go`
**Scope:** M

### T11: Render package (MIME → text)

**Description:** Add an `internal/render` package with `Parse(io.Reader) (mail.Message, error)` built on `go-message`.
- Choose `text/plain` over `text/html` when both exist, and convert HTML-only mail to text with `html2text`.
- Decode charsets and transfer encodings, list attachments by name and size, and wrap text to a given width.

**Acceptance criteria:**
- [ ] Synthetic fixtures parse correctly: plain, HTML-only, `multipart/alternative`, `multipart/mixed` with an attachment, ISO-8859-1, quoted-printable, base64, and a malformed message.
- [ ] Links inside HTML stay visible as `text (url)`, and the output contains no ANSI escape sequences that came from the mail itself (terminal injection is stripped).

**Verification:**
- [ ] `go test -race ./internal/render/...` passes with at least 80% coverage.

**Dependencies:** T4
**Files:** `internal/render/render.go`, `internal/render/sanitize.go`, `internal/render/render_test.go`, `internal/render/testdata/*.eml`
**Scope:** S

### T12: IMAP `Message` fetch + reader pane + mark read on open

**Description:**
- **IMAP side:** `Message(ctx, folder, uid)` fetches `BODY.PEEK[]` and parses it with `render`.
- **UI side:** the `internal/ui/reader` package shows a conversation stacked oldest to newest, with the latest or first unread message expanded and the earlier ones collapsed to a single line.
  - Each message shows a From/To/Cc/Date header block, and the body scrolls in a viewport.
  - Opening a message marks it `\Seen`. Until `SetFlag` exists (T16), the fake records the call and the IMAP implementation returns "not implemented".

**Acceptance criteria:**
- [ ] `enter` on a thread shows the whole conversation, and `j`/`k` scroll the reader.
- [ ] Fetching never sets `\Seen` implicitly, which the memserver test asserts.
- [ ] A slow fetch shows "Loading…" without blocking key handling.

**Verification:**
- [ ] `go test -race ./...` passes.
- [ ] Manual: read real Gmail threads with the live account.

**Dependencies:** T10, T11
**Files:** `internal/mail/imap/message.go`, `internal/mail/imap/message_test.go`, `internal/ui/reader/reader.go`, `internal/ui/reader/reader_test.go`, `internal/ui/model.go`
**Scope:** M

### T13: Narrow mode header + account/folder picker (`a`)

**Description:**
- In the narrow layout, the left pane shows an `Account › Folder` header with an unread count.
- `a` opens a popup picker listing every account's folders with their unread counts. Selecting one switches account and folder.
- In wide mode, `a` jumps focus to the sidebar instead.

**Acceptance criteria:**
- [ ] The header always matches the current selection, and picking a folder in another account switches to it and loads that folder.
- [ ] `esc` closes the picker without changing anything.

**Verification:**
- [ ] `go test -race ./internal/ui/...` passes, with teatest golden output for the narrow layout with the picker open.

**Dependencies:** T10
**Files:** `internal/ui/popup/picker.go`, `internal/ui/popup/picker_test.go`, `internal/ui/model.go`, `internal/ui/threadlist/threadlist.go`
**Scope:** S

### T14: List + popup mode (`v`, forced below 80 columns)

**Description:** `v` toggles between split and list mode, the latter showing the full-width thread list.
- In list mode, `enter` opens the reader model inside a centered popup sized to 90% × 90%, and `esc` closes it.
- Below 80 columns list mode is forced and `v` does nothing. Widening the terminal again restores the mode you last chose.

**Acceptance criteria:**
- [ ] The popup reuses `reader` without duplicating it, and triage keys still work in the popup once T17 adds them.
- [ ] Resizing from 130 → 70 → 130 columns ends in the mode you chose, with the cursor unchanged.

**Verification:**
- [ ] `go test -race ./internal/ui/...` passes, with teatest golden output for the popup.

**Dependencies:** T12
**Files:** `internal/ui/model.go`, `internal/ui/popup/overlay.go`, `internal/ui/model_test.go`
**Scope:** S

### T15: Release pipeline (GoReleaser, `release.yml`, Homebrew cask)

**Description:** Write `.goreleaser.yaml` (`version: 2`) with:
- builds for darwin and linux on arm64 and amd64 with `CGO_ENABLED=0`, using linker flags for version, commit and date
- `tar.gz` archives and checksums
- a changelog grouped by Conventional Commit type
- `release.prerelease: auto`
- a `homebrew_casks` entry for `dkouvdis/homebrew-tap` using `HOMEBREW_TAP_GITHUB_TOKEN`, with `skip_upload: auto` and `postflight_steps` that clear the quarantine attribute

Add `release.yml`, triggered by `v*` tags, which runs the tests and then `goreleaser/goreleaser-action`. Add `goreleaser check` to `ci.yml`.

**Acceptance criteria:**
- [ ] `goreleaser check` passes and the config uses no deprecated keys.
- [ ] `goreleaser release --snapshot --clean` produces four archives whose binaries print the snapshot version.
- [ ] Pre-release tags never update the tap (`skip_upload: auto`).

**Verification:**
- [ ] CI is green, including `goreleaser check`.
- [ ] Local snapshot build, if `goreleaser` is installed, or a manual run of the workflow on a branch with `--snapshot`.

**Dependencies:** T1, T14 (so the first beta is useful)
**Prerequisites (yours):** the `dkouvdis/homebrew-tap` repo exists and the `HOMEBREW_TAP_GITHUB_TOKEN` secret is set on `dkouvdis/dk-email`.
**Files:** `.goreleaser.yaml`, `.github/workflows/release.yml`, `.github/workflows/ci.yml`
**Scope:** S

### ✅ Checkpoint 2: Read-only client
- [ ] All tests pass and CI is green.
- [ ] Manual check against real Gmail: browse folders and read threads in the wide, narrow and list + popup layouts, across two accounts.
- [ ] **You approve tagging `v0.1.0-beta.1`.** After it's tagged, a GitHub pre-release with four archives exists and the tap is untouched.

---

## Phase 3: Triage

### T16: IMAP `SetFlag` + `Move` + archive/Trash folder resolution

**Description:**
- `SetFlag` uses `UID STORE ±FLAGS` for `\Seen` and `\Flagged`.
- `Move` uses `UID MOVE`, falling back to `COPY` + `\Deleted` + `UID EXPUNGE` of only those UIDs when the server lacks the MOVE capability.
- Resolve each account's archive folder in this order: the config's `archive_folder`, then the `\Archive` role, then the `\All` role, otherwise none.
- Resolve its trash folder from the `\Trash` role.

**Acceptance criteria:**
- [ ] Memserver tests cover setting and clearing both flags, moving with and without the MOVE capability, and the fallback expunging only the moved UIDs.
- [ ] Table tests cover every combination of archive and trash folder resolution.

**Verification:**
- [ ] `go test -race ./internal/mail/imap/...` passes.

**Dependencies:** T12
**Files:** `internal/mail/imap/actions.go`, `internal/mail/imap/roles.go`, `internal/mail/imap/actions_test.go`
**Scope:** S

### T17: Triage keys `u` / `s` / `e` / `d` with optimistic update

**Description:**
- The keys work in the list, the reader and the popup.
- On a collapsed thread they apply to every message in it; on an expanded row or in the reader they apply to the selected message.
- The UI updates immediately and the server call runs asynchronously. If the call fails, the change is rolled back and the error appears in the status line.
- `e` is disabled, with an explanation, when an account has no archive folder.
- Archived and deleted threads are removed from the list and the cursor moves to the next thread.

**Acceptance criteria:**
- [ ] Update-level tests on the fake cover every key in both thread and message scope, plus the rollback when the fake returns an error.
- [ ] No key ever leads to a permanent expunge: delete moves to Trash.

**Verification:**
- [ ] `go test -race ./internal/ui/...` passes.
- [ ] **Live (you approve first):** run `u`, `s`, `e` and `d` on a throwaway email sent to yourself, and confirm the result in the Gmail web UI.

**Dependencies:** T16, T14
**Files:** `internal/ui/actions.go`, `internal/ui/actions_test.go`, `internal/ui/model.go`, `internal/ui/threadlist/threadlist.go`
**Scope:** M

### T18: Move-to-folder picker (`m`)

**Description:** `m` opens the picker (reused from T13), limited to the current account's folders and excluding the current folder. Choosing a folder moves the target using the same optimistic flow as T17.

**Acceptance criteria:**
- [ ] The picker shows only the current account's folders. A completed move removes the thread from the list, and `esc` cancels.

**Verification:**
- [ ] `go test -race ./internal/ui/...` passes.

**Dependencies:** T17
**Files:** `internal/ui/actions.go`, `internal/ui/popup/picker.go`, `internal/ui/actions_test.go`
**Scope:** S

### T19: Auto-refresh (120s) + `r`

**Description:**
- Use `tea.Tick` at the configured `refresh_interval` to re-fetch the selected folder and every account's unread counts.
- `r` triggers a refresh immediately.
- A refresh merges new messages without moving the cursor or collapsing expanded threads, and never overlaps with one already running.

**Acceptance criteria:**
- [ ] New mail on the fake appears after a tick, with the cursor still on the same thread.
- [ ] A refresh that fails shows in the status line, and the next tick tries again.

**Verification:**
- [ ] `go test -race ./internal/ui/...` passes.

**Dependencies:** T10
**Files:** `internal/ui/refresh.go`, `internal/ui/refresh_test.go`, `internal/ui/model.go`
**Scope:** S

### ✅ Checkpoint 3: Triage
- [ ] All tests pass and CI is green.
- [ ] Spec success criteria 5–7 are verified on real Gmail.
- [ ] **You approve tagging `v0.1.0-beta.2`.**

---

## Phase 4: Account onboarding

### T20: Append an account to the config without re-encoding the file

**Description:** Add `config.AppendAccount(path, acct)`, which writes a new `[[account]]` block as text at the end of the file (creating the file, with mode `0600`, if needed), then parses the result back. If that parse fails, the original file is restored.

**Acceptance criteria:**
- [ ] Fixtures cover a file with comments, one with no trailing newline, CRLF line endings, and an empty or missing file. Comments are kept exactly and the result parses.
- [ ] Values are escaped as valid TOML, including quotes and backslashes in an `op://` reference.
- [ ] A failed parse leaves the original file byte-for-byte unchanged.

**Verification:**
- [ ] `go test -race ./internal/config/...` passes with at least 80% coverage.

**Dependencies:** T2
**Files:** `internal/config/append.go`, `internal/config/append_test.go`, `internal/config/testdata/*.toml`
**Scope:** S

### T21: Connect-account popup (first run, `A`, re-entering a missing password)

**Description:** A form popup, built on `bubbles/textinput`, with these fields:
- name, email, username, host, port, TLS mode
- password source, which is either a 1Password reference (saved as `password_cmd = "op read <ref>"`) or "enter once → keychain" (a masked input)

How it behaves:
- Entering a `@gmail.com` or `@googlemail.com` address pre-fills `imap.gmail.com`, 993 and implicit TLS.
- "Test & save" dials asynchronously. On success it saves the account (T20, plus `secret.Store` for the keychain option) and adds it to the sidebar; on failure the error is shown in the popup and nothing is saved.
- The popup opens automatically on first run when there are no accounts, and with `A` at any time.
- For an account whose password is missing, it opens with the fields filled in and only the password editable.

**Acceptance criteria:**
- [ ] Update-level tests cover the Gmail pre-fill, validation of required fields, a failed test leaving no saved state, a successful test calling append and store, and the first run opening the popup automatically.
- [ ] The password is never written to `config.toml` or to logs, which a test asserts.

**Verification:**
- [ ] `go test -race ./...` passes.
- [ ] **Manual:** with `XDG_CONFIG_HOME=$(mktemp -d)`, add a Gmail account using an `op://` reference. Check that `config.toml` gains the block, the config-folder `.gitignore` exists, and the inbox loads.

**Dependencies:** T20, T3, T9
**Files:** `internal/ui/popup/connect.go`, `internal/ui/popup/connect_test.go`, `internal/ui/model.go`, `internal/ui/accounts.go`
**Scope:** M

### ✅ Checkpoint 4: Onboarding
- [ ] Spec success criteria 1–3 are verified manually from an empty config folder.

---

## Phase 5: Release v0.1.0

### T22: README (install, config, keys, quarantine fallback)

**Description:** Write a README covering:
- what dk-email is and a screenshot
- installing via `brew install dkouvdis/tap/dk-email` or `go install`, plus the `xattr` fallback for quarantined binaries
- a config reference with 1Password and keychain examples
- the keybinding table
- the release process for maintainers (tag format, prerequisites, token expiry)

**Acceptance criteria:**
- [ ] Every command and key in the README matches the spec and the code.

**Verification:**
- [ ] Manual review.

**Dependencies:** T21
**Files:** `README.md`
**Scope:** XS

### ✅ Checkpoint 5: v0.1.0
- [ ] All of spec success criteria 1–9 are met.
- [ ] **You approve tagging `v0.1.0-rc.1`, then `v0.1.0`.** Afterwards, `brew install dkouvdis/tap/dk-email && dk-email --version` prints `dk-email 0.1.0 (…)`.
