package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/giammarcoferranti/deja/internal/config"
	"github.com/giammarcoferranti/deja/internal/daemon"
)

func runEmpty(args []string) {
	fs := flag.NewFlagSet("empty", flag.ContinueOnError)
	fs.Usage = func() {
		w := os.Stdout
		fs.SetOutput(w)
		fmt.Fprintln(w, "deja empty — suppress ghost text suggestions on an empty prompt")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  deja empty                 show whether empty-prompt suggestions are on")
		fmt.Fprintln(w, "  deja empty on|off          turn empty-prompt suggestions on or off (aliases: show|hide)")
		fmt.Fprintln(w, "  deja empty toggle          flip the setting and print just the new state")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "When on (the default), deja predicts the next command on a fresh prompt using")
		fmt.Fprintln(w, "command-sequence, frecency, and directory signals. When off, ghost text only")
		fmt.Fprintln(w, "appears once you start typing.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Changes take effect immediately if the daemon is running, and are persisted")
		fmt.Fprintln(w, "to ~/.local/share/deja/config so they survive restarts.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Override at session level with: export DEJA_EMPTY=off")
		fmt.Fprintln(w, "In zsh, press Shift+↑ to flip the setting without typing.")
		fmt.Fprintln(w, "This is a global, persisted setting — distinct from Ctrl+X, which suppresses")
		fmt.Fprintln(w, "all suggestions for the current shell session only.")
	}
	parseFlags(fs, args)

	rest := fs.Args()
	switch len(rest) {
	case 0:
		printEmptyStatus(readCurrentEmpty())
	case 1:
		if strings.ToLower(strings.TrimSpace(rest[0])) == "toggle" {
			toggleEmpty()
		} else {
			setEmpty(rest[0])
		}
	default:
		fmt.Fprintln(os.Stderr, "deja empty: too many arguments")
		printEmptyStatus(readCurrentEmpty())
		os.Exit(2)
	}
}

// readCurrentEmpty returns the effective setting. Prefer asking the daemon (it
// knows the live in-memory value); fall back to the persisted file + env.
//
// GetConfigResp.Empty is a *bool: a daemon that predates the setting sends nil,
// and we then fall back to the persisted default rather than trusting a
// zero-valued false.
func readCurrentEmpty() bool {
	if resp, err := dialGetConfig(); err == nil && resp.Empty != nil {
		return *resp.Empty
	}
	dir, err := dataDir()
	if err != nil {
		return config.EmptyDefault
	}
	show, _ := config.LoadEmpty(dir)
	return show
}

// applyEmpty persists show to the config file and pushes it to the running
// daemon (if reachable). prev is the **file-persisted** previous value; it
// reflects what the file said before the write, not the env-resolved effective
// value, so callers can print an accurate diff.
func applyEmpty(show bool) (prev bool, hadFile, daemonApplied bool, err error) {
	dir, derr := dataDir()
	if derr != nil {
		return config.EmptyDefault, false, false, derr
	}
	prev, hadFile = config.LoadEmptyFile(dir)

	if err := config.SaveEmpty(dir, show); err != nil {
		return prev, hadFile, false, fmt.Errorf("persist: %w", err)
	}

	if _, derr := dialSetConfig(daemon.SetConfigReq{Empty: &show}); derr == nil {
		daemonApplied = true
	}
	return prev, hadFile, daemonApplied, nil
}

func setEmpty(raw string) {
	show, err := config.ParseEmpty(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja empty: %v\n", err)
		printEmptyStatus(readCurrentEmpty())
		os.Exit(2)
	}
	commitEmpty(show)
}

func commitEmpty(show bool) {
	prev, hadFile, daemonApplied, err := applyEmpty(show)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja empty: %v\n", err)
		os.Exit(1)
	}

	switch {
	case !hadFile:
		fmt.Printf("empty: (unset) → %s\n", config.FormatEmpty(show))
	case prev == show:
		fmt.Printf("empty: %s (unchanged)\n", config.FormatEmpty(show))
	default:
		fmt.Printf("empty: %s → %s\n", config.FormatEmpty(prev), config.FormatEmpty(show))
	}
	if !daemonApplied {
		fmt.Println("note: daemon not reachable; new setting will apply on next start")
	}
	// Compare the env override semantically: DEJA_EMPTY=true and "on" mean the
	// same thing, so a raw string compare would warn spuriously.
	if env := strings.TrimSpace(os.Getenv(config.EnvEmpty)); env != "" {
		if envShow, perr := config.ParseEmpty(env); perr == nil && envShow != show {
			fmt.Printf("note: %s=%s is set in your environment and will override on next daemon start\n", config.EnvEmpty, env)
		}
	}
}

// toggleEmpty flips the setting and prints just the new state (`on` or `off`)
// to stdout. The zsh Shift+↑ binding consumes that single token directly, the
// same way it consumes `deja fuzzy cycle`; humans use `deja empty <on|off>` for
// prose.
func toggleEmpty() {
	show := !readCurrentEmpty()
	if _, _, _, err := applyEmpty(show); err != nil {
		fmt.Fprintf(os.Stderr, "deja empty: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(config.FormatEmpty(show))
}

func printEmptyStatus(current bool) {
	fmt.Printf("empty-prompt suggestions: %s\n\n", config.FormatEmpty(current))
	if current {
		fmt.Println("deja predicts the next command on a fresh (empty) prompt and shows it as ghost text.")
	} else {
		fmt.Println("deja shows nothing on a fresh (empty) prompt; suggestions start once you type.")
	}
	fmt.Println()
	fmt.Println("change with:  deja empty <on|off>   (aliases: show|hide)")
	fmt.Println("flip it:      deja empty toggle     (or press Shift+↑ in zsh)")
	fmt.Println("set in shell: export DEJA_EMPTY=off")
}
