# Passwords from GPG

You can keep an account's password in a GPG-encrypted file and have
dk-email decrypt it with `password_cmd`. The same applies to
[`pass`](https://www.passwordstore.org/) and `gopass`, which use GPG
underneath.

The catch is the passphrase prompt. dk-email runs `password_cmd` with no
terminal to type into, so gpg must ask through a **graphical pinentry**, or
already have the passphrase cached. Set that up once and it works like
1Password's Touch ID prompt.

- [Encrypt the password](#encrypt-the-password)
- [Point the account at it](#point-the-account-at-it)
- [Set up a graphical pinentry](#set-up-a-graphical-pinentry)
- [Caching the passphrase](#caching-the-passphrase)
- [Test it like dk-email runs it](#test-it-like-dk-email-runs-it)
- [Troubleshooting](#troubleshooting)

## Encrypt the password

Encrypt to your own key, so only your GPG key can read it. Type the password,
press `enter`, then `ctrl+d`. This way the password never lands in your shell
history:

```sh
mkdir -p ~/.config/dk-email
gpg --encrypt --recipient you@example.com --output ~/.config/dk-email/work.gpg
```

Only the first line of the decrypted output is used, so a trailing newline
or notes on later lines are fine.

With `pass`, store it as usual instead:

```sh
pass insert mail/work
```

## Point the account at it

Set `password_cmd` in the account's block in `config.toml`:

```toml
password_cmd = "gpg --quiet --decrypt ~/.config/dk-email/work.gpg"
```

or, with `pass`:

```toml
password_cmd = "pass show mail/work"
```

The command runs with `sh -c` in your environment, so `~` and your `PATH`
work. If gpg isn't found, use its absolute path (`which gpg`). dk-email
waits up to 60 seconds for the command, which is plenty for a passphrase
dialog.

## Set up a graphical pinentry

gpg asks for your passphrase through gpg-agent, which starts the program
named by `pinentry-program` in `~/.gnupg/gpg-agent.conf`. A terminal
pinentry (`pinentry-curses`, `pinentry-tty`) can't work here: dk-email owns
the terminal, and the command has none.

### macOS

```sh
brew install pinentry-mac
echo "pinentry-program $(brew --prefix)/bin/pinentry-mac" >> ~/.gnupg/gpg-agent.conf
gpgconf --kill gpg-agent
```

GPG Suite (GPGTools) already uses its own graphical pinentry, so there's
nothing to change if you installed gpg that way.

In the pinentry-mac dialog, tick **Save in Keychain** to store the
passphrase in the macOS Keychain, so it's never asked again.

### Linux

Install the pinentry for your desktop and point gpg-agent at it:

```sh
# GNOME and most GTK desktops
echo "pinentry-program /usr/bin/pinentry-gnome3" >> ~/.gnupg/gpg-agent.conf
# KDE and Qt desktops
echo "pinentry-program /usr/bin/pinentry-qt" >> ~/.gnupg/gpg-agent.conf

gpgconf --kill gpg-agent
```

Use only one `pinentry-program` line. The dialog needs your desktop session
(`DISPLAY` or `WAYLAND_DISPLAY`), which dk-email passes on from your
environment, so it won't appear over SSH.

## Caching the passphrase

gpg-agent remembers the passphrase for a while after you enter it, so
reconnects and new dk-email sessions don't ask every time. The defaults are
short (10 minutes since last use, 2 hours at most). To ask less often, add
to `~/.gnupg/gpg-agent.conf`:

```
default-cache-ttl 28800
max-cache-ttl 86400
```

(8 hours since last use, 1 day at most), then run `gpgconf --kill gpg-agent`.

Once the passphrase is cached, any pinentry works, because none is shown.
You can unlock it in a normal terminal before starting dk-email, for example
with `gpg --quiet --decrypt ~/.config/dk-email/work.gpg > /dev/null`.

## Test it like dk-email runs it

dk-email runs `password_cmd` with stdin closed and without `GPG_TTY`. To
check your setup the same way, clear the cache, then run:

```sh
gpgconf --reload gpg-agent
env -u GPG_TTY sh -c 'gpg --quiet --decrypt ~/.config/dk-email/work.gpg' < /dev/null
```

A passphrase dialog should appear, and the password is printed after you
enter it. If you get an error instead, see below.

## Troubleshooting

When the command fails, the account shows as failed and the status bar
shows gpg's message. Fix the cause and press `ctrl+r` on the main screen to
try again.

| Message | Meaning |
|---|---|
| *No pinentry* | `pinentry-program` points to a program that isn't installed. Check the path in `~/.gnupg/gpg-agent.conf`. |
| *Inappropriate ioctl for device* | gpg-agent tried a terminal pinentry. Set up a [graphical pinentry](#set-up-a-graphical-pinentry). |
| *Operation cancelled* | The passphrase dialog was closed or cancelled. Press `ctrl+r` to try again. |
| *No secret key* | The file was encrypted to a key you don't have on this machine. |
| *timed out after 1m0s* | Nobody answered the dialog in time, or it opened somewhere you couldn't see it. |

**The prompt shows up inside dk-email's screen.** dk-email removes `GPG_TTY`
from the command's environment so gpg doesn't use the app's terminal. But if
your shell runs `gpg-connect-agent updatestartuptty /bye` (some setups do,
for SSH via gpg-agent), gpg-agent is already pointed at that terminal and
a terminal pinentry can still draw there. Switch to a graphical pinentry and
the problem goes away.
