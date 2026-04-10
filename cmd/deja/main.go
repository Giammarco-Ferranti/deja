package main

import (
	"fmt"
	"os"
	"time"

	"github.com/giammarcoferranti/deja/internal/importer"
	"github.com/giammarcoferranti/deja/internal/store"
)

func main() {
	fmt.Println("starting deja...")
	db, err := store.InitDB("deja.db")
	if err != nil {
		fmt.Fprintf(os.Stderr, "init db failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("db ready")
	history, err := importer.ReadHistory()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("history loaded")
	var commands []store.Command

	for _, h := range history {
		ts := time.Now()
		if h.Timestamp != nil {
			ts = *h.Timestamp
		}

		dur := 0
		if h.Duration != nil {
			dur = (*h.Duration) * 1000
		}

		command := store.Command{
			Command:    h.Command,
			Directory:  "",
			Timestamp:  ts,
			ExitCode:   0,
			DurationMS: dur,
			SessionID:  "import",
		}
		commands = append(commands, command)
	}

	if err := store.SaveImportBatch(db, commands); err != nil {
		fmt.Fprintf(os.Stderr, "failed importing commands: %v\n", err)
		os.Exit(1)
	}
}
