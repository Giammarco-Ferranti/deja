package daemon

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestState_ConcurrentRecordAndSuggest exercises the RWMutex paths in
// state.go under -race. It mixes Record (write lock), Suggest (read lock),
// and SeqCounts cache fill (read-then-write lock) across goroutines and
// asserts no race, no panic, and that every Record reaches both memory
// and SQLite.
func TestState_ConcurrentRecordAndSuggest(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)

	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	recorded := make(map[string]*atomic.Int64, 4)

	// 4 record goroutines. Each one repeatedly records a single command so we
	// can assert exact persisted counts at the end.
	for i := 0; i < 4; i++ {
		cmd := fmt.Sprintf("recorded-cmd-%d", i)
		counter := &atomic.Int64{}
		recorded[cmd] = counter
		prev := "git status"
		if i%2 == 0 {
			// Mix in an uncached prev to exercise the SeqCounts cache-fill path.
			prev = "uncached-prev"
		}

		wg.Add(1)
		go func(cmd, prev string, counter *atomic.Int64) {
			defer wg.Done()
			for ctx.Err() == nil {
				if err := state.Record(RecordReq{
					Command:   cmd,
					Dir:       "/repo",
					SessionID: "concurrent",
					Prev:      prev,
				}); err != nil {
					t.Errorf("record %q: %v", cmd, err)
					return
				}
				counter.Add(1)
			}
		}(cmd, prev, counter)
	}

	// 4 suggest goroutines.
	for i := 0; i < 4; i++ {
		buf := []string{"git", "rec", "go", ""}[i]
		wg.Add(1)
		go func(buf string) {
			defer wg.Done()
			for ctx.Err() == nil {
				_ = state.Suggest(SuggestReq{Buffer: buf, Dir: "/repo", Prev: "git status"}, time.Now())
			}
		}(buf)
	}

	// 1 SeqCounts goroutine to keep exercising the cache lookup/fill path
	// alongside writes that may be invalidating cached prevs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			if _, err := state.SeqCounts("git status"); err != nil {
				t.Errorf("seq counts: %v", err)
				return
			}
		}
	}()

	wg.Wait()

	// Verify each recorded command reached both layers exactly counter times.
	for cmd, counter := range recorded {
		want := counter.Load()
		if want == 0 {
			t.Errorf("counter for %q is 0 — goroutine never ran", cmd)
			continue
		}

		stat := findStat(state.stats, cmd)
		if stat == nil {
			t.Errorf("memory: stats missing %q", cmd)
			continue
		}
		if int64(stat.Count) != want {
			t.Errorf("memory: stats[%q].Count = %d, want %d", cmd, stat.Count, want)
		}

		var dbCount int64
		dbCount = countCommandRows(t, db, cmd)
		if dbCount != want {
			t.Errorf("sqlite: rows for %q = %d, want %d", cmd, dbCount, want)
		}
	}
}
