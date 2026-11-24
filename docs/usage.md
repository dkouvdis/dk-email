# Using dk-email

This guide covers the screen, moving around, reading and triaging mail, and
every key. Writing and sending mail has its own guide: [Sending
mail](sending.md).

- [The screen](#the-screen)
- [Layouts](#layouts)
- [Moving around](#moving-around)
- [Threads](#threads)
- [Reading](#reading)
- [Triage](#triage)
- [Refreshing](#refreshing)
- [Mouse](#mouse)
- [Key reference](#key-reference)

## The screen

In the default layout the screen has three parts:

```
┌ folders ────────────┐┌ Inbox · 3 unread ───────────────────────────┐
│ Work                ││ ● *  Alice          Lunch tomorrow (3)  12:00│
│   Inbox           3 ││      Bob            Invoice            Nov 9 │
│   Sent              ││                                              │
│ Personal            ││                                              │
└─────────────────────┘└──────────────────────────────────────────────┘
 status line                                                    narrow
 hint bar
```

- **Sidebar.** Each account's folders as a tree, indented two spaces per
  level, with an unread count on the right. Trash shows no count. Special
  folders (Inbox, Drafts, Sent, Archive, All Mail, Trash, Junk) stay at the top
  of each account in that order, so Gmail's `[Gmail]/…` folders don't nest.
  Other folders follow, nested by path and sorted by name. A parent that isn't
  a folder itself, such as `Work` when only `Work/Clients` exists, is dimmed and
  can't be selected. The tree is always expanded. Accounts other than the one
  your cursor is in are dimmed. An account that is still connecting, or failed
  to, shows that in place of its folders.
- **Thread list.** The open folder's conversations, newest activity first. The
  header above the rows shows the folder name and its unread count, such as
  `Inbox · 3 unread`. See [Threads](#threads).
- **Status line.** Progress and errors on the left (`Archiving…`, `Deleted 1
  message.`), and the layout in use on the right (`narrow`, `list`, or `list
  (forced)`). An error stays until your next key press.
- **Hint bar.** The keys that work right now, in lazygit style, description
  first: `Archive: e | Delete: d | … | Help: ? | Quit: q`. It follows the
  focused pane, and a popup replaces it with its own keys. A narrow terminal
  cuts the list with a `…`; `?` and `q` always stay.
- **Window title.** The terminal title reads `dk-email (N)`, where N is the
  unread count across all INBOXes, and `dk-email` at 0 (the `title` setting turns it off). See [New-mail
  notifications](notifications.md).

Popups (the reader, pickers, help, the composer) open in the middle of the
screen, about 90% of its size. The panes behind them are dimmed. Nothing is
cached on disk: headers and bodies live in memory for the session.

## Layouts

There are two layouts:

| Layout | What you see |
|---|---|
| **Narrow** (the default) | Folders sidebar (32 columns) on the left, thread list on the right. |
| **List** | The thread list alone, at full width. The header shows `account › folder`. |

Press `v` to switch between them. Below 80 columns the list layout is forced:
`v` does nothing but say `List mode is forced below 80 columns.`, and the status
line shows `list (forced)`. Widening the terminal brings the layout you asked
for back.

To start in the list layout, set `default_layout = "list"` under `[ui]` (the
values are `"split"` and `"list"`). See [Configuration](configuration.md).

There's no reading pane. In both layouts `enter` opens a conversation in a
popup. See [Reading](#reading).

## Moving around

In the narrow layout there are two panes, the sidebar and the thread list. The
focused pane has a highlighted border, and the keys act on it.

- `tab` / `shift+tab` move focus to the next / previous pane, wrapping around.
  `h` / `←` and `l` / `→` move it left and right without wrapping. In the list
  layout there's only the thread list.
- `j` / `k` (or `↓` / `↑`) move the cursor, and `g` / `G` jump to the top or
  bottom.
- In the sidebar the cursor only moves: press `enter` to open the folder under
  it. Moving the cursor also changes which account is "active", the one `c`
  writes from.
- In the thread list, moving to the last row loads the next page of older
  messages (each folder loads its newest 200 first, and `Loading more…` shows
  while it fetches).
- **`a`** goes to a folder. In the narrow layout it closes any popup and puts
  focus on the sidebar. In the list layout, where there is no sidebar, it opens
  a **Go to folder** picker with every account's folders. Pick one with `enter`.
- **`1`–`9`** in the sidebar or the Go to folder picker move the cursor to that
  account's Inbox (`1` is the first account), ready for `enter`. While a `/`
  filter is open, digits are typed into it instead.
- **`/`** filters folders by name when the sidebar (or a folder picker) has
  focus, and searches the open folder when the thread list has focus. See
  [Searching](search.md).

### Filtering folders

Press `/` in the sidebar, or in a folder picker, and type. Matches are listed
flat with their full path (`Work/Clients/Acme`), and the filter ignores case.
While you type, every key goes to the filter, `q` included, so `j` and `k` are
letters: use `↑` / `↓` to move between matches, `enter` to open the highlighted
folder, and `esc` to clear the filter.

## Threads

dk-email groups messages into conversations itself, per folder, from what is in
the message headers:

- Messages are linked through `Message-ID`, `In-Reply-To` and `References`.
  Replies still join the right thread when they arrive out of order or when a
  parent is missing.
- A message with no references but a `Re:` or `Fwd:` subject is attached by
  its subject, ignoring case, `Re:`/`Fwd:`/`Fw:` prefixes and `[list]` tags. It
  joins the latest original with that subject sent before it.
- Mail that isn't a reply is never merged by subject, so two unrelated "Hello"
  messages stay apart.
- Threading is per folder. A reply sitting in Sent doesn't appear inside the
  Inbox thread it answers.

Threads are ordered by their newest message, newest first.

A **collapsed** thread is one row: the latest message's sender and subject, the
message count in parentheses when there is more than one, and its date (the
time for today, `Jan 2` for earlier this year, the full date before that). A
`●` marks a thread with unread mail and a yellow `*` one with a starred
message. Unread rows are bold.

`space` expands a collapsed thread into one indented row per message, oldest
first, each with its own markers. `space` on any row of an expanded thread
collapses it again and puts the cursor on the thread row.

## Reading

Press `enter` on a thread to open the conversation in the reader popup. `esc`
or `q` closes it. `enter` on a message row of an expanded thread opens the
conversation with that message expanded.

- The popup stacks the thread's messages from oldest to newest, under the
  subject. One message is expanded: the one you opened on, else the oldest
  unread, else the latest. The others take one line each, with the sender, a
  star and the date.
- An expanded message shows From, To, Cc and Date, then its body and its
  attachments.
- `space` expands every message, or goes back to the first state if all are
  already expanded. Bodies load when a message is expanded, and `Loading…` shows
  until they arrive.
- **Opening marks read.** Each expanded message is marked read once its body
  loads, so expanding more messages marks them too. A collapsed message stays as
  it was.

Scroll with `j` / `k` (or `↓` / `↑`), `pgup` / `pgdn` (or `ctrl+u` /
`ctrl+d`) for a page, and `g` / `G` (or `home` / `end`) for the top and
bottom. A scrollbar on the right shows where you are in a long conversation.

### How bodies are shown

- **Plain text** is shown as written, only wrapped to the popup's width. Its
  `*`, `_` and `#` stay as they are.
- **HTML** is converted to Markdown and styled for the terminal. Email clutter
  is stripped first: hidden preheaders, tracking pixels, layout tables and
  conditional comments.
- When a message has both parts, the plain text part wins, unless it looks
  generated from the HTML: leftover tags or entities, template code, preheader
  padding, a "your client can't show HTML" notice, or mostly blank lines.
  Then dk-email shows the HTML instead, which reads better.
- A message with no text shows `(no text)`.

### Links

Links in HTML bodies show only their text, underlined, and the terminal can open
them: hold cmd (ctrl on Linux) and click, in a terminal that
supports hyperlinks. dk-email releases the mouse while a popup is open so
this works. A bare URL shows the URL itself. In plain text bodies a URL is just
text.

### Selecting text

Hold shift (option on macOS) while dragging to select text in the terminal.

### Attachments

Attachments are listed under the body with their type and size. Press `o` to
save one to your Downloads folder and open it. See
[Attachments](attachments.md#opening-attachments).

## Triage

Triage keys work in the thread list and in the reader. They change the mail on
the server in the background: the row goes or changes at once, and if the server
refuses, the change is undone and the status line says why.

| Key | What it does |
|---|---|
| `u` | Toggle read / unread. Marks everything read if any target is unread, otherwise everything unread. |
| `s` | Toggle the star. Unstars everything if any target is starred, otherwise stars everything. |
| `e` | Archive: move to the account's archive folder. |
| `d` | Delete: move to the account's Trash. |
| `m` | Move to any other folder of the account, from a picker. |

**What a key acts on:**

- On a **collapsed thread** row, every message in the thread.
- On a **message row** of an expanded thread, that message.
- In the **reader**, the message it opened on (the one expanded first), not
  whichever you've scrolled to. When it goes, the next one takes its place, and
  the reader closes when the thread is empty.

**Archive** moves mail to the folder the server marks `\Archive`, or else `\All`
(Gmail's All Mail), unless the account sets `archive_folder`. If there is none,
archive is off for that account and the status line says why, and
`Already in the archive folder.` appears if you're already there. See
[Configuration](configuration.md) and [Accounts](accounts.md).

**Delete** moves mail to the Trash folder, without asking. dk-email never
deletes mail permanently: pressing `d` in Trash says `Already in Trash;
dk-email never deletes mail permanently.` To remove something for good, use
another mail client. If the account has no Trash folder, delete is off for it.

**Move** (`m`) opens a **Move N messages to** picker with the account's folder
tree. The open folder is dimmed so the tree keeps its shape. Press `/` to filter
it as described in [Filtering folders](#filtering-folders), then `enter` to
move. `esc` or `q` closes it.

## Refreshing

dk-email refreshes every connected account on its own: the folders and their
unread counts, and the newest page of the open folder.

- **`ctrl+r`** refreshes now. It also reconnects accounts whose connection
  failed (a password prompt you dismissed, a server that was down) and, while
  you are searching, runs the search again.
- **`refresh_interval`** sets how often it happens by itself. The default is
  `120s`. See [Configuration](configuration.md).
- **Push.** When the server supports IMAP IDLE, dk-email also watches each
  INBOX and refreshes that account within seconds of new mail. See
  [Push](notifications.md#push).

A refresh that overlaps a flag change or move you made is dropped, so it can't
undo it. The next one picks up.

## Mouse

- Click a **folder** in the sidebar to open it.
- Click a **thread** to select it, and click it again to open it.
- The **scroll wheel** moves the selection in the pane under the pointer, like
  `↑` / `↓`. It never opens anything. While a popup is open, it scrolls the
  popup.
- Clicks are ignored while a popup is open. dk-email hands the mouse back to
  the terminal then, so you can open links (see [Links](#links)).

## Key reference

The keys below are the ones the `?` help popup lists, in its order and its
words. The popup is the source of truth.

**Navigation**

| Key | What it does |
|---|---|
| `k/↑` | up |
| `j/↓` | down |
| `g` | top |
| `G` | bottom |
| `h/←` | left pane |
| `l/→` | right pane |
| `tab` | next pane |
| `shift+tab` | previous pane |

**Threads**

| Key | What it does |
|---|---|
| `enter` | open |
| `space` | expand |
| `u` | read/unread |
| `s` | star |
| `e` | archive |
| `d` | delete |
| `m` | move |
| `ctrl+r` | refresh |
| `o` | open attachment |

`space` expands a collapsed thread and collapses an expanded one in the list.
In the reader it expands or collapses every message. `ctrl+r` also reconnects
accounts that failed. `o` works in the reader.

**Search (thread list)**

| Key | What it does |
|---|---|
| `/` | search this folder |
| `enter` | run the search |
| `esc` | clear the search |
| `ctrl+r` | run the search again |
| `from: to: cc:` | match an address |
| `subject: body:` | match a field |
| `is:unread` | also is:read, is:starred |
| `since: before:` | dates as YYYY-MM-DD |
| `-word` | exclude a term |
| `"a phrase"` | match exact words |

See [Searching](search.md).

**Compose**

| Key | What it does |
|---|---|
| `r` | reply |
| `R` | reply all |
| `f` | forward |
| `c` | new message |
| `enter` | edit draft (in Drafts) |

`r`, `R` and `f` work on the thread list and in the reader. `c` works anywhere
a folder is open.

**Composer**

| Key | What it does |
|---|---|
| `ctrl+s` | send |
| `tab` | next field |
| `shift+tab` | previous field |
| `enter / ←/→` | choose From |
| `enter` | next field (in a header) |
| `ctrl+o` | attach a file |
| `drag & drop` | attach files |
| `←/→` | choose attachment |
| `d` | remove attachment |
| `esc` | close, or save / discard |
| `ctrl+c` | quit now |

See [Sending mail](sending.md) for the composer in full.

**Accounts & view**

| Key | What it does |
|---|---|
| `a` | accounts |
| `/` | filter folder names |
| `A` | connect account |
| `E` | edit account |
| `v` | split/list |

`a` focuses the sidebar in the narrow layout and opens the folder picker in the
list layout. `/` filters folders when the sidebar has focus, and searches when
the thread list does. `E` also deletes an account. See
[Accounts](accounts.md).

**General**

| Key | What it does |
|---|---|
| `?` | help |
| `esc` | back |
| `q` | quit |
| `ctrl+c` | quit now |

`q` closes the open popup, and quits only when none is open. `ctrl+c` quits
at once, even with a popup open or while composing.

### Other popups

**Help.** `j` / `k`, `pgup` / `pgdn` and `g` / `G` scroll. `/` filters the keys
by key or description, `enter` keeps the filter and goes back to scrolling, and
`esc` clears it. `esc`, `q` or `?` closes the popup.

**Folder pickers** (`a` in the list layout, `m`). `/` filters, `j` / `k` move,
`enter` chooses, `esc` or `q` closes. While filtering: `↑` / `↓` move, `enter`
chooses, `esc` clears.

**Connect and edit account** (`A`, `E`). `tab` / `↓` and `shift+tab` / `↑`
move between fields. `←` / `→` or `space` change a choice. `enter` submits
(`test & save`, or `sign in & save` for OAuth) and `esc` cancels. After a failed
test, `ctrl+s` saves anyway. See [Accounts](accounts.md).
