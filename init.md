# Deja — Predictive Shell Autosuggestions

> You've typed this before.

## What it is

Deja is a smarter replacement for `zsh-autosuggestions`. Instead of only matching commands that start with what you've typed, Deja uses **fuzzy matching**, **directory awareness**, and **command sequence prediction** to suggest what you actually want to run — inline, as ghost text, with no mode switching.

## How it differs from Atuin

Atuin is a history *search* tool: press `Ctrl+R`, a TUI opens, you search, you select. It interrupts your flow and requires an account/sync server.

Deja is a *prediction* tool: suggestions appear inline as ghost text while you type. No account, no sync server, no TUI. One local binary, one SQLite file, zero configuration.

| | Atuin | Deja |
|---|---|---|
| Primary UX | TUI search (Ctrl+R) | Inline ghost text |
| Account required | Optional | Never |
| Sync | Yes (server) | No (local-only) |
| Sequence prediction | No | Yes |
| Directory awareness | Stored, lightly used | Core ranking signal |
| Empty-buffer suggestions | No | Yes |

**Pitch:** Atuin says "search your shell history." Deja says "your shell already knows what you want to type."

## The four ranking signals

1. **Fuzzy match** — subsequence matching with bonuses for consecutive characters, word boundaries, and prefix matches.
2. **Frecency** — log-scaled frequency combined with exponential recency decay (1-week half-life).
3. **Directory affinity** — commands run in this directory (or parents) rank higher.
4. **Sequence prediction** — if you just ran `git add .`, Deja knows `git commit` usually follows. Even with an empty buffer, it shows the predicted next command.

Combined score:

```
final = 1.0 × fuzzy
      + 0.4 × frecency
      + 0.3 × directory_affinity
      + 0.5 × sequence_score
```

## Architecture

### The daemon

A long-running Go process that holds everything in memory and listens on a Unix socket at `~/.local/share/deja/sock`. On startup it loads:

- All `command_stats` into a `map[string]*CommandStat`
- All `sequences` into a `map[string][]Sequence` (prev → list of nexts)
- Directory affinity for the top-100 most-visited directories

Concurrency: a `sync.RWMutex` protects the state. Queries take read locks (concurrent), recording takes write locks (brief, microseconds).

The daemon survives across shell sessions. Multiple terminals share the same daemon.

### Storage

SQLite with three tables:

**`commands`** — raw history
```
id, command, directory, timestamp, exit_code, duration_ms, session_id
```

**`command_stats`** — pre-aggregated per command
```
command (PK), count, last_used
```

**`sequences`** — pairs of consecutive commands within a session
```
prev_command, next_command, count
```

WAL mode is enabled so concurrent reads/writes don't conflict.

### Socket protocol

JSON over Unix socket. Three message types:

- **`suggest`** — `{buffer, dir, prev_command}` → `{suggestion, alternatives[]}`
- **`record`** — `{command, dir, exit_code, duration, session_id, prev_command}` → `{}`
- **`ping`** — `{}` → `{pong: true}`

### The thin client

The zsh widget doesn't construct JSON itself. It runs `deja query --buffer "..." --dir "..." --prev "..."`. That client:

1. Connects to the socket
2. Sends a suggest request
3. Reads one response
4. Prints the suggestion to stdout
5. Exits

Total round trip: under 1ms. If the socket doesn't exist, the client falls back to querying SQLite directly so the tool never breaks.

### Zsh integration

The init script (output by `deja init zsh`) does several things:

- Generates a session ID (`head -c 16 /dev/urandom | xxd -p`)
- Auto-spawns the daemon if it's not already running
- Overrides `self-insert` and `backward-delete-char` widgets to query the daemon after every keystroke and set `POSTDISPLAY` for ghost text
- Tracks the previous command in `__prev_command` so the daemon can do sequence prediction
- Registers `preexec`/`precmd` hooks to record each command in the background with `&!`
- Binds keys: right arrow accepts the suggestion, Ctrl+right accepts one word, Tab shows alternatives, Ctrl+X suppresses an annoying suggestion

## Installation

Three commands:

```bash
brew install yourname/tap/deja
deja import
eval "$(deja init zsh)"
```

The `import` command auto-detects the shell, finds `~/.zsh_history`, and imports it. It prints something concrete:

```
Imported 14,203 unique commands from 38,291 history entries
Top commands: git commit (342×), cd ~/projects (231×), docker compose up (189×)
Ready — add this to your ~/.zshrc:
  eval "$(deja init zsh)"
```

Other install methods:

- `go install github.com/yourname/deja@latest`
- AUR package
- Direct binary download from GitHub releases (via GoReleaser + GitHub Actions)

## What the user sees

**Prefix match:** types `git co`, sees `git co`**`mmit -m "`** in dim gray after the cursor. Right arrow accepts.

**Fuzzy match:** types `deploy stag`, sees `deploy stag` **`⊳ kubectl apply -f ./k8s/deploy-staging.yaml`** as a hint. Right arrow replaces the whole buffer.

**Sequence prediction:** just ran `git add .`, opens a new prompt, sees **`git commit -m "`** already as ghost text before typing anything.

**Multiple matches:** types `docker`, presses Tab, sees a 5-row inline picker with counts and recency. Arrow keys to select, Enter to accept.

**Annoying suggestion:** presses Ctrl+X to suppress the current suggestion temporarily.

**Empty result:** no suggestion means no ghost text at all. Silent when useless, helpful when it can be.

## Project structure

```
deja/
├── cmd/
│   └── deja/
│       ├── main.go          # CLI entrypoint
│       ├── daemon.go        # daemon subcommand
│       └── query.go         # thin client
├── internal/
│   ├── store/               # SQLite logic
│   ├── fuzzy/               # Matching algorithm
│   ├── scorer/              # Frecency + composite ranking
│   ├── daemon/              # State, handlers, protocol
│   └── shell/               # Zsh init script generator
├── go.mod
├── go.sum
├── LICENSE
├── README.md
├── Makefile
└── .goreleaser.yaml
```

## Build order

**Phase 1 — Foundation.** SQLite store with all three tables. Import from zsh history. Inspect with `sqlite3` to verify.

**Phase 2 — Scoring.** Fuzzy matcher and composite scorer as pure functions. Test harness loads real history, prints top-5 results for queries. Tune weights until rankings feel right. *This is where you'll spend the most iteration time.*

**Phase 3 — Daemon.** Bare socket server first (just responds to ping). Then JSON protocol. Then in-memory state with mutex. Then the suggest handler. Then the record handler. Each step is a working daemon.

**Phase 4 — Shell integration.** Init script, widgets, hooks. Source it in your shell and dogfood daily. Edge cases will surface fast: multiline commands, quotes, very long commands, rapid typing.

**Phase 5 — Polish.** Picker, suppress binding, daemon crash recovery, fallback to direct invocation, ghost text color tuning across terminals.

## Distribution

- **Repository:** `github.com/yourname/deja`
- **Releases:** GoReleaser builds for `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64` on git tag push
- **Homebrew tap:** auto-published by GoReleaser to `yourname/homebrew-tap`
- **CI:** GitHub Actions runs `go test ./...` on PRs, releases on tags
- **README:** must include a GIF of the ghost text in action — this is what sells it

## Launch checklist

- [ ] Working daemon + zsh integration
- [ ] Dogfooded for at least a week
- [ ] README with install steps, GIF, and comparison table
- [ ] GoReleaser config + GitHub Action
- [ ] Tagged `v0.1.0` release with prebuilt binaries
- [ ] Homebrew tap set up
- [ ] Posted to r/commandline, r/zsh, Hacker News
