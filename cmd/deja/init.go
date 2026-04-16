package main

import (
	"flag"
	"fmt"
	"os"
)

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	fs.Parse(args)

	shell := "zsh"
	if fs.NArg() > 0 {
		shell = fs.Arg(0)
	}
	if shell != "zsh" {
		fmt.Fprintf(os.Stderr, "deja init: unsupported shell %q (only zsh for now)\n", shell)
		os.Exit(2)
	}
	fmt.Println("# deja zsh integration — coming in Phase 4")
}
