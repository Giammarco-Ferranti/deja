package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// shortTempDir returns a temporary directory short enough to leave room for a
// unix socket beneath it.
//
// t.TempDir embeds the test's own name in the path. Under macOS's $TMPDIR
// (/var/folders/<...>/T/, 49 bytes before anything else) a descriptive test
// name pushes the result past sun_path, so these fixtures failed on the fixture
// rather than on the code. os.MkdirTemp omits the name, which is what
// internal/daemon's shortSockPath already relies on.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "deja")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// longHome returns a $HOME deep enough that the socket beside the database
// would exceed sun_path, built under root so it is still a real directory.
func longHome(t *testing.T, root string) string {
	t.Helper()
	home := root
	for len(filepath.Join(home, ".local", "share", "deja", "sock")) <= maxSockPath {
		home = filepath.Join(home, "deeper-than-you-would-think")
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir long home: %v", err)
	}
	return home
}

// TestSockPath_ShortHomeKeepsSocketBesideDatabase pins the ordinary case: the
// fallback must not fire for a normal $HOME, or every existing install would
// silently move its socket and lose its running daemon.
func TestSockPath_ShortHomeKeepsSocketBesideDatabase(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_RUNTIME_DIR", shortTempDir(t))

	// Name the fixture if it ever grows too long itself, rather than failing the
	// assertion below and pointing at the code.
	want := filepath.Join(home, ".local", "share", "deja", "sock")
	if len(want) > maxSockPath {
		t.Fatalf("fixture home is too long (%d bytes) to exercise the ordinary case", len(want))
	}

	got, err := sockPath()
	if err != nil {
		t.Fatalf("sockPath: %v", err)
	}
	if got != want {
		t.Errorf("sockPath = %q, want %q", got, want)
	}
}

// TestSockPath_LongHomeProducesABindableSocket is the regression test for the
// bug. Asserting on the length alone would pass for a path that still cannot be
// bound, so this binds it — the operation that actually failed with EINVAL.
func TestSockPath_LongHomeProducesABindableSocket(t *testing.T) {
	t.Setenv("HOME", longHome(t, shortTempDir(t)))
	t.Setenv("XDG_RUNTIME_DIR", shortTempDir(t))

	got, err := sockPath()
	if err != nil {
		t.Fatalf("sockPath: %v", err)
	}
	if len(got) > maxSockPath {
		t.Fatalf("sockPath returned %d bytes (%q), over the %d-byte limit", len(got), got, maxSockPath)
	}

	l, err := net.Listen("unix", got)
	if err != nil {
		t.Fatalf("the returned path could not be bound, which is the whole bug: %v", err)
	}
	l.Close()
}

// The fallback directory holds a socket into the command-history daemon, so it
// must be owner-only like the data directory it stands in for.
func TestSockPath_FallbackDirectoryIsOwnerOnly(t *testing.T) {
	t.Setenv("HOME", longHome(t, shortTempDir(t)))
	t.Setenv("XDG_RUNTIME_DIR", shortTempDir(t))

	got, err := sockPath()
	if err != nil {
		t.Fatalf("sockPath: %v", err)
	}
	info, err := os.Stat(filepath.Dir(got))
	if err != nil {
		t.Fatalf("stat fallback dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("fallback directory has mode %04o, want 0700", perm)
	}
}

// Two accounts sharing one runtime directory must not land on the same socket,
// or one daemon answers the other's shells out of the wrong history.
func TestSockPath_FallbackIsPerDataDirectory(t *testing.T) {
	runtimeDir := shortTempDir(t)

	paths := make([]string, 2)
	for i := range paths {
		t.Setenv("HOME", longHome(t, shortTempDir(t)))
		t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
		p, err := sockPath()
		if err != nil {
			t.Fatalf("sockPath: %v", err)
		}
		paths[i] = p
	}
	if paths[0] == paths[1] {
		t.Errorf("two different homes share socket %q", paths[0])
	}
}

// The same home must resolve to the same socket on every call, or the daemon
// and its clients would disagree about where to meet.
func TestSockPath_FallbackIsStable(t *testing.T) {
	t.Setenv("HOME", longHome(t, shortTempDir(t)))
	t.Setenv("XDG_RUNTIME_DIR", shortTempDir(t))

	first, err := sockPath()
	if err != nil {
		t.Fatalf("sockPath: %v", err)
	}
	second, err := sockPath()
	if err != nil {
		t.Fatalf("sockPath: %v", err)
	}
	if first != second {
		t.Errorf("sockPath is not stable: %q then %q", first, second)
	}
}

// With no runtime directory to fall back to there is nothing safe left to try,
// but the error must still name the limit and the way out — the whole point is
// that "bind: invalid argument" did neither.
func TestSockPath_WithoutRuntimeDirExplainsItself(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Setenv("TMPDIR", "")
	}
	t.Setenv("HOME", longHome(t, shortTempDir(t)))
	t.Setenv("XDG_RUNTIME_DIR", "")

	_, err := sockPath()
	if err == nil {
		t.Fatal("expected an error when no runtime directory is available")
	}
	for _, want := range []string{"unix socket", "XDG_RUNTIME_DIR", "shorter HOME"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
