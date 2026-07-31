package scorer

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/giammarcoferranti/deja/internal/store"
	"github.com/sahilm/fuzzy"
)

type Result struct {
	Command string
	Score   float64
}

// Fuzzy is the strictness preset that controls how far apart typed letters may
// be in a candidate command. Smaller gaps = tighter matches.
type Fuzzy int

const (
	FuzzyLoose Fuzzy = iota
	FuzzySmart
	FuzzyTight
)

// FuzzyDefault is the preset used when nothing else is configured.
const FuzzyDefault = FuzzySmart

func (f Fuzzy) String() string {
	switch f {
	case FuzzyLoose:
		return "loose"
	case FuzzyTight:
		return "tight"
	default:
		return "smart"
	}
}

// NextFuzzy returns the next preset in the strictness ramp:
// tight → smart → loose → tight. Used by `deja fuzzy cycle` and the
// Shift+→ zsh keybinding to step through presets without typing a name.
func NextFuzzy(f Fuzzy) Fuzzy {
	switch f {
	case FuzzyTight:
		return FuzzySmart
	case FuzzySmart:
		return FuzzyLoose
	case FuzzyLoose:
		return FuzzyTight
	default:
		return FuzzyDefault
	}
}

// PrevFuzzy is the inverse of NextFuzzy: loose → smart → tight → loose.
// Used by `deja fuzzy back` and the Shift+← keybinding.
func PrevFuzzy(f Fuzzy) Fuzzy {
	switch f {
	case FuzzyLoose:
		return FuzzySmart
	case FuzzySmart:
		return FuzzyTight
	case FuzzyTight:
		return FuzzyLoose
	default:
		return FuzzyDefault
	}
}

// ParseFuzzy turns a user-supplied preset name into a Fuzzy value.
// Empty input is treated as the default.
func ParseFuzzy(s string) (Fuzzy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return FuzzyDefault, nil
	case "loose":
		return FuzzyLoose, nil
	case "smart":
		return FuzzySmart, nil
	case "tight":
		return FuzzyTight, nil
	default:
		return FuzzyDefault, fmt.Errorf("unknown fuzzy preset %q (want loose|smart|tight)", s)
	}
}

// maxGap returns the maximum number of unmatched characters allowed between
// two consecutive typed letters in a candidate. Values are tuned so `smart`
// handles canonical shell fuzzy cases (`gco` → `git checkout`, gap 4) and
// `tight` requires near-adjacency.
func maxGap(f Fuzzy) int {
	switch f {
	case FuzzyLoose:
		return 8
	case FuzzyTight:
		return 1
	default:
		return 4
	}
}

const halfLife = 7 * 24 * time.Hour

const fuzzyWeight = 1.0
const seqWeight = 0.5
const frecencyWeight = 0.4
const dirWeight = 0.3

// MaxDirBoost is the most that directory affinity can add to a score: affinity
// is a ratio in [0,1], so the weighted term tops out at dirWeight. Callers that
// rank before knowing affinities use this to bound which candidates could still
// change the outcome once affinities arrive (see ApplyDirAffinity).
const MaxDirBoost = dirWeight

func Rank(
	candidates []store.CommandStat,
	buffer, dir, prev string,
	seqCounts map[string]int,
	dirCounts map[string]map[string]int,
	now time.Time,
	fuzziness Fuzzy,
) []Result {
	n := len(candidates)
	if n == 0 {
		return nil
	}

	fuzzyScores := computeFuzzy(candidates, buffer, fuzziness)
	frecencyScores := computeFrecency(candidates, now)
	dirScores := computeDirAffinity(candidates, dir, dirCounts)
	seqScores := computeSequence(candidates, seqCounts)

	out := make([]Result, 0, n)

	for i, c := range candidates {
		// Skip candidates that don't match the buffer at all.
		if buffer != "" && fuzzyScores[i] == 0 {
			continue
		}

		final := fuzzyWeight*fuzzyScores[i] + seqWeight*seqScores[i] + frecencyWeight*frecencyScores[i] + dirWeight*dirScores[i]

		out = append(out, Result{Command: c.Command, Score: final})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })

	return out

}

// ApplyDirAffinity folds directory affinity into an existing ranking and
// re-sorts, for callers that cannot afford to look up affinities before ranking.
//
// This is exact, not an approximation, and only because of an asymmetry in the
// signals: fuzzy and frecency are min-max normalised across the candidate set,
// so they cannot be recomputed over a subset without changing every score,
// whereas directory affinity is purely per-command — dc[dir] over that
// command's own total. Adding its weighted term afterwards therefore lands on
// exactly the scores Rank would have produced with dirCounts passed in.
//
// Commands absent from dirCounts score zero affinity, which is also what Rank
// does for a command with no recorded directories. That is what lets a caller
// fetch affinities for a shortlist and leave the tail alone.
func ApplyDirAffinity(results []Result, dir string, dirCounts map[string]map[string]int) []Result {
	if dir == "" || len(dirCounts) == 0 {
		return results
	}

	for i := range results {
		dc := dirCounts[results[i].Command]
		if len(dc) == 0 {
			continue
		}
		total := 0
		for _, n := range dc {
			total += n
		}
		if total == 0 {
			continue
		}
		results[i].Score += dirWeight * (float64(dc[dir]) / float64(total))
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })

	return results
}

func computeFuzzy(candidates []store.CommandStat, buffer string, fuzziness Fuzzy) []float64 {
	scores := make([]float64, len(candidates))

	if buffer == "" {
		for i := range scores {
			scores[i] = 1
		}
		return scores
	}

	list := make([]string, len(candidates))
	for i, c := range candidates {
		list[i] = c.Command
	}

	matches := fuzzy.Find(buffer, list)
	if len(matches) == 0 {
		return scores
	}

	// Drop matches whose typed letters are spread further apart than the
	// configured preset allows. Single-character buffers have no gap to
	// measure, so they bypass the filter.
	gapCap := maxGap(fuzziness)
	filterByGap := len(buffer) > 1

	matched := make([]bool, len(candidates))
	raw := make([]int, len(candidates))
	first := true
	var min, max int
	for _, m := range matches {
		if filterByGap && maxConsecutiveGap(m.MatchedIndexes) > gapCap {
			continue
		}
		raw[m.Index] = m.Score
		matched[m.Index] = true
		if first {
			min, max = m.Score, m.Score
			first = false
			continue
		}
		if m.Score < min {
			min = m.Score
		}
		if m.Score > max {
			max = m.Score
		}
	}
	if first {
		// Every match was filtered out by the gap cap.
		return scores
	}

	span := max - min
	for i := range raw {
		if !matched[i] {
			continue
		}
		if span == 0 {
			scores[i] = 1
		} else {
			scores[i] = float64(raw[i]-min) / float64(span)
		}
		// Keep a small positive floor so the buffer-match filter treats
		// weak-but-present matches as matches rather than dropping them.
		if scores[i] < 1e-6 {
			scores[i] = 1e-6
		}
	}

	return scores
}

func computeFrecency(candidates []store.CommandStat, now time.Time) []float64 {
	raw := make([]float64, len(candidates))
	var max float64

	for i, c := range candidates {
		dt := now.Sub(c.LastUsed).Seconds()
		if dt < 0 {
			dt = 0
		}
		recency := math.Exp(-dt / halfLife.Seconds())
		raw[i] = math.Log1p(float64(c.Count)) * recency
		if raw[i] > max {
			max = raw[i]
		}
	}

	if max == 0 {
		return raw
	}

	for i := range raw {
		raw[i] /= max
	}

	return raw
}

func computeDirAffinity(candidates []store.CommandStat, dir string, dirCounts map[string]map[string]int) []float64 {
	scores := make([]float64, len(candidates))

	if dir == "" {
		return scores
	}

	for i, c := range candidates {
		dc := dirCounts[c.Command]
		if len(dc) == 0 {
			continue
		}

		total := 0
		for _, n := range dc {
			total += n
		}

		if total == 0 {
			continue
		}
		scores[i] = float64(dc[dir]) / float64(total)
	}

	return scores
}

func computeSequence(candidates []store.CommandStat, seqCounts map[string]int) []float64 {
	scores := make([]float64, len(candidates))
	max := 0

	for _, n := range seqCounts {
		if n > max {
			max = n
		}
	}

	if max == 0 {
		return scores
	}

	for i, c := range candidates {
		if n, ok := seqCounts[c.Command]; ok {
			scores[i] = float64(n) / float64(max)
		}
	}

	return scores
}

// maxConsecutiveGap returns the largest run of unmatched characters between
// consecutive matched positions. Returns 0 when fewer than two matches exist.
func maxConsecutiveGap(indexes []int) int {
	if len(indexes) < 2 {
		return 0
	}
	worst := 0
	for i := 1; i < len(indexes); i++ {
		gap := indexes[i] - indexes[i-1] - 1
		if gap > worst {
			worst = gap
		}
	}
	return worst
}
