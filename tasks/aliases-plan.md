# Implementation Plan: Send-as aliases

Spec: [aliases-spec.md](aliases-spec.md). Tasks: [aliases-todo.md](aliases-todo.md).

## Overview

An account can list aliases (`{email, name}`) in config or in the
connect-account popup. The composer gets a From row that picks between the
account address and its aliases. Replies and forwards start from the address
the original was sent to. Transport doesn't change: `mail.Draft.From` is
already what `compose.Build` writes, and the SMTP envelope sender stays the
account login.

## Architecture Decisions

- **Config shape:** `aliases = [{ email = "...", name = "..." }]` on
  `[[account]]`, decoded into `[]config.Alias`. `name` is optional and falls
  back to `from_name`. It is a sending key, so like `from_name` it needs
  `smtp_host`. Validation rejects a bad email address, a duplicate (case-insensitive),
  or the account's own email. `config` stays free of `mail`, so turning an
  account into a list of `mail.Address` happens in `ui` (next to
  `Model.identity`).
- **"Me" is a list.** `compose.Reply`, `ReplyAll` and `Forward` take
  `mine []mail.Address` (account address first) instead of one `me`. From
  is the first of `mine` found in the original's To, then Cc, else
  `mine[0]`. "Is this message from me" and "drop me from reply-all" check
  every address in `mine`. Today a reply-all sent to an alias would Cc that
  alias back to yourself, so this fixes that as well.
- **From is chosen, not typed.** The composer's From row is a selector
  (`←`/`→` or `space` cycles, like the connect popup's TLS row). It is not a
  text input, so From can only be an address the account can send as.
  `compose.Fields` gains `From mail.Address`; `FieldsOf` and `draft` carry
  it through.
- **Reopened drafts keep their From.** If a saved draft's From isn't among
  the account's addresses (the alias was removed from config), it is
  added to that composer's choices so nothing is silently changed.
- **Connect popup:** one `Aliases` field after `SMTP TLS`, holding
  comma-separated emails (names are set in config). It is hidden or ignored
  when the SMTP host is empty. `config.Append` writes `aliases` as an
  inline array.

## Task List

### Phase 1: Foundation

- [x] Task 1: `aliases` config key (parse, validate, append, defaults)
- [x] Task 2: Replies and forwards pick From from a list of my addresses

### Checkpoint: Foundation

- [x] `go test -race ./internal/config ./internal/compose` passes; existing
      configs load unchanged

### Phase 2: Core

- [ ] Task 3: From row in the composer, wired to the account's addresses
- [ ] Task 4: Aliases field in the connect-account popup

### Checkpoint: Core

- [ ] Full suite, vet, gofmt clean
- [ ] Manual: reply to a mail sent to an alias, which starts from that
      alias; switch From; send; check the headers at the recipient

### Phase 3: Docs and validation

- [ ] Task 5: README, SPEC, AGENTS notes; real sends on Gmail, Fastmail and iCloud

### Checkpoint: Complete

- [ ] Every acceptance criterion met, spec assumptions checked off
- [ ] Review with the user before committing

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Gmail rewrites From to the account address for an unverified alias, with no error | High: user thinks they sent from the domain | README says to verify the alias in Gmail's "Send mail as" first; a manual send test in Task 5 |
| A provider needs separate SMTP credentials for the domain | Med | Out of scope: that's a separate account, which already works. Say so in the README |
| Changing `Reply`/`Forward` signatures ripples through tests | Low | Task 2 is pure `compose` code plus one call site in `ui/reply.go` |
| Composer header height grows by one row | Low | `composeRows` and the popup tests cover the layout; check small terminals |
| Strict config: `aliases` without `smtp_host` now errors | Low | Same rule as `from_name`; error message names the key |

## Open Questions

- None blocking. The spec's assumptions are checked in Task 5.
