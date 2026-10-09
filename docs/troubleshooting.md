# Troubleshooting

[Back to the README](../README.md#troubleshooting)

[Missing suggestions](#missing-suggestions) · [Faint ghost text](#faint-ghost-text) ·
[Plugin conflicts](#plugin-conflicts) · [Daemon recovery](#daemon-recovery) ·
[Stale sockets](#stale-sockets) · [Reset the database](#reset-the-database)

Each subcommand supports `--help`, for example `deja query --help`.

## Missing suggestions

1. Run `deja ping`. A reachable daemon prints `pong`.
2. Check that `.zshrc` loads the cached integration or your plugin manager loads
   deja. Follow the [installation steps](../README.md#installation), then run
   `exec zsh`.
3. Press `Ctrl+X` if you suppressed suggestions in this session. A new shell
   also clears session suppression.
4. Confirm that you imported history with `deja import`. Use `--file` if your
   history is elsewhere and `$HISTFILE` is not exported.

If only empty-prompt suggestions are missing, check `deja empty` and use
`deja empty on` to enable them.

## Faint ghost text

The default `fg=8` can be hard to read on some terminal themes. Export a
brighter style above the lines that load deja, then restart the shell:

```zsh
export DEJA_HIGHLIGHT_STYLE='fg=244'
```

See [ghost text appearance](configuration.md#ghost-text-appearance) for styles.

## Plugin conflicts

deja detects zsh-autosuggestions, prints a notice, and disables its own
integration to avoid both plugins wrapping the same widgets.

Remove `zsh-autosuggestions` from `plugins=(...)`, or remove its `source`
line, then restart with `exec zsh`. Load deja through one activation method;
remove the separate activation block if a plugin manager loads it.

If `Tab` conflicts with native or fzf completion, move the alternatives picker
to another key. See [key bindings](configuration.md#key-bindings).

## Daemon recovery

To stop the current daemon and start a replacement:

```sh
deja daemon --restart
```

This command stays in the foreground. Use another terminal while it runs,
or press `Ctrl+C` to stop it and open a new shell to let deja start a daemon.

To stop the daemon without starting a foreground replacement:

```sh
pkill -f 'deja daemon'
```

Open a fresh shell afterward. Its integration starts the daemon.

After an upgrade, a daemon from the previous version can continue serving
requests. If it lacks the current socket protocol, the integration uses the
subprocess fallback. Replace that daemon to restore direct socket requests.

Older daemons without a pidfile cannot be stopped with `--restart`; use the
`pkill` command once, then start a new shell.

## Stale sockets

If a crash leaves a socket and no daemon is listening, remove the stale socket:

```sh
rm "$HOME/.local/share/deja/sock"
```

Open a new shell afterward. Stop any running daemon before manually removing
its socket. For a runtime fallback socket, use the `DEJA_SOCK` path from the
generated integration; see [data locations](privacy.md#data-locations).

## Reset the database

This deletes deja's recorded history, statistics, and sequences. First remove
any unwanted commands from the history file you intend to import. The importer
does not apply `HISTORY_IGNORE` to old entries.

Close other deja-enabled shells and perform the reset from a shell without
the integration loaded, such as `zsh -f`, so recording hooks do not write
commands during the reset:

```sh
pkill -f 'deja daemon'
rm -f "$HOME/.local/share/deja/deja.db" \
  "$HOME/.local/share/deja/deja.db-wal" \
  "$HOME/.local/share/deja/deja.db-shm"
deja import
```

Use `deja import --file /path/to/history` for a different history file.
Start a fresh shell afterward to reload deja.

The reset keeps your saved configuration and cached integration script.
For a full removal, follow [uninstall](installation.md#uninstall).
