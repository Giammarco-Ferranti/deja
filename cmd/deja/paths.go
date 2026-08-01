package main

import (
	"fmt"
	"os"
	"path/filepath"
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

func sockPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sock"), nil
}
