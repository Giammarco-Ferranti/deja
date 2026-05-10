package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/giammarcoferranti/deja/internal/store"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	return db
}

func seed(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	commands := []store.Command{
		{Command: "git commit -m", Directory: "/repo", Timestamp: now, SessionID: "s1"},
		{Command: "git status", Directory: "/repo", Timestamp: now.Add(time.Minute), SessionID: "s1"},
		{Command: "git commit -m", Directory: "/repo", Timestamp: now.Add(2 * time.Minute), SessionID: "s1"},
		{Command: "go test ./...", Directory: "/other", Timestamp: now.Add(3 * time.Minute), SessionID: "s1"},
	}
	if err := store.SaveImportBatch(db, commands); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestLoad_PopulatesStatsAndDirCounts(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)

	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(state.stats) != 3 {
		t.Errorf("want 3 stats, got %d: %+v", len(state.stats), state.stats)
	}

	dc := state.dirCounts["git commit -m"]
	if dc["/repo"] != 2 {
		t.Errorf("want /repo count=2 for 'git commit -m', got %d", dc["/repo"])
	}
}

func TestSuggest_RanksGitCommitFirst(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)

	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	resp := state.Suggest(SuggestReq{Buffer: "git c", Dir: "/repo", Prev: ""}, time.Date(2026, 4, 16, 10, 10, 0, 0, time.UTC))
	if resp.Suggestion != "git commit -m" {
		t.Errorf("want 'git commit -m', got %q (alts=%v)", resp.Suggestion, resp.Alternatives)
	}
}

func TestRecord_MutatesDBAndMemory(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)

	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// warm the seqByPrev cache so the in-memory mutation path is exercised
	if _, err := state.SeqCounts("git status"); err != nil {
		t.Fatalf("seq counts: %v", err)
	}

	req := RecordReq{
		Command:   "git push",
		Dir:       "/repo",
		SessionID: "s2",
		Prev:      "git status",
	}
	if err := state.Record(req); err != nil {
		t.Fatalf("record: %v", err)
	}

	// memory: dirCounts updated
	if state.dirCounts["git push"]["/repo"] != 1 {
		t.Errorf("want dirCounts[git push][/repo]=1, got %d", state.dirCounts["git push"]["/repo"])
	}
	// memory: seqByPrev updated (cache was warm)
	if state.seqByPrev["git status"]["git push"] != 1 {
		t.Errorf("want seq[git status][git push]=1, got %d", state.seqByPrev["git status"]["git push"])
	}

	// memory: stats has the new entry so the next Suggest call sees it
	stat := findStat(state.stats, "git push")
	if stat == nil {
		t.Fatalf("want stats entry for 'git push', got none: %+v", state.stats)
	}
	if stat.Count != 1 {
		t.Errorf("want stats[git push].Count=1, got %d", stat.Count)
	}

	// durable: sqlite has the new row
	var cnt int64
	db.Model(&store.Command{}).Where("command = ?", "git push").Count(&cnt)
	if cnt != 1 {
		t.Errorf("want 1 persisted 'git push' row, got %d", cnt)
	}
}

func TestRecord_IncrementsExistingStat(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)

	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	before := findStat(state.stats, "git commit -m")
	if before == nil {
		t.Fatalf("seed missing 'git commit -m'")
	}
	beforeCount := before.Count
	beforeLast := before.LastUsed

	if err := state.Record(RecordReq{
		Command:   "git commit -m",
		Dir:       "/repo",
		SessionID: "s2",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	after := findStat(state.stats, "git commit -m")
	if after == nil {
		t.Fatalf("stats lost 'git commit -m' after record")
	}
	if after.Count != beforeCount+1 {
		t.Errorf("want stats[git commit -m].Count=%d, got %d", beforeCount+1, after.Count)
	}
	if !after.LastUsed.After(beforeLast) {
		t.Errorf("want LastUsed to advance from %v, got %v", beforeLast, after.LastUsed)
	}
}

func TestSuggest_SeesCommandRecordedInSameSession(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)

	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	const novel = "echo hello-deja-bug"
	if err := state.Record(RecordReq{
		Command:   novel,
		Dir:       "/repo",
		SessionID: "s2",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	resp := state.Suggest(SuggestReq{Buffer: "echo hello-deja", Dir: "/repo"}, time.Date(2026, 4, 16, 10, 10, 0, 0, time.UTC))
	if resp.Suggestion != novel {
		t.Errorf("want freshly-recorded %q to surface as suggestion, got %q (alts=%v)", novel, resp.Suggestion, resp.Alternatives)
	}
}

func findStat(stats []store.CommandStat, cmd string) *store.CommandStat {
	for i := range stats {
		if stats[i].Command == cmd {
			return &stats[i]
		}
	}
	return nil
}
