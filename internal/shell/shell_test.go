package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	builtinActionsRe = regexp.MustCompile(`(?m)^_DEJA_BUILTIN_ACTIONS=\(([^)]*)\)`)
	bindkeyRe        = regexp.MustCompile(`bindkey\s+"[^"]*"\s+deja-(\w+)`)
)

// builtinActions parses the _DEJA_BUILTIN_ACTIONS array out of the script. Every
// entry gets a generated `_deja_widget_<action>` wrapper and a `deja-<action>`
// zle widget, so it is the authoritative list of bindable actions.
func builtinActions(t *testing.T) map[string]bool {
	t.Helper()

	m := builtinActionsRe.FindStringSubmatch(ZshInit())
	if m == nil {
		t.Fatal("could not find _DEJA_BUILTIN_ACTIONS assignment in zsh.sh")
	}
	actions := make(map[string]bool)
	for _, a := range strings.Fields(m[1]) {
		actions[a] = true
	}
	return actions
}

// TestZshInit_TogglesEmptyOnShiftUp pins the Shift+↑ wiring: the action is
// registered, the key has its documented default, and the binding names that
// action. A typo in any one of the three leaves the key silently dead.
func TestZshInit_TogglesEmptyOnShiftUp(t *testing.T) {
	script := ZshInit()

	if !builtinActions(t)["toggle_empty"] {
		t.Error("toggle_empty missing from _DEJA_BUILTIN_ACTIONS; deja-toggle_empty widget won't be created")
	}
	// Shift+↑ in xterm-style terminals. `=` (not `:=`) so an explicitly-empty
	// value survives and leaves the key unbound.
	if !strings.Contains(script, `: ${DEJA_TOGGLE_EMPTY_KEY='^[[1;2A'}`) {
		t.Error("DEJA_TOGGLE_EMPTY_KEY default (Shift+up, ^[[1;2A) not declared as expected")
	}
	if !strings.Contains(script, `bindkey "$DEJA_TOGGLE_EMPTY_KEY"`) {
		t.Error("DEJA_TOGGLE_EMPTY_KEY is never passed to bindkey")
	}
}

// TestZshInit_BindkeyActionsExist guards every key binding, not just the new
// one: a `bindkey ... deja-<action>` whose action isn't in
// _DEJA_BUILTIN_ACTIONS binds a nonexistent widget, which zsh reports only when
// the user presses the key.
func TestZshInit_BindkeyActionsExist(t *testing.T) {
	actions := builtinActions(t)

	matches := bindkeyRe.FindAllStringSubmatch(ZshInit(), -1)
	if len(matches) == 0 {
		t.Fatal("no `bindkey ... deja-<action>` lines found; regexp or script layout changed")
	}
	for _, m := range matches {
		if !actions[m[1]] {
			t.Errorf("bindkey references deja-%s, which is not in _DEJA_BUILTIN_ACTIONS", m[1])
		}
	}
}

// TestZshInit_ActionsHaveHandlers checks the other half of the contract: the
// generated wrapper calls `_deja_<action>`, so each registered action needs a
// function of that name.
func TestZshInit_ActionsHaveHandlers(t *testing.T) {
	script := ZshInit()

	for action := range builtinActions(t) {
		if !strings.Contains(script, "\n_deja_"+action+"()") {
			t.Errorf("action %q has no _deja_%s() function", action, action)
		}
	}
}

// TestZshInit_Syntax parses the generated script with zsh itself, the way
// `deja init zsh` emits it. Skipped where zsh isn't installed (some CI images).
func TestZshInit_Syntax(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed; skipping syntax check")
	}

	// Same substitution cmd/deja/init.go performs before printing the script.
	script := strings.ReplaceAll(ZshInit(), "{{DEJA_BIN}}", "/nonexistent/deja")
	path := filepath.Join(t.TempDir(), "init.zsh")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	out, err := exec.Command(zsh, "-n", path).CombinedOutput()
	if err != nil {
		t.Fatalf("zsh -n rejected the init script: %v\n%s", err, out)
	}
}

// TestZshInit_HighlightCarriesMemo pins the memo tag on the ghost's
// region_highlight entry. zsh mangles the entry's offsets whenever a widget
// metafies the line (every completion widget does), so the entry can no longer be
// found by exact string match — the memo is the only durable handle on it. The
// comma after the style matters too: it terminates the style list so zsh <= 5.8
// drops the unknown memo word instead of folding it into the style.
func TestZshInit_HighlightCarriesMemo(t *testing.T) {
	script := ZshInit()

	if !strings.Contains(script, `${DEJA_HIGHLIGHT_STYLE}, memo=deja`) {
		t.Error("highlight entry is not tagged `<style>, memo=deja`; offset mangling will orphan it")
	}
	if !strings.Contains(script, `region_highlight=("${(@)region_highlight:#*memo=deja*}")`) {
		t.Error("_deja_highlight_reset does not remove entries by memo tag")
	}
	// The tag is only usable on zsh 5.9+, and that is detected by reading the
	// stored entry back rather than by a version test.
	if !strings.Contains(script, `_DEJA_LAST_HIGHLIGHT="${region_highlight[-1]}"`) {
		t.Error("_deja_highlight_apply does not read the stored entry back; it would compare against the string it wrote")
	}
	if !strings.Contains(script, "_DEJA_MEMO_SUPPORTED") {
		t.Error("no _DEJA_MEMO_SUPPORTED probe; memo removal would break on zsh < 5.9")
	}
}

// TestZshInit_InstallsRedrawHook guards the repair path. Completion widgets are
// deliberately left unwrapped (`zle -N` over a `zle -C` widget wedges the line
// editor), so a zle-line-pre-redraw hook is what reconciles POSTDISPLAY and the
// highlight after such a widget rewrites BUFFER.
func TestZshInit_InstallsRedrawHook(t *testing.T) {
	script := ZshInit()

	if !strings.Contains(script, "add-zle-hook-widget zle-line-pre-redraw _deja_line_pre_redraw") {
		t.Error("zle-line-pre-redraw hook is never registered")
	}
	if !strings.Contains(script, "\n_deja_line_pre_redraw()") {
		t.Error("_deja_line_pre_redraw() is not defined")
	}
	// Must be re-asserted on precmd and at startup, like the keybindings, so a
	// plugin that takes the hook over with a bare `zle -N` doesn't drop deja.
	if n := strings.Count(script, "_deja_install_redraw_hook"); n < 3 {
		t.Errorf("_deja_install_redraw_hook referenced %d times; want definition plus startup and precmd calls", n)
	}
	// The hook runs inside zsh's redraw path. Calling a widget from there risks
	// re-entering through recursiveedit(), and a non-zero status would stop
	// add-zle-hook-widget from calling the rest of the chain (e.g. z-sy-h).
	hook := script[strings.Index(script, "\n_deja_line_pre_redraw()"):]
	hook = hook[:strings.Index(hook, "\n}")]
	if strings.Contains(hook, "zle -R") || strings.Contains(hook, "zle deja-") {
		t.Error("_deja_line_pre_redraw invokes a zle widget; it must only assign state")
	}
	if !strings.Contains(hook, "return 0") {
		t.Error("_deja_line_pre_redraw does not force a zero exit status; it would truncate the hook chain")
	}
}

// TestZshInit_IgnoresHookAliases covers a sharp edge in add-zle-hook-widget: it
// preserves an incumbent hook by aliasing it to a name like
// `user:_deja_line_init`, which matches none of the other ignore patterns. Left
// unignored, _deja_bind_widgets would wrap deja's own hook as a modify widget.
func TestZshInit_IgnoresHookAliases(t *testing.T) {
	script := ZshInit()

	start := strings.Index(script, "DEJA_IGNORE_WIDGETS=(")
	if start < 0 {
		t.Fatal("could not find DEJA_IGNORE_WIDGETS assignment")
	}
	block := script[start : start+strings.Index(script[start:], ")")]
	if !strings.Contains(block, `user:\*`) {
		t.Error(`DEJA_IGNORE_WIDGETS is missing user:\*; add-zle-hook-widget aliases would get wrapped`)
	}
}

// TestZshInit_PreservesSuggestionIdentity pins the two places that used to leave
// POSTDISPLAY on screen with an empty _DEJA_SUGGESTION_MODE. Both now matter more
// than before: _deja_accept reads the mode to decide whether to append
// POSTDISPLAY or swap in the raw suggestion, and _deja_line_pre_redraw derives
// POSTDISPLAY from the mode plus the cached suggestion.
func TestZshInit_PreservesSuggestionIdentity(t *testing.T) {
	script := ZshInit()

	// _deja_modify's "more keys queued" branch restores the held-over ghost, so
	// it has to restore the ghost's identity alongside the text.
	if !strings.Contains(script, `_DEJA_SUGGESTION_MODE="$orig_mode"`) ||
		!strings.Contains(script, `_DEJA_CURRENT_SUGGESTION="$orig_suggestion"`) {
		t.Error("_deja_modify does not restore mode/suggestion in the PENDING branch; → would paste the fuzzy separator into BUFFER")
	}

	// _deja_partial_accept only moves the BUFFER/POSTDISPLAY boundary, so the
	// cached suggestion still describes the line and must not be cleared.
	fn := script[strings.Index(script, "\n_deja_partial_accept()"):]
	fn = fn[:strings.Index(fn, "\n}")]
	if strings.Contains(fn, `_DEJA_CURRENT_SUGGESTION=""`) || strings.Contains(fn, `_DEJA_SUGGESTION_MODE=""`) {
		t.Error("_deja_partial_accept clears mode/suggestion; the redraw hook would then wipe the remaining tail")
	}
}

// TestZshInit_HistoryIgnorePredicate pins the shape of the ignore check. The
// live test below proves the behavior; these assertions catch a silent
// regression if someone rewrites the function.
func TestZshInit_HistoryIgnorePredicate(t *testing.T) {
	script := ZshInit()

	if !strings.Contains(script, "\n_deja_history_ignored()") {
		t.Fatal("_deja_history_ignored() is not defined")
	}
	// Follow the user's own setopt rather than always ignoring: the shell-side
	// check exists to keep the command out of `deja record`'s argv.
	if !strings.Contains(script, "-o hist_ignore_space") {
		t.Error("hist_ignore_space is never consulted")
	}
	// ${~HISTORY_IGNORE} — the ~ forces glob expansion of the pattern, which is
	// how zsh itself matches it. Without it this is a literal string compare.
	if !strings.Contains(script, "${~HISTORY_IGNORE}") {
		t.Error("HISTORY_IGNORE is not glob-expanded with ${~...}")
	}

	// The chain break. Without this the ignored command survives as the --prev
	// of the next command and lands in sequences.prev_command verbatim.
	fn := script[strings.Index(script, "\n_deja_preexec()"):]
	fn = fn[:strings.Index(fn, "\n}")]
	if !strings.Contains(fn, "_deja_history_ignored") {
		t.Error("_deja_preexec does not call _deja_history_ignored")
	}
	if !strings.Contains(fn, `__deja_prev=""`) {
		t.Error("_deja_preexec does not clear __deja_prev for ignored commands; the secret leaks into sequences.prev_command")
	}
}

// TestZshInit_HistoryIgnoredBehavior runs the real predicate under a real zsh.
// This is the test that actually proves the fix — the string assertions above
// only pin its shape. Sourcing the script non-interactively is enough: we call
// the hook directly rather than driving a live prompt, which keeps this fast and
// free of the background-write races an interactive harness would have.
func TestZshInit_HistoryIgnoredBehavior(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed; skipping behavior check")
	}

	script := strings.ReplaceAll(ZshInit(), "{{DEJA_BIN}}", "/nonexistent/deja")
	dir := t.TempDir()
	path := filepath.Join(dir, "init.zsh")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tests := []struct {
		name    string
		setup   string // zsh run before the call
		command string
		ignored bool
	}{
		{"plain command", "setopt hist_ignore_space", "git status", false},
		{"leading space", "setopt hist_ignore_space", " git status", true},
		{"leading tab", "setopt hist_ignore_space", "\tgit status", true},
		{"leading space, option off", "setopt no_hist_ignore_space", " git status", false},
		{"history_ignore match", "HISTORY_IGNORE='*TOKEN*'", "echo MY_TOKEN_here", true},
		{"history_ignore miss", "HISTORY_IGNORE='*TOKEN*'", "echo something", false},
		{"history_ignore unset", "unset HISTORY_IGNORE", "echo something", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Source the script, then exercise the hook exactly as preexec
			// would, and report the resulting state. Checking __deja_last_cmd
			// (rather than the predicate alone) covers the early return too.
			//
			// The command under test is passed as an argv and read back as $1,
			// not interpolated: embedding it in the program text would require
			// zsh-correct quoting of a tab, which %q does not produce.
			prog := fmt.Sprintf(`
				source %q
				%s
				_deja_preexec "$1"
				print -r -- "last_cmd=[$__deja_last_cmd]"
			`, path, tt.setup)

			out, err := exec.Command(zsh, "-f", "-c", prog, "deja-test", tt.command).CombinedOutput()
			if err != nil {
				t.Fatalf("zsh failed: %v\n%s", err, out)
			}

			got := strings.TrimSpace(string(out))
			want := "last_cmd=[" + tt.command + "]"
			if tt.ignored {
				want = "last_cmd=[]"
			}
			if got != want {
				t.Errorf("command %q with %q:\ngot  %s\nwant %s", tt.command, tt.setup, got, want)
			}
		})
	}
}

// The ignored command must not survive as __deja_prev either — that is the leak
// that reaches sequences.prev_command even though the command itself is never
// recorded.
func TestZshInit_IgnoredCommandBreaksSequenceChain(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed; skipping behavior check")
	}

	script := strings.ReplaceAll(ZshInit(), "{{DEJA_BIN}}", "/nonexistent/deja")
	path := filepath.Join(t.TempDir(), "init.zsh")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	prog := fmt.Sprintf(`
		source %q
		setopt hist_ignore_space
		__deja_prev="git status"
		_deja_preexec " export AWS_SECRET=hunter2"
		print -r -- "prev=[$__deja_prev]"
	`, path)

	out, err := exec.Command(zsh, "-f", "-c", prog).CombinedOutput()
	if err != nil {
		t.Fatalf("zsh failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "prev=[]" {
		t.Errorf("__deja_prev was not cleared: got %s, want prev=[]", got)
	}
}

// escapeCases pins the request-field escaping shared by two languages. The zsh
// half is `_deja_text_request` in zsh.sh; the Go half is escapeTextField /
// unescapeTextField in internal/daemon/text.go, whose own round-trip test
// covers the same inputs. If these two drift, a command containing a backslash,
// a newline, or a US byte gets silently corrupted on its way into the database
// — so the agreement is pinned here by running the actual shell code.
var escapeCases = []struct {
	name string
	in   string
	want string
}{
	{"plain", "git status", "git status"},
	{"backslash", `grep -E '\d+' f`, `grep -E '\\d+' f`},
	{"double backslash", `printf 'a\\b'`, `printf 'a\\\\b'`},
	{"newline", "one\ntwo", `one\ntwo`},
	{"unit separator", "a\x1fb", `a\sb`},
	{"all three", "a\\b\nc\x1fd", `a\\b\nc\sd`},
	{"trailing backslash", `cd C:\`, `cd C:\\`},
	{"empty", "", ""},
	{"non-ascii", "echo 'héllo → 世界'", "echo 'héllo → 世界'"},
	{"already looks escaped", `\n \s`, `\\n \\s`},
}

// TestZshInit_TextRequestEscaping runs _deja_text_request in a real zsh and
// compares the bytes it produces against what the daemon's parser expects.
func TestZshInit_TextRequestEscaping(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed; skipping escaping check")
	}

	script := strings.ReplaceAll(ZshInit(), "{{DEJA_BIN}}", "/nonexistent/deja")
	script = strings.ReplaceAll(script, "{{DEJA_SOCK}}", "/nonexistent/sock")
	path := filepath.Join(t.TempDir(), "init.zsh")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	for _, tc := range escapeCases {
		t.Run(tc.name, func(t *testing.T) {
			// Sourcing the script also runs its startup tail; DEJA_BIN points at
			// nothing, so widget binding and daemon spawn are both no-ops.
			prog := "source " + path + " >/dev/null 2>&1\n" +
				"_deja_text_request suggest \"$1\"\n" +
				"print -rn -- \"$REPLY\"\n"
			out, err := exec.Command(zsh, "-f", "-c", prog, "zsh", tc.in).Output()
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			want := "suggest\x1f" + tc.want
			if string(out) != want {
				t.Errorf("_deja_text_request(%q)\n got %q\nwant %q", tc.in, out, want)
			}
		})
	}
}

// TestZshInit_SubprocessFallbackSurvives guards the property that makes the
// socket transport safe to ship: every path that talks to the daemon must still
// have a working subprocess route for shells where zsh/net/socket is missing, or
// where the running daemon is too old to answer.
func TestZshInit_SubprocessFallbackSurvives(t *testing.T) {
	script := ZshInit()

	// The capability probe must be a probe, not an assumption.
	if !strings.Contains(script, "$+builtins[zsocket]") {
		t.Error("zsocket is used without probing for the builtin; shells without zsh/net/socket would break")
	}

	// Each hot path keeps its `deja <subcommand>` invocation as a fallback.
	for _, want := range []string{
		`"$DEJA_BIN" query --buffer "$buffer"`, // _deja_fetch_suggestion
		`"$DEJA_BIN" query --buffer "$1"`,      // _deja_async_request
		`"$DEJA_BIN" record \`,                 // _deja_precmd
	} {
		if !strings.Contains(script, want) {
			t.Errorf("subprocess fallback %q is gone; a shell without zsocket would lose this path", want)
		}
	}

	// An old daemon accepts the connection but never answers, so the probe must
	// disarm the socket transport rather than leaving the shell mute.
	if !strings.Contains(script, "_DEJA_ZSOCKET=0") {
		t.Error("_deja_ensure_daemon never stands down to the subprocess path; a pre-text-protocol daemon would leave the shell with no suggestions")
	}
}

// scriptForZsh writes the init script with its placeholders substituted, the
// way cmd/deja/init.go does, and returns the path.
func scriptForZsh(t *testing.T) string {
	t.Helper()
	script := strings.ReplaceAll(ZshInit(), "{{DEJA_BIN}}", "/nonexistent/deja")
	script = strings.ReplaceAll(script, "{{DEJA_SOCK}}", "/nonexistent/sock")
	path := filepath.Join(t.TempDir(), "init.zsh")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

// TestZshInit_SessionIDForksNothing guards a startup cost that is easy to
// reintroduce. Generating the id by piping /dev/urandom through head/xxd/tr
// cost 6-13ms of every shell's startup for entropy a session id does not need.
func TestZshInit_SessionIDForksNothing(t *testing.T) {
	script := ZshInit()

	start := strings.Index(script, "typeset -g DEJA_SESSION_ID")
	if start < 0 {
		t.Fatal("DEJA_SESSION_ID assignment not found")
	}
	block := script[start:]
	if end := strings.Index(block, "\ntypeset -g __deja_prev"); end > 0 {
		block = block[:end]
	}

	for _, forked := range []string{"head ", "xxd", "tr ", "$(", "`"} {
		if strings.Contains(block, forked) {
			t.Errorf("session id generation runs %q; it must use only shell builtins", forked)
		}
	}
}

// TestZshInit_SessionIDIsUnique checks the replacement actually distinguishes
// shells, including two started back to back within the same shell's lifetime.
func TestZshInit_SessionIDIsUnique(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	path := scriptForZsh(t)

	seen := map[string]bool{}
	for i := 0; i < 12; i++ {
		out, err := exec.Command(zsh, "-f", "-c",
			"source "+path+" >/dev/null 2>&1; print -rn -- $DEJA_SESSION_ID").Output()
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		id := string(out)
		if id == "" {
			t.Fatal("session id is empty")
		}
		if seen[id] {
			t.Errorf("duplicate session id %q across shells", id)
		}
		seen[id] = true
	}
}

// TestZshInit_RebindGuard pins both halves of the precmd early-out: it must skip
// the ~5.4ms re-bind when nothing changed, and must NOT skip it when a plugin
// has replaced a widget deja had wrapped. Getting the second half wrong is the
// dangerous one — deja's widgets would silently stop being reachable.
//
// It also pins that the count settles after startup. The redraw hook and
// zle-line-init install widgets of their own *after* binding, so a count
// recorded inside _deja_bind_widgets never matches what the next prompt sees
// and every prompt re-binds — which is the bug this guard exists to avoid.
func TestZshInit_RebindGuard(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}

	// DEJA_BIN must be executable or the script's startup tail skips binding
	// entirely; /bin/true is a harmless stand-in that spawns no daemon.
	script := strings.ReplaceAll(ZshInit(), "{{DEJA_BIN}}", "/usr/bin/true")
	script = strings.ReplaceAll(script, "{{DEJA_SOCK}}", "/nonexistent/sock")
	path := filepath.Join(t.TempDir(), "init.zsh")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	// precmd's rebind block, verbatim in shape.
	precmd := `
		_deja_widgets_unchanged || _deja_bind_widgets
		_deja_apply_keybindings
		_deja_install_redraw_hook
		_DEJA_BOUND_WIDGET_COUNT=${#widgets}
	`

	prog := `
		source ` + path + ` >/dev/null 2>&1

		# Startup has bound and recorded. The very next prompt must skip.
		_deja_widgets_unchanged && print -r -- "afterstartup:skip" || print -r -- "afterstartup:rebind"

		# A framework adding a widget changes the count.
		zle -N some-new-plugin-widget _deja_noop 2>/dev/null
		_deja_widgets_unchanged && print -r -- "added:skip" || print -r -- "added:rebind"
` + precmd + `
		# ...and the prompt after that settles again.
		_deja_widgets_unchanged && print -r -- "settled:skip" || print -r -- "settled:rebind"

		# A framework *replacing* a widget deja wrapped leaves the count alone.
		zle -N self-insert 2>/dev/null
		_deja_widgets_unchanged && print -r -- "stolen:skip" || print -r -- "stolen:rebind"
` + precmd + `
		[[ ${widgets[self-insert]} == user:_deja_bound_* ]] && print -r -- "repaired:yes" || print -r -- "repaired:no"
	`
	out, err := exec.Command(zsh, "-f", "-c", prog).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}

	got := strings.Fields(string(out))
	want := []string{"afterstartup:skip", "added:rebind", "settled:skip", "stolen:rebind", "repaired:yes"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d: got %q, want %q (full: %q)", i, got[i], want[i], got)
		}
	}
}
