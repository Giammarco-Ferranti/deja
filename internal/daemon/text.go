package daemon

// A line-oriented alternative to the JSON envelope, for clients that cannot
// cheaply build JSON — specifically zsh, which now talks to the daemon directly
// over `zsh/net/socket` so the keystroke path costs no fork+exec at all. Building
// JSON in zsh would mean hand-rolling string escaping for a format with far more
// escaping rules than this one needs.
//
// Request: one \n-terminated line, fields separated by US (0x1f):
//
//	ping
//	suggest<US>buffer<US>dir<US>prev
//	record<US>command<US>dir<US>exit<US>duration_ms<US>session<US>prev
//
// The newline terminator is what tells the daemon a request is complete. zsh
// cannot half-close a socket to signal EOF, so the frame has to be
// self-delimiting — which in turn means a field may contain neither a raw
// newline nor a raw US. Three characters are escaped:
//
//	\  -> \\
//	LF -> \n
//	US -> \s
//
// Unescaping is strict: an unknown escape is an error rather than a silent
// passthrough, so a mismatch between this and the zsh side surfaces in tests
// instead of quietly corrupting somebody's command history.
//
// Responses carry no framing and no escaping: the daemon closes the connection
// when it is done and the client reads to EOF. That makes the suggest response
// byte-identical to `deja query --format lines`, which the zsh side already
// knows how to parse, so this protocol changes only how the request travels.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// textFieldSep separates fields within a request line. US (unit separator)
	// is the same delimiter `deja query --format lines` already uses for
	// candidates, so the shell needs only one special byte in mind.
	textFieldSep = "\x1f"

	// maxTextRequest caps a single request line. Generous next to any real
	// command line, and the point is only to stop a confused or hostile peer
	// from making the daemon buffer without bound.
	maxTextRequest = 64 << 10
)

// errTextTooLong is returned when a request line exceeds maxTextRequest.
var errTextTooLong = errors.New("text request too long")

// escapeTextField renders s safe to place in a US-separated, newline-terminated
// frame. Backslash must be escaped first, or the escapes introduced for the
// other two characters would themselves be re-escaped.
func escapeTextField(s string) string {
	if !strings.ContainsAny(s, "\\\n"+textFieldSep) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\x1f':
			b.WriteString(`\s`)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// unescapeTextField is the inverse of escapeTextField. Unknown escapes and a
// trailing lone backslash are errors, not best-effort guesses.
func unescapeTextField(s string) (string, error) {
	if !strings.Contains(s, `\`) {
		return s, nil
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			return "", errors.New("trailing backslash in text field")
		}
		switch s[i] {
		case '\\':
			b.WriteByte('\\')
		case 'n':
			b.WriteByte('\n')
		case 's':
			b.WriteByte('\x1f')
		default:
			return "", fmt.Errorf("unknown escape %q in text field", `\`+string(s[i]))
		}
	}
	return b.String(), nil
}

// parseTextRequest splits a request line into its verb and unescaped fields.
func parseTextRequest(line string) (string, []string, error) {
	parts := strings.Split(line, textFieldSep)
	verb := parts[0]
	if verb == "" {
		return "", nil, errors.New("empty verb")
	}
	fields := make([]string, 0, len(parts)-1)
	for _, p := range parts[1:] {
		f, err := unescapeTextField(p)
		if err != nil {
			return "", nil, err
		}
		fields = append(fields, f)
	}
	return verb, fields, nil
}

// readTextLine reads up to and including the next newline, refusing to buffer
// more than max bytes. The newline is not returned.
func readTextLine(br *bufio.Reader, max int) (string, error) {
	var b strings.Builder
	for {
		c, err := br.ReadByte()
		if err != nil {
			return "", err
		}
		if c == '\n' {
			return b.String(), nil
		}
		if b.Len() >= max {
			return "", errTextTooLong
		}
		b.WriteByte(c)
	}
}

// formatTextCandidates renders a SuggestResp the way `deja query --format lines`
// does: the primary suggestion first, then alternatives, US-separated, and
// nothing at all when there is no suggestion.
func formatTextCandidates(resp SuggestResp) string {
	if resp.Suggestion == "" {
		return ""
	}
	cands := make([]string, 0, 1+len(resp.Alternatives))
	cands = append(cands, resp.Suggestion)
	cands = append(cands, resp.Alternatives...)
	return strings.Join(cands, textFieldSep)
}

// handleText serves one text-protocol request. Like the JSON path it answers a
// single request per connection and lets the caller close the socket, which is
// what signals end-of-response to the client.
//
// A malformed or unknown request draws no response at all — the same treatment
// the JSON path gives an unknown envelope type. The client learns "this daemon
// does not understand me" from the empty read, which is exactly how the zsh side
// detects a daemon too old to speak this protocol.
func handleText(br *bufio.Reader, w io.Writer, state *State) {
	line, err := readTextLine(br, maxTextRequest)
	if err != nil {
		return
	}

	verb, fields, err := parseTextRequest(line)
	if err != nil {
		return
	}

	switch verb {
	case "ping":
		io.WriteString(w, "pong")

	case "suggest":
		if len(fields) != 3 {
			return
		}
		resp := state.Suggest(SuggestReq{Buffer: fields[0], Dir: fields[1], Prev: fields[2]}, time.Now())
		io.WriteString(w, formatTextCandidates(resp))

	case "record":
		if len(fields) != 6 {
			return
		}
		// A non-numeric exit code or duration is not worth dropping the whole
		// record over; the command itself is the valuable part.
		exit, _ := strconv.Atoi(fields[2])
		dur, _ := strconv.Atoi(fields[3])
		req := RecordReq{
			Command:    fields[0],
			Dir:        fields[1],
			ExitCode:   exit,
			DurationMS: dur,
			SessionID:  fields[4],
			Prev:       fields[5],
		}
		if err := state.Record(req); err != nil {
			fmt.Fprintf(os.Stderr, "deja daemon: record: %v\n", err)
			return
		}
		io.WriteString(w, "ok")
	}
}
