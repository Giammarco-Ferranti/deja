package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// shortSockPath returns a Unix socket path that fits within macOS's
// sun_path limit (~104 bytes), regardless of the test name length.
func shortSockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "djs")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

// startServer boots Serve on a temp socket and returns a dial helper plus a
// cancel that shuts the server down at test cleanup.
func startServer(t *testing.T, state *State) func(t *testing.T) net.Conn {
	t.Helper()

	sock := shortSockPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, state, sock) }()

	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Error("serve did not shut down within 500ms")
		}
	})

	// Wait for the listener to come up.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	return func(t *testing.T) net.Conn {
		t.Helper()
		conn, err := net.Dial("unix", sock)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
		return conn
	}
}

func TestProtocol_Ping(t *testing.T) {
	db := newTestDB(t)
	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dial := startServer(t, state)

	conn := dial(t)
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(Envelope{Type: "ping"}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp PingResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Pong {
		t.Errorf("want pong=true, got %+v", resp)
	}
}

func TestProtocol_RecordThenSuggest(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)
	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dial := startServer(t, state)

	const novel = "echo socket-record"

	// Record over the socket.
	{
		conn := dial(t)
		payload, _ := json.Marshal(RecordReq{
			Command:   novel,
			Dir:       "/repo",
			SessionID: "p1",
		})
		if err := json.NewEncoder(conn).Encode(Envelope{Type: "record", Payload: payload}); err != nil {
			t.Fatalf("encode record: %v", err)
		}
		var resp RecordResp
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		conn.Close()
	}

	// Suggest sees the freshly recorded command.
	{
		conn := dial(t)
		defer conn.Close()
		payload, _ := json.Marshal(SuggestReq{Buffer: "echo socket", Dir: "/repo"})
		if err := json.NewEncoder(conn).Encode(Envelope{Type: "suggest", Payload: payload}); err != nil {
			t.Fatalf("encode suggest: %v", err)
		}
		var resp SuggestResp
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			t.Fatalf("decode suggest: %v", err)
		}
		if resp.Suggestion != novel {
			t.Errorf("want %q, got %q (alts=%v)", novel, resp.Suggestion, resp.Alternatives)
		}
	}
}

func TestProtocol_UnknownTypeKeepsServerAlive(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)
	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dial := startServer(t, state)

	// Send a bogus envelope. The handler hits no case and closes the conn.
	{
		conn := dial(t)
		if err := json.NewEncoder(conn).Encode(Envelope{Type: "bogus"}); err != nil {
			t.Fatalf("encode bogus: %v", err)
		}
		conn.Close()
	}

	// A subsequent suggest still works — server didn't crash.
	conn := dial(t)
	defer conn.Close()
	payload, _ := json.Marshal(SuggestReq{Buffer: "git c", Dir: "/repo"})
	if err := json.NewEncoder(conn).Encode(Envelope{Type: "suggest", Payload: payload}); err != nil {
		t.Fatalf("encode suggest: %v", err)
	}
	var resp SuggestResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode suggest: %v", err)
	}
	if resp.Suggestion == "" {
		t.Errorf("want a suggestion, got empty response %+v", resp)
	}
}

func TestProtocol_MalformedJSONKeepsServerAlive(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)
	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dial := startServer(t, state)

	// Garbage bytes — Decode fails, handler returns and closes.
	{
		conn := dial(t)
		if _, err := conn.Write([]byte("not-json-at-all\n")); err != nil {
			t.Fatalf("write garbage: %v", err)
		}
		conn.Close()
	}

	// Server still serves.
	conn := dial(t)
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(Envelope{Type: "ping"}); err != nil {
		t.Fatalf("encode ping: %v", err)
	}
	var resp PingResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode ping: %v", err)
	}
	if !resp.Pong {
		t.Errorf("want pong=true, got %+v", resp)
	}
}

func TestProtocol_ImmediateCloseKeepsServerAlive(t *testing.T) {
	db := newTestDB(t)
	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dial := startServer(t, state)

	// Dial and close without sending anything.
	conn := dial(t)
	conn.Close()

	// Server still answers a ping.
	conn = dial(t)
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(Envelope{Type: "ping"}); err != nil {
		t.Fatalf("encode ping: %v", err)
	}
	var resp PingResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode ping: %v", err)
	}
	if !resp.Pong {
		t.Errorf("want pong=true, got %+v", resp)
	}
}

func TestProtocol_MalformedPayloadInValidEnvelope(t *testing.T) {
	db := newTestDB(t)
	seed(t, db)
	state, err := Load(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dial := startServer(t, state)

	// Valid envelope, payload is not a JSON object the handler can unmarshal.
	{
		conn := dial(t)
		env := Envelope{Type: "suggest", Payload: json.RawMessage(`"this is a string, not an object"`)}
		if err := json.NewEncoder(conn).Encode(env); err != nil {
			t.Fatalf("encode: %v", err)
		}
		// Read deadline elapses or conn closes — either way no panic.
		var resp SuggestResp
		_ = json.NewDecoder(conn).Decode(&resp)
		conn.Close()
	}

	// Server is still alive.
	conn := dial(t)
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(Envelope{Type: "ping"}); err != nil {
		t.Fatalf("encode ping: %v", err)
	}
	var resp PingResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode ping: %v", err)
	}
	if !resp.Pong {
		t.Errorf("want pong=true, got %+v", resp)
	}
}
