package shell

import (
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
