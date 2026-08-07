package store

import (
	"database/sql"
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

// The assertions below used GORM's query builder to inspect the database.
// These helpers keep them reading the same way over database/sql.

func countTable(t *testing.T, db *sql.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func findStat(db *sql.DB, command string) (CommandStat, error) {
	var c CommandStat
	err := db.QueryRow("SELECT command, count, last_used FROM command_stats WHERE command = ?", command).
		Scan(&c.Command, &c.Count, &c.LastUsed)
	return c, err
}

func allStats(t *testing.T, db *sql.DB) []CommandStat {
	t.Helper()
	rows, err := db.Query("SELECT command, count, last_used FROM command_stats")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	defer rows.Close()
	var out []CommandStat
	for rows.Next() {
		var c CommandStat
		if err := rows.Scan(&c.Command, &c.Count, &c.LastUsed); err != nil {
			t.Fatalf("scan stat: %v", err)
		}
		out = append(out, c)
	}
	return out
}

func allSeqs(t *testing.T, db *sql.DB) []Sequence {
	t.Helper()
	rows, err := db.Query("SELECT prev_command, next_command, count FROM sequences")
	if err != nil {
		t.Fatalf("seqs: %v", err)
	}
	defer rows.Close()
	var out []Sequence
	for rows.Next() {
		var q Sequence
		if err := rows.Scan(&q.PrevCommand, &q.NextCommand, &q.Count); err != nil {
			t.Fatalf("scan seq: %v", err)
		}
		out = append(out, q)
	}
	return out
}

func findSeq(db *sql.DB, prev, next string) (Sequence, error) {
	var q Sequence
	err := db.QueryRow("SELECT prev_command, next_command, count FROM sequences WHERE prev_command = ? AND next_command = ?", prev, next).
		Scan(&q.PrevCommand, &q.NextCommand, &q.Count)
	return q, err
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
	cmdCount = countTable(t, db, "commands")
	if cmdCount != 1 {
		t.Fatalf("expected 1 command row, got %d", cmdCount)
	}

	var stat CommandStat
	stat, err = findStat(db, "git status")
	if err != nil {
		t.Fatalf("stat not found: %v", err)
	}
	if stat.Count != 1 {
		t.Errorf("want count=1, got %d", stat.Count)
	}
	if !stat.LastUsed.Equal(now) {
		t.Errorf("want last_used=%v, got %v", now, stat.LastUsed)
	}

	var seq Sequence
	seq, err = findSeq(db, "git add .", "git status")
	if err != nil {
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
	stat, err = findStat(db, "make test")
	if err != nil {
		t.Fatalf("stat not found: %v", err)
	}
	if stat.Count != 3 {
		t.Errorf("want count=3, got %d", stat.Count)
	}
	if !stat.LastUsed.Equal(t0.Add(2 * time.Minute)) {
		t.Errorf("want last_used=%v, got %v", t0.Add(2*time.Minute), stat.LastUsed)
	}

	var seq Sequence
	seq, err = findSeq(db, "make build", "make test")
	if err != nil {
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
	seqCount = countTable(t, db, "sequences")
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
	cmdCount = countTable(t, db, "commands")
	statCount = countTable(t, db, "command_stats")
	seqCount = countTable(t, db, "sequences")
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
	gitStat, err = findStat(db, "git status")
	if err != nil {
		t.Fatalf("git status stat: %v", err)
	}
	if gitStat.Count != 3 {
		t.Errorf("git status count: want 3, got %d", gitStat.Count)
	}
	lsStat, err = findStat(db, "ls")
	if err != nil {
		t.Fatalf("ls stat: %v", err)
	}
	if lsStat.Count != 2 {
		t.Errorf("ls count: want 2, got %d", lsStat.Count)
	}

	var gitToLs, lsToGit Sequence
	gitToLs, err = findSeq(db, "git status", "ls")
	if err != nil {
		t.Fatalf("git→ls seq: %v", err)
	}
	if gitToLs.Count != 2 {
		t.Errorf("git→ls: want 2, got %d", gitToLs.Count)
	}
	lsToGit, err = findSeq(db, "ls", "git status")
	if err != nil {
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
	aStat, err = findStat(db, "a")
	if err != nil {
		t.Fatalf("a stat: %v", err)
	}
	if aStat.Count != 4 {
		t.Errorf("a count: want 4, got %d", aStat.Count)
	}

	var aToB Sequence
	aToB, err = findSeq(db, "a", "b")
	if err != nil {
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
	if err := db.Close(); err != nil {
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

	for _, table := range []string{"commands", "command_stats", "sequences"} {
		if n := countTable(t, db, table); n != 0 {
			t.Errorf("%s: want 0 rows, got %d", table, n)
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
	if _, err := findStat(db, "git status"); err != nil {
		t.Fatalf("stat not found: %v", err)
	}

	if seqCount := countTable(t, db, "sequences"); seqCount != 0 {
		t.Errorf("want no sequence rows, got %d: %+v", seqCount, allSeqs(t, db))
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

	for _, s := range allStats(t, db) {
		if strings.Contains(s.Command, "secret") {
			t.Errorf("ignored command reached command_stats: %q", s.Command)
		}
	}

	for _, s := range allSeqs(t, db) {
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

// gormSchema is the DDL earlier releases produced via GORM's AutoMigrate,
// captured verbatim from an install created by one. Databases in the wild look
// exactly like this, with user_version still 0.
var gormSchema = []string{
	"CREATE TABLE `commands` (`id` integer PRIMARY KEY AUTOINCREMENT,`command` text NOT NULL,`directory` text NOT NULL,`timestamp` datetime NOT NULL,`exit_code` integer NOT NULL,`duration_ms` integer NOT NULL,`session_id` text NOT NULL)",
	"CREATE INDEX `idx_commands_session` ON `commands`(`session_id`)",
	"CREATE INDEX `idx_commands_timestamp` ON `commands`(`timestamp`)",
	"CREATE INDEX `idx_commands_directory` ON `commands`(`directory`)",
	"CREATE INDEX `idx_commands_command` ON `commands`(`command`)",
	"CREATE TABLE `command_stats` (`command` text,`count` integer NOT NULL DEFAULT 0,`last_used` datetime NOT NULL,PRIMARY KEY (`command`))",
	"CREATE INDEX `idx_command_stats_last_used` ON `command_stats`(`last_used`)",
	"CREATE TABLE `sequences` (`prev_command` text NOT NULL,`next_command` text NOT NULL,`count` integer NOT NULL DEFAULT 0)",
	"CREATE UNIQUE INDEX `idx_seq_pair` ON `sequences`(`prev_command`,`next_command`)",
}

func schemaOf(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY type DESC, name")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan schema: %v", err)
		}
		out = append(out, s)
	}
	return out
}

// TestInitDB_AdoptsExistingGormDatabase is the compatibility contract for
// dropping GORM. A database created by an earlier release must be picked up in
// place — same tables, same indexes, same rows — and merely stamped with a
// user_version. Anything that rewrites or recreates it would be silently
// destroying somebody's history.
func TestInitDB_AdoptsExistingGormDatabase(t *testing.T) {
	path := openTestDB(t)

	// Build a database exactly as the GORM releases left it, user_version and all.
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, stmt := range gormSchema {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("apply legacy schema (%s): %v", stmt, err)
		}
	}
	then := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	if _, err := raw.Exec(
		"INSERT INTO commands (command, directory, timestamp, exit_code, duration_ms, session_id) VALUES (?,?,?,?,?,?)",
		"legacy command", "/legacy", then, 0, 5, "legacy-session"); err != nil {
		t.Fatalf("seed command: %v", err)
	}
	if _, err := raw.Exec("INSERT INTO command_stats (command, count, last_used) VALUES (?,?,?)",
		"legacy command", 7, then); err != nil {
		t.Fatalf("seed stat: %v", err)
	}
	var legacyVersion int
	if err := raw.QueryRow("PRAGMA user_version;").Scan(&legacyVersion); err != nil {
		t.Fatalf("read legacy user_version: %v", err)
	}
	if legacyVersion != 0 {
		t.Fatalf("legacy user_version = %d, want 0", legacyVersion)
	}
	before := schemaOf(t, raw)
	raw.Close()

	// Now open it the way this build does.
	db, err := InitDB(path)
	if err != nil {
		t.Fatalf("adopt legacy database: %v", err)
	}
	defer db.Close()

	after := schemaOf(t, db)
	if len(before) != len(after) {
		t.Fatalf("schema object count changed: %d -> %d\nbefore: %q\nafter:  %q", len(before), len(after), before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("schema rewritten:\n before: %s\n after:  %s", before[i], after[i])
		}
	}

	var version int
	if err := db.QueryRow("PRAGMA user_version;").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("user_version = %d, want %d", version, schemaVersion)
	}

	// The existing data must still be there, and still be usable.
	stat, err := findStat(db, "legacy command")
	if err != nil {
		t.Fatalf("legacy stat gone: %v", err)
	}
	if stat.Count != 7 || !stat.LastUsed.Equal(then) {
		t.Errorf("legacy stat = %+v, want count=7 last_used=%v", stat, then)
	}

	// And writing must keep working against the adopted schema, updating rather
	// than duplicating the row that was already there.
	if err := RecordCommand(db, Command{
		Command: "legacy command", Directory: "/legacy", Timestamp: then.Add(time.Hour), SessionID: "s",
	}, "prev command"); err != nil {
		t.Fatalf("record into adopted database: %v", err)
	}
	stat, err = findStat(db, "legacy command")
	if err != nil {
		t.Fatalf("stat after record: %v", err)
	}
	if stat.Count != 8 {
		t.Errorf("count = %d, want 8 (the upsert must add to the legacy count)", stat.Count)
	}
}

// Running the migration twice must be a no-op, since every daemon start calls it.
func TestInitDB_MigrationIsIdempotent(t *testing.T) {
	path := openTestDB(t)

	db, err := InitDB(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	first := schemaOf(t, db)
	db.Close()

	db, err = InitDB(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer db.Close()
	second := schemaOf(t, db)

	if len(first) != len(second) {
		t.Fatalf("schema changed on reopen: %q -> %q", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("schema changed on reopen:\n %s\n %s", first[i], second[i])
		}
	}
}

// A fresh database must end up with the same schema a GORM-created one has, or
// adopted and new installs would diverge.
func TestInitDB_FreshSchemaMatchesGorm(t *testing.T) {
	db, err := InitDB(openTestDB(t))
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	defer db.Close()

	got := schemaOf(t, db)
	gotSet := map[string]bool{}
	for _, s := range got {
		gotSet[s] = true
	}
	for _, want := range gormSchema {
		if !gotSet[want] {
			t.Errorf("fresh schema is missing the object GORM created:\n  %s\ngot:\n  %s", want, strings.Join(got, "\n  "))
		}
	}
}
