# Security policy

dk-email handles mail passwords, OAuth tokens and the contents of your
mailbox, so security reports are welcome.

## Reporting a vulnerability

Please report it privately through GitHub:
[Report a vulnerability](https://github.com/dkouvdis/dk-email/security/advisories/new).
Don't open a public issue or pull request for it.

Include what you found, how to reproduce it, and the version
(`dk-email --version`). Leave out real passwords, tokens and mail; a debug log
is fine once you've checked it (see [Troubleshooting](docs/troubleshooting.md)).

You'll get a reply within a week. Fixes go into a new release, and the
advisory is published once that release is out.

## Supported versions

Only the latest release gets security fixes.

## Scope

In scope: anything that could expose a password, OAuth token or client
secret (in files, logs, error messages or the terminal), send or change mail
without the user asking, or weaken TLS. dk-email never writes secrets to
`config.toml`, and logs never contain them; a case where either happens is a
vulnerability.

Out of scope: the security of your mail provider, password manager, keychain
or terminal.
