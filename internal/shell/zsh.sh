# deja — predictive inline shell autosuggestions for zsh.
# every relevant ZLE widget is
# wrapped (not replaced), suggestions render via POSTDISPLAY + region_highlight,
# and fetches run asynchronously via `zle -F` so the keystroke path never blocks.
#
# The DEJA_BIN, DEJA_SOCK and DEJA_BIN_STAMP values below are substituted by
# `deja init`, which writes this file to ~/.local/share/deja/init.zsh.

#--------------------------------------------------------------------#
# 1. Globals & config                                                #
#--------------------------------------------------------------------#

typeset -g DEJA_BIN="{{DEJA_BIN}}"
typeset -g DEJA_SOCK="{{DEJA_SOCK}}"

# Stat identity (size-mtime-inode) of the binary that generated this file, so a
# shell can tell whether the cached script still matches the installed deja.
typeset -g _DEJA_BIN_STAMP="{{DEJA_BIN_STAMP}}"

# Every request to the daemon can travel one of two ways. When zsh can open the
# socket itself — zsh/net/socket is a standard module, present in stock zsh 5.9
# — a request costs a connect and a write. When it cannot, each request costs a
# fork+exec of the deja binary instead: ~30ms against ~0.3ms, on a path that runs
# on every keystroke. The subprocess path stays as a complete fallback, never as
# the default. See internal/daemon/text.go for the wire format.
typeset -gi _DEJA_ZSOCKET=0
if zmodload zsh/net/socket 2>/dev/null && (( $+builtins[zsocket] )); then
	_DEJA_ZSOCKET=1
fi

: ${DEJA_HIGHLIGHT_STYLE:=fg=8}
: ${DEJA_USE_ASYNC:=1}
: ${DEJA_MANUAL_REBIND:=}
: ${DEJA_BUFFER_MAX_SIZE:=}
: ${DEJA_ORIGINAL_WIDGET_PREFIX:=deja-orig-}
: ${DEJA_FUZZY_SEPARATOR:='  ⊳ '}
# DEJA_FUZZY (unset by default): override the fuzzy strictness preset for the
# daemon spawned by this shell. One of loose|smart|tight. Use `deja fuzzy` to
# change the persisted preset instead.
# DEJA_EMPTY (unset by default): override whether deja suggests on an empty
# prompt for the daemon spawned by this shell. on|off (aliases show|hide). Like
# DEJA_FUZZY it is read by the daemon at startup; use `deja empty` or Shift+↑ to
# change the persisted setting instead. Distinct from Ctrl+X, which suppresses
# all suggestions for this session only.
# DEJA_CYCLE_FUZZY_KEY / DEJA_CYCLE_FUZZY_BACK_KEY: zle key sequences bound to
# `deja fuzzy cycle` and `deja fuzzy back`. Default to Shift+→ / Shift+←
# (^[[1;2C / ^[[1;2D in xterm-style terminals). Set either to empty to disable
# that direction. In tmux you may need `set -g xterm-keys on` for the default
# Shift+arrow sequences to reach zle.
# DEJA_TOGGLE_EMPTY_KEY: zle key sequence bound to `deja empty toggle`. Defaults
# to Shift+↑ (^[[1;2A). Like the fuzzy keys it changes the *persisted, global*
# setting, not just this session.
# Use `=` (not `:=`) so an explicitly-empty value is preserved and leaves the key
# unbound — `:=` would overwrite empty with the default, defeating "disable".
: ${DEJA_CYCLE_FUZZY_KEY='^[[1;2C'}
: ${DEJA_CYCLE_FUZZY_BACK_KEY='^[[1;2D'}
: ${DEJA_TOGGLE_EMPTY_KEY='^[[1;2A'}
# Key sequences for the core suggestion bindings. Set any to empty to leave that
# key unbound (e.g. to hand Tab back to native completion). Values are zle key
# strings — use `bindkey -L` or `cat -v` / `read` to discover a key's sequence.
: ${DEJA_CYCLE_KEY='^I'}          # cycle alternative suggestions (Tab)
: ${DEJA_TOGGLE_KEY='^X'}         # toggle per-session suppression (Ctrl+X)
: ${DEJA_ACCEPT_KEY=}             # accept full suggestion (off by default; → already accepts)
: ${DEJA_WORD_ACCEPT_KEY=}        # accept next word only (off by default; Ctrl+→ already does this)
: ${DEJA_DISMISS_KEY=}            # dismiss/clear the current ghost without suppressing the session

typeset -ga DEJA_ACCEPT_WIDGETS
DEJA_ACCEPT_WIDGETS=(
	forward-char
	end-of-line
	vi-forward-char
	vi-end-of-line
	vi-add-eol
)

typeset -ga DEJA_PARTIAL_ACCEPT_WIDGETS
DEJA_PARTIAL_ACCEPT_WIDGETS=(
	forward-word
	emacs-forward-word
	vi-forward-word
	vi-forward-word-end
	vi-forward-blank-word
	vi-forward-blank-word-end
)

typeset -ga DEJA_CLEAR_WIDGETS
DEJA_CLEAR_WIDGETS=(
	history-search-forward
	history-search-backward
	history-beginning-search-forward
	history-beginning-search-backward
	history-substring-search-up
	history-substring-search-down
	up-line-or-beginning-search
	down-line-or-beginning-search
	up-line-or-history
	down-line-or-history
	accept-line
	copy-earlier-word
)

typeset -ga DEJA_IGNORE_WIDGETS
DEJA_IGNORE_WIDGETS=(
	orig-\*
	beep
	run-help
	set-local-history
	which-command
	yank
	yank-pop
	zle-\*
	expand-or-complete
	# add-zle-hook-widget preserves an incumbent hook widget by aliasing it to a
	# name like `user:_deja_line_init`, which matches none of the patterns above.
	# Wrapping such an alias would turn deja's own hooks into `modify` widgets.
	user:\*
)

typeset -ga _DEJA_BUILTIN_ACTIONS
_DEJA_BUILTIN_ACTIONS=(clear fetch suggest accept partial_accept execute enable disable toggle cycle cycle_fuzzy cycle_fuzzy_back toggle_empty)

zmodload zsh/datetime 2>/dev/null  # $EPOCHREALTIME, for session ids and timings

# A session id groups commands so consecutive pairs can be learned. It is not a
# secret and not a token: the only cost of a collision is two shells' sequence
# data being merged. It was drawing 128 bits from /dev/urandom through
# `head | xxd | tr` — three forked processes, measured at 6-13ms of every
# shell's startup, for entropy nothing here needs.
#
# pid plus microsecond timestamp is already unique in practice: two live shells
# cannot share a pid, and $RANDOM separates the case where a pid is recycled
# within the same microsecond. All three are shell builtins, so this forks
# nothing.
#
# Reading /dev/urandom with zsh/system's sysread avoids the forks too, and was
# tried first — but random bytes are not a valid character string. Across 300
# reads only 181 kept all 16 bytes; the rest silently lost one to three to NUL
# and multibyte handling, making the id's length depend on the locale.
typeset -g DEJA_SESSION_ID
if [[ -z "$DEJA_SESSION_ID" ]]; then
	DEJA_SESSION_ID="$$-${EPOCHREALTIME:-$SECONDS}-$RANDOM"
fi

typeset -g __deja_prev=""
typeset -g __deja_last_cmd=""
typeset -gi __deja_last_start=0

# Raw text of the currently-shown suggestion and the rendering mode that
# produced POSTDISPLAY. `prefix` means POSTDISPLAY is the tail beyond BUFFER;
# `fuzzy` means POSTDISPLAY is "${DEJA_FUZZY_SEPARATOR}${suggestion}" so accept
# must replace BUFFER wholesale instead of appending.
typeset -g _DEJA_CURRENT_SUGGESTION=""
typeset -g _DEJA_SUGGESTION_MODE=""

# Ranked candidates returned by the last fetch. Index 1 is the primary
# suggestion; subsequent entries are the daemon's alternatives (up to 4).
# Tab cycles through them.
typeset -ga _DEJA_ALTERNATIVES
typeset -gi _DEJA_ALT_INDEX=1

# True when another inline-suggestion engine (notably zsh-autosuggestions) is
# loaded. It wraps the same ZLE widgets and drives POSTDISPLAY too, so layering
# deja on top wedges the line editor. deja replaces such plugins rather than
# coexisting, so we stand down when one is present.
_deja_conflicting_plugin() {
	(( ${+functions[_zsh_autosuggest_start]} || ${+functions[_zsh_autosuggest_bind_widgets]} ))
}

_deja_warn_conflict() {
	(( ${+_DEJA_CONFLICT_WARNED} )) && return
	typeset -g _DEJA_CONFLICT_WARNED=1
	print -ru2 -- "deja: zsh-autosuggestions is active — deja replaces it and won't run alongside it. Remove zsh-autosuggestions from plugins=() (or remove the deja init line), then restart your shell. deja is standing down to keep your terminal usable."
}

#--------------------------------------------------------------------#
# 2. Daemon transport & auto-spawn                                   #
#--------------------------------------------------------------------#

# Opens a connection to the daemon, leaving the file descriptor in REPLY.
# Returns non-zero when zsh cannot open sockets or nothing is listening — both
# mean "use the subprocess path".
_deja_connect() {
	(( _DEJA_ZSOCKET )) || return 1
	[[ -n "$DEJA_SOCK" ]] || return 1
	zsocket "$DEJA_SOCK" 2>/dev/null
}

# Builds a request line in REPLY: the verb, then each field escaped and
# US-separated. The daemon frames requests by newline (zsh cannot half-close a
# socket to signal EOF), so a field may contain neither a raw newline nor a raw
# US. These three substitutions are the exact inverse of unescapeTextField in
# internal/daemon/text.go and must stay in lockstep with it — backslash first,
# or the escapes introduced below would themselves be re-escaped.
_deja_text_request() {
	local f out=$1
	shift
	for f in "$@"; do
		f=${f//\\/\\\\}
		f=${f//$'\n'/\\n}
		f=${f//$'\x1f'/\\s}
		out+=$'\x1f'$f
	done
	REPLY=$out
}

_deja_ensure_daemon() {
	[[ -x "$DEJA_BIN" ]] || return 1

	if (( _DEJA_ZSOCKET )); then
		local -i fd
		if _deja_connect; then
			fd=$REPLY

			# A daemon predating the text protocol accepts the connection and then
			# closes without answering. It cannot be upgraded in place: the wire
			# protocol has no "quit", and adding one would not reach precisely the
			# builds that need replacing. So this shell stands down to the
			# subprocess path — correct, merely slower — until the daemon is
			# replaced by a reboot or `deja daemon --restart`.
			local pong
			if print -u $fd -r -- ping 2>/dev/null; then
				IFS= read -rd '' -t 0.5 -u $fd pong 2>/dev/null
			fi
			exec {fd}<&-

			[[ "$pong" == pong ]] && return 0
			_DEJA_ZSOCKET=0
			return 0
		fi

		# Nothing listening. Spawn detached and return without waiting: a retry
		# loop here would be startup latency spent for nothing, since until the
		# daemon is up `query` falls back to reading SQLite directly.
		{ "$DEJA_BIN" daemon >/dev/null 2>&1 &! } 2>/dev/null
		return 0
	fi

	# No zsocket: fall back to a stat. A socket file means a daemon is almost
	# certainly up, so confirm it in the background rather than blocking the
	# shell on a ~25ms process launch. `-S` also passes for a socket a crashed
	# daemon left behind, which is what the backgrounded ping catches.
	if [[ -S "$DEJA_SOCK" ]]; then
		{ ( "$DEJA_BIN" ping >/dev/null 2>&1 || "$DEJA_BIN" daemon >/dev/null 2>&1 ) &! } 2>/dev/null
		return 0
	fi

	# No socket: a cold start, worth blocking for so the first keystroke does not
	# race the daemon. Ping first anyway, in case DEJA_SOCK is stale or the
	# daemon is listening somewhere this script does not know about.
	"$DEJA_BIN" ping >/dev/null 2>&1 && return 0

	# Spawn detached. `&!` disowns immediately so the daemon outlives this shell.
	{ "$DEJA_BIN" daemon >/dev/null 2>&1 &! } 2>/dev/null

	# Brief retry so the first keystroke doesn't race startup.
	local -i i
	for (( i = 0; i < 20; i++ )); do
		"$DEJA_BIN" ping >/dev/null 2>&1 && return 0
		sleep 0.01 2>/dev/null || break
	done
	return 1
}

#--------------------------------------------------------------------#
# 3. Highlighting                                                    #
#--------------------------------------------------------------------#

# deja's ghost is one region_highlight entry whose `end` points past the end of
# the ZLE line, into POSTDISPLAY. zsh renders that but does not track it: when a
# widget metafies the line, zlelineasstring() (Src/Zle/zle_utils.c) derives each
# entry's metafied offsets with a countdown bounded by the line length, so an
# `end` beyond it never gets a value and the return trip writes the
# uninitialized one back — "15 37 fg=8" comes back as "38 0". Zero width, so the
# ghost paints unstyled (default foreground, looks white), and the entry no
# longer equals the string we appended, so removing it by exact match leaks an
# orphan. Every completion:* widget metafies, and _deja_bind_widget deliberately
# leaves those unwrapped (`zle -N` over a `zle -C` widget wedges the line
# editor — #46, #47), so this has to be repaired rather than prevented.
#
# zsh 5.9 added a `memo=` field that survives that mutation untouched, so tag the
# entry and identify it by the tag. The trailing comma after the style
# terminates the style list, which is what makes 5.8 and older drop the unknown
# `memo=...` word instead of folding it into the style. Whether the tag stuck is
# learned by reading the entry back, not by a version test.
typeset -gi _DEJA_MEMO_SUPPORTED=-1   # -1 unknown, 1 supported, 0 not

_deja_highlight_reset() {
	typeset -g _DEJA_LAST_HIGHLIGHT

	if (( _DEJA_MEMO_SUPPORTED == 1 )); then
		# Collects orphans a metafying widget mangled, not just the live entry.
		region_highlight=("${(@)region_highlight:#*memo=deja*}")
	elif [[ -n "$_DEJA_LAST_HIGHLIGHT" ]]; then
		region_highlight=("${(@)region_highlight:#$_DEJA_LAST_HIGHLIGHT}")
	fi

	unset _DEJA_LAST_HIGHLIGHT
}

_deja_highlight_apply() {
	typeset -g _DEJA_LAST_HIGHLIGHT

	if (( ! $#POSTDISPLAY )); then
		unset _DEJA_LAST_HIGHLIGHT
		return
	fi

	region_highlight+=("$#BUFFER $(($#BUFFER + $#POSTDISPLAY)) ${DEJA_HIGHLIGHT_STYLE}, memo=deja")

	# Read it back: zsh reserializes the entry (the comma before `memo=` is gone;
	# on zsh < 5.9 the whole memo is gone). The stored form — not the string we
	# wrote — is what removal and _deja_line_pre_redraw must compare against.
	_DEJA_LAST_HIGHLIGHT="${region_highlight[-1]}"

	if (( _DEJA_MEMO_SUPPORTED < 0 )); then
		if [[ "$_DEJA_LAST_HIGHLIGHT" == *memo=deja* ]]; then
			_DEJA_MEMO_SUPPORTED=1
		else
			_DEJA_MEMO_SUPPORTED=0
		fi
	fi
}

#--------------------------------------------------------------------#
# 4. Suggestion fetch                                                #
#--------------------------------------------------------------------#

# Runs `deja query` and stores the result in the `suggestion` local that
# the caller declared. Silent on any error — an empty result means "no
# suggestion" and leaves POSTDISPLAY untouched upstream.
_deja_fetch_suggestion() {
	local buffer="$1"

	local -i fd
	if _deja_connect; then
		fd=$REPLY
		_deja_text_request suggest "$buffer" "$PWD" "$__deja_prev"
		if print -u $fd -r -- "$REPLY" 2>/dev/null; then
			# Bounded, because this is the synchronous path and a wedged daemon
			# must not freeze the line editor. The subprocess path applies the
			# same kind of ceiling internally (readTimeout in cmd/deja/query.go).
			# A timeout leaves `suggestion` empty, which reads as "no suggestion".
			IFS= read -rd '' -t 0.5 -u $fd suggestion 2>/dev/null
			exec {fd}<&-
			return 0
		fi
		exec {fd}<&-
	fi

	[[ -x "$DEJA_BIN" ]] || return 0
	suggestion="$("$DEJA_BIN" query --buffer "$buffer" --dir "$PWD" --prev "$__deja_prev" --format lines 2>/dev/null)"
}

# Drops any in-flight request so a stale response can't paint over the buffer.
_deja_async_cancel() {
	[[ -n "$_DEJA_ASYNC_FD" ]] || return 0

	if { true <&$_DEJA_ASYNC_FD } 2>/dev/null; then
		# Unregister before closing: a handler left armed on a closed descriptor
		# would read from whatever recycled the number.
		zle -F $_DEJA_ASYNC_FD 2>/dev/null
		builtin exec {_DEJA_ASYNC_FD}<&-

		# Only the subprocess path has a child to stop; over a socket, closing the
		# descriptor is the whole of cancellation.
		if [[ -n "$_DEJA_CHILD_PID" ]]; then
			if [[ -o MONITOR ]]; then
				kill -TERM -$_DEJA_CHILD_PID 2>/dev/null
			else
				kill -TERM $_DEJA_CHILD_PID 2>/dev/null
			fi
		fi
	fi

	_DEJA_ASYNC_FD=
	_DEJA_CHILD_PID=
}

_deja_async_request() {
	zmodload zsh/system 2>/dev/null

	typeset -g _DEJA_ASYNC_FD _DEJA_CHILD_PID

	_deja_async_cancel

	local -i fd
	if _deja_connect; then
		fd=$REPLY
		_deja_text_request suggest "$1" "$PWD" "$__deja_prev"
		if print -u $fd -r -- "$REPLY" 2>/dev/null; then
			_DEJA_ASYNC_FD=$fd
			_DEJA_CHILD_PID=
			# The daemon closes once it has written, so the same handler that
			# drains a process substitution drains this: read to EOF, paint.
			zle -F "$fd" _deja_async_response
			return
		fi
		exec {fd}<&-
	fi

	builtin exec {_DEJA_ASYNC_FD}< <(
		echo $sysparams[pid]
		"$DEJA_BIN" query --buffer "$1" --dir "$PWD" --prev "$__deja_prev" --format lines 2>/dev/null
	)

	# workaround for a Ctrl+C bug pre-5.8.
	autoload -Uz is-at-least
	is-at-least 5.8 || command true

	read _DEJA_CHILD_PID <&$_DEJA_ASYNC_FD

	zle -F "$_DEJA_ASYNC_FD" _deja_async_response
}

_deja_async_response() {
	emulate -L zsh

	local suggestion

	if [[ -z "$2" || "$2" == "hup" ]]; then
		# Bounded read. Over a process substitution EOF always arrives, because
		# the child exits; over a socket it arrives only if the daemon closes, so
		# an unbounded read here would let a wedged daemon freeze the line editor.
		# A timeout just leaves the ghost unpainted until the next keystroke.
		IFS='' read -rd '' -t 0.5 -u $1 suggestion 2>/dev/null
		zle deja-suggest -- "$suggestion"
		# Close only if the fd is still ours — another zle -F user may have
		# recycled the number. (Never `2>/dev/null` a bare exec: that makes
		# the redirect permanent for the whole shell.)
		{ true <&$1 } 2>/dev/null && builtin exec {1}<&-
	fi

	zle -F "$1" 2>/dev/null
	[[ "$1" == "$_DEJA_ASYNC_FD" ]] && _DEJA_ASYNC_FD=
}

#--------------------------------------------------------------------#
# 5. Widget actions                                                  #
#--------------------------------------------------------------------#

_deja_disable() {
	typeset -g _DEJA_DISABLED
	_deja_clear
}

_deja_enable() {
	unset _DEJA_DISABLED
	(( $#BUFFER )) && _deja_fetch
}

_deja_toggle() {
	if (( ${+_DEJA_DISABLED} )); then
		_deja_enable
	else
		_deja_disable
	fi
}

_deja_clear() {
	POSTDISPLAY=
	_DEJA_ALTERNATIVES=()
	_DEJA_ALT_INDEX=1
	_DEJA_CURRENT_SUGGESTION=""
	_DEJA_SUGGESTION_MODE=""
	_deja_invoke_original_widget $@
}

_deja_modify() {
	local -i retval
	local -i KEYS_QUEUED_COUNT

	local orig_buffer="$BUFFER"
	local orig_postdisplay="$POSTDISPLAY"
	local orig_mode="$_DEJA_SUGGESTION_MODE"
	local orig_suggestion="$_DEJA_CURRENT_SUGGESTION"

	POSTDISPLAY=
	# Stale alternatives indexed to the prior buffer must not survive an
	# edit. Drop the array so Tab can't cycle to an alternative that no
	# longer matches BUFFER; the fast path below re-fires _deja_fetch so
	# _deja_suggest repopulates it shortly after.
	_DEJA_ALTERNATIVES=()
	_DEJA_ALT_INDEX=1
	_DEJA_CURRENT_SUGGESTION=""
	_DEJA_SUGGESTION_MODE=""

	_deja_invoke_original_widget $@
	retval=$?

	emulate -L zsh

	# More keys queued — don't fetch yet, keep prior suggestion visible.
	if (( $PENDING > 0 || $KEYS_QUEUED_COUNT > 0 )); then
		POSTDISPLAY="$orig_postdisplay"
		# Keep the ghost's identity in step with the text we just put back.
		# _deja_accept reads the mode to decide whether to append POSTDISPLAY or
		# swap in the raw suggestion, and _deja_line_pre_redraw derives
		# POSTDISPLAY from both — leaving them cleared here would make → paste
		# the fuzzy separator into BUFFER, and would make the hook drop the ghost.
		_DEJA_SUGGESTION_MODE="$orig_mode"
		_DEJA_CURRENT_SUGGESTION="$orig_suggestion"
		return $retval
	fi

	# User is typing into the current suggestion. Just shrink POSTDISPLAY
	# rather than round-tripping for a fresh fetch. This path is only
	# reachable in prefix mode (user typed forward and their input still
	# matches the ghost tail), so restore the mode flag the clear above
	# wiped out.
	if [[ "$BUFFER" = "$orig_buffer"* && "$orig_postdisplay" = "${BUFFER:$#orig_buffer}"* ]]; then
		POSTDISPLAY="${orig_postdisplay:$(($#BUFFER - $#orig_buffer))}"
		_DEJA_SUGGESTION_MODE=prefix
		_DEJA_CURRENT_SUGGESTION="$BUFFER$POSTDISPLAY"
		# POSTDISPLAY is already painted, but alternatives were cleared
		# above and are still indexed to the prior buffer. Fire an async
		# fetch so Tab-cycle has candidates by the time the user reaches
		# it; the in-common-case identical primary suggestion means the
		# async response won't flicker the ghost.
		(( ${+_DEJA_DISABLED} )) || _deja_fetch
		return $retval
	fi

	(( ${+_DEJA_DISABLED} )) && return $retval

	if (( $#BUFFER > 0 )); then
		if [[ -z "$DEJA_BUFFER_MAX_SIZE" ]] || (( $#BUFFER <= $DEJA_BUFFER_MAX_SIZE )); then
			_deja_fetch
		fi
	fi

	return $retval
}

_deja_fetch() {
	if (( ${+DEJA_USE_ASYNC} )) && [[ -n "$DEJA_USE_ASYNC" && "$DEJA_USE_ASYNC" != "0" ]]; then
		_deja_async_request "$BUFFER"
	else
		local suggestion
		_deja_fetch_suggestion "$BUFFER"
		_deja_suggest "$suggestion"
	fi
}

# Prefix match: paint the tail beyond what the user has already typed.
# Fuzzy match: the daemon returned a command that doesn't start with
# BUFFER — prepend DEJA_FUZZY_SEPARATOR so the ghost reads as
# "buffer ⊳ suggestion" instead of colliding with the cursor. Accept
# swaps BUFFER for _DEJA_CURRENT_SUGGESTION (the raw command, no separator).
# Empty buffer (sequence prediction on a fresh prompt) uses the prefix
# branch so the separator doesn't leak into an otherwise-empty line.
_deja_render_suggestion() {
	local s="$1"
	_DEJA_CURRENT_SUGGESTION="$s"
	if [[ -z "$BUFFER" || "$s" = "$BUFFER"* ]]; then
		_DEJA_SUGGESTION_MODE=prefix
		POSTDISPLAY="${s#$BUFFER}"
	else
		_DEJA_SUGGESTION_MODE=fuzzy
		POSTDISPLAY="${DEJA_FUZZY_SEPARATOR}${s}"
	fi
}

_deja_suggest() {
	emulate -L zsh

	local raw="$1"

	# The daemon now emits one candidate per line (--format lines): the
	# primary suggestion first, then up to 4 alternatives.
	local sep=$'\x1f'
	_DEJA_ALTERNATIVES=("${(@ps:$sep:)raw}")
	_DEJA_ALT_INDEX=1

	local suggestion="${_DEJA_ALTERNATIVES[1]}"

	# Empty or disabled? Drop any previous ghost text and alternatives.
	if [[ -z "$suggestion" ]] || (( ${+_DEJA_DISABLED} )); then
		POSTDISPLAY=
		_DEJA_ALTERNATIVES=()
		_DEJA_CURRENT_SUGGESTION=""
		_DEJA_SUGGESTION_MODE=""
		return
	fi

	_deja_render_suggestion "$suggestion"
}

# Builds the picker-style status line: "deja: fuzzy   tight   *smart*   loose"
# with the currently-active preset wrapped in *…*. The order tight→smart→loose
# mirrors strictness left-to-right so Shift+→ visually moves rightward.
_deja_fuzzy_picker_line() {
	local current="$1" p out=""
	for p in tight smart loose; do
		if [[ "$p" = "$current" ]]; then
			out+="  *${p}*"
		else
			out+="   ${p} "
		fi
	done
	print -r -- "deja: fuzzy${out}"
}

# Step the persisted fuzzy preset one direction ($1 = "cycle" or "back") and
# repaint the ghost suggestion synchronously so the change is visible in the
# same frame — async would land the new ghost only after the next keystroke,
# making it feel like the keypress did nothing.
_deja_step_fuzzy() {
	[[ -x "$DEJA_BIN" ]] || return 0

	local direction="$1" new
	new="$("$DEJA_BIN" fuzzy "$direction" 2>/dev/null)"
	new="${new%$'\n'}"
	[[ -z "$new" ]] && return 0

	zle -M "$(_deja_fuzzy_picker_line "$new")"

	if (( ${+_DEJA_DISABLED} )); then
		return 0
	fi

	# Sync fetch on purpose: async would put the new ghost in place only
	# after zle returns to its read loop, so the user wouldn't see the
	# preset change reflected until another keystroke.
	local suggestion
	_deja_fetch_suggestion "$BUFFER"
	_deja_suggest "$suggestion"
}

_deja_cycle_fuzzy()      { _deja_step_fuzzy cycle; }
_deja_cycle_fuzzy_back() { _deja_step_fuzzy back; }

# Builds the picker-style status line: "deja: empty   *on*    off " with the
# active state wrapped in *…*, mirroring _deja_fuzzy_picker_line.
_deja_empty_picker_line() {
	local current="$1" v out=""
	for v in on off; do
		if [[ "$v" = "$current" ]]; then
			out+="  *${v}*"
		else
			out+="   ${v} "
		fi
	done
	print -r -- "deja: empty${out}"
}

# Flip whether deja suggests on an empty prompt (persisted, global — same scope
# as the fuzzy preset) and repaint synchronously so the ghost appears/disappears
# in the same frame as the keypress.
_deja_toggle_empty() {
	[[ -x "$DEJA_BIN" ]] || return 0

	local new
	new="$("$DEJA_BIN" empty toggle 2>/dev/null)"
	new="${new%$'\n'}"
	[[ -z "$new" ]] && return 0

	zle -M "$(_deja_empty_picker_line "$new")"

	if (( ${+_DEJA_DISABLED} )); then
		return 0
	fi

	# The setting only gates an empty buffer, so a non-empty line's ghost is
	# unaffected — skip the re-fetch rather than pay for a round trip.
	[[ -n "$BUFFER" ]] && return 0

	# Sync fetch on purpose: async would put the new ghost in place only after
	# zle returns to its read loop, so the user wouldn't see the setting change
	# reflected until another keystroke.
	local suggestion
	_deja_fetch_suggestion "$BUFFER"
	_deja_suggest "$suggestion"
}

_deja_cycle() {
	local -i n=${#_DEJA_ALTERNATIVES}
	local -i max_cursor_pos=$#BUFFER

	if [[ "$KEYMAP" = "vicmd" ]]; then
		max_cursor_pos=$((max_cursor_pos - 1))
	fi

	# Cursor not at end of buffer: Tab should mean normal completion at
	# that position, not cycle.
	if (( CURSOR != max_cursor_pos )); then
		_deja_invoke_original_widget expand-or-complete
		return
	fi

	# No ghost currently painted. If an async fetch is in flight the ghost
	# is about to land — swallow Tab silently so a completion listing
	# doesn't flash on top of the suggestion that's about to appear.
	# Otherwise fall through to normal completion.
	if (( $#POSTDISPLAY == 0 )); then
		[[ -n "$_DEJA_ASYNC_FD" ]] && return
		_deja_invoke_original_widget expand-or-complete
		return
	fi

	# Ghost is visible but daemon only returned one candidate. No-op
	# instead of falling through — showing a file listing here would
	# clobber the ghost the user is reading.
	(( n < 2 )) && return

	_DEJA_ALT_INDEX=$(( (_DEJA_ALT_INDEX % n) + 1 ))
	_deja_render_suggestion "${_DEJA_ALTERNATIVES[$_DEJA_ALT_INDEX]}"
}

_deja_accept() {
	local -i retval max_cursor_pos=$#BUFFER

	if [[ "$KEYMAP" = "vicmd" ]]; then
		max_cursor_pos=$((max_cursor_pos - 1))
	fi

	# Not at end of buffer, or no suggestion to accept: fall through to original.
	if (( $CURSOR != $max_cursor_pos || !$#POSTDISPLAY )); then
		_deja_invoke_original_widget $@
		return
	fi

	# Fuzzy: POSTDISPLAY is " ⊳ <command>"; replace BUFFER with the raw
	# suggestion we stashed at render time. Prefix: POSTDISPLAY is the tail
	# that continues BUFFER — append it.
	if [[ "$_DEJA_SUGGESTION_MODE" = fuzzy ]]; then
		BUFFER="$_DEJA_CURRENT_SUGGESTION"
	else
		BUFFER="$BUFFER$POSTDISPLAY"
	fi

	POSTDISPLAY=
	_DEJA_ALTERNATIVES=()
	_DEJA_ALT_INDEX=1
	_DEJA_CURRENT_SUGGESTION=""
	_DEJA_SUGGESTION_MODE=""

	_deja_invoke_original_widget $@
	retval=$?

	if [[ "$KEYMAP" = "vicmd" ]]; then
		CURSOR=$(($#BUFFER - 1))
	else
		CURSOR=$#BUFFER
	fi

	return $retval
}

_deja_execute() {
	if [[ "$_DEJA_SUGGESTION_MODE" = fuzzy ]]; then
		BUFFER="$_DEJA_CURRENT_SUGGESTION"
	else
		BUFFER="$BUFFER$POSTDISPLAY"
	fi
	POSTDISPLAY=
	_DEJA_ALTERNATIVES=()
	_DEJA_ALT_INDEX=1
	_DEJA_CURRENT_SUGGESTION=""
	_DEJA_SUGGESTION_MODE=""
	_deja_invoke_original_widget "accept-line"
}

_deja_partial_accept() {
	local -i retval cursor_loc
	local original_buffer="$BUFFER"

	# Fuzzy mode: "accept one word" is ambiguous because BUFFER is different
	# text from the suggestion. Fall through so Ctrl+Right just moves the
	# cursor within the user's real buffer; they can use → to take the whole
	# suggestion.
	if [[ "$_DEJA_SUGGESTION_MODE" = fuzzy ]]; then
		_deja_invoke_original_widget $@
		return $?
	fi

	# A partial accept commits part of the current suggestion to BUFFER.
	# The remaining POSTDISPLAY tail (if any) is still conceptually tied
	# to the primary suggestion; stale alternatives indexed to the old
	# buffer are no longer valid candidates to cycle to.
	#
	# _DEJA_CURRENT_SUGGESTION / _DEJA_SUGGESTION_MODE are deliberately kept:
	# this only moves the boundary between BUFFER and POSTDISPLAY, so
	# "$BUFFER$POSTDISPLAY" still equals the suggestion on both exits below.
	# Clearing them would make _deja_line_pre_redraw wipe the remaining tail.
	_DEJA_ALTERNATIVES=()
	_DEJA_ALT_INDEX=1

	BUFFER="$BUFFER$POSTDISPLAY"

	_deja_invoke_original_widget $@
	retval=$?

	cursor_loc=$CURSOR
	if [[ "$KEYMAP" = "vicmd" ]]; then
		cursor_loc=$((cursor_loc + 1))
	fi

	if (( $cursor_loc > $#original_buffer )); then
		POSTDISPLAY="${BUFFER[$(($cursor_loc + 1)),$#BUFFER]}"
		BUFFER="${BUFFER[1,$cursor_loc]}"
	else
		BUFFER="$original_buffer"
	fi

	return $retval
}

#--------------------------------------------------------------------#
# 6. Widget wrapping                                                 #
#--------------------------------------------------------------------#

_deja_invoke_original_widget() {
	(( $# )) || return 0

	local original_widget_name="$1"
	shift

	if (( ${+widgets[$original_widget_name]} )); then
		zle $original_widget_name -- $@
	fi
}

_deja_incr_bind_count() {
	typeset -gi bind_count=$((_DEJA_BIND_COUNTS[$1] + 1))
	_DEJA_BIND_COUNTS[$1]=$bind_count
}

_deja_bind_widget() {
	typeset -gA _DEJA_BIND_COUNTS

	local widget=$1
	local deja_action=$2
	local prefix=$DEJA_ORIGINAL_WIDGET_PREFIX
	local -i bind_count

	case $widgets[$widget] in
		# Already wrapped — reuse the existing orig reference.
		user:_deja_(bound|orig)_*)
			bind_count=$((_DEJA_BIND_COUNTS[$widget]))
			;;

		user:*)
			_deja_incr_bind_count $widget
			zle -N $prefix$bind_count-$widget ${widgets[$widget]#*:}
			;;

		builtin)
			_deja_incr_bind_count $widget
			eval "_deja_orig_${(q)widget}() { zle .${(q)widget} }"
			zle -N $prefix$bind_count-$widget _deja_orig_$widget
			;;

		completion:*)
			return
			;;
	esac

	# Pass the original widget name explicitly — $WIDGET can be wrong when
	# other plugins invoke us without `zle -w`.
	eval "_deja_bound_${bind_count}_${(q)widget}() {
		_deja_widget_$deja_action $prefix$bind_count-${(q)widget} \$@
	}"

	zle -N -- $widget _deja_bound_${bind_count}_$widget
}

# Set by _deja_bind_widgets to the widget count it last left behind, so the
# precmd re-bind can tell "nothing has changed" from "a plugin moved something".
typeset -gi _DEJA_BOUND_WIDGET_COUNT=-1

# True when the widget table still looks exactly as _deja_bind_widgets left it.
#
# _deja_bind_widgets runs on every precmd, not just at startup, because plugins
# and frameworks add and replace widgets after deja loads. It costs ~5.4ms —
# iterating ~600 widgets and pattern-matching each against the ignore list —
# which was the largest remaining per-prompt cost, paid before every prompt for
# a table that almost never changes.
#
# The count alone is too weak a test: the failure this re-binding exists for is
# a framework *replacing* a widget, which leaves the count identical. So also
# spot-check that our wrapper is still installed on self-insert, the widget
# every keystroke goes through and the one whose loss would be most visible.
_deja_widgets_unchanged() {
	(( _DEJA_BOUND_WIDGET_COUNT >= 0 )) || return 1
	(( ${#widgets} == _DEJA_BOUND_WIDGET_COUNT )) || return 1
	[[ ${widgets[self-insert]} == user:_deja_bound_* ]]
}

_deja_bind_widgets() {
	emulate -L zsh

	local widget
	local -a ignore_widgets

	ignore_widgets=(
		.\*
		_\*
		${_DEJA_BUILTIN_ACTIONS/#/deja-}
		$DEJA_ORIGINAL_WIDGET_PREFIX\*
		$DEJA_IGNORE_WIDGETS
	)

	for widget in ${${(f)"$(builtin zle -la)"}:#${(j:|:)~ignore_widgets}}; do
		if [[ -n ${DEJA_CLEAR_WIDGETS[(r)$widget]} ]]; then
			_deja_bind_widget $widget clear
		elif [[ -n ${DEJA_ACCEPT_WIDGETS[(r)$widget]} ]]; then
			_deja_bind_widget $widget accept
		elif [[ -n ${DEJA_PARTIAL_ACCEPT_WIDGETS[(r)$widget]} ]]; then
			_deja_bind_widget $widget partial_accept
		else
			# Default: any unclassified widget is assumed to modify the buffer.
			_deja_bind_widget $widget modify
		fi
	done
}

# Generate `_deja_widget_<action>` wrappers: highlight_reset → action → highlight_apply → zle -R.
() {
	local action
	for action in $_DEJA_BUILTIN_ACTIONS modify; do
		eval "_deja_widget_$action() {
			local -i retval

			_deja_highlight_reset

			_deja_$action \$@
			retval=\$?

			_deja_highlight_apply

			zle -R

			return \$retval
		}"
	done

	for action in $_DEJA_BUILTIN_ACTIONS; do
		zle -N deja-$action _deja_widget_$action
	done
}

#--------------------------------------------------------------------#
# 7. Hooks: record executed commands, re-bind on precmd              #
#--------------------------------------------------------------------#

autoload -Uz add-zsh-hook

# zsh's history-ignore rules, reimplemented. Users rely on a leading space
# (HIST_IGNORE_SPACE) and on HISTORY_IGNORE as privacy gestures, so deja must not
# record what zsh itself refuses to remember. zshaddhistory can't be used here —
# it fires for ignored lines too, because zsh applies the ignore rules after the
# hook — but preexec's $1 is the line as typed, with leading whitespace intact.
# [[:space:]] matches space and tab, which is what zsh treats as ignorable.
#
# Doing this in the shell, rather than only in Go, is what keeps the secret out
# of `deja record`'s argv — and therefore out of `ps aux`.
_deja_history_ignored() {
	[[ -o hist_ignore_space && $1 == [[:space:]]* ]] && return 0
	# ${~HISTORY_IGNORE} forces glob expansion of the pattern; zsh matches it
	# against the whole line.
	[[ -n $HISTORY_IGNORE && $1 == ${~HISTORY_IGNORE} ]] && return 0
	return 1
}

_deja_preexec() {
	if _deja_history_ignored "$1"; then
		__deja_last_cmd=""
		__deja_last_start=0
		# Break the sequence chain too: leaving __deja_prev set would make the
		# ignored command the `--prev` of the next one and leak it verbatim into
		# the `sequences` table. Clearing it (rather than leaving the older
		# command in place) also avoids inventing an adjacency that never
		# happened.
		__deja_prev=""
		return
	fi
	__deja_last_cmd="$1"
	__deja_last_start=$EPOCHREALTIME
}

_deja_precmd() {
	local -i exit_code=$?

	if [[ -n "$__deja_last_cmd" ]]; then
		local -i duration_ms=0
		if (( __deja_last_start > 0 )); then
			# EPOCHREALTIME is a float; integer-typed duration_ms truncates on assignment.
			duration_ms=$(( (EPOCHREALTIME - __deja_last_start) * 1000 ))
			(( duration_ms < 0 )) && duration_ms=0
		fi

		local -i recorded=0 fd
		if _deja_connect; then
			fd=$REPLY
			_deja_text_request record "$__deja_last_cmd" "$PWD" "$exit_code" \
				"$duration_ms" "$DEJA_SESSION_ID" "$__deja_prev"
			# Fire and forget. Reading the ack would mean waiting on the daemon's
			# SQLite write, and a busy database must never stall the prompt — while
			# the only failure the ack could report (a write error) would hit the
			# subprocess fallback just as hard, since it writes to the same file.
			# A dead daemon is caught earlier, by the connect.
			print -u $fd -r -- "$REPLY" 2>/dev/null && recorded=1
			exec {fd}<&-
		fi

		if (( ! recorded )) && [[ -x "$DEJA_BIN" ]]; then
			{ "$DEJA_BIN" record \
				--command "$__deja_last_cmd" \
				--dir "$PWD" \
				--exit "$exit_code" \
				--duration "$duration_ms" \
				--session "$DEJA_SESSION_ID" \
				--prev "$__deja_prev" >/dev/null 2>&1 &! } 2>/dev/null
		fi

		__deja_prev="$__deja_last_cmd"
	fi

	__deja_last_cmd=""
	__deja_last_start=0

	# Re-bind so widgets added by other plugins after our init still get wrapped.
	# Keybindings are re-asserted too — frameworks (oh-my-zsh, prezto, etc.)
	# frequently rebind Tab during their own precmd, and deja's widgets
	# become unreachable without this.
	if [[ -z "$DEJA_MANUAL_REBIND" ]] && ! _deja_conflicting_plugin && [[ -x "$DEJA_BIN" ]]; then
		# Skipped when the widget table is untouched since the last bind, which is
		# the overwhelmingly common case; see _deja_widgets_unchanged.
		_deja_widgets_unchanged || _deja_bind_widgets

		# Keybindings are re-asserted unconditionally: eight bindkey calls, too
		# cheap to be worth guarding, and frameworks rebind Tab during their own
		# precmd — which leaves the widget table identical while making deja's
		# widgets unreachable, so the check above would not catch it.
		_deja_apply_keybindings

		# Same reasoning for the redraw hook: a plugin that takes over
		# zle-line-pre-redraw with a bare `zle -N` drops us off the chain.
		_deja_install_redraw_hook

		# Recorded after the whole sequence, not inside _deja_bind_widgets: the
		# redraw hook installs widgets of its own, so a count taken mid-sequence
		# never matches what the next prompt sees, and every prompt would re-bind.
		_DEJA_BOUND_WIDGET_COUNT=${#widgets}
	fi
}

add-zsh-hook preexec _deja_preexec
add-zsh-hook precmd _deja_precmd

#--------------------------------------------------------------------#
# 7b. zle-line-init: fetch on fresh prompts so sequence prediction   #
#     paints ghost text before the first keystroke.                  #
#--------------------------------------------------------------------#

# Chain any pre-existing zle-line-init so frameworks that define one keep working.
# Skip if deja is already installed (re-sourcing init must not chain ourselves
# as the "original", which would infinite-recurse on the next prompt).
if (( ${+widgets[zle-line-init]} )); then
	case $widgets[zle-line-init] in
		user:_deja_*) ;;
		user:*) zle -N _deja_orig_line_init ${widgets[zle-line-init]#*:} ;;
		builtin) zle -N _deja_orig_line_init .zle-line-init ;;
	esac
fi

_deja_line_init() {
	# Delegate to any prior zle-line-init first (framework compatibility).
	(( ${+widgets[_deja_orig_line_init]} )) && zle _deja_orig_line_init -- "$@"

	# Only act on a genuinely empty buffer, when we have a previous command
	# to seed sequence prediction, and suggestions aren't suppressed.
	[[ -n "$BUFFER" ]] && return
	[[ -z "$__deja_prev" ]] && return
	(( ${+_DEJA_DISABLED} )) && return

	_deja_widget_fetch
}

if ! _deja_conflicting_plugin && [[ -x "$DEJA_BIN" ]]; then
	zle -N zle-line-init _deja_line_init
fi

#--------------------------------------------------------------------#
# 7c. zle-line-pre-redraw: keep the ghost consistent with the buffer #
#--------------------------------------------------------------------#

# POSTDISPLAY is derived state: the tail of _DEJA_CURRENT_SUGGESTION beyond
# BUFFER (prefix mode), or the separator plus the whole suggestion (fuzzy mode,
# where it is deliberately not a continuation of BUFFER). Widgets deja does not
# wrap — every completion widget, incremental search, expand-history, anything a
# user binds with `zle -C` — rewrite BUFFER without telling deja, which leaves
# the ghost reading as garbage ("git checkout zsh-deja-autosuggestions
# h-deja-autosuggestions") and its highlight offsets mangled. zsh calls this hook
# once per redraw, after the widget has run and before the screen is painted, so
# recomputing the derived state here repairs both without wrapping a single
# completion widget.
#
# This cannot recurse: redrawhook() (Src/Zle/zle_main.c) runs from zlecore(),
# zleread(), recursiveedit() and compresult.c's do_single(), never from
# zrefresh(), so assigning POSTDISPLAY or region_highlight here does not schedule
# another hook run. The body is idempotent, so it needs no one-shot guard — and
# it must stay that way, which is why it calls no `zle` widget, not even
# `zle -R`; the refresh that follows this hook picks the changes up.
_deja_line_pre_redraw() {
	emulate -L zsh

	local want=""
	case "$_DEJA_SUGGESTION_MODE" in
		prefix)
			if [[ -z "$BUFFER" || "$_DEJA_CURRENT_SUGGESTION" = "$BUFFER"* ]]; then
				want="${_DEJA_CURRENT_SUGGESTION#$BUFFER}"
			fi
			;;
		fuzzy)
			want="${DEJA_FUZZY_SEPARATOR}${_DEJA_CURRENT_SUGGESTION}"
			;;
	esac

	if [[ "$POSTDISPLAY" != "$want" ]]; then
		# Either something outside deja moved BUFFER out from under the ghost
		# (re-anchor it, or drop it if the suggestion no longer continues the
		# line) or the ghost was orphaned outright.
		POSTDISPLAY="$want"
		if [[ -z "$want" ]]; then
			_DEJA_ALTERNATIVES=()
			_DEJA_ALT_INDEX=1
			_DEJA_CURRENT_SUGGESTION=""
			_DEJA_SUGGESTION_MODE=""
		fi
	elif (( _DEJA_MEMO_SUPPORTED == 1 )); then
		# Nothing moved. region_highlight is a special array — assigning it
		# reparses every entry — so skip the rebuild unless zsh mangled ours or
		# an orphan is present. A metafying widget that leaves BUFFER alone
		# (list-choices, ^D, a no-match completion) still mutates the entry, so
		# the byte comparison fails and the repair below runs anyway.
		local -a mine
		mine=("${(@M)region_highlight:#*memo=deja*}")
		if (( $#mine <= 1 )) && [[ "$mine[1]" == "$_DEJA_LAST_HIGHLIGHT" ]]; then
			return 0
		fi
	fi

	_deja_highlight_reset
	_deja_highlight_apply

	# add-zle-hook-widget stops calling the rest of the chain as soon as one hook
	# returns non-zero, so never leak a status.
	return 0
}

# add-zle-hook-widget (zsh 5.3+) shares zle-line-pre-redraw instead of owning it,
# which matters because zsh-syntax-highlighting registers the same hook. On 5.9+
# z-sy-h rebuilds with
# `region_highlight=( ${(@)region_highlight:#*memo=zsh-syntax-highlighting*} )`,
# so it preserves deja's memo'd entry and the order the two hooks run in does not
# matter. Registering twice is a no-op, so re-sourcing deja's init stays safe
# (unlike the `zle -N` chaining zle-line-init needs). Where the function is
# unavailable (pre-5.3, trimmed fpath) deja keeps its old behavior: the ghost is
# painted, just not repaired after an unwrapped widget rewrites the line.
_deja_install_redraw_hook() {
	local -a azhw
	azhw=(${^fpath}/add-zle-hook-widget(N))
	(( $#azhw || ${+functions[add-zle-hook-widget]} )) || return 1

	autoload -Uz add-zle-hook-widget
	add-zle-hook-widget zle-line-pre-redraw _deja_line_pre_redraw
}

#--------------------------------------------------------------------#
# 8. Startup                                                         #
#--------------------------------------------------------------------#

# User-facing key bindings. Each key is configurable via the DEJA_*_KEY env vars
# declared above; set one to empty to leave that key unbound. Right arrow and
# Ctrl+right always accept / word-accept via DEJA_ACCEPT_WIDGETS and
# DEJA_PARTIAL_ACCEPT_WIDGETS regardless of the dedicated keys below.
# DEJA_CYCLE_KEY       (Tab):    cycle alternatives (falls through to expand-or-complete when none).
# DEJA_TOGGLE_KEY      (Ctrl+X): toggle per-session suppression.
# DEJA_ACCEPT_KEY      (unset):  accept the full suggestion.
# DEJA_WORD_ACCEPT_KEY (unset):  accept the next word only.
# DEJA_DISMISS_KEY     (unset):  clear the current ghost for this line (does not suppress the session).
# DEJA_CYCLE_FUZZY_KEY      (Shift+right): cycle the persisted fuzzy preset forward (tight→smart→loose→tight).
# DEJA_CYCLE_FUZZY_BACK_KEY (Shift+left):  cycle backward (loose→smart→tight→loose).
# DEJA_TOGGLE_EMPTY_KEY     (Shift+up):    flip the persisted empty-prompt suggestion setting (on↔off).
_deja_apply_keybindings() {
	[[ -n "$DEJA_CYCLE_KEY" ]]            && bindkey "$DEJA_CYCLE_KEY"            deja-cycle
	[[ -n "$DEJA_TOGGLE_KEY" ]]           && bindkey "$DEJA_TOGGLE_KEY"           deja-toggle
	[[ -n "$DEJA_ACCEPT_KEY" ]]           && bindkey "$DEJA_ACCEPT_KEY"           deja-accept
	[[ -n "$DEJA_WORD_ACCEPT_KEY" ]]      && bindkey "$DEJA_WORD_ACCEPT_KEY"      deja-partial_accept
	[[ -n "$DEJA_DISMISS_KEY" ]]          && bindkey "$DEJA_DISMISS_KEY"          deja-clear
	[[ -n "$DEJA_CYCLE_FUZZY_KEY" ]]      && bindkey "$DEJA_CYCLE_FUZZY_KEY"      deja-cycle_fuzzy
	[[ -n "$DEJA_CYCLE_FUZZY_BACK_KEY" ]] && bindkey "$DEJA_CYCLE_FUZZY_BACK_KEY" deja-cycle_fuzzy_back
	[[ -n "$DEJA_TOGGLE_EMPTY_KEY" ]]     && bindkey "$DEJA_TOGGLE_EMPTY_KEY"     deja-toggle_empty
}

# Regenerate this file when the installed binary is no longer the one that
# produced it — after a `brew upgrade`, say, which would otherwise leave every
# shell sourcing the previous release's integration indefinitely.
#
# This is why deja can document `source ~/.local/share/deja/init.zsh` as the
# supported fast path instead of `eval "$(deja init zsh)"`. That eval spends a
# full binary launch, ~25-36ms on every shell, regenerating a file that is
# almost always byte-identical. Here the check is a stat and a string compare:
# 0.083ms, no fork.
#
# The regeneration itself is disowned and its result is deliberately not used by
# this shell — that would put the launch back on the startup path, which is the
# whole thing being avoided. The next shell picks it up. `deja init` installs the
# file with a rename, so a shell sourcing it concurrently never sees a partial
# write.
_deja_refresh_init_script() {
	[[ -n "$_DEJA_BIN_STAMP" ]] || return 0
	[[ -x "$DEJA_BIN" ]] || return 0
	zmodload zsh/stat 2>/dev/null || return 0
	(( $+builtins[zstat] )) || return 0

	local -A st
	zstat -H st "$DEJA_BIN" 2>/dev/null || return 0
	[[ "${st[size]}-${st[mtime]}-${st[inode]}" == "$_DEJA_BIN_STAMP" ]] && return 0

	{ "$DEJA_BIN" init zsh >/dev/null 2>&1 &! } 2>/dev/null
}

_deja_refresh_init_script
_deja_ensure_daemon

# Don't wrap widgets or grab keybindings when another suggestion engine owns
# them — that's what wedges the terminal. Warn once and leave the shell alone.
if _deja_conflicting_plugin; then
	_deja_warn_conflict
elif [[ -x "$DEJA_BIN" ]]; then
	_deja_bind_widgets
	_deja_apply_keybindings
	_deja_install_redraw_hook
	_DEJA_BOUND_WIDGET_COUNT=${#widgets}
fi
