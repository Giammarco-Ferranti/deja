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
	// Empty buffer is the "predict the next command on a fresh prompt" case
	// (scorer.computeFuzzy special-cases buffer == "" to a constant score,
	// letting frecency/sequence/dir decide). When the user has opted out of
	// that, short-circuit before the (possibly cold) SeqCounts read. Match the
	// scorer's exact "" test — a whitespace-only buffer is ordinary fuzzy
	// matching, not this prediction, so it must not be suppressed.
	if req.Buffer == "" && !s.ShowEmpty() {
		return SuggestResp{}
	}

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
// A command zsh refuses to remember must not reach either layer. The store
// guard alone is not enough: the in-memory maps below are updated after
// RecordCommand returns, so a store-only fix would leave the command
// suggestable until the daemon is bounced — i.e. ~never.
func (s *State) Record(req RecordReq) error {
	key := strings.TrimSpace(req.Command)
	if store.IgnoredCommand(req.Command) {
		return nil
	}
	// Likewise for the predecessor: an ignored command must not survive as the
	// key of a sequence entry, in SQLite or in seqByPrev.
	if store.IgnoredCommand(req.Prev) {
		req.Prev = ""
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
	// Apply the infallible setting first, so a rejected fuzzy value in the same
	// request can't silently drop a valid empty change on the early return.
	if req.Empty != nil {
		s.SetShowEmpty(*req.Empty)
	}
	if req.Fuzzy != "" {
		f, err := scorer.ParseFuzzy(req.Fuzzy)
		if err != nil {
			return s.setConfigResp(err.Error())
		}
		s.SetFuzzy(f)
	}
	return s.setConfigResp("")
}

func (s *State) setConfigResp(errMsg string) SetConfigResp {
	show := s.ShowEmpty()
	return SetConfigResp{Fuzzy: s.GetFuzzy().String(), Empty: &show, Error: errMsg}
}

// GetConfig returns the current runtime settings.
func (s *State) GetConfig() GetConfigResp {
	show := s.ShowEmpty()
	return GetConfigResp{Fuzzy: s.GetFuzzy().String(), Empty: &show}
}
