<div align="center">
  <img src="deja-mascot.png" alt="Deja mascot" width="120" />
  <h1>deja</h1>
  <p><strong>Predictive ghost-text autosuggestions for zsh — smarter than history, lighter than a plugin.</strong></p>

  <p>
    <a href="https://github.com/Giammarco-Ferranti/deja/releases"><img src="https://img.shields.io/github/v/release/Giammarco-Ferranti/deja?style=flat-square" alt="Latest release" /></a>
    <a href="https://github.com/Giammarco-Ferranti/deja/actions"><img src="https://img.shields.io/github/actions/workflow/status/Giammarco-Ferranti/deja/ci.yml?style=flat-square" alt="CI" /></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/Giammarco-Ferranti/deja?style=flat-square" alt="License" /></a>
    <a href="https://goreportcard.com/report/github.com/Giammarco-Ferranti/deja"><img src="https://goreportcard.com/badge/github.com/Giammarco-Ferranti/deja?style=flat-square" alt="Go Report Card" /></a>
  </p>
</div>

---

Deja is a smarter replacement for [`zsh-autosuggestions`](https://github.com/zsh-users/zsh-autosuggestions). Instead of only surfacing commands that start with what you've typed, Deja uses **fuzzy matching**, **directory awareness**, and **command sequence prediction** to suggest what you actually want to run — as inline ghost text, after every keystroke, with zero latency.

No account. No sync server. No TUI. Just ghost text that knows where you are.

```
~/projects/myapp  $ dc up
                       ▏docker compose up --build   ←  press → to accept
```

---

## Features

- **Fuzzy matching** — suggests commands even when you skip letters or mix up order
- **Directory awareness** — commands you run in `~/projects/foo` rank higher when you're in `~/projects/foo`
- **Sequence prediction** — knows that you usually run `make test` after `make build`
- **Frecency scoring** — blends frequency + recency with a 1-week exponential decay
- **Ghost text inline** — uses zsh's `POSTDISPLAY` widget, not a separate pane
- **Daemon architecture** — one lightweight background process serves all terminal windows; `<1ms` response per keystroke
- **Local-only** — all data stays in a local SQLite database; nothing leaves your machine
- **Alternatives picker** — press `Tab` to cycle through ranked alternatives without leaving the line

---

## Installation

### Homebrew (macOS & Linux)

```bash
brew install Giammarco-Ferranti/tap/deja
```

### Go

```bash
go install github.com/Giammarco-Ferranti/deja/cmd/deja@latest
```

### Prebuilt binaries

Download from the [releases page](https://github.com/Giammarco-Ferranti/deja/releases) for `darwin/amd64`, `darwin/arm64`, `linux/amd64`, or `linux/arm64`.

---

## Setup

After installing, run once to import your existing zsh history and activate the shell integration:

```bash
deja import
eval "$(deja init zsh)"
```

To make it permanent, add the `eval` line to your `~/.zshrc`:

```zsh
# ~/.zshrc
eval "$(deja init zsh)"
```

Deja auto-spawns its daemon on first use and keeps it running across sessions.

---

## Key Bindings

| Key | Action |
|---|---|
| `→` (right arrow) | Accept full suggestion |
| `Ctrl+→` | Accept next word only |
| `Tab` | Open inline alternatives picker |
| `Ctrl+X` | Suppress current suggestion |

---

## How It Works

Deja is built around four signals that are combined into a single composite score:

```
score = 1.0 × fuzzy
      + 0.4 × frecency
      + 0.3 × directory_affinity
      + 0.5 × sequence_score
```

| Signal | What it measures |
|---|---|
| **Fuzzy** | Subsequence match quality with bonuses for consecutive characters, word boundaries, and prefix hits |
| **Frecency** | Log-scaled frequency combined with exponential recency decay (1-week half-life) |
| **Directory affinity** | How often you've run this command from the current directory |
| **Sequence score** | Probability that this command follows the one you just ran |

### Architecture

```
┌─────────────────┐     JSON/Unix socket      ┌──────────────────────┐
│   zsh widget    │ ──────────────────────▶   │   deja daemon        │
│  (per keystroke)│ ◀──────────────────────   │  (single process,    │
└─────────────────┘    suggestion (<1ms)       │   all terminals)     │
                                               └──────────┬───────────┘
                                                          │
                                                    SQLite (WAL)
                                               commands · stats · seqs
```

The daemon loads all state into memory at startup (`map[string]*CommandStat`, top-100 directory affinities, sequence pairs) and uses a `sync.RWMutex` so reads never block each other. Writes (command recording) take microseconds.

If the daemon is unavailable, `deja query` falls back to a direct SQLite read automatically.

---

## Building from Source

```bash
git clone https://github.com/Giammarco-Ferranti/deja.git
cd deja
make build        # produces ./bin/deja

go test ./...     # run all tests
go vet ./...      # lint
```

---

## Contributing

Contributions are welcome. Please open an issue before submitting a large PR so we can align on direction.

1. Fork the repo and create a branch from `main`
2. Make your changes with tests
3. Run `go test ./...` and `go vet ./...`
4. Open a pull request

The scorer (`internal/scorer/`) is the most iteration-heavy part of the codebase — the four signal weights are the best place to experiment if you want to improve suggestion quality.

---

## License

MIT — see [LICENSE](LICENSE).

---

<div align="center">
  <sub>Made with ☕ and a friendly ghost.</sub>
</div>
