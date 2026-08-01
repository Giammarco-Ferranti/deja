package main

import (
	"os"
	"path/filepath"
	"testing"
)

// referenceMode creates a directory with the given mode and reports what it
// actually came out as, so assertions account for the ambient umask instead of
// assuming 022.
func referenceMode(t *testing.T, mode os.FileMode) os.FileMode {
	t.Helper()
	ref := filepath.Join(t.TempDir(), "ref")
	if err := os.MkdirAll(ref, mode); err != nil {
		t.Fatalf("mkdir reference: %v", err)
	}
	info, err := os.Stat(ref)
	if err != nil {
		t.Fatalf("stat reference: %v", err)
	}
	return info.Mode().Perm()
}

func TestDataDir_IsOwnerOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := dataDir()
	if err != nil {
		t.Fatalf("dataDir: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("%s has mode %04o, want 0700", dir, perm)
	}
}

// The leaf is private, but the shared XDG parents are not deja's to tighten. A
// single MkdirAll(dir, 0o700) would apply 0700 to every level it creates, so
// this pins the two-step create.
func TestDataDir_DoesNotTightenSharedParents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, err := dataDir(); err != nil {
		t.Fatalf("dataDir: %v", err)
	}

	want := referenceMode(t, 0o755)
	for _, rel := range []string{".local", ".local/share"} {
		p := filepath.Join(home, filepath.FromSlash(rel))
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if perm := info.Mode().Perm(); perm != want {
			t.Errorf("%s has mode %04o, want %04o: deja tightened a directory it does not own", rel, perm, want)
		}
	}
}

// Installs created before this fix are 0755 on disk. MkdirAll is a no-op on an
// existing directory and never touches its mode, so without the explicit Chmod
// those installs would stay world-traversable forever.
func TestDataDir_RepairsExistingLooseDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".local", "share", "deja")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("pre-create: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("pre-chmod: %v", err)
	}

	if _, err := dataDir(); err != nil {
		t.Fatalf("dataDir: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("existing dir was not repaired: mode %04o, want 0700", perm)
	}
}
