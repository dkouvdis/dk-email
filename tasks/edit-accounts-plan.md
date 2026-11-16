# Implementation Plan: Edit accounts

Spec: [edit-accounts-spec.md](edit-accounts-spec.md). Tasks: [edit-accounts-todo.md](edit-accounts-todo.md).

## Architecture Decisions

- **Rewriting the block (`config`).** `ReplaceAccount(path, index, oldName,
  update func(Account) Account)` and `RemoveAccount(path, index, oldName)`.
  - `index` is the position of the `[[account]]` block in the file, which is
    also the order `Load` returns. `oldName` must match, or nothing is
    written and the error says the config changed on disk.
  - `update` gets the account as written in the file, with no defaults
    applied and `tls_cert` left as typed. Keys the form doesn't show
    survive, and defaults aren't written out.
  - **Block span:** from the header line to the last line before the next
    table header (or EOF) that isn't blank or a comment. Comments sitting
    right above the next header stay with that header.
  - **Safety:** the result is reparsed. If it doesn't have the same number
    of accounts, or any other account changed, nothing is written.
    Line endings are kept, and the write is atomic.
- **Keychain (`secret`, `main`).** Entries are keyed by account name.
  - `secret.Delete(account)` ignores a missing entry.
  - `Onboarding.UpdateAccount` stores the new password under the new name,
    or moves an unchanged keychain password on rename. It then rewrites the
    config, and only after that removes the old entry.
  - `Onboarding.RemoveAccount(index, acct, deletePassword)`.
- **Popup (`popup`).** `NewEditAccount` is the connect form, prefilled.
  - Password source in edit mode: **Keep current** (the default; it
    reuses the keychain entry or `password_cmd`), 1Password or Keychain.
  - The aliases field holds emails only. Names for emails that stay in the
    list are carried over.
  - A **Delete account** row comes after Submit. `enter` on it shows a
    confirmation in the footer.
  - After a failed test, `ctrl+s` saves anyway.
- **UI (`ui`).** `E` edits the account under the sidebar cursor.
  - On save, the account is replaced in every per-account slice and map,
    renamed in the sidebar, and reconnected.
  - On delete, every per-account index above the deleted one shifts down.
  - Connect errors say "press E to edit it".

## Tasks

1. `config.ReplaceAccount` / `RemoveAccount` (golden and round-trip tests)
2. `secret.Delete`, plus `Onboarding.UpdateAccount` / `RemoveAccount` in main
3. Edit popup: prefill, Keep current, alias names, Delete row, save anyway
4. UI: `E`, test then save, reconnect, delete, status hint, help
5. Docs: `docs/accounts.md`, README, SPEC
