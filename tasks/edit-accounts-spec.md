# Edit accounts

## Problem Statement

How might we change or remove an existing account from inside dk-email
without hand-editing TOML or breaking the user's config file?

## Recommended Direction

`E` on a sidebar account opens the connect popup prefilled, titled "Edit
account". Every field is editable: name, email, username, IMAP host/port/TLS,
password source, SMTP host/port/TLS and aliases. An empty password field keeps
the current password. Submit tests the IMAP login (and SMTP, if set) exactly
like connect, then saves and reconnects the account in place.

Saving replaces that account's `[[account]]` block where it stands, so the
account order doesn't change. The new block is rendered from the full loaded
account with the form's fields applied, so keys the form doesn't show
(`tls_cert`, `save_sent`, alias names) survive. Comments inside the block are
lost. Everything outside it is kept byte for byte.

Deleting happens only inside the edit popup, from a **Delete account** row
after Submit. A plain `D` would be typed into the text fields, so it can't
be the trigger. `enter` on that row asks for confirmation, then removes the
block and offers to delete the keychain password too. A failing account's
status suggests `E` to fix it.

## Key Assumptions to Validate

- [ ] Block boundaries can be found without a TOML AST: from the header to
      the next table header, with the next block's leading comments
      excluded. Test with golden files: several accounts, comments between
      blocks, the last block.
- [ ] Rendering from the loaded account loses nothing: Load → replace →
      Load gives an equal account, checked by a round-trip test over every key.
- [ ] Keychain entries can be moved or kept when the email or username
      changes. Check how `internal/secret` keys its entries.

## MVP Scope

In:
- `E` opens the edit popup with every connect field.
- The block is replaced in place.
- The login is tested before saving.
- The account reconnects without a restart.
- A Delete account row with a confirmation and optional keychain removal.
- Round-trip and golden tests.
- The last task is docs: a user guide `docs/accounts.md` covering editing
  (`E`, the fields, the empty password field, save anyway) and deleting an
  account, linked from the README, plus README and SPEC notes.

Out: everything below.

## Not Doing (and Why)

- **Keeping comments inside the edited block:** replacing the block was
  chosen. Editing single keys would need a TOML-aware rewriter.
- **Editing alias names in the form:** the form takes emails. Names stay in
  the config and are kept.
- **Editing UI or global settings:** that's "Settings in the app", a
  separate item.
- **A top-level delete key:** deleting stays inside the edit popup, behind a
  confirmation.
- **Undoing a delete:** the confirmation is enough.

## Save anyway

When the login test fails while editing, the footer shows the error and the
hint bar adds `ctrl+s` **save anyway**, alongside `enter`, which tests again.
This allows a correct fix to be saved while offline, while the server is
down or while a `password_cmd` vault is locked. A wrong setting only breaks
that one account, which shows its connect error and points back to `E`.

- Only the login test is skipped. Config validation always runs.
- It's offered only after a failed test, never as a way to skip the test.
- The status bar says the account was saved without a working login.
- New accounts in the connect popup don't get it: a new account that has
  never worked is more likely a typo than a network blip.

## Open Questions

- None.
