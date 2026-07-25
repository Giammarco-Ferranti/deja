package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/giammarcoferranti/deja/internal/config"
	"github.com/giammarcoferranti/deja/internal/daemon"
	"github.com/giammarcoferranti/deja/internal/store"
)

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.Usage = func() {
		w := os.Stdout
		fs.SetOutput(w)
		fmt.Fprintln(w, "deja daemon — run the suggestion daemon (Unix socket)")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  deja daemon")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Loads the SQLite database (~/.local/share/deja/deja.db) into memory and")
		fmt.Fprintln(w, "listens on ~/.local/share/deja/sock for suggest/record/ping requests from")
		fmt.Fprintln(w, "the zsh integration. Normally auto-spawned by the init script — run")
		fmt.Fprintln(w, "manually only for debugging. Stop with SIGINT/SIGTERM (Ctrl+C).")
	}
	parseFlags(fs, args)

	path, err := dbPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja daemon: %v\n", err)
		os.Exit(1)
	}
	sock, err := sockPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja daemon: %v\n", err)
		os.Exit(1)
	}

	db, err := store.InitDB(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja daemon: init db: %v\n", err)
		os.Exit(1)
	}

	state, err := daemon.Load(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja daemon: load state: %v\n", err)
		os.Exit(1)
	}

	cfgDir, err := dataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja daemon: %v\n", err)
		os.Exit(1)
	}
	fuzzy, fsrc := config.LoadFuzzy(cfgDir)
	state.SetFuzzy(fuzzy)
	fmt.Fprintf(os.Stderr, "deja daemon: fuzzy=%s (%s)\n", fuzzy, sourceLabel(fsrc, config.EnvFuzzy))

	showEmpty, esrc := config.LoadEmpty(cfgDir)
	state.SetShowEmpty(showEmpty)
	fmt.Fprintf(os.Stderr, "deja daemon: empty=%s (%s)\n", config.FormatEmpty(showEmpty), sourceLabel(esrc, config.EnvEmpty))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Fprintf(os.Stderr, "deja daemon: listening on %s\n", sock)
	if err := daemon.Serve(ctx, state, sock); err != nil {
		fmt.Fprintf(os.Stderr, "deja daemon: %v\n", err)
		os.Exit(1)
	}
}

func sourceLabel(s config.Source, envName string) string {
	switch s {
	case config.SourceEnv:
		return envName
	case config.SourceFile:
		return "config file"
	default:
		return "default"
	}
}
