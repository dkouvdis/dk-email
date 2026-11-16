# Edit accounts: tasks

Plan: [edit-accounts-plan.md](edit-accounts-plan.md). Spec: [edit-accounts-spec.md](edit-accounts-spec.md).

- [x] Task 1: `config.ReplaceAccount` / `RemoveAccount`
  - Comments and blocks outside the edited block are kept byte for byte.
  - Account order is unchanged.
  - Comments above the next header aren't lost.
  - CRLF line endings are kept.
  - A stale index or name writes nothing.
- [x] Task 2: `secret.Delete`, plus `Onboarding.UpdateAccount` / `RemoveAccount`
  - Renaming moves the keychain password, and a new password replaces it.
  - Deleting can keep or remove the password.
- [x] Task 3: edit popup
  - The form is prefilled from the account, with Keep current as the
    default password source.
  - Alias names are kept for emails that stay in the list.
  - The Delete row asks for confirmation.
  - `ctrl+s` saves anyway, only after a failed test.
- [x] Task 4: UI
  - `E` opens the popup, and saving tests the login first.
  - The account is renamed and reconnected in place, with no restart.
  - Deleting fixes every per-account index.
  - Save anyway shows "Saved without a working login".
  - The help popup lists `E`.
- [x] Task 5: docs
  - `docs/accounts.md`, linked from the README.
  - README and SPEC notes.
- [x] Checkpoint: `go test -race ./...`, `go vet ./...`, `gofmt -l .` clean
