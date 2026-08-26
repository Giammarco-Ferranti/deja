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

	fuzzyScores, matched := computeFuzzy(candidates, buffer, fuzziness)

	// Frecency stays a full pass because it is normalised by the maximum across
	// every candidate, matched or not — the divisor cannot be known without
	// visiting all of them. Sequence and directory affinity have no such
	// dependency: each is a function of one command alone (sequence divides by a
	// maximum taken over seqCounts, not over candidates). So they are computed
	// inline, only for candidates that survived the fuzzy filter, which for a
	// six-character buffer is single digits out of thousands.
	frecencyScores := computeFrecency(candidates, now)
	seqMax := 0
	for _, n := range seqCounts {
		if n > seqMax {
			seqMax = n
		}
	}

	// Sized to what will actually survive the fuzzy filter. Reserving room for
	// every candidate costs 84KB a call at this history size to hold, typically,
	// a few dozen results.
	out := make([]Result, 0, matched)

	for i, c := range candidates {
		// Skip candidates that don't match the buffer at all.
		if buffer != "" && fuzzyScores[i] == 0 {
			continue
		}

		final := fuzzyWeight*fuzzyScores[i] + frecencyWeight*frecencyScores[i]
		if seqMax > 0 {
			if n, ok := seqCounts[c.Command]; ok {
				final += seqWeight * (float64(n) / float64(seqMax))
			}
		}
		final += dirWeight * dirAffinity(c.Command, dir, dirCounts)

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
		results[i].Score += dirWeight * dirAffinity(results[i].Command, dir, dirCounts)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })

	return results
}

// candidateSource lets the fuzzy matcher read commands straight out of the
// candidate slice, implementing fuzzy.Source.
type candidateSource []store.CommandStat

func (c candidateSource) String(i int) string { return c[i].Command }
func (c candidateSource) Len() int            { return len(c) }

// anchor restricts candidates to continuations of the buffer: fuzzy matching
// alone reads `cd ` as a subsequence of `claude --resume`. Zero value = no-op.
type anchor struct {
	buffer string
	prefix string // tier 1: candidate must start with this and continue past it
	head   string // tier 2: candidate's first word must equal this
}

// A candidate equal to the buffer never survives: its ghost text is empty.
func (a anchor) allows(cmd string) bool {
	switch {
	case a.prefix != "":
		return len(cmd) > len(a.prefix) && strings.HasPrefix(cmd, a.prefix)
	case a.head != "":
		return cmd != a.buffer && commandWord(cmd) == a.head
	default:
		return true
	}
}

// newAnchor takes the tightest anchor the candidates support, else none, which
// leaves `gco` → `git checkout` to fuzzy matching. Tier 2 skips a candidate
// equal to the buffer, or an alias like `gco` would anchor to itself.
func newAnchor(candidates []store.CommandStat, buffer string) anchor {
	if buffer == "" {
		return anchor{}
	}

	head := commandWord(buffer)
	headKnown := false

	for _, c := range candidates {
		if len(c.Command) > len(buffer) && strings.HasPrefix(c.Command, buffer) {
			return anchor{buffer: buffer, prefix: buffer}
		}
		if !headKnown && head != "" && c.Command != buffer && commandWord(c.Command) == head {
			headKnown = true
		}
	}

	if headKnown {
		return anchor{buffer: buffer, head: head}
	}
	return anchor{}
}

func commandWord(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

// computeFuzzy returns the per-candidate fuzzy score and how many candidates
// scored above zero, so the caller can size its result slice to the survivors
// rather than to the whole history.
func computeFuzzy(candidates []store.CommandStat, buffer string, fuzziness Fuzzy) ([]float64, int) {
	scores := make([]float64, len(candidates))

	if buffer == "" {
		for i := range scores {
			scores[i] = 1
		}
		return scores, len(candidates)
	}

	// FindFrom reads commands in place; fuzzy.Find would need a []string copy of
	// the entire history built fresh on every keystroke. NoSort because the
	// library's ordering is by its own match score, which we then throw away —
	// every match is rescored below and re-sorted by the composite.
	matches := fuzzy.FindFromNoSort(buffer, candidateSource(candidates))
	if len(matches) == 0 {
		return scores, 0
	}

	// Drop matches whose typed letters are spread further apart than the
	// configured preset allows. Single-character buffers have no gap to
	// measure, so they bypass the filter.
	gapCap := maxGap(fuzziness)
	filterByGap := len(buffer) > 1

	// Before the normalisation below, so anchored-out commands score zero.
	anch := newAnchor(candidates, buffer)

	matched := make([]bool, len(candidates))
	raw := make([]int, len(candidates))
	first := true
	var min, max int
	for _, m := range matches {
		if !anch.allows(candidates[m.Index].Command) {
			continue
		}
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
		// Every match was filtered out by the anchor or the gap cap.
		return scores, 0
	}

	span := max - min
	survivors := 0
	for i := range raw {
		if !matched[i] {
			continue
		}
		survivors++
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

	return scores, survivors
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

// dirAffinity is the share of a command's recorded runs that happened in dir.
// Per-command by construction: no normalisation across candidates, which is
// what lets both Rank and ApplyDirAffinity add it independently.
func dirAffinity(command, dir string, dirCounts map[string]map[string]int) float64 {
	if dir == "" {
		return 0
	}
	dc := dirCounts[command]
	if len(dc) == 0 {
		return 0
	}
	total := 0
	for _, n := range dc {
		total += n
	}
	if total == 0 {
		return 0
	}
	return float64(dc[dir]) / float64(total)
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
