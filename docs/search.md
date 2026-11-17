# Searching

How to search a folder in dk-email, which operators you can use, and what to
expect from each provider. Search runs on your mail server, so it finds
messages that aren't loaded in the list yet.

- [Searching a folder](#searching-a-folder)
- [Working with results](#working-with-results)
- [Operators](#operators)
- [Dates](#dates)
- [Input dk-email doesn't understand](#input-dk-email-doesnt-understand)
- [Providers](#providers)
- [Privacy](#privacy)
- [Not available yet](#not-available-yet)

## Searching a folder

Press `/` in the thread list. A `/ ` prompt opens in the list header. Type a
query and press `enter`. The results replace the folder's threads, threaded as
usual, and the header shows `search: <query> (N)`, where N is how many
messages match.

| Key | What it does |
|---|---|
| `/` | Open the prompt. If a search is active, its query is filled in so you can edit it. |
| `enter` | Run the query. On an empty prompt, clear the active search. |
| `esc` while typing | Close the prompt without searching. An active search stays. |
| `esc` in the thread list | Clear the active search and show the folder again. |

Opening another folder also clears the search.

`/` in the folders sidebar is a different feature: it filters folder names.

Search covers the open folder only. On Gmail, open All Mail and search there
to search everything.

## Working with results

Everything that works on a normal thread list works on results: open a
conversation, reply (`r`, `R`), forward (`f`), mark read or unread (`u`), star
(`s`), archive (`e`), delete (`d`) and move (`m`).

Results load 200 at a time. Move to the last row to load older matches.

Automatic refresh doesn't replace the results. `ctrl+r` refreshes the folder
and runs the search again.

## Operators

| Term | Matches | Example |
|---|---|---|
| a word | The word anywhere in the headers or body | `invoice` |
| `"a phrase"` | The phrase anywhere in the headers or body | `"quarterly report"` |
| `from:` | The sender | `from:jane` |
| `to:` | The To header | `to:team@example.com` |
| `cc:` | The Cc header | `cc:finance` |
| `subject:` | The subject | `subject:"weekly report"` |
| `body:` | The body only | `body:deadline` |
| `is:unread` | Unread messages | `is:unread` |
| `is:read` | Read messages | `is:read` |
| `is:starred` | Starred messages (`is:flagged` means the same) | `is:starred` |
| `since:` or `after:` | Received on or after a date | `since:2026-01-01` |
| `before:` | Received before a date | `before:2026-07-01` |
| `-` before any term | Excludes it | `-from:newsletter`, `-is:read`, `-"out of office"` |

Values for `from:`, `to:`, `cc:` and `subject:` can be quoted, as in
`from:"Jane Doe"`. Operator names are case-insensitive, so `From:jane` works.

Every term must match. `from:jane invoice is:unread` finds unread messages
from Jane that mention an invoice. There is no OR and no grouping.

## Dates

`since:` and `after:` include the day you give. `before:` excludes it. Write
dates as `YYYY-MM-DD`.

IMAP compares the day the server received the message, in the server's time
zone. A message that arrives near midnight can land on the neighbouring day.

## Input dk-email doesn't understand

dk-email never rejects a query. Anything it can't read is searched as plain
text instead:

- an unknown operator, such as `foo:bar`;
- an empty value, such as `from:`;
- an unknown `is:` value;
- a malformed date, such as `before:yesterday`;
- an unclosed quote: the rest of the line becomes the phrase.

A quoted phrase is always plain text, so `"from:bob"` searches for that text
and doesn't filter by sender.

## Providers

How text matches is up to the server. Most servers match parts of words and
ignore case. Gmail matches whole words.

Search is verified with Gmail, and with a standard IMAP server (Dovecot-style)
in the test suite. Microsoft 365's IMAP search is more limited, especially for
body text and non-English text, so it may find fewer matches than Outlook's
own search. Non-ASCII queries are sent as UTF-8.

## Privacy

The query goes to your mail server and nowhere else. dk-email's debug log
never records it.

## Not available yet

- Searching all folders at once.
- OR and grouping.
- Saved searches.
