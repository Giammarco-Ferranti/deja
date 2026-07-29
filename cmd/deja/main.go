package main

import (
	"fmt"
	"os"
)

// Set via -ldflags at release time by GoReleaser.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	args := os.Args[2:]
	switch os.Args[1] {
	case "import":
		runImport(args)
	case "daemon":
		runDaemon(args)
	case "query":
		runQuery(args)
	case "init":
		runInit(args)
	case "record":
		runRecord(args)
	case "ping":
		runPing(args)
	case "fuzzy":
		runFuzzy(args)
	case "empty":
		runEmpty(args)
	case "version", "-v", "--version":
		printVersion()
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "deja: unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func printVersion() {
	fmt.Printf("deja %s (%s, %s)\n", version, commit, date)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: deja <subcommand> [flags]

subcommands:
  import   import ~/.zsh_history into the local database
  daemon   run the suggestion daemon (unix socket)
  query    ask the daemon (or fall back to sqlite) for a suggestion
  init     print shell integration script
  record   record an executed command (used by shell hooks)
  ping     check if the daemon is running
  fuzzy    show or change the fuzzy strictness preset
  empty    suppress ghost text suggestions on an empty prompt
  version  print the version, commit, and build date`)
}
