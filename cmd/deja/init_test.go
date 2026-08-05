package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestWriteInitScript_AtomicUnderConcurrentReads is the reason writeInitScript
// renames instead of truncating in place. A shell notices a stale script and
// regenerates it in the background, so "one shell rewrites init.zsh while
// another is sourcing it" is routine — and a shell that reads a half-written
// file gets a broken ZLE, silently.
//
// os.WriteFile would fail this: readers would observe truncated content.
func TestWriteInitScript_AtomicUnderConcurrentReads(t *testing.T) {
	dir := t.TempDir()
	initPath := filepath.Join(dir, "init.zsh")

	if err := writeInitScript(initPath, "/usr/bin/true", "/tmp/sock"); err != nil {
		t.Fatalf("initial write: %v", err)
	}
	full, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	wantLen := len(full)
	if wantLen == 0 {
		t.Fatal("script is empty")
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Readers: every observation must be a whole script, never a prefix.
	var bad int
	var mu sync.Mutex
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				got, err := os.ReadFile(initPath)
				if err != nil {
					continue // a rename can briefly race an open on some systems
				}
				if len(got) != wantLen || !strings.HasSuffix(string(got), "\n") {
					mu.Lock()
					bad++
					mu.Unlock()
				}
			}
		}()
	}

	// Writers, hammering the same target.
	var writers sync.WaitGroup
	for i := 0; i < 3; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for j := 0; j < 60; j++ {
				if err := writeInitScript(initPath, "/usr/bin/true", "/tmp/sock"); err != nil {
					t.Errorf("concurrent write: %v", err)
					return
				}
			}
		}()
	}

	writers.Wait()
	close(stop)
	wg.Wait()

	if bad != 0 {
		t.Errorf("%d reads saw a partially written script", bad)
	}
}

// The generated script must carry a stamp the shell can compare against, or the
// self-heal silently never fires.
func TestWriteInitScript_BakesBinaryStamp(t *testing.T) {
	dir := t.TempDir()
	initPath := filepath.Join(dir, "init.zsh")

	bin := filepath.Join(dir, "deja")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	if err := writeInitScript(initPath, bin, "/tmp/sock"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	stamp := binaryStamp(bin)
	if stamp == "" {
		t.Fatal("binaryStamp returned empty for an existing file")
	}
	if !strings.Contains(string(got), `_DEJA_BIN_STAMP="`+stamp+`"`) {
		t.Errorf("script does not carry the stamp %q", stamp)
	}
	if strings.Contains(string(got), "{{") {
		t.Error("script still contains an unsubstituted placeholder")
	}

	// Mode matters: CreateTemp makes 0600, and a script nobody can read is a
	// broken shell.
	fi, err := os.Stat(initPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
	}
}
