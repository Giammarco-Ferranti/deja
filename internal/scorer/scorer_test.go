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

func findResult(rs []Result, cmd string) *Result {
	for i := range rs {
		if rs[i].Command == cmd {
			return &rs[i]
		}
	}
	return nil
}
