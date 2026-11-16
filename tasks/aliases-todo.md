# Send-as aliases: tasks

Plan: [aliases-plan.md](aliases-plan.md). Spec: [aliases-spec.md](aliases-spec.md).

## Task 1: `aliases` config key

**Description:** Accounts accept `aliases = [{ email = "...", name = "..." }]`,
which is strictly validated and written back by `config.Append`.

**Acceptance criteria:**
- [x] `config.Alias{Email, Name}` and `Account.Aliases []Alias` decode from
      an inline array of tables; unknown keys inside an alias fail.
- [x] Validation errors for: bad email address, a duplicate (case-insensitive, also
      against the account email), and `aliases` set without `smtp_host`.
- [x] `Append` writes `aliases` so that `Load` reads back an equal account.

**Verification:**
- [x] `go test -race ./internal/config`
- [x] An existing config without `aliases` loads unchanged

**Dependencies:** None

**Files likely touched:** `internal/config/config.go`, `internal/config/append.go`,
`internal/config/config_test.go`, `internal/config/append_test.go`

**Estimated scope:** S

## Task 2: Replies and forwards pick From from my addresses

**Description:** `compose.Reply`, `ReplyAll` and `Forward` take
`mine []mail.Address`. From is the first of mine in the original's To, then
Cc, else `mine[0]`. "From me" and the reply-all exclusion use every address.

**Acceptance criteria:**
- [x] A reply to mail sent to an alias (in To or Cc) is From that alias,
      with its name; otherwise From is the account address.
- [x] Reply-all never includes any of my addresses; a reply to my own
      message sent from an alias goes to its recipients.
- [x] `ui.Model` builds `mine` from the account (account address with
      `from_name`, then the aliases, with each alias's name falling back to `from_name`) and passes it at the one call site.

**Verification:**
- [x] `go test -race ./internal/compose ./internal/ui`

**Dependencies:** Task 1

**Files likely touched:** `internal/compose/reply.go`, `internal/compose/forward.go`,
`internal/compose/reply_test.go`, `internal/ui/compose.go`, `internal/ui/reply.go`

**Estimated scope:** M

## Checkpoint: Foundation
- [x] `go test -race ./internal/config ./internal/compose ./internal/ui` passes
- [x] `go vet ./...`, `gofmt -l .` clean

## Task 3: From row in the composer

**Description:** The composer shows a From row above To when the account has
more than one address, and it cycles through them. The chosen address
becomes `Draft.From` for both send and save.

**Acceptance criteria:**
- [ ] With aliases there is a From row, and it is focusable with `tab`; `←`/`→` (and `space`)
      cycle through the addresses; it shows `Name <email>`. With no aliases there is no row
      (the layout is unchanged).
- [ ] `compose.Fields` carries From; send and save use it; the hint bar lists
      the cycle keys while the row is focused.
- [ ] Reopening a draft keeps its From, even when it isn't in the current list.

**Verification:**
- [ ] `go test -race ./internal/ui/... ./internal/compose`
- [ ] Manual: open the composer on an account with and without aliases at a
      small terminal size

**Dependencies:** Task 2

**Files likely touched:** `internal/ui/popup/compose.go`, `internal/ui/popup/compose_test.go`,
`internal/compose/fields.go`, `internal/ui/compose.go`, `internal/ui/compose_test.go`

**Estimated scope:** M

## Task 4: Aliases field in the connect-account popup

**Description:** An optional `Aliases` field after `SMTP TLS` that takes
comma-separated emails. It is saved as `aliases` with no names, and only
when an SMTP host is set.

**Acceptance criteria:**
- [ ] Invalid or duplicate addresses show an error in the popup and nothing is saved.
- [ ] Saved accounts get `aliases = [{ email = "..." }, …]`, and the composer
      offers them straight away, with no restart.
- [ ] Empty field or empty SMTP host: no `aliases` key written.

**Verification:**
- [ ] `go test -race ./internal/ui/... ./internal/config`

**Dependencies:** Task 1 (independent of Tasks 2 and 3, so it can run in parallel)

**Files likely touched:** `internal/ui/popup/connect.go`, `internal/ui/popup/connect_test.go`,
`internal/ui/connect.go`, `internal/ui/connect_test.go`

**Estimated scope:** M

## Checkpoint: Core
- [ ] `go test -race ./...`, `go vet ./...`, `gofmt -l .` clean
- [ ] Manual: a reply to alias mail starts from the alias; switch From;
      send; the recipient sees the alias

## Task 5: Docs and real-provider checks

**Description:** Document aliases for users and check the spec's
assumptions with real sends.

**Acceptance criteria:**
- [ ] `docs/send-as.md`: a user guide to sending from aliases, covering what an
      alias is, setting one up in config or the connect popup, the From row, how
      replies choose From, and setup per provider (Gmail "Send mail as"
      verification, Fastmail, iCloud), plus what isn't supported. Linked from
      the README.
- [ ] README: the `aliases` key, the From row, how replies choose From,
      Gmail's "Send mail as" verification warning, and that a domain needing its own
      SMTP login is a separate account.
- [ ] SPEC config block and composer table updated; `aliases-spec.md`
      assumptions ticked or noted as failing.
- [ ] Sends from an alias checked on Gmail, Fastmail and iCloud (the From
      header at the recipient).

**Verification:**
- [ ] Manual sends as listed

**Dependencies:** Tasks 1–4

**Files likely touched:** `README.md`, `SPEC.md`, `tasks/aliases-spec.md`

**Estimated scope:** S

## Checkpoint: Complete
- [ ] All acceptance criteria met
- [ ] Review with the user before committing
