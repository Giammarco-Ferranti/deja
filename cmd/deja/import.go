package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/giammarcoferranti/deja/internal/importer"
	"github.com/giammarcoferranti/deja/internal/store"
)

func runImport(args []string) {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	file := fs.String("file", "", "path to history file (defaults to $HISTFILE or ~/.zsh_history)")
	fs.Usage = func() {
		w := os.Stdout
		fs.SetOutput(w)
		fmt.Fprintln(w, "deja import — import ~/.zsh_history into the local database")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  deja import")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Reads ~/.zsh_history and inserts each command into the SQLite database")
		fmt.Fprintln(w, "at ~/.local/share/deja/deja.db. Run once after installing; safe to re-run")
		fmt.Fprintln(w, "to pick up new history (duplicate commands accumulate frequency, not rows).")
	}
	parseFlags(fs, args)

	path, err := dbPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja import: %v\n", err)
		os.Exit(1)
	}

	db, err := store.InitDB(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja import: init db: %v\n", err)
		os.Exit(1)
	}

	history, err := importer.ReadHistory(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja import: read history: %v\n", err)
		os.Exit(1)
	}

	commands := make([]store.Command, 0, len(history))
	for _, h := range history {
		ts := time.Now()
		if h.Timestamp != nil {
			ts = *h.Timestamp
		}
		dur := 0
		if h.Duration != nil {
			dur = (*h.Duration) * 1000
		}
		commands = append(commands, store.Command{
			Command:    h.Command,
			Directory:  "",
			Timestamp:  ts,
			ExitCode:   0,
			DurationMS: dur,
			SessionID:  "import",
		})
	}

	if err := store.SaveImportBatch(db, commands); err != nil {
		fmt.Fprintf(os.Stderr, "deja import: save: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("imported %d commands into %s\n", len(commands), path)
}
