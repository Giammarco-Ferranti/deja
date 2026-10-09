# Privacy

[Back to the README](../README.md#privacy)

[Exclude a command](#exclude-a-command) · [Exclude patterns](#exclude-patterns) ·
[History import](#history-import) · [Data locations](#data-locations) ·
[Existing data](#existing-data)

deja records commands and execution metadata in a local SQLite database.
It does not send command history to a server. The database is plaintext:
deja does not encrypt it or automatically redact secrets embedded in commands.

## Exclude a command

Enable zsh's leading-space exclusion in your `.zshrc`:

```zsh
setopt hist_ignore_space
```

Then put a space before a sensitive command:

```zsh
 export AWS_SECRET_ACCESS_KEY=example-secret
```

With that option enabled, deja's shell hook skips commands starting with
whitespace before forwarding them to its recording process or socket.
It also clears the previous command used for sequence prediction.

The storage layer rejects commands starting with a space or tab even without
`hist_ignore_space`. That protects the database, but the shell hook does not
skip forwarding them unless the option is enabled. Enable the option when you
want to exclude sensitive commands at the shell hook as well.

This controls deja's recording. Other processes or commands may expose their
own arguments independently.

## Exclude patterns

The live shell hook checks `HISTORY_IGNORE` as a zsh glob against the whole
command line:

```zsh
HISTORY_IGNORE='(*AWS_SECRET*|*--password*)'
```

Matching commands are skipped, and the hook breaks the sequence prediction
chain. Set the pattern in your shell configuration.

These exclusions do not remove commands that deja recorded earlier.

## History import

`deja import` reads an exported `$HISTFILE`, falling back to `~/.zsh_history`.
To choose the file explicitly:

```sh
deja import --file /path/to/history
```

The importer skips blank commands and commands beginning with a space or tab,
including commands in zsh's extended-history format. It does not evaluate
`HISTORY_IGNORE` against existing entries. Remove sensitive entries from a
history file before importing it.

## Data locations

| Path | Contents |
| --- | --- |
| `~/.local/share/deja/deja.db` | Command history, statistics, and sequences |
| `~/.local/share/deja/deja.db-wal` and `deja.db-shm` | SQLite write-ahead log and shared-memory files |
| `~/.local/share/deja/config` | Saved fuzzy and empty-prompt settings |
| `~/.local/share/deja/init.zsh` | Cached shell integration |
| `~/.local/share/deja/sock` | Daemon socket, for the usual data path |
| `~/.local/share/deja/sock.pid` | Daemon process ID, for the usual socket path |

deja sets the data directory to `0700` and the database files and socket to
`0600`. It reapplies those permissions when opening the database or starting
the daemon, including for installations created by older versions.
Processes running as your account or as root can still read the data.

When the normal socket path exceeds the platform limit, deja uses a short
socket name under an owner-only runtime directory: `$XDG_RUNTIME_DIR/deja`
or, on macOS, `$TMPDIR/deja`. The socket's pidfile sits alongside it.
The generated integration script contains the actual path in `DEJA_SOCK`.

## Existing data

Enabling an exclusion does not delete older database entries. To rebuild the
database from a history file you have cleaned, follow
[reset the database](troubleshooting.md#reset-the-database).

For vulnerability reports and the supported-version policy, see
[SECURITY.md](../SECURITY.md).
