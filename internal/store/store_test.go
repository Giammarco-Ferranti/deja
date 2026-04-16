package store

import (
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
