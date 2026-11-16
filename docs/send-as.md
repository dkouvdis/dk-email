# Sending from aliases

An alias is another address you can send mail as through an account you
already have: a custom domain connected to your mailbox (`me@mydomain.com`
on Fastmail or iCloud+), a Gmail "Send mail as" address, or a provider
alias. dk-email sends it through the account's own SMTP server and login, so
an alias needs nothing but its address.

If the address needs its own SMTP server or password, it isn't an alias:
add it as a separate account.

## Before you start

Your provider has to allow sending as the address, or the mail may be
rejected or rewritten:

- **Gmail:** add the address under *Settings → Accounts → Send mail as* and
  confirm the verification email. Without that, Gmail silently replaces
  the From with your Gmail address. There's no error, so check this first.
- **Fastmail:** add the domain or alias under *Settings → Domains* or
  *Settings → Aliases*. Sending from it then works with your usual app
  password.
- **iCloud+ (Custom Email Domain):** add the domain and its addresses under
  *iCloud Settings → Custom Email Domain*. Addresses must be set up there
  before iCloud accepts them as From.
- **Other providers:** look for "aliases", "identities" or "send as" in the
  settings. The SMTP server must accept the address as From for your login.

## Setting up aliases

The account must be able to send, so it needs `smtp_host`.

### In the config file

Add `aliases` to the account in `config.toml`:

```toml
[[account]]
name         = "Personal"
email        = "me@fastmail.com"
imap_host    = "imap.fastmail.com"
smtp_host    = "smtp.fastmail.com"
from_name    = "Jane Doe"
aliases      = [
  { email = "jane@mydomain.com" },
  { email = "hello@mydomain.com", name = "Jane at MyDomain" },
]
```

- `email` is required and must be a bare address (`jane@mydomain.com`, not
  `Jane <jane@mydomain.com>`).
- `name` is optional. Without it, the alias uses the account's `from_name`.
- An alias can't repeat another alias or the account's own `email`
  (case doesn't matter).
- `aliases` without `smtp_host` is a config error, like `from_name`.

Restart dk-email after editing the file.

### When connecting an account

The connect popup has an **Aliases** field after **SMTP TLS**. Type the
addresses separated by commas:

```
me@mydomain.com, hello@mydomain.com
```

The field is only saved when an SMTP host is set. Invalid or duplicate
addresses show an error and nothing is saved. To give an alias its own
name, add `name` to it in the config file afterwards.

## Choosing the From address

When an account has aliases, the composer shows a **From** row above **To**,
and the composer's title shows the selected address.

| Key | Action |
|---|---|
| `tab` / `shift+tab` | move to and from the From row |
| `←` / `→` or `space` | switch to the previous / next address |
| `enter` | open a list of addresses |

In the list, `↑`/`↓` (or `k`/`j`) move, `enter` picks the address and `esc`
closes the list without changing it.

Without aliases there's no From row, and mail is sent from the account's
address.

## How replies choose From

Replies, reply-all and forwards start from the address the message was sent
to, so you answer as the address people wrote to:

1. If you're replying to your own message, From is the address you sent it from.
2. Otherwise, the first of your addresses found in the message's **To**,
   then **Cc**.
3. Otherwise (for example, mail that reached you by Bcc or a mailing list),
   the account's own address.

You can always switch it in the From row before sending. Reply-all never
copies any of your own addresses back to you.

Reopened drafts keep the From they were saved with, even if that alias has
since been removed from the config.

## What isn't supported

- **Aliases with their own SMTP login.** Add the address as a separate account.
- **Typing any From address.** From is picked from the configured list, so
  you can only send as addresses you set up.
- **A signature per alias.** Signatures aren't supported yet; when they are,
  an account's aliases will share its signature.
