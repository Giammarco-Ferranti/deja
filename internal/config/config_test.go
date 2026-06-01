package config

import (
	"os"
	"testing"

	"github.com/giammarcoferranti/deja/internal/scorer"
)

func TestLoadFuzzy_DefaultWhenEmpty(t *testing.T) {
	t.Setenv(EnvFuzzy, "")
	dir := t.TempDir()

	got, src := LoadFuzzy(dir)
	if got != scorer.FuzzyDefault || src != SourceDefault {
		t.Errorf("LoadFuzzy(empty) = (%v, %v), want (%v, %v)", got, src, scorer.FuzzyDefault, SourceDefault)
	}
}

func TestLoadFuzzy_FromFile(t *testing.T) {
	t.Setenv(EnvFuzzy, "")
	dir := t.TempDir()

	if err := SaveFuzzy(dir, scorer.FuzzyTight); err != nil {
		t.Fatalf("SaveFuzzy: %v", err)
	}

	got, src := LoadFuzzy(dir)
	if got != scorer.FuzzyTight || src != SourceFile {
		t.Errorf("LoadFuzzy(file=tight) = (%v, %v), want (%v, %v)", got, src, scorer.FuzzyTight, SourceFile)
	}
}

func TestLoadFuzzy_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFuzzy(dir, scorer.FuzzyTight); err != nil {
		t.Fatalf("SaveFuzzy: %v", err)
	}
	t.Setenv(EnvFuzzy, "loose")

	got, src := LoadFuzzy(dir)
	if got != scorer.FuzzyLoose || src != SourceEnv {
		t.Errorf("LoadFuzzy(env=loose, file=tight) = (%v, %v), want (%v, %v)", got, src, scorer.FuzzyLoose, SourceEnv)
	}
}

func TestLoadFuzzy_InvalidEnvFallsThrough(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFuzzy(dir, scorer.FuzzyTight); err != nil {
		t.Fatalf("SaveFuzzy: %v", err)
	}
	t.Setenv(EnvFuzzy, "bananas")

	got, src := LoadFuzzy(dir)
	if got != scorer.FuzzyTight || src != SourceFile {
		t.Errorf("LoadFuzzy(env=invalid) should fall through to file, got (%v, %v)", got, src)
	}
}

func TestLoadFuzzyFile_IgnoresEnv(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFuzzy(dir, scorer.FuzzyTight); err != nil {
		t.Fatalf("SaveFuzzy: %v", err)
	}
	t.Setenv(EnvFuzzy, "loose")

	got, hadFile := LoadFuzzyFile(dir)
	if !hadFile || got != scorer.FuzzyTight {
		t.Errorf("LoadFuzzyFile(env=loose, file=tight) = (%v, %v), want (tight, true)", got, hadFile)
	}
}

func TestLoadFuzzyFile_MissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvFuzzy, "tight")

	got, hadFile := LoadFuzzyFile(dir)
	if hadFile || got != scorer.FuzzyDefault {
		t.Errorf("LoadFuzzyFile(no file) = (%v, %v), want (default, false)", got, hadFile)
	}
}

func TestSaveFuzzy_OverwritesPreviousValue(t *testing.T) {
	t.Setenv(EnvFuzzy, "")
	dir := t.TempDir()

	if err := SaveFuzzy(dir, scorer.FuzzyTight); err != nil {
		t.Fatalf("SaveFuzzy tight: %v", err)
	}
	if err := SaveFuzzy(dir, scorer.FuzzyLoose); err != nil {
		t.Fatalf("SaveFuzzy loose: %v", err)
	}

	got, _ := LoadFuzzy(dir)
	if got != scorer.FuzzyLoose {
		t.Errorf("after overwrite want loose, got %v", got)
	}
}

func TestSaveFuzzy_PreservesUnrelatedLines(t *testing.T) {
	t.Setenv(EnvFuzzy, "")
	dir := t.TempDir()
	cfg := dir + "/config"

	if err := os.WriteFile(cfg, []byte("# header comment\nunrelated=value\nfuzzy=loose\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SaveFuzzy(dir, scorer.FuzzyTight); err != nil {
		t.Fatalf("SaveFuzzy: %v", err)
	}

	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(data)
	if !contains(s, "unrelated=value") {
		t.Errorf("unrelated line dropped: %q", s)
	}
	if !contains(s, "fuzzy=tight") {
		t.Errorf("fuzzy not updated to tight: %q", s)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
