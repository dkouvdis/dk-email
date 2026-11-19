# dk-email

A keyboard-driven terminal email client for triaging several IMAP accounts (Gmail included). It shows a folders sidebar beside a thread list, opens conversations in a popup, and uses lazygit-style single-key actions.

## Features

- Triage several accounts at once, with a folders tree, unread counts and threaded conversations. [Usage](docs/usage.md)
- Work from the keyboard (mark, star, archive, delete, move) or with the mouse. [Usage](docs/usage.md)
- Write, reply, forward and save drafts in a built-in composer, sent over SMTP. [Sending mail](docs/sending.md)
- Send from your aliases and custom domains. [Sending from aliases](docs/send-as.md)
- Attach files and open attachments. [Attachments](docs/attachments.md)
- Search a folder with Gmail-style operators. [Searching](docs/search.md)
- Keep passwords out of the config: 1Password, any command, the OS keychain or GPG. [Passwords](docs/passwords.md)
- Sign in to Microsoft 365, Outlook.com and Google with OAuth 2.0. [OAuth](docs/oauth.md)
- Get new-mail alerts (bell, desktop, window title), pushed within seconds. [Notifications](docs/notifications.md)

v0.x is a personal tool: it reads, triages and sends mail. Nothing is cached on disk.

## Install

```sh
brew install dkouvdis/tap/dk-email
```

For Linux, the release archive and Go, see [Installing dk-email](docs/install.md).

## Quick start

```sh
dk-email                       # uses $XDG_CONFIG_HOME/dk-email/config.toml (or ~/.config/dk-email/config.toml)
dk-email --config path/to.toml # use another config file
dk-email --version
```

On first run with no accounts, the connect popup opens and walks you through adding one; see [Accounts](docs/accounts.md). Press `?` to see the keys, and `A` to add another account. To write the file by hand instead, start from [config.example.toml](config.example.toml).

## Documentation

| Guide | What it covers |
|---|---|
| [Usage](docs/usage.md) | Layouts, keys, the mouse, threads and the reader |
| [Accounts](docs/accounts.md) | Adding, editing and deleting accounts, which login to choose, 2FA, provider settings |
| [Passwords](docs/passwords.md) | `password_cmd`, 1Password, the OS keychain |
| [Passwords from GPG](docs/gpg.md) | GPG and `pass`, with a graphical pinentry |
| [Signing in with OAuth 2.0](docs/oauth.md) | Microsoft 365, Outlook.com and Google |
| [Configuration](docs/configuration.md) | Every `config.toml` key, defaults, colours and validation |
| [Sending mail](docs/sending.md) | SMTP, the composer, drafts, reply and forward |
| [Sending from aliases](docs/send-as.md) | Other From addresses |
| [Attachments](docs/attachments.md) | Attaching files and opening attachments |
| [Searching](docs/search.md) | Operators, dates and provider differences |
| [New-mail notifications](docs/notifications.md) | Bell, desktop alerts, window title and push |
| [Installing dk-email](docs/install.md) | Homebrew, release archive, Go, updating and removing |
| [Troubleshooting](docs/troubleshooting.md) | The debug log and where to look for each problem |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development and releases. The spec is in [SPEC.md](SPEC.md) and the task plan in [tasks/](tasks/).

## Licence

[MIT](LICENSE)
