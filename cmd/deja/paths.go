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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return dir, nil
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
