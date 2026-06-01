// Package config persists user-tunable settings (currently just the fuzzy
// strictness preset) in a tiny key=value file. The daemon reads it once at
// startup; the `deja fuzzy` CLI writes to it when the user changes the preset.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/giammarcoferranti/deja/internal/scorer"
)

const (
	fileName = "config"
	fuzzyKey = "fuzzy"

	// EnvFuzzy is the environment variable that overrides the persisted
	// fuzzy preset at daemon startup.
	EnvFuzzy = "DEJA_FUZZY"
)

// Source indicates where a resolved value came from.
type Source int

const (
	SourceDefault Source = iota
	SourceFile
	SourceEnv
)

// LoadFuzzy resolves the fuzzy preset by checking DEJA_FUZZY first, then the
// persisted config file in dir, then falling back to FuzzyDefault. The second
// return value identifies which source supplied the value.
func LoadFuzzy(dir string) (scorer.Fuzzy, Source) {
	if v := strings.TrimSpace(os.Getenv(EnvFuzzy)); v != "" {
		if f, err := scorer.ParseFuzzy(v); err == nil {
			return f, SourceEnv
		}
	}
	if v, ok := readKey(dir, fuzzyKey); ok {
		if f, err := scorer.ParseFuzzy(v); err == nil {
			return f, SourceFile
		}
	}
	return scorer.FuzzyDefault, SourceDefault
}

// LoadFuzzyFile reads only the persisted fuzzy preset, ignoring DEJA_FUZZY.
// The second return is false when no valid file value is present.
//
// Use this when you need to diff the user's *persisted* state (e.g. to print
// "fuzzy: tight → smart"); LoadFuzzy resolves env-first and so reports the
// effective value, which lies about what the file change did.
func LoadFuzzyFile(dir string) (scorer.Fuzzy, bool) {
	if v, ok := readKey(dir, fuzzyKey); ok {
		if f, err := scorer.ParseFuzzy(v); err == nil {
			return f, true
		}
	}
	return scorer.FuzzyDefault, false
}

// SaveFuzzy atomically persists the fuzzy preset to the config file in dir.
func SaveFuzzy(dir string, f scorer.Fuzzy) error {
	return writeKey(dir, fuzzyKey, f.String())
}

func readKey(dir, key string) (string, bool) {
	if dir == "" {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		return strings.TrimSpace(v), true
	}
	return "", false
}

func writeKey(dir, key, value string) error {
	if dir == "" {
		return errors.New("config dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, fileName)

	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if k, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == key {
				continue
			}
			lines = append(lines, line)
		}
	}
	lines = append(lines, key+"="+value)
	out := strings.Join(lines, "\n") + "\n"

	tmp, err := os.CreateTemp(dir, fileName+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
