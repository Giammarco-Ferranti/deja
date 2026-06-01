package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/giammarcoferranti/deja/internal/config"
	"github.com/giammarcoferranti/deja/internal/daemon"
	"github.com/giammarcoferranti/deja/internal/scorer"
)

func runFuzzy(args []string) {
	fs := flag.NewFlagSet("fuzzy", flag.ContinueOnError)
	fs.Usage = func() {
		w := os.Stdout
		fs.SetOutput(w)
		fmt.Fprintln(w, "deja fuzzy — show or change the fuzzy strictness preset")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  deja fuzzy                 show the current preset and examples")
		fmt.Fprintln(w, "  deja fuzzy <preset>        set the preset (loose|smart|tight)")
		fmt.Fprintln(w, "  deja fuzzy cycle           advance to the next preset (tight→smart→loose→tight)")
		fmt.Fprintln(w, "  deja fuzzy back            step to the previous preset (loose→smart→tight→loose)")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "The preset controls how far apart typed letters may be in a candidate")
		fmt.Fprintln(w, "command. Changes take effect immediately if the daemon is running, and")
		fmt.Fprintln(w, "are persisted to ~/.local/share/deja/config so they survive restarts.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Override at session level with: export DEJA_FUZZY=smart")
		fmt.Fprintln(w, "In zsh, press Shift+→ / Shift+← to cycle forward / backward without typing.")
	}
	parseFlags(fs, args)

	rest := fs.Args()
	switch len(rest) {
	case 0:
		current := readCurrentFuzzy()
		printFuzzyHelp(current, nil)
	case 1:
		switch strings.ToLower(strings.TrimSpace(rest[0])) {
		case "cycle":
			cycleFuzzy()
		case "back":
			backFuzzy()
		default:
			setFuzzy(rest[0])
		}
	default:
		fmt.Fprintln(os.Stderr, "deja fuzzy: too many arguments")
		printFuzzyHelp(readCurrentFuzzy(), nil)
		os.Exit(2)
	}
}

// readCurrentFuzzy returns the effective preset. Prefer asking the daemon (it
// knows the live in-memory value); fall back to the persisted file + env.
func readCurrentFuzzy() scorer.Fuzzy {
	if resp, err := dialGetConfig(); err == nil {
		if f, perr := scorer.ParseFuzzy(resp.Fuzzy); perr == nil {
			return f
		}
	}
	dir, err := dataDir()
	if err != nil {
		return scorer.FuzzyDefault
	}
	f, _ := config.LoadFuzzy(dir)
	return f
}

// applyFuzzy persists f to the config file and pushes it to the running
// daemon (if reachable). prev is the **file-persisted** previous value; it
// reflects what the file said before the write, not the env-resolved
// effective value, so callers can print an accurate diff.
func applyFuzzy(f scorer.Fuzzy) (prev scorer.Fuzzy, hadFile, daemonApplied bool, err error) {
	dir, derr := dataDir()
	if derr != nil {
		return scorer.FuzzyDefault, false, false, derr
	}
	prev, hadFile = config.LoadFuzzyFile(dir)

	if err := config.SaveFuzzy(dir, f); err != nil {
		return prev, hadFile, false, fmt.Errorf("persist: %w", err)
	}

	if _, derr := dialSetConfig(daemon.SetConfigReq{Fuzzy: f.String()}); derr == nil {
		daemonApplied = true
	}
	return prev, hadFile, daemonApplied, nil
}

func setFuzzy(raw string) {
	f, err := scorer.ParseFuzzy(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja fuzzy: %v\n", err)
		printFuzzyHelp(readCurrentFuzzy(), nil)
		os.Exit(2)
	}

	prev, hadFile, daemonApplied, err := applyFuzzy(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja fuzzy: %v\n", err)
		os.Exit(1)
	}

	switch {
	case !hadFile:
		fmt.Printf("fuzzy: (unset) → %s\n", f)
	case prev == f:
		fmt.Printf("fuzzy: %s (unchanged)\n", f)
	default:
		fmt.Printf("fuzzy: %s → %s\n", prev, f)
	}
	if !daemonApplied {
		fmt.Println("note: daemon not reachable; new preset will apply on next start")
	}
	if env := strings.TrimSpace(os.Getenv(config.EnvFuzzy)); env != "" && env != f.String() {
		fmt.Printf("note: DEJA_FUZZY=%s is set in your environment and will override on next daemon start\n", env)
	}
}

// cycleFuzzy advances to the next preset (tight→smart→loose→tight) and
// prints just the new preset name to stdout. The zsh keybinding consumes
// that single token directly; humans use `deja fuzzy <preset>` for prose.
func cycleFuzzy() {
	stepFuzzy(scorer.NextFuzzy)
}

// backFuzzy is the inverse of cycleFuzzy (loose→smart→tight→loose).
func backFuzzy() {
	stepFuzzy(scorer.PrevFuzzy)
}

func stepFuzzy(step func(scorer.Fuzzy) scorer.Fuzzy) {
	next := step(readCurrentFuzzy())
	if _, _, _, err := applyFuzzy(next); err != nil {
		fmt.Fprintf(os.Stderr, "deja fuzzy: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(next)
}

func printFuzzyHelp(current scorer.Fuzzy, _ error) {
	mark := func(p scorer.Fuzzy) string {
		if p == current {
			return "*"
		}
		return " "
	}
	fmt.Printf("current: %s\n\n", current)
	fmt.Printf("  %s loose   typed letters can be far apart (up to 8 chars between)\n", mark(scorer.FuzzyLoose))
	fmt.Printf("            e.g. `gco` → `git checkout -- README`\n")
	fmt.Printf("  %s smart   typed letters stay close together (up to 4 chars between)   [default]\n", mark(scorer.FuzzySmart))
	fmt.Printf("            e.g. `gco` → `git checkout main`\n")
	fmt.Printf("  %s tight   typed letters must be near-adjacent (up to 1 char between)\n", mark(scorer.FuzzyTight))
	fmt.Printf("            e.g. `gco` → `gco`, `g.co`, `gc.o`\n\n")
	fmt.Println("change with:  deja fuzzy <loose|smart|tight>")
	fmt.Println("cycle next:   deja fuzzy cycle    (or press Shift+→ in zsh)")
	fmt.Println("cycle prev:   deja fuzzy back     (or press Shift+← in zsh)")
	fmt.Println("set in shell: export DEJA_FUZZY=smart")
}

func dialGetConfig() (daemon.GetConfigResp, error) {
	sock, err := sockPath()
	if err != nil {
		return daemon.GetConfigResp{}, err
	}
	conn, err := net.DialTimeout("unix", sock, dialTimeout)
	if err != nil {
		return daemon.GetConfigResp{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(readTimeout))

	if err := json.NewEncoder(conn).Encode(daemon.Envelope{Type: "getconfig"}); err != nil {
		return daemon.GetConfigResp{}, err
	}
	var resp daemon.GetConfigResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return daemon.GetConfigResp{}, err
	}
	return resp, nil
}

func dialSetConfig(req daemon.SetConfigReq) (daemon.SetConfigResp, error) {
	sock, err := sockPath()
	if err != nil {
		return daemon.SetConfigResp{}, err
	}
	conn, err := net.DialTimeout("unix", sock, dialTimeout)
	if err != nil {
		return daemon.SetConfigResp{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(readTimeout))

	payload, err := json.Marshal(req)
	if err != nil {
		return daemon.SetConfigResp{}, err
	}
	if err := json.NewEncoder(conn).Encode(daemon.Envelope{Type: "setconfig", Payload: payload}); err != nil {
		return daemon.SetConfigResp{}, err
	}
	var resp daemon.SetConfigResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return daemon.SetConfigResp{}, err
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("daemon: %s", resp.Error)
	}
	return resp, nil
}
