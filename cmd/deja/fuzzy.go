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
		fmt.Fprintln(w)
		fmt.Fprintln(w, "The preset controls how far apart typed letters may be in a candidate")
		fmt.Fprintln(w, "command. Changes take effect immediately if the daemon is running, and")
		fmt.Fprintln(w, "are persisted to ~/.local/share/deja/config so they survive restarts.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Override at session level with: export DEJA_FUZZY=smart")
	}
	parseFlags(fs, args)

	rest := fs.Args()
	switch len(rest) {
	case 0:
		current := readCurrentFuzzy()
		printFuzzyHelp(current, nil)
	case 1:
		setFuzzy(rest[0])
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

func setFuzzy(raw string) {
	f, err := scorer.ParseFuzzy(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja fuzzy: %v\n", err)
		printFuzzyHelp(readCurrentFuzzy(), nil)
		os.Exit(2)
	}

	dir, err := dataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja fuzzy: %v\n", err)
		os.Exit(1)
	}
	prev, _ := config.LoadFuzzy(dir)

	if err := config.SaveFuzzy(dir, f); err != nil {
		fmt.Fprintf(os.Stderr, "deja fuzzy: persist: %v\n", err)
		os.Exit(1)
	}

	daemonApplied := false
	if _, derr := dialSetConfig(daemon.SetConfigReq{Fuzzy: f.String()}); derr == nil {
		daemonApplied = true
	}

	if prev == f {
		fmt.Printf("fuzzy: %s (unchanged)\n", f)
	} else {
		fmt.Printf("fuzzy: %s → %s\n", prev, f)
	}
	if !daemonApplied {
		fmt.Println("note: daemon not reachable; new preset will apply on next start")
	}
	if env := strings.TrimSpace(os.Getenv(config.EnvFuzzy)); env != "" && env != f.String() {
		fmt.Printf("note: DEJA_FUZZY=%s is set in your environment and will override on next daemon start\n", env)
	}
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
