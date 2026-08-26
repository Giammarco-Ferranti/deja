package daemon

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/giammarcoferranti/deja/internal/store"
)

// countCommandRows replaces the GORM query the assertions used to make.
func countCommandRows(t *testing.T, db *sql.DB, command string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow("SELECT COUNT(*) FROM commands WHERE command = ?", command).Scan(&n); err != nil {
		t.Fatalf("count rows for %q: %v", command, err)
	}
	return n
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	return db
}

func seed(t *testing.T, db *sql.DB) {
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
	cnt = countCommandRows(t, db, "git push")
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

func TestServe_RoundTrip(t *testing.T) {
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

func TestServe_RebindsOverStaleSocket(t *testing.T) {
	sockPath := shortSockPath(t)
	if err := os.WriteFile(sockPath, nil, 0o600); err != nil {
		t.Fatalf("seed stale socket file: %v", err)
	}

	state, err := Load(newTestDB(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveErr := make(chan error, 1)
	go func() { serveErr <- Serve(ctx, state, sockPath) }()

	if !waitForPing(t, sockPath, 2*time.Second) {
		t.Fatalf("daemon never came up on stale-socket path %s", sockPath)
	}

	cancel()
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("Serve returned error after cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
}

func TestServe_RefusesWhenLiveDaemonPresent(t *testing.T) {
	sockPath := shortSockPath(t)

	state, err := Load(newTestDB(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	serveAErr := make(chan error, 1)
	go func() { serveAErr <- Serve(ctxA, state, sockPath) }()

	if !waitForPing(t, sockPath, 2*time.Second) {
		t.Fatalf("daemon A never came up")
	}

	stateB, err := Load(newTestDB(t))
	if err != nil {
		t.Fatalf("load B: %v", err)
	}
	err = Serve(context.Background(), stateB, sockPath)
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("want 'already running' error from second Serve, got %v", err)
	}

	if !IsLiveSocket(sockPath, 200*time.Millisecond) {
		t.Fatal("daemon A stopped responding after second Serve attempt — its socket was clobbered")
	}

	cancelA()
	select {
	case <-serveAErr:
	case <-time.After(2 * time.Second):
		t.Fatal("daemon A did not return after cancel")
	}
}

func waitForPing(t *testing.T, sockPath string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if IsLiveSocket(sockPath, 100*time.Millisecond) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func findStat(stats []store.CommandStat, cmd string) *store.CommandStat {
	for i := range stats {
		if stats[i].Command == cmd {
			return &stats[i]
		}
	}
	return nil
}

func ptrBool(b bool) *bool { return &b }

// TestSuggest_EmptyPromptGate covers the ShowEmpty gate: on an empty buffer the
// daemon predicts by default but returns nothing when the user opts out, while
// a non-empty buffer is never affected by the setting.
func TestSuggest_EmptyPromptGate(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)

	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	now := time.Date(2026, 4, 16, 10, 10, 0, 0, time.UTC)

	// Default (showEmpty=true): an empty buffer yields a prediction.
	if resp := state.Suggest(SuggestReq{Buffer: "", Dir: "/repo"}, now); resp.Suggestion == "" {
		t.Errorf("default empty-prompt suggest returned nothing, want a prediction")
	}

	// Opted out: an empty buffer yields nothing.
	state.SetShowEmpty(false)
	if resp := state.Suggest(SuggestReq{Buffer: "", Dir: "/repo"}, now); resp.Suggestion != "" || len(resp.Alternatives) != 0 {
		t.Errorf("with showEmpty=false, empty-prompt suggest = %+v, want empty", resp)
	}

	// The gate is scoped to an exactly-empty buffer — a real buffer still ranks.
	if resp := state.Suggest(SuggestReq{Buffer: "git c", Dir: "/repo"}, now); resp.Suggestion != "git commit -m" {
		t.Errorf("non-empty buffer must ignore showEmpty; got %q", resp.Suggestion)
	}

	// Re-enabled: predictions return.
	state.SetShowEmpty(true)
	if resp := state.Suggest(SuggestReq{Buffer: "", Dir: "/repo"}, now); resp.Suggestion == "" {
		t.Errorf("re-enabled empty-prompt suggest returned nothing, want a prediction")
	}
}

// TestSetGetConfig_Empty exercises the Empty (*bool) round-trip: GetConfig
// echoes a non-nil pointer, nil in a request leaves the setting alone, and an
// invalid fuzzy value in the same request must not drop a valid empty change.
func TestSetGetConfig_Empty(t *testing.T) {
	state, err := Load(newTestDB(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Default is reported as a non-nil pointer to true.
	if got := state.GetConfig(); got.Empty == nil || *got.Empty != true {
		t.Fatalf("GetConfig default Empty = %v, want *true", got.Empty)
	}

	// Setting empty=false takes effect and is echoed.
	resp := state.SetConfig(SetConfigReq{Empty: ptrBool(false)})
	if resp.Error != "" {
		t.Fatalf("SetConfig(empty=false) error: %s", resp.Error)
	}
	if resp.Empty == nil || *resp.Empty != false {
		t.Errorf("SetConfig echo Empty = %v, want *false", resp.Empty)
	}
	if state.ShowEmpty() {
		t.Error("ShowEmpty() still true after SetConfig(empty=false)")
	}
	if got := state.GetConfig(); got.Empty == nil || *got.Empty != false {
		t.Errorf("GetConfig Empty = %v, want *false", got.Empty)
	}

	// A nil Empty leaves the setting alone (only fuzzy changes here).
	if resp := state.SetConfig(SetConfigReq{Fuzzy: "tight"}); resp.Error != "" {
		t.Fatalf("SetConfig(fuzzy=tight) error: %s", resp.Error)
	}
	if state.ShowEmpty() {
		t.Error("ShowEmpty() flipped by a request that omitted Empty")
	}

	// A rejected fuzzy value must not drop a valid empty change applied in the
	// same request.
	resp = state.SetConfig(SetConfigReq{Fuzzy: "bogus", Empty: ptrBool(true)})
	if resp.Error == "" {
		t.Error("SetConfig(fuzzy=bogus) should report an error")
	}
	if !state.ShowEmpty() {
		t.Error("valid empty change dropped when the request's fuzzy value was rejected")
	}
	if resp.Empty == nil || *resp.Empty != true {
		t.Errorf("rejected-fuzzy SetConfig echo Empty = %v, want *true", resp.Empty)
	}
}

// TestState_ConcurrentToggleShowEmpty runs the empty-prompt gate under -race:
// SetShowEmpty (write lock) races empty-buffer Suggest calls (ShowEmpty read
// lock + early return) with no data race or panic.
func TestState_ConcurrentToggleShowEmpty(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)

	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		on := true
		for ctx.Err() == nil {
			state.SetShowEmpty(on)
			on = !on
		}
	}()

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				_ = state.Suggest(SuggestReq{Buffer: "", Dir: "/repo", Prev: "git status"}, time.Now())
			}
		}()
	}

	wg.Wait()
}

// The in-memory half of the HIST_IGNORE_SPACE fix. A store-only guard passes
// every SQLite assertion while leaving the command in stats/dirCounts/seqByPrev,
// so it stays suggestable until the daemon is bounced — which, since daemons
// survive across shell sessions, is ~never.
func TestRecord_DropsIgnoredCommandFromMemory(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)

	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	statsBefore := len(state.stats)

	req := RecordReq{
		Command:   " export AWS_SECRET=hunter2",
		Dir:       "/repo",
		SessionID: "s1",
		Prev:      "git status",
	}
	if err := state.Record(req); err != nil {
		t.Fatalf("record: %v", err)
	}

	if len(state.stats) != statsBefore {
		t.Errorf("stats grew from %d to %d: %+v", statsBefore, len(state.stats), state.stats)
	}
	if _, ok := state.dirCounts[req.Command]; ok {
		t.Errorf("ignored command reached dirCounts: %+v", state.dirCounts)
	}
	for prev, nexts := range state.seqByPrev {
		if strings.Contains(prev, "AWS_SECRET") {
			t.Errorf("ignored command reached seqByPrev as a key: %q", prev)
		}
		for next := range nexts {
			if strings.Contains(next, "AWS_SECRET") {
				t.Errorf("ignored command reached seqByPrev as a value: %q -> %q", prev, next)
			}
		}
	}

	// And it must not be suggestable, without a restart.
	resp := state.Suggest(SuggestReq{Buffer: "export"}, time.Now())
	if strings.Contains(resp.Suggestion, "AWS_SECRET") {
		t.Errorf("ignored command was suggested: %q", resp.Suggestion)
	}
	for _, alt := range resp.Alternatives {
		if strings.Contains(alt, "AWS_SECRET") {
			t.Errorf("ignored command was offered as an alternative: %q", alt)
		}
	}
}

// An ignored command must not survive as the key of the *next* command's
// sequence entry, in SQLite or in memory.
func TestRecord_DropsIgnoredPrevCommand(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)

	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Warm the cache for the ignored prev so the seqByPrev mutation would fire
	// if the guard were missing.
	secret := " export AWS_SECRET=hunter2"
	state.mu.Lock()
	state.seqByPrev[secret] = map[string]int{}
	state.mu.Unlock()

	if err := state.Record(RecordReq{
		Command:   "git push",
		Dir:       "/repo",
		SessionID: "s1",
		Prev:      secret,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	if n := len(state.seqByPrev[secret]); n != 0 {
		t.Errorf("seqByPrev[secret] gained %d entries: %+v", n, state.seqByPrev[secret])
	}

	var leaked int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sequences WHERE prev_command LIKE ?", "%AWS_SECRET%").Scan(&leaked); err != nil {
		t.Fatalf("query sequences: %v", err)
	}
	if leaked != 0 {
		t.Errorf("ignored command reached sequences.prev_command: %d rows", leaked)
	}

	// The command that followed it is legitimate and must still be recorded.
	if findStat(state.stats, "git push") == nil {
		t.Error("git push was not recorded")
	}
}

// TestCheckpointWAL_ShrinksTheLog pins the reason this exists: SQLite's own
// checkpointing bounds the WAL but never makes the file smaller, so a database
// keeps whatever high-water mark it ever reached. One observed install held a
// 28MB WAL against a 35MB database with zero live frames in it.
func TestCheckpointWAL_ShrinksTheLog(t *testing.T) {
	dir, err := os.MkdirTemp("", "djwal")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "deja.db")
	db, err := store.InitDB(dbPath)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Write enough to push the WAL well past a trivial size.
	for i := 0; i < 400; i++ {
		if err := state.Record(RecordReq{
			Command:   fmt.Sprintf("command number %d with some padding to take up space", i),
			Dir:       fmt.Sprintf("/dir/%d", i%10),
			SessionID: "s",
			Prev:      fmt.Sprintf("command number %d with some padding to take up space", i-1),
		}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	walPath := dbPath + "-wal"
	before, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal: %v", err)
	}
	if before.Size() == 0 {
		t.Skip("WAL is empty; nothing to shrink on this SQLite build")
	}

	if err := state.CheckpointWAL(); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	after, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal after: %v", err)
	}
	if after.Size() >= before.Size() {
		t.Errorf("WAL did not shrink: %d -> %d bytes", before.Size(), after.Size())
	}

	// The data has to survive being moved into the main database.
	stats, err := store.GetCommandStats(db)
	if err != nil {
		t.Fatalf("stats after checkpoint: %v", err)
	}
	if len(stats) != 400 {
		t.Errorf("got %d commands after checkpoint, want 400", len(stats))
	}

	// And the daemon must still be able to write afterwards.
	if err := state.Record(RecordReq{Command: "after checkpoint", Dir: "/d", SessionID: "s"}); err != nil {
		t.Fatalf("record after checkpoint: %v", err)
	}
}

// TestCheckpointWAL_ReportsRefusal pins the one outcome CheckpointWAL exists to
// make visible: a truncation SQLite declined to perform.
//
// PRAGMA wal_checkpoint answers with a row — busy|log|checkpointed — rather than
// an error, and a refusal is busy=1 inside that row. db.Exec discards the row,
// so "truncated" and "could not truncate" are both a nil error, and
// checkpointLoop logs nothing either way. That is the silence its own comment
// promises not to keep: "a checkpoint that fails every time means the WAL grows
// forever, and silence would hide that."
//
// The reader parked on an open snapshot below is what makes SQLite refuse. It
// is not a contrived state; it is one connection reading while another writes.
//
// Slow by construction: InitDB sets busy_timeout=5000, so the checkpoint waits
// out the full timeout before reporting busy.
func TestCheckpointWAL_ReportsRefusal(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "deja.db")
	db, err := store.InitDB(dbPath)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for i := 0; i < 400; i++ {
		if err := state.Record(RecordReq{
			Command:   fmt.Sprintf("command number %d with some padding to take up space", i),
			Dir:       fmt.Sprintf("/dir/%d", i%10),
			SessionID: "s",
		}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	walPath := dbPath + "-wal"
	before, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal: %v", err)
	}
	if before.Size() == 0 {
		t.Fatalf("no WAL to truncate — the premise of this test does not hold on this build")
	}

	// Hold a read snapshot open across the checkpoint.
	rdb, err := store.OpenReader(dbPath)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	tx, err := rdb.Begin()
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow("SELECT count(*) FROM commands").Scan(&n); err != nil {
		t.Fatalf("read inside tx: %v", err)
	}

	checkpointErr := state.CheckpointWAL()

	after, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal after: %v", err)
	}

	if after.Size() < before.Size() {
		// SQLite truncated it despite the reader, so there is no refusal to report.
		return
	}
	if checkpointErr == nil {
		t.Errorf("checkpoint truncated nothing (WAL %d -> %d bytes) but CheckpointWAL returned nil; "+
			"a refusal has to be observable or checkpointLoop cannot log a WAL that never shrinks",
			before.Size(), after.Size())
	}
}
