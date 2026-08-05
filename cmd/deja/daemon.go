package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/giammarcoferranti/deja/internal/config"
	"github.com/giammarcoferranti/deja/internal/daemon"
	"github.com/giammarcoferranti/deja/internal/store"
)

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	restart := fs.Bool("restart", false, "stop the running daemon first, then start")
	fs.Usage = func() {
		w := os.Stdout
		fs.SetOutput(w)
		fmt.Fprintln(w, "deja daemon — run the suggestion daemon (Unix socket)")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  deja daemon [--restart]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Loads the SQLite database (~/.local/share/deja/deja.db) into memory and")
		fmt.Fprintln(w, "listens on ~/.local/share/deja/sock for suggest/record/ping requests from")
		fmt.Fprintln(w, "the zsh integration. Normally auto-spawned by the init script — run")
		fmt.Fprintln(w, "manually only for debugging. Stop with SIGINT/SIGTERM (Ctrl+C).")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Daemons outlive shell sessions, so after upgrading deja the old one keeps")
		fmt.Fprintln(w, "serving until it is replaced. --restart stops it and starts a new one; use")
		fmt.Fprintln(w, "it when a new shell reports that the running daemon is out of date.")
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

	if *restart {
		if err := stopRunningDaemon(sock); err != nil {
			fmt.Fprintf(os.Stderr, "deja daemon: %v\n", err)
			os.Exit(1)
		}
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

// stopRunningDaemon terminates the daemon currently listening on sock and waits
// for it to release the socket. It is a no-op when nothing is listening.
//
// The pidfile is the only handle we have: the wire protocol has no "quit"
// message, and adding one would not help against precisely the daemon that
// needs replacing — an older build that predates it. A daemon started before
// the pidfile existed therefore cannot be stopped from here, and saying so is
// better than killing something by matching a process name.
func stopRunningDaemon(sock string) error {
	pidPath := daemon.PidPath(sock)

	raw, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			if _, statErr := os.Stat(sock); statErr != nil {
				return nil // nothing listening, nothing to stop
			}
			return fmt.Errorf("a daemon is listening on %s but wrote no pidfile "+
				"(it predates --restart); stop it yourself, e.g. pkill -f 'deja daemon'", sock)
		}
		return fmt.Errorf("read %s: %w", pidPath, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return fmt.Errorf("pidfile %s does not contain a pid (%q)", pidPath, raw)
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			// Daemon died without cleaning up. Clear the stale files so the
			// fresh one starts from a known state.
			os.Remove(pidPath)
			os.Remove(sock)
			return nil
		}
		return fmt.Errorf("signal pid %d: %w", pid, err)
	}

	// Serve removes the socket on its way out, so its disappearance is the
	// signal that the port is free.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); os.IsNotExist(err) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("daemon %d did not release %s within 2s", pid, sock)
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
