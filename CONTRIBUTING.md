# Contributing to deja

You can contribute bug reports, documentation fixes, or code changes.
Open an issue before starting a larger change so maintainers can discuss it
with you.

[Development setup](#development-setup) · [Workflow](#workflow) ·
[Commit messages](#commit-messages) · [Releases](#releases) · [Security](#security)

## Development setup

Use the Go version in [go.mod](https://github.com/Giammarco-Ferranti/deja/blob/main/go.mod)
or a newer compatible version. The SQLite dependency requires CGO and a C compiler.

```sh
git clone https://github.com/Giammarco-Ferranti/deja.git
cd deja
make build         # release-style binary at ./bin/deja
make build-debug   # unstripped binary at the same path for debugging

go test ./...      # run tests
go test -race ./... # run with the race detector, as CI does
go vet ./...       # check Go code
```

For shell setup, see the [README](README.md#installation).

## Workflow

1. Fork the repository and create a topic branch from `main`, such as
   `fix/daemon-socket-cleanup` or `feat/scorer-prefix-boost`.
2. Make your change. For behavior changes, add or update tests in the affected
   package's `_test.go` files.
3. Run `go test -race ./...` and `go vet ./...`. Both must pass.
4. Push your branch and open a pull request against `main`.

CI runs vet and race tests on macOS and Linux. The test checks must pass
before merge.

## Commit messages

Use [conventional commits](https://www.conventionalcommits.org/).
release-please uses them to choose version bumps and assemble the changelog.
The squash-merge commit uses the PR title, so use the same format there.

| Prefix | Change | Version bump |
| --- | --- | --- |
| `feat:` | User-visible feature | Minor |
| `fix:` | Bug fix | Patch |
| `feat!:` or a `BREAKING CHANGE:` footer | Breaking change | Major |
| `chore:`, `docs:`, `test:`, `refactor:`, `ci:` | Maintenance | None |

## Areas to work on

The scorer in `internal/scorer/` combines fuzzy matching, frecency, directory
affinity, and command sequences. If you adjust signal weights, include examples
showing how the change affects suggestion quality.

The integration in `internal/shell/zsh.sh` wraps ZLE widgets. Check changes
with multiline buffers, quoted strings, and rapid typing.
The [architecture guide](docs/architecture.md) describes both components.

## Bug reports

Use the [Bug Report template](https://github.com/Giammarco-Ferranti/deja/issues/new/choose).
Include:

- `deja --version`, your operating system, and `zsh --version`.
- Reproduction steps with sample commands.
- What you expected and what happened.

For daemon problems, include `deja ping` output and whether stopping the daemon
and opening a fresh shell resolves the problem. See
[daemon recovery](docs/troubleshooting.md#daemon-recovery).

## Releases

[release-please](https://github.com/googleapis/release-please) reads qualifying
commits on `main` and opens or updates a release PR. That PR updates
`.release-please-manifest.json` and `CHANGELOG.md`.

Merging the release PR creates the `vX.Y.Z` tag. The tag triggers the release
workflow, which runs the test suite before GoReleaser publishes binary archives.

Maintain releases through the release PR; do not create release tags manually.
Release archives include the README and its linked guides and images.

## Security

Follow [SECURITY.md](SECURITY.md) and
[report vulnerabilities privately](https://github.com/Giammarco-Ferranti/deja/security/advisories/new).
Keep vulnerability details out of public issues and pull requests.

## Conduct

Be kind and assume good faith. Maintainers may remove comments or block
accounts for personal attacks, harassment, or discriminatory behavior.
