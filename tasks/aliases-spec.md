# Send-as aliases

## Problem Statement

How might we send from own-domain addresses through accounts that are
already connected, with replies picking the right address automatically?

## Recommended Direction

Each account gets an optional list of aliases in config, which the
connect-account popup can also fill in (adding an account still appends a
TOML block). The composer gets a **From** field above To, limited to the
account email and its aliases. New messages start from the account email.
Replies and forwards start from whichever of the user's addresses the
original was sent to (To, then Cc), falling back to the account email.

Nothing changes in transport: `mail.Draft.From` already exists and `send`
and SMTP already use it. The SMTP envelope sender stays the account login,
which Gmail, Fastmail and iCloud all expect.

## Key Assumptions to Validate

- [ ] Gmail sends from a verified alias over `smtp.gmail.com` with the
      account's app password. Test with a real send and check the headers.
- [ ] Fastmail and iCloud accept a custom-domain From with the main login.
      Test one send on each.
- [ ] To and Cc find the right alias for most replies. Check against a
      real inbox.

## MVP Scope

In:

- `aliases = [{ email = "...", name = "..." }]` on an account. `name` is
  optional and falls back to `from_name`. Strictly validated; an alias
  can't repeat the account email or another alias.
- An Aliases field in the connect-account popup, comma-separated.
- A From field in the composer that only accepts the account email or an
  alias; `tab` there cycles through them.
- Replies and forwards pick From from the original's To, then Cc.
- Saved drafts keep their From.
- README: Gmail rewrites From silently unless the alias is verified in its
  "Send mail as" settings.

## Not Doing (and Why)

- **Fetching aliases from the provider:** needs OAuth or the Gmail API,
  which aren't available. Config is enough.
- **SMTP credentials per alias:** that is a separate account, which
  already works.
- **Catch-all matching** (`*@domain`): rare. Later, if wanted.
- **Offering to add an unknown alias a mail was sent to:** telling the
  user's own addresses from other recipients is a guess. Switching From
  and editing config by hand covers it.
- **Signature per alias:** the signature is per account, shared by its
  aliases.
- **Detecting a Gmail rewrite:** not possible from SMTP. Documented
  instead.
- **Editing aliases in the app:** waits for "Edit accounts".
