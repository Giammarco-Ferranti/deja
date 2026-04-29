package main

import (
	"errors"
	"flag"
	"os"
)

// parseFlags runs fs.Parse and translates --help into a clean exit 0
// (flag.ContinueOnError surfaces -h/--help as flag.ErrHelp after fs.Usage
// has already printed). Any other parse error exits 2 — fs.Parse has
// already written the error to fs.Output().
func parseFlags(fs *flag.FlagSet, args []string) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
}
