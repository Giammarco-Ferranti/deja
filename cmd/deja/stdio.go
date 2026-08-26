package main

import (
	"fmt"
	"os"
)

// releasePipedStdio points any of the daemon's standard descriptors that is an
// inherited pipe at /dev/null, and reports how many it released.
//
// The daemon outlives the shell that spawned it, so every descriptor it keeps,
// it keeps for hours. When one of them is the write end of a command
// substitution's pipe, the shell reading that pipe never sees EOF and waits
// forever: the hang in #101, which presents as a plugin-manager update that
// never returns while the daemon itself sits there perfectly healthy, and as a
// reader blocked in anon_pipe_read with no child process left to blame.
//
// The init script redirects on every spawn path it owns, but it can only cover
// the paths it owns. A plugin-manager hook, a CI step, or a hand-written
// `deja daemon &` hands the daemon whatever the caller happened to have open,
// and an init.zsh cached from an older release keeps doing whatever it did.
// Releasing the pipe here covers all of them at once, without depending on
// every caller getting its redirections right.
//
// A terminal is deliberately left alone, so running `deja daemon` by hand stays
// readable, and so is a regular file, so `deja daemon 2>log` still collects
// logs. Only a pipe can deadlock a reader, so only a pipe is released.
func releasePipedStdio() int { return releasePiped(os.Stdin, os.Stdout, os.Stderr) }

func releasePiped(files ...*os.File) int {
	var piped []*os.File
	for _, f := range files {
		if isPipe(f) {
			piped = append(piped, f)
		}
	}
	if len(piped) == 0 {
		return 0
	}

	// Say so before letting go. Someone who piped the daemon's output on
	// purpose would otherwise just watch it stop, with nothing to search for.
	fmt.Fprintln(os.Stderr, "deja daemon: stdio is a pipe and the daemon is long-lived; releasing it "+
		"so the reader is not held open for the daemon's lifetime (redirect to a file to keep these logs)")

	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		// Nothing safe to do: closing the descriptor outright would leave fd 1
		// or 2 free for the next open to claim, and a stray write would then
		// land in the database. Holding the pipe is the lesser failure.
		return 0
	}
	defer null.Close()

	released := 0
	for _, f := range piped {
		if err := dupOnto(int(null.Fd()), int(f.Fd())); err == nil {
			released++
		}
	}
	return released
}

// isPipe reports whether f is a pipe. Stat reports both anonymous pipes (a
// command substitution, a shell pipeline) and named ones as ModeNamedPipe.
func isPipe(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeNamedPipe != 0
}
