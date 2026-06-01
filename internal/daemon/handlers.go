package daemon

import (
	"strings"
	"time"

	"github.com/giammarcoferranti/deja/internal/scorer"
	"github.com/giammarcoferranti/deja/internal/store"
)

const maxAlternatives = 4

// Suggest runs the ranker on the current in-memory snapshot and returns the
// top result plus up to maxAlternatives follow-ups.
func (s *State) Suggest(req SuggestReq, now time.Time) SuggestResp {
	seq, _ := s.SeqCounts(req.Prev)

	// Hold the RLock for the duration of Rank: stats is a slice that Record
	// can grow via append (slice header can tear), and dirCounts is a map
	// whose inner maps Record mutates. Releasing the lock before Rank
	// runs lets concurrent Records race the scorer's reads.
	s.mu.RLock()
	defer s.mu.RUnlock()

	ranked := scorer.Rank(s.stats, req.Buffer, req.Dir, req.Prev, seq, s.dirCounts, now, s.fuzzy)
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
	key := strings.TrimSpace(req.Command)
	if key == "" {
		return nil
	}

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

	// Mirror the command_stats upsert in store.RecordCommand so the next
	// Suggest call ranks against fresh data. Order doesn't matter: scorer.Rank
	// re-sorts by score every call.
	updated := false
	for i := range s.stats {
		if s.stats[i].Command == key {
			s.stats[i].Count++
			s.stats[i].LastUsed = cmd.Timestamp
			updated = true
			break
		}
	}
	if !updated {
		s.stats = append(s.stats, store.CommandStat{
			Command:  key,
			Count:    1,
			LastUsed: cmd.Timestamp,
		})
	}

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

// SetConfig applies runtime settings sent by the CLI. Invalid values are
// rejected and the previous setting is preserved.
func (s *State) SetConfig(req SetConfigReq) SetConfigResp {
	if req.Fuzzy != "" {
		f, err := scorer.ParseFuzzy(req.Fuzzy)
		if err != nil {
			return SetConfigResp{Fuzzy: s.GetFuzzy().String(), Error: err.Error()}
		}
		s.SetFuzzy(f)
	}
	return SetConfigResp{Fuzzy: s.GetFuzzy().String()}
}

// GetConfig returns the current runtime settings.
func (s *State) GetConfig() GetConfigResp {
	return GetConfigResp{Fuzzy: s.GetFuzzy().String()}
}
