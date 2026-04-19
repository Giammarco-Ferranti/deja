# deja — zsh integration
# eval "$(deja init zsh)" in your .zshrc

__deja_bin="{{DEJA_BIN}}"

zmodload zsh/datetime

# --- session id ---
if command -v xxd >/dev/null 2>&1; then
  __deja_session="$(head -c 16 /dev/urandom | xxd -p)"
else
  __deja_session="${RANDOM}${RANDOM}${RANDOM}"
fi

# --- spawn daemon if not running ---
"$__deja_bin" ping >/dev/null 2>&1 || ("$__deja_bin" daemon </dev/null >/dev/null 2>&1 &!)

# --- state ---
__deja_prev=""
__deja_cmd=""
__deja_cmd_start=""
__deja_last_dir="$PWD"
__deja_last_exit=0
__deja_alts=()
__deja_alt_idx=0
__deja_last_hl=""

# --- highlight (zle-line-pre-redraw) ---
# Recompute the ghost-text highlight right before every display refresh.
# This is robust against other plugins (e.g. zsh-syntax-highlighting) that
# rebuild region_highlight from scratch.
if (( ${+functions[zle-line-pre-redraw]} )); then
  functions[__deja_orig_pre_redraw]="$functions[zle-line-pre-redraw]"
fi

zle-line-pre-redraw() {
  (( ${+functions[__deja_orig_pre_redraw]} )) && __deja_orig_pre_redraw "$@"

  # Remove stale deja highlight entry
  if [[ -n "$__deja_last_hl" ]]; then
    region_highlight=("${(@)region_highlight:#$__deja_last_hl}")
    __deja_last_hl=""
  fi
  # Apply fresh highlight if ghost text is showing
  if [[ -n "$POSTDISPLAY" ]]; then
    __deja_last_hl="${#BUFFER} $(( ${#BUFFER} + ${#POSTDISPLAY} )) fg=8"
    region_highlight+=("$__deja_last_hl")
  fi
}
zle -N zle-line-pre-redraw

# --- alternatives state reset ---
__deja_reset_alts() {
  __deja_alts=()
  __deja_alt_idx=0
}

# --- suggestion helper (called after every keystroke) ---
__deja_suggest() {
  local suggestion
  suggestion="$("$__deja_bin" query --buffer "$BUFFER" --dir "$PWD" --prev "$__deja_prev" 2>/dev/null)"

  if [[ -z "$suggestion" ]]; then
    unset POSTDISPLAY
    return
  fi

  # If the suggestion starts with what the user typed, show only the suffix.
  if [[ "$suggestion" == "$BUFFER"* ]]; then
    POSTDISPLAY="${suggestion#$BUFFER}"
  elif [[ ${#BUFFER} -ge 2 ]]; then
    # Only show fuzzy (full-replacement) suggestions for 2+ chars typed.
    POSTDISPLAY=" → $suggestion"
  else
    unset POSTDISPLAY
    return
  fi
}

# --- widget wrappers ---
__deja_self_insert() {
  zle .self-insert
  __deja_reset_alts
  __deja_suggest
}

__deja_backward_delete_char() {
  zle .backward-delete-char
  __deja_reset_alts
  __deja_suggest
}

__deja_line_init() {
  __deja_suggest
}

zle -N zle-line-init __deja_line_init
zle -N self-insert __deja_self_insert
zle -N backward-delete-char __deja_backward_delete_char

# --- accept suggestion (right arrow) ---
__deja_accept() {
  __deja_reset_alts
  if [[ -n "$POSTDISPLAY" ]]; then
    if [[ "$POSTDISPLAY" == " → "* ]]; then
      BUFFER="${POSTDISPLAY#" → "}"
    else
      BUFFER="${BUFFER}${POSTDISPLAY}"
    fi
    unset POSTDISPLAY
    CURSOR=${#BUFFER}
    zle -R
  else
    zle .forward-char
  fi
}

zle -N __deja_accept
bindkey '^[[C' __deja_accept   # normal mode
bindkey '^[OC' __deja_accept   # application mode

# --- tab: cycle alternatives ---
__deja_tab() {
  # No suggestion visible — fall back to normal completion
  if [[ -z "$POSTDISPLAY" && ${#__deja_alts[@]} -eq 0 ]]; then
    zle expand-or-complete
    return
  fi

  # First Tab press — fetch alternatives from the daemon
  if [[ ${#__deja_alts[@]} -eq 0 ]]; then
    local json
    json="$("$__deja_bin" query --json --buffer "$BUFFER" --dir "$PWD" --prev "$__deja_prev" 2>/dev/null)"
    if [[ -z "$json" ]]; then
      zle expand-or-complete
      return
    fi

    # Parse alternatives from JSON using zsh parameter expansion.
    # Disable globbing so patterns like *"..." don't expand.
    setopt localoptions noglob

    local raw=${json##*\"alternatives\":\[}
    raw=${raw%%\]*}
    if [[ -z "$raw" ]]; then
      zle expand-or-complete
      return
    fi

    __deja_alts=()
    local entry
    while [[ -n "$raw" ]]; do
      # Trim leading comma/whitespace
      raw="${raw#,}"
      raw="${raw## }"
      # Extract quoted string
      if [[ "$raw" == \"* ]]; then
        raw="${raw#\"}"
        entry="${raw%%\"*}"
        raw="${raw#*\"}"
        [[ -n "$entry" ]] && __deja_alts+=("$entry")
      else
        break
      fi
    done

    if [[ ${#__deja_alts[@]} -eq 0 ]]; then
      zle expand-or-complete
      return
    fi

    __deja_alt_idx=1
  else
    # Subsequent presses — cycle through
    (( __deja_alt_idx++ ))
    if (( __deja_alt_idx > ${#__deja_alts[@]} )); then
      __deja_alt_idx=1
    fi
  fi

  local alt="${__deja_alts[$__deja_alt_idx]}"
  if [[ "$alt" == "$BUFFER"* ]]; then
    POSTDISPLAY="${alt#$BUFFER}"
  elif [[ ${#BUFFER} -ge 2 ]]; then
    POSTDISPLAY=" → $alt"
  else
    POSTDISPLAY="$alt"
  fi
}

zle -N __deja_tab
bindkey '^I' __deja_tab

# --- ctrl+x: dismiss suggestion ---
__deja_dismiss() {
  unset POSTDISPLAY
  __deja_reset_alts
}

zle -N __deja_dismiss
bindkey '^X' __deja_dismiss

# --- record hooks ---
__deja_preexec() {
  __deja_cmd="$1"
  __deja_cmd_start="$EPOCHREALTIME"
  __deja_last_dir="$PWD"
}

__deja_precmd() {
  __deja_last_exit=$?

  if [[ -n "$__deja_cmd" ]]; then
    local dur=0
    if [[ -n "$__deja_cmd_start" ]]; then
      local elapsed=$(( EPOCHREALTIME - __deja_cmd_start ))
      dur=$(( ${elapsed%.*} * 1000 + ${elapsed#*.} / 1000000 ))
    fi

    "$__deja_bin" record \
      --command "$__deja_cmd" \
      --dir "$__deja_last_dir" \
      --exit "$__deja_last_exit" \
      --duration "$dur" \
      --session "$__deja_session" \
      --prev "$__deja_prev" &!

    __deja_prev="$__deja_cmd"
    __deja_cmd=""
    __deja_cmd_start=""
  fi
}

autoload -Uz add-zsh-hook
add-zsh-hook preexec __deja_preexec
add-zsh-hook precmd __deja_precmd
