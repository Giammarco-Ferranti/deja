package main

import (
	"fmt"
	"os"
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
	case "ping":
		runPing(args)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "deja: unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: deja <subcommand> [flags]

subcommands:
  import   import ~/.zsh_history into the local database
  daemon   run the suggestion daemon (unix socket)
  query    ask the daemon (or fall back to sqlite) for a suggestion
  init     print shell integration script
  ping     check if the daemon is running`)
}
