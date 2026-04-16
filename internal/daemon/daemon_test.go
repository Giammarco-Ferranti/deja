package daemon

import (
	"context"
	"encoding/json"
	"net"
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

	// durable: sqlite has the new row
	var cnt int64
	db.Model(&store.Command{}).Where("command = ?", "git push").Count(&cnt)
	if cnt != 1 {
		t.Errorf("want 1 persisted 'git push' row, got %d", cnt)
	}
}

func TestServe_RoundTrip(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)

	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	sock := filepath.Join(t.TempDir(), "sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Serve(ctx, state, sock) }()

	// wait for the listener (at most ~500ms)
	deadline := time.Now().Add(500 * time.Millisecond)
	var conn net.Conn
	for time.Now().Before(deadline) {
		conn, err = net.Dial("unix", sock)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(500 * time.Millisecond))

	payload, _ := json.Marshal(SuggestReq{Buffer: "git c", Dir: "/repo"})
	if err := json.NewEncoder(conn).Encode(Envelope{Type: "suggest", Payload: payload}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp SuggestResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Suggestion != "git commit -m" {
		t.Errorf("want 'git commit -m', got %q", resp.Suggestion)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("serve did not shut down within 500ms")
	}
}
