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

	got := Rank(candidates, "gi", "/repo", "git add .", seqCounts, dirCounts, now, FuzzyLoose)

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
	got := Rank(candidates, "", "/repo", "git add .", seqCounts, dirCounts, now, FuzzyLoose)

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
		got := Rank(nil, "git", "/repo", "", nil, nil, now, FuzzyLoose)
		if got != nil {
			t.Errorf("want nil, got %+v", got)
		}
	})

	t.Run("buffer matches nothing returns empty slice without panic", func(t *testing.T) {
		candidates := []store.CommandStat{
			{Command: "git status", Count: 1, LastUsed: now},
			{Command: "make build", Count: 1, LastUsed: now},
		}
		got := Rank(candidates, "zzzz", "/repo", "", nil, nil, now, FuzzyLoose)
		if len(got) != 0 {
			t.Errorf("want empty results, got %+v", got)
		}
	})

	t.Run("all-zero counts produce well-defined scores", func(t *testing.T) {
		candidates := []store.CommandStat{
			{Command: "git status", Count: 0, LastUsed: now},
			{Command: "git commit", Count: 0, LastUsed: now},
		}
		got := Rank(candidates, "", "/repo", "", map[string]int{}, map[string]map[string]int{}, now, FuzzyLoose)
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
		got := Rank(candidates, "git", "", "", nil, nil, now, FuzzyLoose)
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
		got := Rank(candidates, "", "", "", nil, nil, now, FuzzyLoose)
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
		gotEmptyDir := Rank(candidates, "", "", "", nil, dirCounts, now, FuzzyLoose)
		gotMatchedDir := Rank(candidates, "", "/repo", "", nil, dirCounts, now, FuzzyLoose)

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
		got := Rank(candidates, "", "", "no-such-prev", map[string]int{}, nil, now, FuzzyLoose)
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
		got := Rank(candidates, "", "", "", nil, nil, now, FuzzyLoose)
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

func TestParseFuzzy(t *testing.T) {
	tests := []struct {
		in      string
		want    Fuzzy
		wantErr bool
	}{
		{"loose", FuzzyLoose, false},
		{"smart", FuzzySmart, false},
		{"tight", FuzzyTight, false},
		{"  Smart  ", FuzzySmart, false},
		{"TIGHT", FuzzyTight, false},
		{"", FuzzyDefault, false},
		{"medium", FuzzyDefault, true},
	}
	for _, tc := range tests {
		got, err := ParseFuzzy(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseFuzzy(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
		if got != tc.want {
			t.Errorf("ParseFuzzy(%q) = %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestNextFuzzy(t *testing.T) {
	for _, tc := range []struct {
		in, want Fuzzy
	}{
		{FuzzyTight, FuzzySmart},
		{FuzzySmart, FuzzyLoose},
		{FuzzyLoose, FuzzyTight},
	} {
		if got := NextFuzzy(tc.in); got != tc.want {
			t.Errorf("NextFuzzy(%v) = %v want %v", tc.in, got, tc.want)
		}
	}

	// Three rotations land back on the starting preset.
	start := FuzzyTight
	if got := NextFuzzy(NextFuzzy(NextFuzzy(start))); got != start {
		t.Errorf("3x NextFuzzy(%v) = %v want %v", start, got, start)
	}
}

func TestPrevFuzzy(t *testing.T) {
	for _, tc := range []struct {
		in, want Fuzzy
	}{
		{FuzzyLoose, FuzzySmart},
		{FuzzySmart, FuzzyTight},
		{FuzzyTight, FuzzyLoose},
	} {
		if got := PrevFuzzy(tc.in); got != tc.want {
			t.Errorf("PrevFuzzy(%v) = %v want %v", tc.in, got, tc.want)
		}
	}

	// Prev undoes Next from every starting preset.
	for _, f := range []Fuzzy{FuzzyTight, FuzzySmart, FuzzyLoose} {
		if got := PrevFuzzy(NextFuzzy(f)); got != f {
			t.Errorf("PrevFuzzy(NextFuzzy(%v)) = %v want %v", f, got, f)
		}
	}
}

func TestFuzzy_String(t *testing.T) {
	for _, tc := range []struct {
		f    Fuzzy
		want string
	}{
		{FuzzyLoose, "loose"},
		{FuzzySmart, "smart"},
		{FuzzyTight, "tight"},
	} {
		if got := tc.f.String(); got != tc.want {
			t.Errorf("Fuzzy(%d).String() = %q want %q", tc.f, got, tc.want)
		}
	}
}

func TestRank_GapFilter(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	candidates := []store.CommandStat{
		// gco gap pattern in each (smart cap=4, loose cap=8, tight cap=1):
		{Command: "gco"},                     // gaps: 0, 0     — all presets pass
		{Command: "g.co"},                    // gaps: 1, 0     — all presets pass
		{Command: "git co main"},             // g(0),c(4),o(5) — gaps: 3, 0 — smart + loose
		{Command: "git checkout main"},       // g(0),c(4),o(9) — gaps: 3, 4 — smart + loose
		{Command: "g.....c.....o"},           // g(0),c(6),o(12)— gaps: 5, 5 — loose only
		{Command: "g........c........o"},     // gaps: 8, 8     — loose only, at the edge
		{Command: "g..........c..........o"}, // gaps: 10, 10   — none pass
	}

	hasCmd := func(rs []Result, cmd string) bool {
		for _, r := range rs {
			if r.Command == cmd {
				return true
			}
		}
		return false
	}

	t.Run("tight only keeps gap<=1", func(t *testing.T) {
		got := Rank(candidates, "gco", "", "", nil, nil, now, FuzzyTight)
		if !hasCmd(got, "gco") || !hasCmd(got, "g.co") {
			t.Errorf("tight: expected gco and g.co, got %+v", got)
		}
		for _, bad := range []string{"git co main", "git checkout main", "g.....c.....o"} {
			if hasCmd(got, bad) {
				t.Errorf("tight: did not expect %q in results, got %+v", bad, got)
			}
		}
	})

	t.Run("smart keeps gap<=4 (canonical gco→git checkout works)", func(t *testing.T) {
		got := Rank(candidates, "gco", "", "", nil, nil, now, FuzzySmart)
		for _, want := range []string{"gco", "g.co", "git co main", "git checkout main"} {
			if !hasCmd(got, want) {
				t.Errorf("smart: expected %q in results, got %+v", want, got)
			}
		}
		for _, bad := range []string{"g.....c.....o", "g........c........o", "g..........c..........o"} {
			if hasCmd(got, bad) {
				t.Errorf("smart: did not expect %q in results, got %+v", bad, got)
			}
		}
	})

	t.Run("loose keeps gap<=8", func(t *testing.T) {
		got := Rank(candidates, "gco", "", "", nil, nil, now, FuzzyLoose)
		for _, want := range []string{"gco", "g.co", "git checkout main", "g.....c.....o", "g........c........o"} {
			if !hasCmd(got, want) {
				t.Errorf("loose: expected %q in results, got %+v", want, got)
			}
		}
		if hasCmd(got, "g..........c..........o") {
			t.Errorf("loose: did not expect very-wide gap match, got %+v", got)
		}
	})

	t.Run("single-char buffer bypasses gap filter", func(t *testing.T) {
		// One letter has no gap to measure; every match must pass even on tight.
		got := Rank(candidates, "g", "", "", nil, nil, now, FuzzyTight)
		if len(got) != len(candidates) {
			t.Errorf("single-char tight: expected all %d to match, got %d: %+v",
				len(candidates), len(got), got)
		}
	})

	t.Run("empty buffer bypasses gap filter", func(t *testing.T) {
		got := Rank(candidates, "", "", "", nil, nil, now, FuzzyTight)
		if len(got) != len(candidates) {
			t.Errorf("empty tight: expected all %d to match, got %d: %+v",
				len(candidates), len(got), got)
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

// TestApplyDirAffinity_EquivalentToRank is the whole justification for the
// function existing: folding affinity in afterwards must land on exactly the
// scores and order Rank produces when given dirCounts up front. If that ever
// stops holding — say because directory affinity gains a normalisation across
// candidates, the way fuzzy and frecency have — then cmd/deja/query.go's
// fallback silently starts ranking differently from the daemon.
func TestApplyDirAffinity_EquivalentToRank(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)

	candidates := []store.CommandStat{
		{Command: "git status", Count: 40, LastUsed: now.Add(-time.Hour)},
		{Command: "git stash", Count: 12, LastUsed: now.Add(-48 * time.Hour)},
		{Command: "git commit -m", Count: 30, LastUsed: now.Add(-2 * time.Hour)},
		{Command: "go test ./...", Count: 25, LastUsed: now.Add(-30 * time.Minute)},
		{Command: "grep -r", Count: 3, LastUsed: now.Add(-200 * time.Hour)},
		{Command: "make build", Count: 8, LastUsed: now.Add(-6 * time.Hour)},
	}
	dirCounts := map[string]map[string]int{
		"git status":    {"/repo": 30, "/other": 10},
		"git stash":     {"/other": 12},
		"git commit -m": {"/repo": 30},
		"go test ./...": {"/repo": 5, "/elsewhere": 20},
		"make build":    {},
	}
	seq := map[string]int{"go test ./...": 4, "git status": 1}

	for _, buffer := range []string{"", "g", "gi", "git", "git s", "go t", "make", "zzz"} {
		for _, dir := range []string{"/repo", "/other", "/elsewhere", "/unseen", ""} {
			t.Run(buffer+"|"+dir, func(t *testing.T) {
				want := Rank(candidates, buffer, dir, "prev", seq, dirCounts, now, FuzzySmart)

				got := Rank(candidates, buffer, dir, "prev", seq, nil, now, FuzzySmart)
				got = ApplyDirAffinity(got, dir, dirCounts)

				if len(got) != len(want) {
					t.Fatalf("len: got %d, want %d", len(got), len(want))
				}
				for i := range want {
					if got[i].Command != want[i].Command {
						t.Errorf("position %d: got %q, want %q\n got: %v\nwant: %v",
							i, got[i].Command, want[i].Command, got, want)
					}
					if math.Abs(got[i].Score-want[i].Score) > 1e-12 {
						t.Errorf("%q score: got %v, want %v", got[i].Command, got[i].Score, want[i].Score)
					}
				}
			})
		}
	}
}

// A command missing from dirCounts must score zero affinity, not be dropped —
// that is what lets the fallback fetch affinities for a shortlist and leave the
// tail in place.
func TestApplyDirAffinity_PartialCountsKeepTail(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	candidates := []store.CommandStat{
		{Command: "alpha", Count: 10, LastUsed: now},
		{Command: "beta", Count: 10, LastUsed: now},
		{Command: "gamma", Count: 10, LastUsed: now},
	}

	full := Rank(candidates, "", "/repo", "", nil, nil, now, FuzzySmart)
	partial := ApplyDirAffinity(full, "/repo", map[string]map[string]int{
		"beta": {"/repo": 5},
	})

	if len(partial) != 3 {
		t.Fatalf("got %d results, want 3 — commands without affinity data were dropped", len(partial))
	}
	if partial[0].Command != "beta" {
		t.Errorf("got %q first, want beta promoted by its affinity", partial[0].Command)
	}
}
