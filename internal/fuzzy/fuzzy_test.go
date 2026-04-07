package fuzzy

import "testing"

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern   string
		candidate string
		wantZero  bool
	}{
		{"gco", "git checkout", false}, // subsequence match
		{"gco", "git commit", false},   // g, c, o all present in order
		{"xyz", "git checkout", true},  // no x in candidate
		{"gc", "grep -c foo", false},   // subsequence match
		{"", "git checkout", false},    // empty pattern always matches
		{"git checkout", "gco", true},  // pattern longer than candidate
	}

	for _, tt := range tests {
		score := Match(tt.pattern, tt.candidate)
		t.Logf("Match(%q, %q) = %.4f", tt.pattern, tt.candidate, score)
		if tt.wantZero && score != 0 {
			t.Errorf("Match(%q, %q) = %.2f, want 0", tt.pattern, tt.candidate, score)
		}
		if !tt.wantZero && score == 0 {
			t.Errorf("Match(%q, %q) = 0, want > 0", tt.pattern, tt.candidate)
		}
	}
}
