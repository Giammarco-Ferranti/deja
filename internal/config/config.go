// Package config persists user-tunable settings (the fuzzy strictness preset
// and whether deja suggests on an empty prompt) in a tiny key=value file. The
// daemon reads it once at startup; the `deja fuzzy` / `deja empty` CLIs write
// to it when the user changes a setting.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/giammarcoferranti/deja/internal/scorer"
)

const (
	fileName = "config"
	fuzzyKey = "fuzzy"
	emptyKey = "empty_suggestions"

	// EnvFuzzy is the environment variable that overrides the persisted
	// fuzzy preset at daemon startup.
	EnvFuzzy = "DEJA_FUZZY"

	// EnvEmpty is the environment variable that overrides the persisted
	// empty-prompt suggestion setting at daemon startup.
	EnvEmpty = "DEJA_EMPTY"
)

// EmptyDefault is whether deja suggests on an empty prompt when nothing else is
// configured. True preserves the historical behavior (predict the next command
// on a fresh prompt).
const EmptyDefault = true

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

// ParseEmpty interprets a user-supplied on/off value for empty-prompt
// suggestions. Empty input yields EmptyDefault (mirroring ParseFuzzy) so that
// the "unset means default" filtering lives in the loaders, not here. Besides
// on/off it accepts show/hide, true/false, 1/0, and yes/no.
func ParseEmpty(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return EmptyDefault, nil
	case "on", "show", "true", "1", "yes":
		return true, nil
	case "off", "hide", "false", "0", "no":
		return false, nil
	default:
		return EmptyDefault, fmt.Errorf("unknown empty setting %q (want on|off)", s)
	}
}

// FormatEmpty renders the setting as the canonical persisted/display token.
func FormatEmpty(show bool) string {
	if show {
		return "on"
	}
	return "off"
}

// LoadEmpty resolves whether empty-prompt suggestions are shown by checking
// DEJA_EMPTY first, then the persisted config file in dir, then falling back to
// EmptyDefault. The second return identifies which source supplied the value.
func LoadEmpty(dir string) (bool, Source) {
	if v := strings.TrimSpace(os.Getenv(EnvEmpty)); v != "" {
		if b, err := ParseEmpty(v); err == nil {
			return b, SourceEnv
		}
	}
	if v, ok := readKey(dir, emptyKey); ok {
		if b, err := ParseEmpty(v); err == nil {
			return b, SourceFile
		}
	}
	return EmptyDefault, SourceDefault
}

// LoadEmptyFile reads only the persisted empty-prompt setting, ignoring
// DEJA_EMPTY. The second return is false when no valid file value is present.
//
// Use this (not LoadEmpty) to diff the user's *persisted* state, e.g. to print
// "empty: on → off"; LoadEmpty resolves env-first and so reports the effective
// value, which lies about what a file change did.
func LoadEmptyFile(dir string) (bool, bool) {
	if v, ok := readKey(dir, emptyKey); ok {
		if b, err := ParseEmpty(v); err == nil {
			return b, true
		}
	}
	return EmptyDefault, false
}

// SaveEmpty atomically persists the empty-prompt suggestion setting to dir.
func SaveEmpty(dir string, show bool) error {
	return writeKey(dir, emptyKey, FormatEmpty(show))
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
	// Owner-only, matching cmd/deja.ensurePrivateDir: this is the same directory
	// as the history database, and writeKey can be the first thing to create it.
	if err := os.MkdirAll(dir, 0o700); err != nil {
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
