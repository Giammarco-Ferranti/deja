package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestReleasePiped_ReaderSeesEOF is the regression test for #101.
//
// The daemon holds every descriptor it inherits for its whole lifetime. When
// one of them is the write end of a command substitution's pipe, the shell
// reading that pipe blocks until the daemon exits, which for a daemon means
// "not today". The reproduction is exactly this: hold the write end, try to
// read, and see whether the read ever returns.
func TestReleasePiped_ReaderSeesEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	if got := releasePiped(w); got != 1 {
		t.Fatalf("releasePiped released %d descriptors, want 1", got)
	}

	// The write end is now /dev/null and the pipe has no writer left, so a read
	// must return EOF rather than parking forever.
	done := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 1))
		done <- err
	}()

	select {
	case err := <-done:
		if err != io.EOF {
			t.Errorf("read returned %v, want EOF", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reader is still blocked: a daemon spawned inside $(...) would hang its caller forever (#101)")
	}
}

// A regular file must survive untouched, or `deja daemon 2>log` silently stops
// collecting the logs it was pointed at.
func TestReleasePiped_LeavesRegularFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()

	if got := releasePiped(f); got != 0 {
		t.Fatalf("releasePiped released %d descriptors for a regular file, want 0", got)
	}

	const line = "still logging\n"
	if _, err := f.WriteString(line); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != line {
		t.Errorf("log file holds %q, want %q; the descriptor was redirected away", got, line)
	}
}

// isPipe drives the decision, so pin it on both kinds of descriptor directly.
func TestIsPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()

	if !isPipe(r) || !isPipe(w) {
		t.Error("isPipe does not recognise an anonymous pipe")
	}
	if isPipe(f) {
		t.Error("isPipe treats a regular file as a pipe")
	}
}
