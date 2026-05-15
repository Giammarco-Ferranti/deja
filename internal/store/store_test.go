package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

func TestRecordCommand_FirstInsert(t *testing.T) {
	db, err := InitDB(openTestDB(t))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	now := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	cmd := Command{
		Command:   "git status",
		Directory: "/repo",
		Timestamp: now,
		SessionID: "s1",
	}
	if err := RecordCommand(db, cmd, "git add ."); err != nil {
		t.Fatalf("record: %v", err)
	}

	var cmdCount int64
	db.Model(&Command{}).Count(&cmdCount)
	if cmdCount != 1 {
		t.Fatalf("expected 1 command row, got %d", cmdCount)
	}

	var stat CommandStat
	if err := db.Where("command = ?", "git status").First(&stat).Error; err != nil {
		t.Fatalf("stat not found: %v", err)
	}
	if stat.Count != 1 {
		t.Errorf("want count=1, got %d", stat.Count)
	}
	if !stat.LastUsed.Equal(now) {
		t.Errorf("want last_used=%v, got %v", now, stat.LastUsed)
	}

	var seq Sequence
	if err := db.Where("prev_command = ? AND next_command = ?", "git add .", "git status").First(&seq).Error; err != nil {
		t.Fatalf("seq not found: %v", err)
	}
	if seq.Count != 1 {
		t.Errorf("want seq count=1, got %d", seq.Count)
	}
}

func TestRecordCommand_RepeatIncrements(t *testing.T) {
	db, err := InitDB(openTestDB(t))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	t0 := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	mk := func(ts time.Time) Command {
		return Command{Command: "make test", Directory: "/repo", Timestamp: ts, SessionID: "s1"}
	}

	for i, ts := range []time.Time{t0, t0.Add(time.Minute), t0.Add(2 * time.Minute)} {
		if err := RecordCommand(db, mk(ts), "make build"); err != nil {
			t.Fatalf("record[%d]: %v", i, err)
		}
	}

	var stat CommandStat
	if err := db.Where("command = ?", "make test").First(&stat).Error; err != nil {
		t.Fatalf("stat not found: %v", err)
	}
	if stat.Count != 3 {
		t.Errorf("want count=3, got %d", stat.Count)
	}
	if !stat.LastUsed.Equal(t0.Add(2 * time.Minute)) {
		t.Errorf("want last_used=%v, got %v", t0.Add(2*time.Minute), stat.LastUsed)
	}

	var seq Sequence
	if err := db.Where("prev_command = ? AND next_command = ?", "make build", "make test").First(&seq).Error; err != nil {
		t.Fatalf("seq not found: %v", err)
	}
	if seq.Count != 3 {
		t.Errorf("want seq count=3, got %d", seq.Count)
	}
}

func TestRecordCommand_SkipsEmptyPrev(t *testing.T) {
	db, err := InitDB(openTestDB(t))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	cmd := Command{Command: "ls", Timestamp: time.Now(), SessionID: "s1"}
	if err := RecordCommand(db, cmd, ""); err != nil {
		t.Fatalf("record: %v", err)
	}

	var seqCount int64
	db.Model(&Sequence{}).Count(&seqCount)
	if seqCount != 0 {
		t.Errorf("expected no sequence rows with empty prev, got %d", seqCount)
	}
}

// Regression: previously, SaveImportBatch passed len(commands) as the batch
// size, producing a single INSERT that exceeded SQLite's host-parameter
// ceiling on real-world zsh histories.
func TestSaveImportBatch_LargeBatch(t *testing.T) {
	db, err := InitDB(openTestDB(t))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	const n = 50000
	t0 := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	commands := make([]Command, n)
	for i := range n {
		commands[i] = Command{
			Command:   fmt.Sprintf("cmd-%05d", i),
			Timestamp: t0.Add(time.Duration(i) * time.Second),
			SessionID: "import",
		}
	}

	if err := SaveImportBatch(db, commands); err != nil {
		t.Fatalf("save: %v", err)
	}

	var cmdCount, statCount, seqCount int64
	db.Model(&Command{}).Count(&cmdCount)
	db.Model(&CommandStat{}).Count(&statCount)
	db.Model(&Sequence{}).Count(&seqCount)
	if cmdCount != n {
		t.Errorf("commands: want %d, got %d", n, cmdCount)
	}
	if statCount != n {
		t.Errorf("command_stats: want %d, got %d", n, statCount)
	}
	if seqCount != n-1 {
		t.Errorf("sequences: want %d, got %d", n-1, seqCount)
	}
}

func TestSaveImportBatch_AggregatesStatsAndSequences(t *testing.T) {
	db, err := InitDB(openTestDB(t))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	t0 := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	mk := func(name string, offset int) Command {
		return Command{
			Command:   name,
			Timestamp: t0.Add(time.Duration(offset) * time.Second),
			SessionID: "import",
		}
	}
	commands := []Command{
		mk("git status", 0),
		mk("ls", 1),
		mk("git status", 2),
		mk("ls", 3),
		mk("git status", 4),
	}

	if err := SaveImportBatch(db, commands); err != nil {
		t.Fatalf("save: %v", err)
	}

	var gitStat, lsStat CommandStat
	if err := db.Where("command = ?", "git status").First(&gitStat).Error; err != nil {
		t.Fatalf("git status stat: %v", err)
	}
	if gitStat.Count != 3 {
		t.Errorf("git status count: want 3, got %d", gitStat.Count)
	}
	if err := db.Where("command = ?", "ls").First(&lsStat).Error; err != nil {
		t.Fatalf("ls stat: %v", err)
	}
	if lsStat.Count != 2 {
		t.Errorf("ls count: want 2, got %d", lsStat.Count)
	}

	var gitToLs, lsToGit Sequence
	if err := db.Where("prev_command = ? AND next_command = ?", "git status", "ls").First(&gitToLs).Error; err != nil {
		t.Fatalf("git→ls seq: %v", err)
	}
	if gitToLs.Count != 2 {
		t.Errorf("git→ls: want 2, got %d", gitToLs.Count)
	}
	if err := db.Where("prev_command = ? AND next_command = ?", "ls", "git status").First(&lsToGit).Error; err != nil {
		t.Fatalf("ls→git seq: %v", err)
	}
	if lsToGit.Count != 2 {
		t.Errorf("ls→git: want 2, got %d", lsToGit.Count)
	}
}

// Verifies the OnConflict `excluded.count` accumulation survives auto-chunking
// across two separate SaveImportBatch calls.
func TestSaveImportBatch_IsIdempotentlyAdditive(t *testing.T) {
	db, err := InitDB(openTestDB(t))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	t0 := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	build := func() []Command {
		mk := func(name string, offset int) Command {
			return Command{
				Command:   name,
				Timestamp: t0.Add(time.Duration(offset) * time.Second),
				SessionID: "import",
			}
		}
		return []Command{mk("a", 0), mk("b", 1), mk("a", 2), mk("b", 3)}
	}

	if err := SaveImportBatch(db, build()); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	if err := SaveImportBatch(db, build()); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	var aStat CommandStat
	if err := db.Where("command = ?", "a").First(&aStat).Error; err != nil {
		t.Fatalf("a stat: %v", err)
	}
	if aStat.Count != 4 {
		t.Errorf("a count: want 4, got %d", aStat.Count)
	}

	var aToB Sequence
	if err := db.Where("prev_command = ? AND next_command = ?", "a", "b").First(&aToB).Error; err != nil {
		t.Fatalf("a→b seq: %v", err)
	}
	if aToB.Count != 4 {
		t.Errorf("a→b: want 4, got %d", aToB.Count)
	}
}
