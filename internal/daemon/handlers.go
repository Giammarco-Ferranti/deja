package daemon

import (
	"time"

	"github.com/giammarcoferranti/deja/internal/scorer"
	"github.com/giammarcoferranti/deja/internal/store"
)

const maxAlternatives = 4

// Suggest runs the ranker on the current in-memory snapshot and returns the
// top result plus up to maxAlternatives follow-ups.
func (s *State) Suggest(req SuggestReq, now time.Time) SuggestResp {
	seq, _ := s.SeqCounts(req.Prev)

	s.mu.RLock()
	stats := s.stats
	dirCounts := s.dirCounts
	s.mu.RUnlock()

	ranked := scorer.Rank(stats, req.Buffer, req.Dir, req.Prev, seq, dirCounts, now)
	if len(ranked) == 0 {
		return SuggestResp{}
	}

	alts := make([]string, 0, maxAlternatives)
	for i := 1; i < len(ranked) && len(alts) < maxAlternatives; i++ {
		alts = append(alts, ranked[i].Command)
	}
	return SuggestResp{Suggestion: ranked[0].Command, Alternatives: alts}
}

// Record persists a newly executed command in two layers:
//  1. SQLite (durable, survives daemon restarts)
//  2. In-memory state (hot path — next suggest call sees the new data)
//
// Both are required. Skipping (1) loses data on restart; skipping (2) means
// new directory/sequence signal is invisible until the daemon is bounced —
// and daemons survive across shell sessions, so that would be ~never.
func (s *State) Record(req RecordReq) error {
	cmd := store.Command{
		Command:    req.Command,
		Directory:  req.Dir,
		Timestamp:  time.Now(),
		ExitCode:   req.ExitCode,
		DurationMS: req.DurationMS,
		SessionID:  req.SessionID,
	}

	if err := store.RecordCommand(s.db, cmd, req.Prev); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if req.Dir != "" {
		dc, ok := s.dirCounts[req.Command]
		if !ok {
			dc = make(map[string]int)
			s.dirCounts[req.Command] = dc
		}
		dc[req.Dir]++
	}

	if req.Prev != "" {
		// Only mutate if we've already cached this prev — otherwise the next
		// SeqCounts call will pull a fresh row from SQLite that already
		// reflects this write.
		if seq, ok := s.seqByPrev[req.Prev]; ok {
			seq[req.Command]++
		}
	}

	return nil
}

// Ping is the liveness check used by `deja ping` and by the zsh init script
// to decide whether to spawn a new daemon.
func (s *State) Ping() PingResp {
	return PingResp{Pong: true}
}
