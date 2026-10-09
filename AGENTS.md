# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**Deja** is a Go CLI tool that provides predictive inline shell autosuggestions for zsh. It uses fuzzy matching, directory awareness, and command sequence prediction to show ghost text while you type — no TUI, no account, no sync server.

## Commands

Once the project is bootstrapped (go.mod, Makefile exist):

```bash
# Build
make build         # or: go build ./cmd/deja

# Test
go test ./...      # all tests
go test ./internal/fuzzy/...  # single package

# Lint
go vet ./...

# Install locally
go install ./cmd/deja

# After install: import zsh history and activate
deja import
eval "$(deja init zsh)"
```

CI runs `go test ./...` on PRs and releases on git tags via GoReleaser.

## Architecture

### Components

**Daemon** (`internal/daemon/`, `cmd/deja/daemon.go`)
- Long-running Go process, socket at `~/.local/share/deja/sock`
- Loads all state into memory on startup: `map[string]*CommandStat`, `map[string][]Sequence`, top-100 directory affinities
- `sync.RWMutex`: reads are concurrent, writes are brief (microseconds)
- Survives across shell sessions; multiple terminals share one daemon

**Storage** (`internal/store/`)
- SQLite with WAL mode; three tables:
  - `commands` — raw history: `id, command, directory, timestamp, exit_code, duration_ms, session_id`
  - `command_stats` — aggregated per command: `command (PK), count, last_used`
  - `sequences` — consecutive command pairs within a session: `prev_command, next_command, count`

**Socket Protocol** — JSON over Unix socket
- `suggest`: `{buffer, dir, prev_command}` → `{suggestion, alternatives[]}`
- `record`: `{command, dir, exit_code, duration, session_id, prev_command}` → `{}`
- `ping`: `{}` → `{pong: true}`

**Thin Client** (`cmd/deja/query.go`)
- `deja query --buffer "..." --dir "..." --prev "..."` connects to socket, prints suggestion, exits (<1ms round trip)
- Falls back to direct SQLite query if daemon is unavailable

**Fuzzy Scorer** (`internal/fuzzy/`, `internal/scorer/`)
- Subsequence matching with bonuses for consecutive chars, word boundaries, and prefix hits
- Composite score: `1.0×fuzzy + 0.4×frecency + 0.3×directory_affinity + 0.5×sequence_score`
- Frecency = log-scaled frequency + exponential recency decay (1-week half-life)

**Shell Integration** (`internal/shell/`, `cmd/deja/main.go`)
- `deja init zsh` outputs a zsh init script that:
  - Generates a session ID, auto-spawns the daemon
  - Overrides `self-insert` / `backward-delete-char` widgets to set `POSTDISPLAY` ghost text after each keystroke
  - Registers `preexec`/`precmd` hooks to record commands in background (`&!`)
  - Skips commands zsh itself won't remember (`HIST_IGNORE_SPACE`, `HISTORY_IGNORE`) via `_deja_history_ignored`, and clears `__deja_prev` so they can't leak into the `sequences` table. `store.IgnoredCommand` re-applies the rule unconditionally as the last chokepoint before SQLite.
  - Key bindings: right arrow = accept, Ctrl+right = accept one word, Tab = inline alternatives picker, Ctrl+X = suppress

### Build Phases

The project is built in order: **Storage → Scorer → Daemon → Shell integration → Polish**. The scorer (`internal/scorer/`) is the most iteration-heavy part — tune the four signal weights against real history until rankings feel right.

### Distribution

GoReleaser builds `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64` on tag push. A Homebrew tap is auto-published by GoReleaser.
