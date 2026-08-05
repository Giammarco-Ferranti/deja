package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// The database holds shell history in plaintext, so it must not be readable by
// other local accounts. The sidecars matter as much as the main file: -wal is
// often the larger of the two and holds the most recent commands.
func TestInitDB_RestrictsFilePermissions(t *testing.T) {
	path := openTestDB(t)
	if _, err := InitDB(path); err != nil {
		t.Fatalf("init db: %v", err)
	}

	checked := 0
	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := path + suffix
		info, err := os.Stat(p)
		if err != nil {
			if suffix != "" && os.IsNotExist(err) {
				continue // sidecars only exist while WAL mode is active
			}
			t.Fatalf("stat %s: %v", p, err)
		}
		checked++
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s has mode %04o, want 0600", filepath.Base(p), perm)
		}
	}
	// Without this the loop above passes vacuously if the sidecars are missing,
	// which is exactly the case it exists to cover.
	if checked < 3 {
		t.Errorf("only %d of 3 database files were present to check; expected the -wal and -shm sidecars to exist after InitDB", checked)
	}
}

// A database created by an earlier version is 0644 on disk. Opening it must
// repair the mode rather than only protecting fresh installs.
func TestInitDB_RepairsLoosePermissions(t *testing.T) {
	path := openTestDB(t)
	db, err := InitDB(path)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Loosen everything the way a pre-fix install would look.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Chmod(path+suffix, 0o644); err != nil && !os.IsNotExist(err) {
			t.Fatalf("chmod %s: %v", path+suffix, err)
		}
	}

	if _, err := InitDB(path); err != nil {
		t.Fatalf("reopen: %v", err)
	}

	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := path + suffix
		info, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("stat %s: %v", p, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s was not repaired: mode %04o, want 0600", filepath.Base(p), perm)
		}
	}
}

func TestIgnoredCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"plain command", "git status", false},
		{"leading space", " git status", true},
		{"leading tab", "\tgit status", true},
		{"two leading spaces", "  secret", true},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"trailing space only", "git status ", false},
		{"inner space", "git commit -m 'x'", false},
		// zsh's rule is "first char is a space or tab", so a unicode space is
		// NOT ignorable. Written as an escape because the distinction is
		// invisible in source. Note strings.TrimSpace *does* treat U+00A0 as
		// space, which is why the second half of the predicate uses TrimLeft.
		{"unicode non-breaking space", "\u00a0git status", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IgnoredCommand(tt.cmd); got != tt.want {
				t.Errorf("IgnoredCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

// A space-prefixed command must not reach any of the three tables. This is the
// last chokepoint before SQLite, and it applies regardless of the shell's
// setopt state.
func TestRecordCommand_DropsIgnoredCommand(t *testing.T) {
	db, err := InitDB(openTestDB(t))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	cmd := Command{
		Command:   " export AWS_SECRET=hunter2",
		Directory: "/repo",
		Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		SessionID: "s1",
	}
	if err := RecordCommand(db, cmd, "git status"); err != nil {
		t.Fatalf("record: %v", err)
	}

	for _, tc := range []struct {
		table string
		model interface{}
	}{
		{"commands", &Command{}},
		{"command_stats", &CommandStat{}},
		{"sequences", &Sequence{}},
	} {
		var n int64
		db.Model(tc.model).Count(&n)
		if n != 0 {
			t.Errorf("%s: want 0 rows, got %d", tc.table, n)
		}
	}
}

// The subtler leak: the ignored command is never recorded itself, but survives
// as the `--prev` of the command that follows it and lands verbatim in
// sequences.prev_command.
func TestRecordCommand_DropsIgnoredPrevCommand(t *testing.T) {
	db, err := InitDB(openTestDB(t))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	cmd := Command{
		Command:   "git status",
		Directory: "/repo",
		Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		SessionID: "s1",
	}
	if err := RecordCommand(db, cmd, " export AWS_SECRET=hunter2"); err != nil {
		t.Fatalf("record: %v", err)
	}

	// The command itself is fine and must still be recorded.
	var stat CommandStat
	if err := db.Where("command = ?", "git status").First(&stat).Error; err != nil {
		t.Fatalf("stat not found: %v", err)
	}

	var seqCount int64
	db.Model(&Sequence{}).Count(&seqCount)
	if seqCount != 0 {
		var seqs []Sequence
		db.Find(&seqs)
		t.Errorf("want no sequence rows, got %d: %+v", seqCount, seqs)
	}
}

func TestSaveImportBatch_DropsIgnoredCommands(t *testing.T) {
	db, err := InitDB(openTestDB(t))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	t0 := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	mk := func(c string, offset int) Command {
		return Command{
			Command:   c,
			Directory: "/repo",
			Timestamp: t0.Add(time.Duration(offset) * time.Second),
			SessionID: "import",
		}
	}
	batch := []Command{mk("git status", 0), mk(" secret-token", 1), mk("git push", 2)}
	if err := SaveImportBatch(db, batch); err != nil {
		t.Fatalf("save: %v", err)
	}

	var stats []CommandStat
	db.Find(&stats)
	for _, s := range stats {
		if strings.Contains(s.Command, "secret") {
			t.Errorf("ignored command reached command_stats: %q", s.Command)
		}
	}

	var seqs []Sequence
	db.Find(&seqs)
	for _, s := range seqs {
		if strings.Contains(s.PrevCommand, "secret") || strings.Contains(s.NextCommand, "secret") {
			t.Errorf("ignored command reached sequences: %+v", s)
		}
	}
}

// OpenReader skips AutoMigrate, so it must not be able to create a schema —
// but it must read an existing one perfectly well.
func TestOpenReader_ReadsWithoutMigrating(t *testing.T) {
	path := openTestDB(t)

	db, err := InitDB(path)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	now := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	if err := RecordCommand(db, Command{
		Command: "git status", Directory: "/repo", Timestamp: now, SessionID: "s",
	}, ""); err != nil {
		t.Fatalf("record: %v", err)
	}

	rdb, err := OpenReader(path)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	stats, err := GetCommandStats(rdb)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(stats) != 1 || stats[0].Command != "git status" {
		t.Fatalf("got %+v, want the one recorded command", stats)
	}
}
