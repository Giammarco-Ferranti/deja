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

func TestLoadEmpty_DefaultWhenEmpty(t *testing.T) {
	t.Setenv(EnvEmpty, "")
	dir := t.TempDir()

	got, src := LoadEmpty(dir)
	if got != EmptyDefault || src != SourceDefault {
		t.Errorf("LoadEmpty(empty) = (%v, %v), want (%v, %v)", got, src, EmptyDefault, SourceDefault)
	}
}

func TestLoadEmpty_FromFile(t *testing.T) {
	t.Setenv(EnvEmpty, "")
	dir := t.TempDir()

	if err := SaveEmpty(dir, false); err != nil {
		t.Fatalf("SaveEmpty: %v", err)
	}

	got, src := LoadEmpty(dir)
	if got != false || src != SourceFile {
		t.Errorf("LoadEmpty(file=off) = (%v, %v), want (false, %v)", got, src, SourceFile)
	}
}

func TestLoadEmpty_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	if err := SaveEmpty(dir, true); err != nil {
		t.Fatalf("SaveEmpty: %v", err)
	}
	t.Setenv(EnvEmpty, "off")

	got, src := LoadEmpty(dir)
	if got != false || src != SourceEnv {
		t.Errorf("LoadEmpty(env=off, file=on) = (%v, %v), want (false, %v)", got, src, SourceEnv)
	}
}

func TestLoadEmpty_InvalidEnvFallsThrough(t *testing.T) {
	dir := t.TempDir()
	if err := SaveEmpty(dir, false); err != nil {
		t.Fatalf("SaveEmpty: %v", err)
	}
	t.Setenv(EnvEmpty, "bananas")

	got, src := LoadEmpty(dir)
	if got != false || src != SourceFile {
		t.Errorf("LoadEmpty(env=invalid) should fall through to file, got (%v, %v)", got, src)
	}
}

func TestLoadEmptyFile_IgnoresEnv(t *testing.T) {
	dir := t.TempDir()
	if err := SaveEmpty(dir, false); err != nil {
		t.Fatalf("SaveEmpty: %v", err)
	}
	t.Setenv(EnvEmpty, "on")

	got, hadFile := LoadEmptyFile(dir)
	if !hadFile || got != false {
		t.Errorf("LoadEmptyFile(env=on, file=off) = (%v, %v), want (false, true)", got, hadFile)
	}
}

func TestLoadEmptyFile_MissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvEmpty, "off")

	got, hadFile := LoadEmptyFile(dir)
	if hadFile || got != EmptyDefault {
		t.Errorf("LoadEmptyFile(no file) = (%v, %v), want (default, false)", got, hadFile)
	}
}

func TestSaveEmpty_OverwritesPreviousValue(t *testing.T) {
	t.Setenv(EnvEmpty, "")
	dir := t.TempDir()

	if err := SaveEmpty(dir, false); err != nil {
		t.Fatalf("SaveEmpty off: %v", err)
	}
	if err := SaveEmpty(dir, true); err != nil {
		t.Fatalf("SaveEmpty on: %v", err)
	}

	got, _ := LoadEmpty(dir)
	if got != true {
		t.Errorf("after overwrite want on, got %v", got)
	}
}

// TestSaveEmpty_CoexistsWithFuzzy verifies the two settings share the config
// file without clobbering each other (writeKey rewrites only its own key).
func TestSaveEmpty_CoexistsWithFuzzy(t *testing.T) {
	t.Setenv(EnvEmpty, "")
	t.Setenv(EnvFuzzy, "")
	dir := t.TempDir()
	cfg := dir + "/config"

	if err := os.WriteFile(cfg, []byte("# header comment\nunrelated=value\nfuzzy=loose\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SaveEmpty(dir, false); err != nil {
		t.Fatalf("SaveEmpty: %v", err)
	}

	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(data)
	if !contains(s, "unrelated=value") {
		t.Errorf("unrelated line dropped: %q", s)
	}
	if !contains(s, "fuzzy=loose") {
		t.Errorf("fuzzy line dropped: %q", s)
	}
	if !contains(s, "empty_suggestions=off") {
		t.Errorf("empty_suggestions not written: %q", s)
	}
}

func TestParseEmpty_Variants(t *testing.T) {
	trueCases := []string{"on", "ON", " on ", "show", "true", "1", "yes"}
	for _, in := range trueCases {
		got, err := ParseEmpty(in)
		if err != nil || got != true {
			t.Errorf("ParseEmpty(%q) = (%v, %v), want (true, nil)", in, got, err)
		}
	}

	falseCases := []string{"off", "OFF", "hide", "false", "0", "no"}
	for _, in := range falseCases {
		got, err := ParseEmpty(in)
		if err != nil || got != false {
			t.Errorf("ParseEmpty(%q) = (%v, %v), want (false, nil)", in, got, err)
		}
	}

	// Empty input yields the default with no error (mirrors ParseFuzzy).
	if got, err := ParseEmpty(""); err != nil || got != EmptyDefault {
		t.Errorf("ParseEmpty(\"\") = (%v, %v), want (%v, nil)", got, err, EmptyDefault)
	}

	if _, err := ParseEmpty("maybe"); err == nil {
		t.Error("ParseEmpty(maybe) should error")
	}
}

func TestFormatEmpty(t *testing.T) {
	if FormatEmpty(true) != "on" {
		t.Errorf("FormatEmpty(true) = %q, want on", FormatEmpty(true))
	}
	if FormatEmpty(false) != "off" {
		t.Errorf("FormatEmpty(false) = %q, want off", FormatEmpty(false))
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
