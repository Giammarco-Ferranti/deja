# Contributing to Deja

Thanks for your interest in improving Deja. Contributions of all sizes — bug reports, docs fixes, code — are welcome.

## Before you start

For anything larger than a small bug fix or doc tweak, please **open an issue first** so we can align on direction before you invest time. This avoids the awkward "thanks, but we're going a different way" outcome on a finished PR.

## Development setup

```bash
git clone https://github.com/Giammarco-Ferranti/deja.git
cd deja
make build        # produces ./bin/deja, stripped and trimmed like a release
make build-debug  # same path, unstripped, for delve

go test ./...     # run all tests
go test -race ./... # what CI runs
go vet ./...      # lint
```

Go version: see `go.mod` (CI uses the version pinned there).

## Workflow

1. Fork the repo and create a topic branch off `main` (e.g. `fix/daemon-socket-cleanup`, `feat/scorer-prefix-boost`).
2. Make your change. Add or update tests — every package has a `_test.go` companion; keep that pattern.
3. Run `go test -race ./...` and `go vet ./...` locally. Both must pass.
4. Push and open a pull request against `main`.

CI (`.github/workflows/test.yml`) runs `go vet` and `go test -race` on every PR. Status check `test` must be green before merge.

## Commit messages

Deja uses [conventional commits](https://www.conventionalcommits.org/) — release-please reads them to decide version bumps and to assemble `CHANGELOG.md`. Use one of:

- `feat: ...` — new user-visible feature (minor bump)
- `fix: ...` — bug fix (patch bump)
- `feat!: ...` or a `BREAKING CHANGE:` footer — breaking change (major bump)
- `chore:`, `docs:`, `test:`, `refactor:`, `ci:` — no version bump

PR titles follow the same convention (the squash-merge commit uses the PR title).

## Areas that take iteration

- **Scorer (`internal/scorer/`)** — the four signal weights (`fuzzy`, `frecency`, `directory_affinity`, `sequence_score`) are the most fruitful place to experiment if you want to improve suggestion quality. Tune against your own zsh history, then justify the change in the PR description with examples.
- **Shell integration (`internal/shell/zsh.sh`)** — widget interactions are subtle. Test against multiline buffers, quoted strings, and very fast typing.

## Bug reports

File issues via the **Bug Report** template. The most useful reports include:

- `deja --version`
- OS and `zsh --version`
- Exact reproduction steps
- What you expected vs. what happened

If it's a daemon problem, `deja ping` output and whether `pkill -f 'deja daemon'` + a fresh shell resolves it is gold.

## Security

Please do not file security issues as public GitHub issues. Use GitHub's private vulnerability reporting on this repo (Security tab → Report a vulnerability).

## Conduct

Be kind. Assume good faith. Personal attacks, harassment, or discriminatory behavior aren't welcome and may result in maintainers removing comments or blocking accounts.
