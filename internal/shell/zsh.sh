# deja — predictive inline shell autosuggestions for zsh.
# every relevant ZLE widget is
# wrapped (not replaced), suggestions render via POSTDISPLAY + region_highlight,
# and fetches run asynchronously via `zle -F` so the keystroke path never blocks.
#
# The DEJA_BIN value below is substituted to an absolute path by `deja init`.

#--------------------------------------------------------------------#
# 1. Globals & config                                                #
#--------------------------------------------------------------------#

typeset -g DEJA_BIN="{{DEJA_BIN}}"

: ${DEJA_HIGHLIGHT_STYLE:=fg=8}
: ${DEJA_USE_ASYNC:=1}
: ${DEJA_MANUAL_REBIND:=}
: ${DEJA_BUFFER_MAX_SIZE:=}
: ${DEJA_ORIGINAL_WIDGET_PREFIX:=deja-orig-}

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
)

typeset -ga _DEJA_BUILTIN_ACTIONS
_DEJA_BUILTIN_ACTIONS=(clear fetch suggest accept execute enable disable toggle)

typeset -g DEJA_SESSION_ID
if [[ -z "$DEJA_SESSION_ID" ]]; then
	if [[ -r /dev/urandom ]] && (( $+commands[xxd] )); then
		DEJA_SESSION_ID="$(head -c 16 /dev/urandom | xxd -p 2>/dev/null | tr -d '\n')"
	else
		DEJA_SESSION_ID="$$-$RANDOM-$EPOCHSECONDS"
	fi
fi

typeset -g __deja_prev=""
typeset -g __deja_last_cmd=""
typeset -gi __deja_last_start=0

#--------------------------------------------------------------------#
# 2. Daemon auto-spawn                                               #
#--------------------------------------------------------------------#

_deja_ensure_daemon() {
	[[ -x "$DEJA_BIN" ]] || return 1

	# Fast path: ping succeeds, daemon is up.
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

_deja_highlight_reset() {
	typeset -g _DEJA_LAST_HIGHLIGHT

	if [[ -n "$_DEJA_LAST_HIGHLIGHT" ]]; then
		region_highlight=("${(@)region_highlight:#$_DEJA_LAST_HIGHLIGHT}")
		unset _DEJA_LAST_HIGHLIGHT
	fi
}

_deja_highlight_apply() {
	typeset -g _DEJA_LAST_HIGHLIGHT

	if (( $#POSTDISPLAY )); then
		typeset -g _DEJA_LAST_HIGHLIGHT="$#BUFFER $(($#BUFFER + $#POSTDISPLAY)) $DEJA_HIGHLIGHT_STYLE"
		region_highlight+=("$_DEJA_LAST_HIGHLIGHT")
	else
		unset _DEJA_LAST_HIGHLIGHT
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
	[[ -x "$DEJA_BIN" ]] || return 0
	suggestion="$("$DEJA_BIN" query --buffer "$buffer" --dir "$PWD" --prev "$__deja_prev" 2>/dev/null)"
}

_deja_async_request() {
	zmodload zsh/system 2>/dev/null

	typeset -g _DEJA_ASYNC_FD _DEJA_CHILD_PID

	# Cancel any pending request so stale responses can't paint over the buffer.
	if [[ -n "$_DEJA_ASYNC_FD" ]] && { true <&$_DEJA_ASYNC_FD } 2>/dev/null; then
		builtin exec {_DEJA_ASYNC_FD}<&-
		zle -F $_DEJA_ASYNC_FD

		if [[ -n "$_DEJA_CHILD_PID" ]]; then
			if [[ -o MONITOR ]]; then
				kill -TERM -$_DEJA_CHILD_PID 2>/dev/null
			else
				kill -TERM $_DEJA_CHILD_PID 2>/dev/null
			fi
		fi
	fi

	builtin exec {_DEJA_ASYNC_FD}< <(
		echo $sysparams[pid]
		"$DEJA_BIN" query --buffer "$1" --dir "$PWD" --prev "$__deja_prev" 2>/dev/null
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
		IFS='' read -rd '' -u $1 suggestion
		# Strip a trailing newline from the `fmt.Println` in query.go.
		suggestion="${suggestion%$'\n'}"
		zle deja-suggest -- "$suggestion"
		builtin exec {1}<&-
	fi

	zle -F "$1"
	_DEJA_ASYNC_FD=
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
	_deja_invoke_original_widget $@
}

_deja_modify() {
	local -i retval
	local -i KEYS_QUEUED_COUNT

	local orig_buffer="$BUFFER"
	local orig_postdisplay="$POSTDISPLAY"

	POSTDISPLAY=

	_deja_invoke_original_widget $@
	retval=$?

	emulate -L zsh

	# More keys queued — don't fetch yet, keep prior suggestion visible.
	if (( $PENDING > 0 || $KEYS_QUEUED_COUNT > 0 )); then
		POSTDISPLAY="$orig_postdisplay"
		return $retval
	fi

	# User is typing into the current suggestion. Just shrink POSTDISPLAY
	# rather than round-tripping for a fresh fetch.
	if [[ "$BUFFER" = "$orig_buffer"* && "$orig_postdisplay" = "${BUFFER:$#orig_buffer}"* ]]; then
		POSTDISPLAY="${orig_postdisplay:$(($#BUFFER - $#orig_buffer))}"
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

_deja_suggest() {
	emulate -L zsh

	local suggestion="$1"

	# Empty or disabled? Drop any previous ghost text.
	if [[ -z "$suggestion" ]] || (( ${+_DEJA_DISABLED} )); then
		POSTDISPLAY=
		return
	fi

	# Prefix match: paint the tail beyond what the user has already typed.
	# Fuzzy match: the daemon returned a command that doesn't start with
	# BUFFER — show it as the whole ghost text. Accepting still swaps it
	# into BUFFER wholesale, matching init.md's "⊳ replace whole buffer" UX.
	if (( $#BUFFER )) && [[ "$suggestion" = "$BUFFER"* ]]; then
		POSTDISPLAY="${suggestion#$BUFFER}"
	else
		POSTDISPLAY="$suggestion"
	fi
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

	# Fuzzy case: POSTDISPLAY doesn't start at the end of BUFFER as a
	# continuation. Swap the whole buffer for the suggestion.
	if [[ "$POSTDISPLAY" = "$BUFFER"* ]]; then
		BUFFER="$POSTDISPLAY"
	else
		BUFFER="$BUFFER$POSTDISPLAY"
	fi

	POSTDISPLAY=

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
	BUFFER="$BUFFER$POSTDISPLAY"
	POSTDISPLAY=
	_deja_invoke_original_widget "accept-line"
}

_deja_partial_accept() {
	local -i retval cursor_loc
	local original_buffer="$BUFFER"

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
			_deja_incr_bind_count $widget
			eval "zle -C $prefix$bind_count-${(q)widget} ${${(s.:.)widgets[$widget]}[2,3]}"
			;;
	esac

	# Pass the original widget name explicitly — $WIDGET can be wrong when
	# other plugins invoke us without `zle -w`.
	eval "_deja_bound_${bind_count}_${(q)widget}() {
		_deja_widget_$deja_action $prefix$bind_count-${(q)widget} \$@
	}"

	zle -N -- $widget _deja_bound_${bind_count}_$widget
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
	for action in $_DEJA_BUILTIN_ACTIONS modify partial_accept; do
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

_deja_preexec() {
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

		if [[ -x "$DEJA_BIN" ]]; then
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
	[[ -z "$DEJA_MANUAL_REBIND" ]] && _deja_bind_widgets
}

zmodload zsh/datetime 2>/dev/null  # For $EPOCHREALTIME

add-zsh-hook preexec _deja_preexec
add-zsh-hook precmd _deja_precmd

#--------------------------------------------------------------------#
# 8. Startup                                                         #
#--------------------------------------------------------------------#

_deja_ensure_daemon
_deja_bind_widgets

# User-facing key bindings.
# Right arrow: accept (forward-char is in DEJA_ACCEPT_WIDGETS, so the wrapped widget does the right thing).
# Ctrl+right: partial accept (forward-word is in DEJA_PARTIAL_ACCEPT_WIDGETS).
# Ctrl+X: toggle suppression.
bindkey '^X' deja-toggle
