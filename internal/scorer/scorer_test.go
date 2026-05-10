package scorer

import (
	"math"
	"testing"
	"time"

	"github.com/giammarcoferranti/deja/internal/store"
)

func TestRank(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	candidates := []store.CommandStat{
		{Command: "git commit -m", Count: 50, LastUsed: now.Add(-1 * time.Hour)},
		{Command: "git status", Count: 30, LastUsed: now.Add(-2 * time.Hour)},
		{Command: "go test ./...", Count: 10, LastUsed: now.Add(-24 * time.Hour)},
	}

	seqCounts := map[string]int{"git commit -m": 40, "git status": 5}
	dirCounts := map[string]map[string]int{
		"git commit -m": {"/repo": 45, "/other": 5},
	}

	got := Rank(candidates, "gi", "/repo", "git add .", seqCounts, dirCounts, now)

	// "go test ./..." has no 'i' after 'g', fuzzy filters it out.
	for _, r := range got {
		if r.Command == "go test ./..." {
			t.Errorf("did not expect 'go test ./...' in results, got %+v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(got), got)
	}
}

func TestRank_ExactScores(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	candidates := []store.CommandStat{
		{Command: "git commit -m", Count: 50, LastUsed: now.Add(-1 * time.Hour)},
		{Command: "git status", Count: 30, LastUsed: now.Add(-2 * time.Hour)},
		{Command: "go test ./...", Count: 10, LastUsed: now.Add(-24 * time.Hour)},
	}

	seqCounts := map[string]int{
		"git commit -m": 40,
		"git status":    5,
	}
	dirCounts := map[string]map[string]int{
		"git commit -m": {"/repo": 45, "/other": 5},
		"git status":    {"/repo": 20, "/other": 10},
		"go test ./...": {"/repo": 5, "/other": 5},
	}

	// Empty buffer: fuzzy = 1 for every candidate, so final score equals
	// 1.0 + weighted non-fuzzy signals. Makes implied-fuzzy checks tight.
	got := Rank(candidates, "", "/repo", "git add .", seqCounts, dirCounts, now)

	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d: %+v", len(got), got)
	}

	halfLifeSec := (7 * 24 * time.Hour).Seconds()
	frRaw := []float64{
		math.Log1p(50) * math.Exp(-3600/halfLifeSec),
		math.Log1p(30) * math.Exp(-7200/halfLifeSec),
		math.Log1p(10) * math.Exp(-86400/halfLifeSec),
	}
	frMax := frRaw[0]
	frNorm := []float64{frRaw[0] / frMax, frRaw[1] / frMax, frRaw[2] / frMax}

	dirNorm := []float64{45.0 / 50.0, 20.0 / 30.0, 5.0 / 10.0}
	seqMax := 40.0
	seqNorm := []float64{40.0 / seqMax, 5.0 / 40.0, 0.0}

	for i, want := range []struct {
		cmd          string
		fr, dir, seq float64
	}{
		{"git commit -m", frNorm[0], dirNorm[0], seqNorm[0]},
		{"git status", frNorm[1], dirNorm[1], seqNorm[1]},
		{"go test ./...", frNorm[2], dirNorm[2], seqNorm[2]},
	} {
		r := findResult(got, want.cmd)
		if r == nil {
			t.Errorf("missing result for %q", want.cmd)
			continue
		}
		nonFuzzy := 0.4*want.fr + 0.3*want.dir + 0.5*want.seq
		wantScore := 1.0 + nonFuzzy // fuzzy=1 for empty buffer
		if math.Abs(r.Score-wantScore) > 1e-9 {
			t.Errorf("%s[%d]: score=%v want=%v (nonFuzzy=%v)",
				want.cmd, i, r.Score, wantScore, nonFuzzy)
		}
	}

	if got[0].Command != "git commit -m" {
		t.Errorf("expected 'git commit -m' first, got %+v", got)
	}
}

func TestRank_EdgeCases(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	t.Run("empty candidates returns nil", func(t *testing.T) {
		got := Rank(nil, "git", "/repo", "", nil, nil, now)
		if got != nil {
			t.Errorf("want nil, got %+v", got)
		}
	})

	t.Run("buffer matches nothing returns empty slice without panic", func(t *testing.T) {
		candidates := []store.CommandStat{
			{Command: "git status", Count: 1, LastUsed: now},
			{Command: "make build", Count: 1, LastUsed: now},
		}
		got := Rank(candidates, "zzzz", "/repo", "", nil, nil, now)
		if len(got) != 0 {
			t.Errorf("want empty results, got %+v", got)
		}
	})

	t.Run("all-zero counts produce well-defined scores", func(t *testing.T) {
		candidates := []store.CommandStat{
			{Command: "git status", Count: 0, LastUsed: now},
			{Command: "git commit", Count: 0, LastUsed: now},
		}
		got := Rank(candidates, "", "/repo", "", map[string]int{}, map[string]map[string]int{}, now)
		if len(got) != 2 {
			t.Fatalf("want 2 results, got %+v", got)
		}
		for _, r := range got {
			if math.IsNaN(r.Score) || math.IsInf(r.Score, 0) {
				t.Errorf("score for %q is not finite: %v", r.Command, r.Score)
			}
		}
	})

	t.Run("single candidate with non-empty buffer applies span=0 floor", func(t *testing.T) {
		candidates := []store.CommandStat{
			{Command: "git status", Count: 1, LastUsed: now},
		}
		got := Rank(candidates, "git", "", "", nil, nil, now)
		if len(got) != 1 {
			t.Fatalf("want 1 result, got %+v", got)
		}
		// When fuzzy span==0 the candidate's fuzzy score collapses to 1 in
		// computeFuzzy, so the final composite must be at least the fuzzy
		// weight contribution (1.0 * 1).
		if got[0].Score < 1.0 {
			t.Errorf("score=%v, want >= 1.0 (fuzzy floor)", got[0].Score)
		}
	})

	t.Run("clock skew (LastUsed in the future) clamps recency to 1", func(t *testing.T) {
		future := now.Add(48 * time.Hour)
		past := now.Add(-1 * time.Hour)
		candidates := []store.CommandStat{
			{Command: "future-cmd", Count: 5, LastUsed: future},
			{Command: "past-cmd", Count: 5, LastUsed: past},
		}
		got := Rank(candidates, "", "", "", nil, nil, now)
		if len(got) != 2 {
			t.Fatalf("want 2 results, got %+v", got)
		}
		// future-cmd has dt<0 (clamped to 0 → recency=1), past-cmd has dt>0
		// (recency<1). With identical Count, future must outrank past.
		future0 := findResult(got, "future-cmd")
		past0 := findResult(got, "past-cmd")
		if future0 == nil || past0 == nil {
			t.Fatalf("missing results: %+v", got)
		}
		if future0.Score < past0.Score {
			t.Errorf("future-cmd score=%v should be >= past-cmd score=%v", future0.Score, past0.Score)
		}
	})

	t.Run("empty dir produces zero directory affinity", func(t *testing.T) {
		candidates := []store.CommandStat{
			{Command: "git status", Count: 1, LastUsed: now},
		}
		dirCounts := map[string]map[string]int{
			"git status": {"/repo": 99},
		}
		// dir="" — computeDirAffinity should return zeros, so the dir signal
		// contributes nothing. Compare against a run with a matching dir.
		gotEmptyDir := Rank(candidates, "", "", "", nil, dirCounts, now)
		gotMatchedDir := Rank(candidates, "", "/repo", "", nil, dirCounts, now)

		if len(gotEmptyDir) != 1 || len(gotMatchedDir) != 1 {
			t.Fatalf("want 1 result each, got %+v / %+v", gotEmptyDir, gotMatchedDir)
		}
		if gotMatchedDir[0].Score <= gotEmptyDir[0].Score {
			t.Errorf("matched-dir score=%v must exceed empty-dir score=%v",
				gotMatchedDir[0].Score, gotEmptyDir[0].Score)
		}
	})

	t.Run("missing prev gives zero sequence contribution", func(t *testing.T) {
		candidates := []store.CommandStat{
			{Command: "git status", Count: 1, LastUsed: now},
		}
		got := Rank(candidates, "", "", "no-such-prev", map[string]int{}, nil, now)
		if len(got) != 1 {
			t.Fatalf("want 1 result, got %+v", got)
		}
		// Only frecency contributes; max==count for one entry → frecency
		// normalized to 1, weighted by 0.4. Score should equal 1.0 (fuzzy) + 0.4.
		want := 1.0 + 0.4
		if math.Abs(got[0].Score-want) > 1e-9 {
			t.Errorf("score=%v, want %v", got[0].Score, want)
		}
	})

	t.Run("very large counts do not overflow", func(t *testing.T) {
		candidates := []store.CommandStat{
			{Command: "git status", Count: 1 << 30, LastUsed: now},
			{Command: "git commit", Count: 1 << 28, LastUsed: now},
		}
		got := Rank(candidates, "", "", "", nil, nil, now)
		if len(got) != 2 {
			t.Fatalf("want 2 results, got %+v", got)
		}
		for _, r := range got {
			if math.IsNaN(r.Score) || math.IsInf(r.Score, 0) {
				t.Errorf("score for %q is not finite: %v", r.Command, r.Score)
			}
		}
	})
}

func findResult(rs []Result, cmd string) *Result {
	for i := range rs {
		if rs[i].Command == cmd {
			return &rs[i]
		}
	}
	return nil
}
