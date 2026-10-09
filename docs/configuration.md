# Configuration

[Back to the README](../README.md#configuration)

[Key bindings](#key-bindings) · [Appearance](#ghost-text-appearance) ·
[Fuzzy matching](#fuzzy-matching) · [Empty prompts](#empty-prompts) ·
[Setting scope](#setting-scope)

Place shell variables above the lines that load deja in your `.zshrc`.
For plugin managers, place them before the manager loads the deja plugin.

## Key bindings

| Default key | Action | Variable |
| --- | --- | --- |
| `Right` | Accept the full suggestion | `DEJA_ACCEPT_KEY` adds a dedicated key |
| `Ctrl+Right` | Accept the next word | `DEJA_WORD_ACCEPT_KEY` adds a dedicated key |
| `Tab` | Cycle alternatives | `DEJA_CYCLE_KEY` |
| `Ctrl+X` | Toggle suggestions for this session | `DEJA_TOGGLE_KEY` |
| `Shift+Right` | Cycle fuzzy presets forward | `DEJA_CYCLE_FUZZY_KEY` |
| `Shift+Left` | Cycle fuzzy presets backward | `DEJA_CYCLE_FUZZY_BACK_KEY` |
| `Shift+Up` | Toggle empty-prompt suggestions | `DEJA_TOGGLE_EMPTY_KEY` |
| Unbound | Dismiss the suggestion on this line | `DEJA_DISMISS_KEY` |

deja accepts ZLE key sequences. For example, `^I` means `Tab`, `^X` means
`Ctrl+X`, and `^[[1;2C` means `Shift+Right` on xterm-style terminals.
Use `bindkey -L` or type a key into `cat -v` to inspect its sequence.

These are the defaults:

```zsh
export DEJA_CYCLE_KEY='^I'
export DEJA_TOGGLE_KEY='^X'
export DEJA_CYCLE_FUZZY_KEY='^[[1;2C'
export DEJA_CYCLE_FUZZY_BACK_KEY='^[[1;2D'
export DEJA_TOGGLE_EMPTY_KEY='^[[1;2A'
export DEJA_ACCEPT_KEY=
export DEJA_WORD_ACCEPT_KEY=
export DEJA_DISMISS_KEY=
```

Set a variable to empty to leave that dedicated binding unbound. The `Right`
and `Ctrl+Right` defaults come from wrapped movement widgets; emptying
`DEJA_ACCEPT_KEY` or `DEJA_WORD_ACCEPT_KEY` does not disable those widgets.

To accept with `Tab` instead of cycling alternatives:

```zsh
export DEJA_ACCEPT_KEY='^I' DEJA_CYCLE_KEY=
```

To leave `Tab` for native or fzf completion, move the picker to `Ctrl+N`:

```zsh
export DEJA_CYCLE_KEY='^N'
```

To dismiss the ghost text only on the current line:

```zsh
export DEJA_DISMISS_KEY='^G'
```

`Ctrl+X` suppresses suggestions until you toggle them back on or start a new
shell. Dismiss clears the current suggestion without turning suggestions off.

Avoid binding bare `Esc` (`^[`) to dismiss. Arrow keys, function keys, and
vi-mode use that prefix, so the binding can interfere with them.

In tmux, you may need `set -g xterm-keys on` to pass the default Shift-arrow
sequences through to zsh.

## Ghost text appearance

The default style is `fg=8`, ANSI bright black, which most themes display as
dim grey. Use a brighter colour if the suggestion is hard to read:

```zsh
export DEJA_HIGHLIGHT_STYLE='fg=cyan'
# Load deja below this line.
```

deja uses zsh's `region_highlight` style syntax, also used by
`ZSH_AUTOSUGGEST_HIGHLIGHT_STYLE` in zsh-autosuggestions.

| Style | Effect |
| --- | --- |
| `fg=8` | Default dim grey |
| `fg=cyan` | Named colour |
| `fg=244` | Colour from the 256-colour palette |
| `fg=cyan,bg=white` | Foreground and background colours |
| `fg=cyan,bold,underline` | Colour and text attributes |

`standout` and `blink` are also supported by zsh. If your theme makes the
default too faint, try `fg=244` or `fg=8,bold`.

Set the variable before the integration loads so the initial suggestion uses
your chosen style.

## Fuzzy matching

The matcher preserves the order of your typed letters and limits the gaps
between them. Commands must already exist in your history.

| Preset | Maximum gap between matched letters | Example input and candidate |
| --- | --- | --- |
| `tight` | 1 character | `gco` matches `g.co` |
| `smart` (default) | 4 characters | `gco` matches `git checkout main` |
| `loose` | 8 characters | `gco` matches `git checkout -- README` |

```sh
deja fuzzy           # show the preset and examples
deja fuzzy tight     # set a preset
deja fuzzy cycle     # tight, smart, loose, then tight again
deja fuzzy back      # loose, smart, tight, then loose again
```

`Shift+Right` cycles forward; `Shift+Left` cycles backward. The integration
refreshes the current suggestion and displays the selected preset:

```text
deja: fuzzy    tight    *smart*    loose
```

The commands and shortcuts update the running daemon and save the choice in
`~/.local/share/deja/config`.

To override the saved preset when a daemon starts:

```zsh
export DEJA_FUZZY=smart
```

See [setting scope](#setting-scope) before using environment overrides.
For prefix restrictions, see [matching and anchoring](architecture.md#matching-and-anchoring).

## Empty prompts

By default, deja suggests a command before you type, using command sequences,
frequency and recency, and your current directory.

```sh
deja empty            # show the current state
deja empty off        # hide suggestions on an empty prompt; alias: hide
deja empty on         # show them; alias: show
deja empty toggle     # switch the setting and print on or off
```

`Shift+Up` toggles the same setting and shows a confirmation:

```text
deja: empty   *on*    off
```

Changes update the running daemon and persist in `~/.local/share/deja/config`.
To override the saved choice when a daemon starts:

```zsh
export DEJA_EMPTY=off
```

## Setting scope

| Setting | Scope | When it applies |
| --- | --- | --- |
| Key bindings and highlight style | Current shell | Configure before loading the integration |
| `Ctrl+X` suppression | Current shell session | Until toggled again or a new shell starts |
| Dismiss | Current command line | Clears the current ghost text |
| `deja fuzzy` and `deja empty`, including their shortcuts | Shared daemon and saved configuration | Immediately, and across daemon restarts |
| `DEJA_FUZZY` and `DEJA_EMPTY` | Daemon started with those variables | At daemon startup; valid values override saved settings |

Changing an environment variable in one shell does not reconfigure an already
running daemon. A daemon started with those variables serves the same settings
to all connected shells. Use the CLI commands for changes to a running daemon.
