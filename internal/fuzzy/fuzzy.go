// Package fuzzy provides subsequence-based fuzzy matching for shell commands.
package fuzzy

// Match returns a score in [0.0, 1.0]: ratio of matched runes to candidate length.
// Returns 0 if pattern is not a subsequence of candidate; empty pattern scores 1.0.
func Match(pattern, candidate string) float64 {
	if pattern == "" {
		return 1.0
	}

	p := []rune(pattern)
	c := []rune(candidate)

	// Consume each pattern rune left-to-right through the candidate.
	pi := 0
	for ci := 0; ci < len(c) && pi < len(p); ci++ {
		if p[pi] == c[ci] {
			pi++
		}
	}

	if pi < len(p) {
		return 0 // pattern not a subsequence
	}

	return float64(pi) / float64(len(c))
}
