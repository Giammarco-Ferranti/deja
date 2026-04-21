package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/giammarcoferranti/deja/internal/daemon"
	"github.com/giammarcoferranti/deja/internal/store"
)

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	fs.Parse(args)

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

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Fprintf(os.Stderr, "deja daemon: listening on %s\n", sock)
	if err := daemon.Serve(ctx, state, sock); err != nil {
		fmt.Fprintf(os.Stderr, "deja daemon: %v\n", err)
		os.Exit(1)
	}
}
