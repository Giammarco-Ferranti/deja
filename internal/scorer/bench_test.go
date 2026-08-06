package scorer

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/giammarcoferranti/deja/internal/store"
)

// realisticCandidates builds a candidate set shaped like a real shell history
// rather than like uniform random strings, because the scorer's cost is driven
// by total text length and by how many candidates survive fuzzy matching.
//
// Proportions are sampled from a 3,463-command history: ~9% one-word commands,
// ~26% short, ~41% medium, ~24% long, and a single very large outlier (a 17 KB
// pasted command, which is the kind of thing that actually lands in a history
// file). Roughly one directory per command.
func realisticCandidates(n int) ([]store.CommandStat, map[string]map[string]int) {
	r := rand.New(rand.NewSource(1))
	now := time.Now()

	verbs := []string{"git", "go", "kubectl", "docker", "make", "brew", "rg", "vim", "cd", "ls", "npm", "cargo", "ssh", "curl"}
	tail := []string{
		"status", "commit -m 'fix the thing'", "build ./cmd/deja", "test -race ./...",
		"get pods -n production --context staging", "compose up -d --build",
		"install --cask some-application", "--hidden --glob '!vendor' pattern",
		"internal/scorer/scorer.go", "-la --color=auto", "run build -- --watch",
	}

	out := make([]store.CommandStat, 0, n)
	dirs := map[string]map[string]int{}
	dirNames := []string{"/Users/x/dev/project", "/Users/x", "/tmp", "/Users/x/dev/other"}

	for i := 0; i < n; i++ {
		var cmd string
		switch bucket := r.Intn(100); {
		case bucket < 9:
			cmd = fmt.Sprintf("%s%d", verbs[r.Intn(len(verbs))], i)
		case bucket < 35:
			cmd = fmt.Sprintf("%s %s%d", verbs[r.Intn(len(verbs))], tail[r.Intn(3)], i)
		case bucket < 76:
			cmd = fmt.Sprintf("%s %s %s%d", verbs[r.Intn(len(verbs))], tail[r.Intn(len(tail))], tail[r.Intn(len(tail))], i)
		default:
			cmd = fmt.Sprintf("%s %s %s %s --flag-%d=%x",
				verbs[r.Intn(len(verbs))], tail[r.Intn(len(tail))], tail[r.Intn(len(tail))],
				tail[r.Intn(len(tail))], i, r.Int63())
		}

		out = append(out, store.CommandStat{
			Command:  cmd,
			Count:    r.Intn(500) + 1,
			LastUsed: now.Add(-time.Duration(r.Intn(1000)) * time.Hour),
		})
		dirs[cmd] = map[string]int{dirNames[r.Intn(len(dirNames))]: r.Intn(20) + 1}
	}

	// One pathological entry. A single pasted heredoc or base64 blob in the
	// history makes every keystroke scan it, and histories really do contain one.
	huge := "echo " + strings.Repeat("abcdefghij", 1730)
	out = append(out, store.CommandStat{Command: huge, Count: 1, LastUsed: now.Add(-500 * time.Hour)})
	dirs[huge] = map[string]int{"/tmp": 1}

	return out, dirs
}

func benchRank(b *testing.B, n int, buffer string) {
	candidates, dirCounts := realisticCandidates(n)
	seq := map[string]int{candidates[0].Command: 5, candidates[1].Command: 2}
	now := time.Now()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Rank(candidates, buffer, "/Users/x/dev/project", "ls", seq, dirCounts, now, FuzzySmart)
	}
}

// Buffer length is the dimension that matters: a one-character buffer matches
// almost everything and leaves the most work to do.
func BenchmarkRank_3500_empty(b *testing.B)  { benchRank(b, 3500, "") }
func BenchmarkRank_3500_1char(b *testing.B)  { benchRank(b, 3500, "g") }
func BenchmarkRank_3500_3char(b *testing.B)  { benchRank(b, 3500, "git") }
func BenchmarkRank_3500_6char(b *testing.B)  { benchRank(b, 3500, "git st") }
func BenchmarkRank_20000_3char(b *testing.B) { benchRank(b, 20000, "git") }
