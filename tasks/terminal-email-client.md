# Intent: Terminal Email Client

Confirmed statement of intent (interview, 2026-11-08). Downstream: [SPEC.md](../SPEC.md).

## Outcome

A keyboard-driven terminal email client in Go + Bubble Tea, with a Thunderbird-style layout and lazygit-style single-key actions, hint bar and `?` help.

## Layout

- **Wide terminal:** three panes, Thunderbird-style:
  - folders sidebar grouped by account, with unread counts
  - thread list (sender, subject, date, star/flag; threads expand inline)
  - reading pane showing the whole conversation
- **Narrow terminal:** falls back to two panes, thread list 35% and reading pane 65%. The current account and folder are shown at the top of the left pane, and a popup picker switches between them.
- **List + popup mode:** a full-width list; a key opens the selected thread in a popup.

## Accounts

- Multiple IMAP accounts, Gmail included (via an app password).
- Accounts can be added in-app through a **connect-account popup** or in the config file. The popup:
  - opens automatically on first run
  - pre-fills Gmail's IMAP settings
  - tests the connection before saving
- Passwords come from **1Password** (`op read …`) or the OS keychain, and are never stored in plain text by default.

## User

The author: a personal side project, structured so it can go public later without a rewrite.

## Success

You can triage every account from the terminal: read threads, mark read/unread, archive, delete, star/flag, and move between folders. The web UI is only needed for replying.

## Constraints

- Config lives in a dedicated `~/.config/dk-email/` folder.
- A generic IMAP layer, separate from the UI, with no Gmail-only shortcuts.
- A slot reserved for a future sending layer (reply/compose).
- Passwords are never stored in plain text by default.
- Downloaded mail and cached data never end up in git.

## Out of scope (v1)

- OAuth
- Reply, compose and SMTP
- Unified inbox across accounts
- Tags and labels (folders only)
- Search and offline sync
- Opening or saving attachments
- Rich HTML display (HTML is converted to text)
