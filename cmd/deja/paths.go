package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func dataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".local", "share", "deja")
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// ensurePrivateDir creates dir as 0700 and re-asserts that mode on every call.
//
// The directory holds the command history database in plaintext, so it is
// owner-only, matching what zsh does with ~/.zsh_history. Per-user data
// directories only isolate users if the permissions enforce it: on macOS every
// account's primary group is staff and home directories are group-traversable,
// so a 0755 directory here leaves the database readable by every other local
// account. (0750 would be no better, for the same reason.)
//
// Two details matter:
//
//   - Parents are created separately at 0755. MkdirAll applies the mode it is
//     given to every level it creates, so a single MkdirAll(dir, 0o700) would
//     also tighten ~/.local and ~/.local/share on a machine where they don't
//     exist yet, directories deja neither owns nor is alone in using.
//   - The Chmod is unconditional because MkdirAll is a no-op on a directory
//     that already exists and never touches its mode. Without it, installs
//     created by earlier versions would stay 0755 forever and only fresh ones
//     would be protected.
func ensurePrivateDir(dir string) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", parent, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	return nil
}

func dbPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "deja.db"), nil
}

// maxSockPath is the longest socket path safe to bind on any platform deja
// releases for. sun_path is 108 bytes on Linux and 104 on macOS, both counting
// the terminating NUL, so 103 is the largest length that works everywhere.
const maxSockPath = 103

// sockPath returns the daemon's socket path, next to the database unless that
// would be too long to bind.
//
// bind(2) rejects an over-long path with EINVAL, which surfaces as
// "bind: invalid argument" -- a message that names neither the limit nor the
// cause. Worse, the daemon is normally spawned with output discarded, so the
// user sees no error at all and deja simply appears inert. A $HOME deep enough
// to trigger it is not exotic; a scratch or workspace path reaches 103 bytes
// easily.
//
// Both the daemon and every client resolve the socket through this function,
// so a deterministic fallback keeps them agreed without extra plumbing.
func sockPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	preferred := filepath.Join(dir, "sock")
	if len(preferred) <= maxSockPath {
		return preferred, nil
	}
	return runtimeSockPath(dir, preferred)
}

// runtimeSockPath places the socket in a short per-user runtime directory.
//
// Only a directory that is already per-user and owner-only qualifies:
// $XDG_RUNTIME_DIR on Linux, and $TMPDIR on macOS, where it is per-user
// (/var/folders/...) rather than shared. A plain /tmp fallback is deliberately
// not attempted -- it is world-writable, and putting the socket there would
// trade a loud failure for the squatting and symlink exposure that the 0700
// parent directory otherwise rules out.
//
// The name is derived from the data directory so two accounts, or two $HOMEs
// belonging to one account, never collide on a shared runtime directory.
func runtimeSockPath(dataDirPath, preferred string) (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" && runtime.GOOS == "darwin" {
		base = os.Getenv("TMPDIR")
	}
	if base == "" {
		return "", fmt.Errorf(
			"socket path %s is %d bytes, over the %d-byte limit for a unix socket, "+
				"and no per-user runtime directory is available to fall back to "+
				"(set XDG_RUNTIME_DIR, or use a shorter HOME)",
			preferred, len(preferred), maxSockPath)
	}

	sum := sha256.Sum256([]byte(dataDirPath))
	dir := filepath.Join(base, "deja")
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, hex.EncodeToString(sum[:4])+".sock")
	if len(path) > maxSockPath {
		return "", fmt.Errorf(
			"socket path %s is %d bytes, over the %d-byte limit for a unix socket, "+
				"and the fallback under %s is too long as well",
			preferred, len(preferred), maxSockPath, base)
	}
	return path, nil
}
