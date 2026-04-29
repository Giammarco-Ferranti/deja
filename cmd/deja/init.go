package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/giammarcoferranti/deja/internal/shell"
)

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.Usage = func() {
		w := os.Stdout
		fs.SetOutput(w)
		fmt.Fprintln(w, "deja init — print the shell integration script")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  deja init [shell]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Writes the integration script to ~/.local/share/deja/init.zsh and prints")
		fmt.Fprintln(w, "a `source` line that loads it. Add this to ~/.zshrc to enable suggestions:")
		fmt.Fprintln(w)
		fmt.Fprintln(w, `  eval "$(deja init zsh)"`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Only zsh is supported today; the [shell] argument defaults to \"zsh\".")
	}
	parseFlags(fs, args)

	sh := "zsh"
	if fs.NArg() > 0 {
		sh = fs.Arg(0)
	}
	if sh != "zsh" {
		fmt.Fprintf(os.Stderr, "deja init: unsupported shell %q (only zsh for now)\n", sh)
		os.Exit(2)
	}

	bin := "deja"
	if exe, err := os.Executable(); err == nil {
		if abs, err := filepath.EvalSymlinks(exe); err == nil {
			bin = abs
		}
	}

	dir, err := dataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja init: %v\n", err)
		os.Exit(1)
	}

	initPath := filepath.Join(dir, "init.zsh")
	script := strings.ReplaceAll(shell.ZshInit(), "{{DEJA_BIN}}", bin)
	if err := os.WriteFile(initPath, []byte(script), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "deja init: write %s: %v\n", initPath, err)
		os.Exit(1)
	}

	fmt.Printf("source '%s'\n", initPath)
}
