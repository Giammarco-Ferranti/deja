package daemon

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func newBufReader(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }

func mustLoad(t *testing.T) *State {
	t.Helper()
	state, err := Load(newTestDB(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return state
}

// sendText writes one text-protocol request and reads the response to EOF —
// exactly what the zsh side does: connect, print one line, read until the
// daemon closes.
func sendText(t *testing.T, dial func(t *testing.T) net.Conn, line string) string {
	t.Helper()
	conn := dial(t)
	defer conn.Close()
	if _, err := io.WriteString(conn, line+"\n"); err != nil {
		t.Fatalf("write %q: %v", line, err)
	}
	out, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read after %q: %v", line, err)
	}
	return string(out)
}

func TestEscapeUnescape_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"plain", "git status"},
		{"backslash", `grep -E '\d+' file`},
		{"double backslash", `printf 'a\\b'`},
		{"newline", "git commit -m 'line one\nline two'"},
		{"unit separator", "weird\x1fcommand"},
		{"all three", "a\\b\nc\x1fd"},
		{"trailing backslash", `cd C:\`},
		{"empty", ""},
		{"non-ascii", "echo 'héllo → 世界'"},
		{"looks like an escape", `\n \s \\ \q`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			esc := escapeTextField(tc.in)
			if strings.ContainsAny(esc, "\n\x1f") {
				t.Fatalf("escaped form still contains a framing byte: %q", esc)
			}
			got, err := unescapeTextField(esc)
			if err != nil {
				t.Fatalf("unescape(%q): %v", esc, err)
			}
			if got != tc.in {
				t.Errorf("round trip: got %q, want %q", got, tc.in)
			}
		})
	}
}

// Unescaping is strict on purpose: a mismatch between the Go and zsh sides must
// fail loudly here rather than silently corrupt a recorded command.
func TestUnescapeTextField_RejectsGarbage(t *testing.T) {
	for _, in := range []string{`\`, `a\`, `\q`, `\x1f`, `ok\zbad`} {
		if got, err := unescapeTextField(in); err == nil {
			t.Errorf("unescape(%q) = %q, want an error", in, got)
		}
	}
}

func TestParseTextRequest(t *testing.T) {
	verb, fields, err := parseTextRequest("suggest\x1fgit st\x1f/tmp\x1fls")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if verb != "suggest" {
		t.Errorf("verb = %q, want suggest", verb)
	}
	want := []string{"git st", "/tmp", "ls"}
	if len(fields) != len(want) {
		t.Fatalf("fields = %q, want %q", fields, want)
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Errorf("field %d = %q, want %q", i, fields[i], want[i])
		}
	}

	// A verb with no fields is legal (ping).
	if verb, fields, err := parseTextRequest("ping"); err != nil || verb != "ping" || len(fields) != 0 {
		t.Errorf("parse(ping) = %q, %q, %v", verb, fields, err)
	}

	if _, _, err := parseTextRequest(""); err == nil {
		t.Error("empty line should not parse")
	}
	if _, _, err := parseTextRequest("suggest\x1fbad\\qescape\x1f/tmp\x1fls"); err == nil {
		t.Error("a bad escape in a field should fail the whole request")
	}
}

func TestFormatTextCandidates(t *testing.T) {
	if got := formatTextCandidates(SuggestResp{}); got != "" {
		t.Errorf("no suggestion should render as empty, got %q", got)
	}
	got := formatTextCandidates(SuggestResp{Suggestion: "git status", Alternatives: []string{"git stash", "git show"}})
	if want := "git status\x1fgit stash\x1fgit show"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadTextLine_CapsLength(t *testing.T) {
	// No newline within the cap: must fail rather than buffer without bound.
	huge := strings.Repeat("x", maxTextRequest+10)
	if _, err := readTextLine(newBufReader(huge), maxTextRequest); err != errTextTooLong {
		t.Errorf("got %v, want errTextTooLong", err)
	}
	// Exactly at the cap, terminated, is fine.
	ok := strings.Repeat("x", maxTextRequest-1) + "\n"
	if got, err := readTextLine(newBufReader(ok), maxTextRequest); err != nil || len(got) != maxTextRequest-1 {
		t.Errorf("len=%d err=%v", len(got), err)
	}
	// Unterminated input is an error (io.EOF), never a silent partial request.
	if _, err := readTextLine(newBufReader("suggest\x1fa"), maxTextRequest); err != io.EOF {
		t.Errorf("got %v, want io.EOF", err)
	}
}

func TestText_Ping(t *testing.T) {
	dial := startServer(t, mustLoad(t))
	if got := sendText(t, dial, "ping"); got != "pong" {
		t.Errorf("ping = %q, want pong", got)
	}
}

func TestText_RecordThenSuggest(t *testing.T) {
	state := mustLoad(t)
	dial := startServer(t, state)

	if got := sendText(t, dial, "record\x1fgit rebase -i\x1f/repo\x1f0\x1f12\x1fsess\x1fgit status"); got != "ok" {
		t.Fatalf("record = %q, want ok", got)
	}

	got := sendText(t, dial, "suggest\x1fgit reb\x1f/repo\x1fgit status")
	if !strings.HasPrefix(got, "git rebase -i") {
		t.Errorf("suggest = %q, want the just-recorded command first", got)
	}
}

// A command containing the framing bytes must survive the whole trip: escaped by
// the client, unescaped into the DB, and returned verbatim in a suggestion.
func TestText_RecordPreservesFramingBytes(t *testing.T) {
	state := mustLoad(t)
	dial := startServer(t, state)

	cmd := "echo 'first\nsecond'"
	req := "record" + textFieldSep + escapeTextField(cmd) +
		textFieldSep + "/repo" + textFieldSep + "0" + textFieldSep + "1" +
		textFieldSep + "sess" + textFieldSep + ""
	if got := sendText(t, dial, req); got != "ok" {
		t.Fatalf("record = %q, want ok", got)
	}

	resp := state.Suggest(SuggestReq{Buffer: "echo", Dir: "/repo"}, time.Now())
	if resp.Suggestion != cmd {
		t.Errorf("suggestion = %q, want %q", resp.Suggestion, cmd)
	}
}

func TestText_MalformedRequestsKeepServerAlive(t *testing.T) {
	dial := startServer(t, mustLoad(t))

	for _, bad := range []string{
		"nosuchverb",
		"suggest",                     // too few fields
		"suggest\x1fa\x1fb\x1fc\x1fd", // too many
		"record\x1fonly-one-field",
		"suggest\x1fbad\\qescape\x1f/tmp\x1fls",
		"",
	} {
		if got := sendText(t, dial, bad); got != "" {
			t.Errorf("%q drew a response %q, want silence", bad, got)
		}
	}

	// The server is still healthy afterwards, on both encodings.
	if got := sendText(t, dial, "ping"); got != "pong" {
		t.Errorf("text ping after malformed input = %q", got)
	}
}

// The two encodings share one socket, so JSON clients must keep working
// untouched. Routing is on a leading `{`, which is what json.Encoder — the only
// JSON client deja has — always emits.
func TestText_JSONClientsStillWork(t *testing.T) {
	dial := startServer(t, mustLoad(t))

	conn := dial(t)
	if _, err := io.WriteString(conn, `{"type":"ping"}`+"\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, _ := io.ReadAll(conn)
	conn.Close()
	if !strings.Contains(string(out), `"pong":true`) {
		t.Errorf("JSON ping returned %q", out)
	}
}

// A lone newline must not be handed to the JSON decoder: it would block until
// connDeadline waiting for a value. The text handler rejects it and closes.
func TestText_EmptyLineClosesPromptly(t *testing.T) {
	dial := startServer(t, mustLoad(t))

	start := time.Now()
	if got := sendText(t, dial, ""); got != "" {
		t.Errorf("empty line drew %q, want silence", got)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Errorf("empty line took %v to close; it is being parsed as JSON", elapsed)
	}
}

func TestText_OversizedRequestIsRejected(t *testing.T) {
	dial := startServer(t, mustLoad(t))

	conn := dial(t)
	// No newline, more than the cap: the daemon must give up rather than buffer.
	io.WriteString(conn, "suggest\x1f"+strings.Repeat("x", maxTextRequest+1024))
	conn.Close()

	if got := sendText(t, dial, "ping"); got != "pong" {
		t.Errorf("server unhealthy after oversized request: ping = %q", got)
	}
}
