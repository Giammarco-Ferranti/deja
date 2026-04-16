package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/giammarcoferranti/deja/internal/daemon"
	"github.com/giammarcoferranti/deja/internal/scorer"
	"github.com/giammarcoferranti/deja/internal/store"
)

const (
	dialTimeout = 50 * time.Millisecond
	readTimeout = 150 * time.Millisecond
)

func runQuery(args []string) {
	fs := flag.NewFlagSet("query", flag.ExitOnError)
	buffer := fs.String("buffer", "", "current zsh buffer")
	dir := fs.String("dir", "", "current working directory")
	prev := fs.String("prev", "", "previously executed command")
	asJSON := fs.Bool("json", false, "emit full response as JSON (default: plain stdout)")
	fs.Parse(args)

	resp, err := dialAndSuggest(*buffer, *dir, *prev)
	if err != nil {
		// Silent fallback — the zsh widget runs this on every keystroke and
		// must never see an error exit. Empty output means "no suggestion".
		resp = fallbackSuggest(*buffer, *dir, *prev)
	}

	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(resp)
		return
	}
	if resp.Suggestion != "" {
		fmt.Println(resp.Suggestion)
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
func fallbackSuggest(buffer, dir, prev string) daemon.SuggestResp {
	path, err := dbPath()
	if err != nil {
		return daemon.SuggestResp{}
	}
	db, err := store.InitDB(path)
	if err != nil {
		return daemon.SuggestResp{}
	}

	stats, err := store.GetCommandStats(db)
	if err != nil || len(stats) == 0 {
		return daemon.SuggestResp{}
	}

	seq, _ := store.GetSequenceCounts(db, prev)

	dirCounts := make(map[string]map[string]int, len(stats))
	for _, s := range stats {
		dc, err := store.GetDirCountsForCommand(db, s.Command)
		if err != nil {
			continue
		}
		dirCounts[s.Command] = dc
	}

	ranked := scorer.Rank(stats, buffer, dir, prev, seq, dirCounts, time.Now())
	if len(ranked) == 0 {
		return daemon.SuggestResp{}
	}

	alts := make([]string, 0, 4)
	for i := 1; i < len(ranked) && len(alts) < 4; i++ {
		alts = append(alts, ranked[i].Command)
	}
	return daemon.SuggestResp{Suggestion: ranked[0].Command, Alternatives: alts}
}
