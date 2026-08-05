package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/giammarcoferranti/deja/internal/config"
	"github.com/giammarcoferranti/deja/internal/daemon"
	"github.com/giammarcoferranti/deja/internal/scorer"
	"github.com/giammarcoferranti/deja/internal/store"
)

const (
	dialTimeout = 50 * time.Millisecond
	readTimeout = 150 * time.Millisecond

	// affinityShortlistCap bounds how many commands get a directory-affinity
	// lookup when the score distribution is too flat for the cutoff to bite —
	// which is the common case for a one- or two-character buffer, where the
	// cutoff admits nearly every candidate.
	//
	// Sized from measurement against a 110k-row history: a lookup costs ~0.02ms,
	// so five hundred of them is ~10ms, against ~70ms for all 3,400 candidates.
	//
	// The cost of the cap is that a command ranked below it cannot be lifted into
	// view by affinity alone. Measured over 120 buffer/directory pairs against the
	// uncapped ranking: the primary suggestion was identical in all 120, and 8
	// differed somewhere in the four alternatives — always a command with strong
	// affinity to that directory but little frecency, which is exactly the
	// population the cap excludes.
	affinityShortlistCap = 500

	// maxFallbackAlternatives mirrors the daemon's maxAlternatives.
	maxFallbackAlternatives = 4
)

func runQuery(args []string) {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	buffer := fs.String("buffer", "", "current zsh buffer")
	dir := fs.String("dir", "", "current working directory")
	prev := fs.String("prev", "", "previously executed command")
	format := fs.String("format", "plain", "output format: plain|lines|json")
	asJSON := fs.Bool("json", false, "shorthand for --format json")
	fs.Usage = func() {
		w := os.Stdout
		fs.SetOutput(w)
		fmt.Fprintln(w, "deja query — ask the daemon for an inline suggestion")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  deja query [flags]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Flags:")
		fs.PrintDefaults()
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Examples:")
		fmt.Fprintln(w, `  deja query --buffer "git st" --dir "$PWD" --prev "git status"`)
		fmt.Fprintln(w, "  deja query --buffer dc --json")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Falls back to a direct SQLite read if the daemon is unavailable.")
	}
	parseFlags(fs, args)

	if *asJSON {
		*format = "json"
	}

	resp, err := dialAndSuggest(*buffer, *dir, *prev)
	if err != nil {
		// Silent fallback — the zsh widget runs this on every keystroke and
		// must never see an error exit. Empty output means "no suggestion".
		resp = fallbackSuggest(*buffer, *dir, *prev)
	}

	switch *format {
	case "json":
		_ = json.NewEncoder(os.Stdout).Encode(resp)
	case "lines":
		if resp.Suggestion != "" {
			cands := append([]string{resp.Suggestion}, resp.Alternatives...)
			fmt.Print(strings.Join(cands, "\x1f"))
		}
	default:
		if resp.Suggestion != "" {
			fmt.Println(resp.Suggestion)
		}
	}
}

func dialAndSuggest(buffer, dir, prev string) (daemon.SuggestResp, error) {
	sock, err := sockPath()
	if err != nil {
		return daemon.SuggestResp{}, err
	}

	conn, err := net.DialTimeout("unix", sock, dialTimeout)
	if err != nil {
		return daemon.SuggestResp{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(readTimeout))

	payload, err := json.Marshal(daemon.SuggestReq{Buffer: buffer, Dir: dir, Prev: prev})
	if err != nil {
		return daemon.SuggestResp{}, err
	}
	if err := json.NewEncoder(conn).Encode(daemon.Envelope{Type: "suggest", Payload: payload}); err != nil {
		return daemon.SuggestResp{}, err
	}

	var resp daemon.SuggestResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return daemon.SuggestResp{}, err
	}
	return resp, nil
}

// fallbackSuggest runs the same scoring path the daemon would, but against
// a freshly opened SQLite connection. Slower (cold reads) but keeps the
// shell responsive when the daemon is down.
// shortlistForAffinity picks the commands worth a directory-affinity lookup
// from a ranking computed without one. ranked must be sorted by score
// descending, as Rank leaves it.
//
// Everything scoring within scorer.MaxDirBoost of the fifth place is included:
// those are exactly the candidates affinity could still lift into view, since
// the five already there can only gain score themselves. The cap is a guard
// against a flat score distribution — an empty buffer gives every candidate the
// same fuzzy score — turning "shortlist" back into "the whole history".
func shortlistForAffinity(ranked []scorer.Result) []string {
	visible := maxFallbackAlternatives + 1
	if len(ranked) <= visible {
		names := make([]string, len(ranked))
		for i, r := range ranked {
			names[i] = r.Command
		}
		return names
	}

	cutoff := ranked[visible-1].Score - scorer.MaxDirBoost

	names := make([]string, 0, visible)
	for _, r := range ranked {
		if r.Score < cutoff || len(names) >= affinityShortlistCap {
			break
		}
		names = append(names, r.Command)
	}
	return names
}

func fallbackSuggest(buffer, dir, prev string) daemon.SuggestResp {
	// Mirror the daemon's empty-prompt gate (handlers.go Suggest): when the
	// user has opted out of predictions on a fresh prompt, don't even open the
	// database. Match the daemon's exact "" test, not a trimmed one.
	if buffer == "" {
		if cfgDir, err := dataDir(); err == nil {
			if show, _ := config.LoadEmpty(cfgDir); !show {
				return daemon.SuggestResp{}
			}
		}
	}

	path, err := dbPath()
	if err != nil {
		return daemon.SuggestResp{}
	}
	db, err := store.OpenReader(path)
	if err != nil {
		return daemon.SuggestResp{}
	}

	stats, err := store.GetCommandStats(db)
	if err != nil || len(stats) == 0 {
		return daemon.SuggestResp{}
	}

	seq, _ := store.GetSequenceCounts(db, prev)

	fuzziness := scorer.FuzzyDefault
	if cfgDir, err := dataDir(); err == nil {
		fuzziness, _ = config.LoadFuzzy(cfgDir)
	}
	now := time.Now()

	// Directory affinity is the one signal needing a per-command lookup, and
	// fetching it for every command in the history is what made this path cost
	// hundreds of milliseconds per keystroke. So rank without it first, then fold
	// it in for the handful of commands that could plausibly win.
	//
	// Folding in afterwards rather than re-ranking a shortlist matters: fuzzy and
	// frecency are min-max normalised across the candidate set, so re-running
	// Rank over 50 survivors would renormalise every signal over a different
	// population and reorder results that had nothing to do with affinity.
	// ApplyDirAffinity instead adds the same per-command term Rank would have
	// added, to the scores Rank already computed over the full set.
	//
	// The shortlist is chosen to be provably sufficient rather than guessed at.
	// Affinity only ever adds, and adds at most scorer.MaxDirBoost, so a
	// candidate can reach the visible results only if its pre-affinity score is
	// within that of the current fifth place — everything below cannot get there
	// however strong its affinity. Looking those up would be work whose outcome
	// is already decided.
	ranked := scorer.Rank(stats, buffer, dir, prev, seq, nil, now, fuzziness)
	if len(ranked) == 0 {
		return daemon.SuggestResp{}
	}

	names := shortlistForAffinity(ranked)

	dirCounts := make(map[string]map[string]int, len(names))
	for _, name := range names {
		// A failed lookup costs this command its affinity, one signal of four,
		// which still leaves a usable ranking. Nothing here is worth abandoning
		// the whole suggestion over.
		if dc, err := store.GetDirCountsForCommand(db, name); err == nil {
			dirCounts[name] = dc
		}
	}
	ranked = scorer.ApplyDirAffinity(ranked, dir, dirCounts)

	alts := make([]string, 0, maxFallbackAlternatives)
	for i := 1; i < len(ranked) && len(alts) < maxFallbackAlternatives; i++ {
		alts = append(alts, ranked[i].Command)
	}
	return daemon.SuggestResp{Suggestion: ranked[0].Command, Alternatives: alts}
}
