# Installing dk-email

How to install, update and remove dk-email. Pick the way that fits your
system.

- [Homebrew (macOS, Linux)](#homebrew-macos-linux)
- [Release archive (Linux, macOS)](#release-archive-linux-macos)
- [Go](#go)
- [From source](#from-source)
- [Updating](#updating)
- [Uninstalling](#uninstalling)

## Homebrew (macOS, Linux)

```sh
brew install dkouvdis/tap/dk-email
```

That one command is all you need. It taps `dkouvdis/homebrew-tap` on first
use, and `brew upgrade dk-email` picks up new releases. Keep the tap:
`brew untap` stops upgrades.

dk-email is published as a cask with builds for macOS and Linux, on
Intel/AMD and ARM.

- **macOS:** the binaries aren't signed by Apple. The cask removes macOS's
  quarantine flag on install, so dk-email opens straight away. If macOS
  still says the binary "is damaged" or "can't be opened", clear the flag
  yourself:

  ```sh
  xattr -d com.apple.quarantine "$(which dk-email)"
  ```

- **Linux:** this needs a Homebrew that installs casks on Linux. Run
  `brew update` first; if Homebrew still refuses the cask, use the
  [release archive](#release-archive-linux-macos).

## Release archive (Linux, macOS)

Each [release](https://github.com/dkouvdis/dk-email/releases) has a `tar.gz`
per system: `dk-email_<version>_<os>_<arch>.tar.gz`, with `os` `linux` or
`darwin` and `arch` `amd64` (Intel/AMD) or `arm64`. For example, on 64-bit
Intel/AMD Linux:

```sh
VERSION=0.2.0
curl -fsSLO "https://github.com/dkouvdis/dk-email/releases/download/v${VERSION}/dk-email_${VERSION}_linux_amd64.tar.gz"
curl -fsSLO "https://github.com/dkouvdis/dk-email/releases/download/v${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
tar -xzf "dk-email_${VERSION}_linux_amd64.tar.gz" dk-email
install -m 755 dk-email ~/.local/bin/
```

Use `linux_arm64` on ARM (for example a Raspberry Pi 4 or 5 with a 64-bit
OS). Any directory on your `PATH` works instead of `~/.local/bin`. The
binary is static, so there's nothing else to install. On macOS, use
`shasum -a 256 --check --ignore-missing checksums.txt` and clear the
quarantine flag as [above](#homebrew-macos-linux).

With the GitHub CLI, `gh release download` fetches the same files:

```sh
gh release download v${VERSION} --repo dkouvdis/dk-email --pattern '*linux_amd64*' --pattern checksums.txt
```

## Go

Requires Go 1.27 or later.

```sh
go install github.com/dkouvdis/dk-email/cmd/dk-email@latest
```

## From source

```sh
go build -o bin/dk-email ./cmd/dk-email
```

## Updating

- **Homebrew:** `brew upgrade dk-email`.
- **Release archive:** download the new archive and install it over the old
  binary, as above.
- **Go:** run `go install github.com/dkouvdis/dk-email/cmd/dk-email@latest`
  again.

`dk-email --version` shows which version you have.

## Uninstalling

Remove the program first:

- **Homebrew:** `brew uninstall dk-email`. Only after that, if you want,
  `brew untap dkouvdis/tap`.
- **Release archive or Go:** delete the binary (`rm "$(which dk-email)"`).

Your settings and logs stay behind:

- the config folder `~/.config/dk-email` (or `$XDG_CONFIG_HOME/dk-email`),
  which holds `config.toml`;
- the cache folder `~/.cache/dk-email` (or `$XDG_CACHE_HOME/dk-email`),
  which holds the [debug log](troubleshooting.md#the-debug-log).

Remove them with `rm -r ~/.config/dk-email ~/.cache/dk-email` if you want
them gone. Passwords and OAuth sign-ins that dk-email stored are in the OS
keychain under the service `dk-email`. Delete those entries in Keychain
Access (macOS) or with your Secret Service tool (Linux), or delete each
account in dk-email first (see [Deleting an account](accounts.md#deleting-an-account)).
