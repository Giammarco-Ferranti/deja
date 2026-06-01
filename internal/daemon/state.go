package daemon

import (
	"sync"

	"github.com/giammarcoferranti/deja/internal/scorer"
	"github.com/giammarcoferranti/deja/internal/store"
	"gorm.io/gorm"
)

// State is the in-memory snapshot the daemon serves suggestions from.
// Reads are cheap and concurrent; writes (from Record) are brief and rare.
type State struct {
	mu        sync.RWMutex
	db        *gorm.DB
	stats     []store.CommandStat       // top-100 most-used, from GetCommandStats
	seqByPrev map[string]map[string]int // prev → next → count, filled lazily
	dirCounts map[string]map[string]int // cmd  → dir  → count
	fuzzy     scorer.Fuzzy              // strictness preset for fuzzy matching
}

// Load warms state from SQLite. It preloads only the dirCounts for the
// top-100 command_stats so memory stays bounded regardless of history size.
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
