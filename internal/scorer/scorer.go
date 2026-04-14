package scorer

import (
	"math"
	"sort"
	"time"

	"github.com/giammarcoferranti/deja/internal/store"
	"github.com/sahilm/fuzzy"
)

type Result struct {
	Command string
	Score   float64
}

const halfLife = 7 * 24 * time.Hour

const fuzzyWeight = 1.0
const seqWeight = 0.5
const frecencyWeight = 0.4
const dirWeight = 0.3

func Rank(
	candidates []store.CommandStat,
	buffer, dir, prev string,
	seqCounts map[string]int,
	dirCounts map[string]map[string]int,
	now time.Time,
) []Result {
	n := len(candidates)
	if n == 0 {
		return nil
	}

	fuzzyScores := computeFuzzy(candidates, buffer)
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

func computeFuzzy(candidates []store.CommandStat, buffer string) []float64 {
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

	matched := make([]bool, len(candidates))
	raw := make([]int, len(candidates))
	min, max := matches[0].Score, matches[0].Score
	for _, m := range matches {
		raw[m.Index] = m.Score
		matched[m.Index] = true
		if m.Score < min {
			min = m.Score
		}
		if m.Score > max {
			max = m.Score
		}
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
