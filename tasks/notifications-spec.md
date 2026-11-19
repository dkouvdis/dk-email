# New mail notifications

Plan: [notifications-plan.md](notifications-plan.md). Tasks: [notifications-todo.md](notifications-todo.md).

## Problem Statement

How might we tell the user that new mail has arrived in an INBOX while
dk-email is in the background, locally or over SSH, without showing what
the mail says?

## Recommended Direction

Ship in two steps. **Step 1** builds notifications on the polling that
already exists: after each refresh, a rise in an account's INBOX `UIDNEXT`
means new mail. dk-email then rings the terminal bell and sends an OSC 9
desktop notification (`dk-email: 3 new in Work`), and keeps the INBOX
unread count in the window title (`dk-email (3)`). It doesn't alert while
its window has focus. The terminal and macOS decide what an alert looks
like (sound, badge, banner), so dk-email only chooses whether and how to
send.

**Step 2** adds IMAP `IDLE` push. A second, watch-only connection per
account idles on INBOX, and when the server reports new mail it triggers
the normal refresh for that account. Alerts then arrive within seconds
instead of up to `refresh_interval`. If the server lacks `IDLE` or the
connection fails, polling still covers it. Step 1 never depends on
step 2.

This is what TUI users expect: mutt and aerc ring a bell or run a command,
and none of them build in desktop notifications. OSC 9 adds desktop
banners without any OS-specific code, and it works over SSH.

## Key Assumptions to Validate

- [ ] OSC 9 shows a banner in Ghostty and iTerm2, locally and over SSH.
      Test by hand in both.
- [ ] tmux passes OSC 9 through with `allow-passthrough on` when it's
      wrapped in tmux's passthrough sequence. Test in tmux inside Ghostty.
- [ ] The terminal reports focus (Ghostty, iTerm2, and tmux with
      `focus-events on`). Test that no alert fires while the window has focus.
- [ ] Gmail, Fastmail and Proton Bridge accept a second connection that
      idles for hours, and drop it in a way the reconnect logic recovers
      from. Test with an overnight run under `DK_EMAIL_DEBUG=1`.

## MVP Scope

**Step 1: notify on poll**
- New mail = unread INBOX messages at or above the last refresh's `UIDNEXT`
  (counted with a UID search once `UIDNEXT` rises, since UIDs may have
  gaps), minus unread messages dk-email itself moved into INBOX. Read mail
  moved into INBOX doesn't alert. The first refresh after
  connecting only sets the baseline. A `UIDVALIDITY` change resets it.
- One alert per refresh round, covering all accounts:
  `dk-email: 3 new in Work, 1 in Personal`. Count only, never sender or
  subject.
- Channels: bell (`\a`), desktop (OSC 9, wrapped for tmux when `$TMUX` is
  set), and title (`dk-email (N)`, N = unread across INBOXes, just
  `dk-email` at 0).
- Bell and desktop alerts are skipped while the window has focus. Before
  the terminal reports focus, any key press or click counts as focus.
- Config, all optional and on by default:
  ```toml
  [notify]
  bell    = true
  desktop = true
  title   = true
  ```

**Step 2: IDLE push**
- A `mail.Watcher` per account (nil when not built) that waits until INBOX
  gains messages, over its own connection. It re-issues `IDLE` every 25
  minutes, reconnects with backoff, and gives up for the session if the
  server lacks `IDLE`.
- Every wake-up runs the existing refresh for that account. An event that
  arrives while a refresh is running queues one more refresh, so it's
  never dropped.

**Docs (ship with each step):** `docs/notifications.md` (user guide:
settings, per-terminal setup, tmux, SSH, macOS banner settings,
troubleshooting), plus README, SPEC.md and AGENTS.md updates.

## Not Doing (and Why)

- **Folders other than INBOX**: one IDLE connection per folder doesn't
  scale, and INBOX is what the user asked for.
- **Sender or subject in alerts**: count only was chosen, so nothing leaks
  on shared screens, screen recordings or Notification Center history.
- **Native notifications (`osascript`, `notify-send`)**: they work only
  locally and need OS-specific code. OSC 9 covers Ghostty and iTerm2 in
  both places.
- **A `notify_cmd` hook**: easy to add later (as with `password_cmd`) if
  someone wants ntfy or a phone push.
- **Per-account muting and quiet hours**: no need yet.
- **mutt-style IDLE on the main connection**: it would only watch the
  open folder and complicate the serialized `do`.
- **A setting for push**: IDLE is used whenever the server supports it,
  and polling stays as the fallback.

## Open Questions

- Terminal.app ignores OSC 9, so its users get only the bell and title.
  Add native notifications if anyone asks.
- Exiting resets the window title to empty (Bubble Tea's behaviour), not
  to what the shell had. Acceptable unless it bothers in practice.
