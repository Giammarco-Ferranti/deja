package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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

	// Baked into the script so the shell can check daemon liveness with a stat on the
	// socket instead of launching the binary for `ping` on every shell startup.
	sock, err := sockPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja init: %v\n", err)
		os.Exit(1)
	}

	initPath := filepath.Join(dir, "init.zsh")
	if err := writeInitScript(initPath, bin, sock); err != nil {
		fmt.Fprintf(os.Stderr, "deja init: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("source '%s'\n", initPath)
}

// binaryStamp identifies a build of the deja binary by its stat metadata, in the
// same `size-mtime-inode` form the generated script rebuilds with `zstat -H`.
//
// A version string cannot do this job: main.version is hardcoded to "dev" and
// only .goreleaser.yaml stamps a real value at release, so every local build
// reports the same string. Stat metadata distinguishes them.
func binaryStamp(bin string) string {
	fi, err := os.Stat(bin)
	if err != nil {
		return ""
	}
	var inode uint64
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		inode = uint64(st.Ino)
	}
	return fmt.Sprintf("%d-%d-%d", fi.Size(), fi.ModTime().Unix(), inode)
}

// writeInitScript renders the integration script and installs it atomically.
//
// The atomicity is not a nicety. `deja init` runs in the background whenever a
// shell notices the script is stale, so "one shell rewrites init.zsh while
// another is sourcing it" is a routine event rather than a race you have to go
// looking for — and a shell that reads a half-written file gets a broken ZLE.
// os.WriteFile truncates in place, which is exactly the window to avoid; a
// rename over the target is atomic within a directory. internal/config's
// writeKey already does this for the same reason.
func writeInitScript(initPath, bin, sock string) error {
	script := strings.ReplaceAll(shell.ZshInit(), "{{DEJA_BIN}}", bin)
	script = strings.ReplaceAll(script, "{{DEJA_SOCK}}", sock)
	script = strings.ReplaceAll(script, "{{DEJA_BIN_STAMP}}", binaryStamp(bin))

	dir := filepath.Dir(initPath)
	tmp, err := os.CreateTemp(dir, "init.zsh.tmp-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpPath, err)
	}
	// CreateTemp makes the file 0600; the script is not secret and a shell may
	// source it as another user's login shell would.
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, initPath); err != nil {
		return fmt.Errorf("install %s: %w", initPath, err)
	}
	return nil
}
