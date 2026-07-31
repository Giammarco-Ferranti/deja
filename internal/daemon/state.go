package daemon

import (
	"sync"

	"github.com/giammarcoferranti/deja/internal/scorer"
	"github.com/giammarcoferranti/deja/internal/store"
	"gorm.io/gorm"
)

// defaultShowEmpty is the in-memory default for whether the daemon suggests on
// an empty prompt. It must track config.EmptyDefault, but the daemon stays
// persistence-agnostic (it never imports config — the CLI loads the persisted
// value and pushes it via SetShowEmpty at startup), mirroring how fuzzy
// defaults to scorer.FuzzyDefault rather than anything in config. This literal
// therefore only surfaces in tests that call Load without SetShowEmpty.
const defaultShowEmpty = true

// State is the in-memory snapshot the daemon serves suggestions from.
// Reads are cheap and concurrent; writes (from Record) are brief and rare.
type State struct {
	mu        sync.RWMutex
	db        *gorm.DB
	stats     []store.CommandStat       // every command_stats row, most-used first
	seqByPrev map[string]map[string]int // prev → next → count, filled lazily
	dirCounts map[string]map[string]int // cmd  → dir  → count
	fuzzy     scorer.Fuzzy              // strictness preset for fuzzy matching
	showEmpty bool                      // suggest on an empty prompt?
}

// Load warms state from SQLite: every command_stats row, plus each one's
// directory affinities.
//
// The per-command affinity loop looks like an obvious candidate for a single
// grouped query, and is not: `GROUP BY command, directory` over the whole
// commands table measures 318ms against a 110k-row history, where this loop of
// index seeks measures ~150ms. It is also paid once, at daemon startup, to keep
// the keystroke path free of SQLite entirely — which is the trade that matters.
// The suggestion path that *cannot* afford it is fallbackSuggest, and that one
// scopes the lookup to a shortlist instead (see cmd/deja/query.go).
func Load(db *gorm.DB) (*State, error) {
	stats, err := store.GetCommandStats(db)
	if err != nil {
		return nil, err
	}

	dirCounts := make(map[string]map[string]int, len(stats))
	for _, s := range stats {
		dc, err := store.GetDirCountsForCommand(db, s.Command)
		if err != nil {
			return nil, err
		}
		dirCounts[s.Command] = dc
	}

	return &State{
		db:        db,
		stats:     stats,
		seqByPrev: make(map[string]map[string]int),
		dirCounts: dirCounts,
		fuzzy:     scorer.FuzzyDefault,
		showEmpty: defaultShowEmpty,
	}, nil
}

// SetFuzzy updates the strictness preset used by Suggest.
func (s *State) SetFuzzy(f scorer.Fuzzy) {
	s.mu.Lock()
	s.fuzzy = f
	s.mu.Unlock()
}

// GetFuzzy returns the current strictness preset.
func (s *State) GetFuzzy() scorer.Fuzzy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fuzzy
}

// SetShowEmpty controls whether Suggest returns a prediction on an empty prompt.
func (s *State) SetShowEmpty(show bool) {
	s.mu.Lock()
	s.showEmpty = show
	s.mu.Unlock()
}

// ShowEmpty reports whether Suggest returns a prediction on an empty prompt.
func (s *State) ShowEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.showEmpty
}

// SeqCounts returns the cached next-command counts for a given prev,
// fetching from SQLite on first miss.
func (s *State) SeqCounts(prev string) (map[string]int, error) {
	if prev == "" {
		return nil, nil
	}

	s.mu.RLock()
	if m, ok := s.seqByPrev[prev]; ok {
		s.mu.RUnlock()
		return m, nil
	}
	s.mu.RUnlock()

	m, err := store.GetSequenceCounts(s.db, prev)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.seqByPrev[prev] = m
	s.mu.Unlock()
	return m, nil
}
