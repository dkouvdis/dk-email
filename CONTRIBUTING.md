# Contributing

For the architecture and the repository's conventions, read
[AGENTS.md](AGENTS.md). The spec lives in [SPEC.md](SPEC.md), and the task
plan in [tasks/](tasks/).

## Development

```sh
go test -race ./...
go vet ./...
gofmt -l .          # must print nothing
```

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org).
The release changelog is grouped by `feat:` and `fix:`, and drops `docs:`,
`test:` and `chore:`.

## Releasing (maintainers)

Versions come only from git tags ([SemVer](https://semver.org)). Nothing in
the source holds a version number. GoReleaser injects it at build time.

**One-time prerequisites:**

- The public tap repo `dkouvdis/homebrew-tap` exists.
- The `HOMEBREW_TAP_GITHUB_TOKEN` secret is set on `dkouvdis/dk-email`. It
  must be a fine-grained personal access token with **Contents: read and
  write** on the tap repo only.
- Fine-grained tokens expire. When it does, stable releases fail at the cask
  upload step. Create a new token and update the secret, then re-run the
  release workflow.
- Optional: `brew install goreleaser` for local dry runs.

**Dry run** (builds into `dist/` and publishes nothing):

```sh
goreleaser check
goreleaser release --snapshot --clean
```

**Release.** Pushing a `v*` tag runs `.github/workflows/release.yml`, which
runs the tests, then builds darwin/linux × amd64/arm64 archives with
checksums and creates the GitHub release.

```sh
git tag -a v0.1.0-beta.1 -m "v0.1.0-beta.1"
git push origin v0.1.0-beta.1
```

- Tags with `-beta.N` or `-rc.N` become GitHub pre-releases and **never**
  update the Homebrew tap.
- A stable tag such as `v0.1.0` also pushes the updated cask to
  `dkouvdis/homebrew-tap`.
- Homebrew installs download the release assets from this public
  repository, so a release is installable as soon as the cask is pushed.
