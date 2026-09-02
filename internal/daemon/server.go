package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"
)

const (
	connDeadline = 2 * time.Second
	// ProbeTimeout bounds the liveness probe. Exported alongside IsLiveSocket so
	// callers outside this package ask the same question with the same bound.
	ProbeTimeout = 50 * time.Millisecond

	// checkpointInterval is how often the WAL is truncated. See checkpointLoop
	// for why it is long rather than eager.
	checkpointInterval = 10 * time.Minute
)

// PidPath returns the pidfile that accompanies a daemon listening on sockPath.
// Deriving it from the socket keeps a single source of truth for "where the
// daemon lives", the same way the socket path already is.
func PidPath(sockPath string) string {
	return filepath.Join(filepath.Dir(sockPath), "daemon.pid")
}

// Serve listens on sockPath and dispatches envelopes to the state handlers.
// It returns when ctx is cancelled or the listener errors. On return the
// socket file is removed.
//
// If the initial bind fails, Serve probes the existing socket: a live daemon
// (one that answers ping) is left alone and Serve returns an error; a stale
// file (no answer within ProbeTimeout) is removed and bind is retried once.
func Serve(ctx context.Context, state *State, sockPath string) error {
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		if IsLiveSocket(sockPath, ProbeTimeout) {
			return fmt.Errorf("daemon already running at %s", sockPath)
		}
		if rmErr := os.Remove(sockPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("remove stale socket %s: %w", sockPath, rmErr)
		}
		l, err = net.Listen("unix", sockPath)
		if err != nil {
			return fmt.Errorf("listen %s: %w", sockPath, err)
		}
	}
	if err := os.Chmod(sockPath, 0o600); err != nil {
		l.Close()
		os.Remove(sockPath)
		return fmt.Errorf("chmod %s: %w", sockPath, err)
	}

	// Written only once the bind has succeeded, so the pidfile means "this
	// process owns the socket" and never points at a daemon that failed to
	// start. A failure to write it is not fatal — the daemon still works; only
	// `deja daemon --restart` loses the ability to identify the process.
	pidPath := PidPath(sockPath)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "deja daemon: write %s: %v (--restart will not be able to find this daemon)\n", pidPath, err)
	} else {
		defer os.Remove(pidPath)
	}

	go func() {
		<-ctx.Done()
		l.Close()
	}()

	go checkpointLoop(ctx, state)

	defer os.Remove(sockPath)

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		go handle(conn, state)
	}
}

// IsLiveSocket reports whether sockPath has a daemon currently answering ping
// within timeout. Any failure (dial error, no/garbled response, Pong=false) is
// treated as not-live so the caller can clear a stale file and rebind.
//
// Exported because it is also the only portable way to tell a daemon that is
// still serving from a pidfile that merely outlived one: see stopRunningDaemon.
func IsLiveSocket(sockPath string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("unix", sockPath, timeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	if err := json.NewEncoder(conn).Encode(Envelope{Type: "ping"}); err != nil {
		return false
	}
	var resp PingResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return false
	}
	return resp.Pong
}

// checkpointLoop truncates the write-ahead log periodically, since SQLite's own
// checkpointing bounds the WAL's size but never reduces it. See State.CheckpointWAL.
//
// The interval is long on purpose. Reclaiming disk is not urgent, and a
// TRUNCATE checkpoint has to wait out readers, so doing it often would trade a
// real cost against a benefit measured in megabytes per day.
func checkpointLoop(ctx context.Context, state *State) {
	t := time.NewTicker(checkpointInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Busy is the expected outcome when a reader is mid-snapshot, and the
			// next tick will retry, so it is not worth reporting on its own. Any
			// error is logged rather than swallowed: a checkpoint that fails every
			// time means the WAL grows forever, and silence would hide that.
			if err := state.CheckpointWAL(); err != nil {
				fmt.Fprintf(os.Stderr, "deja daemon: wal checkpoint: %v\n", err)
			}
		}
	}
}

func handle(conn net.Conn, state *State) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "deja daemon: connection panic: %v\n%s", r, debug.Stack())
		}
	}()
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(connDeadline))

	// Two request encodings share this socket. Peek one byte to tell them
	// apart: `{` is JSON, anything else is the text protocol. See text.go for
	// why zsh gets its own encoding rather than building JSON by hand.
	//
	// Routing on `{` alone rather than also skipping leading whitespace is
	// deliberate. Every JSON client is `json.Encoder`, which never emits leading
	// whitespace, whereas a stray newline from a text client is plausible — and
	// handing a lone "\n" to the JSON decoder would hold the connection open for
	// the full connDeadline waiting for a value that never comes. The text
	// handler rejects what it does not understand and closes immediately.
	br := bufio.NewReader(conn)
	first, err := br.Peek(1)
	if err != nil {
		return
	}
	if first[0] != '{' {
		handleText(br, conn, state)
		return
	}

	dec := json.NewDecoder(br)
	enc := json.NewEncoder(conn)

	var env Envelope
	if err := dec.Decode(&env); err != nil {
		return
	}

	switch env.Type {
	case "suggest":
		var req SuggestReq
		if err := json.Unmarshal(env.Payload, &req); err != nil {
			return
		}
		_ = enc.Encode(state.Suggest(req, time.Now()))

	case "record":
		var req RecordReq
		if err := json.Unmarshal(env.Payload, &req); err != nil {
			return
		}
		if err := state.Record(req); err != nil {
			fmt.Fprintf(os.Stderr, "deja daemon: record: %v\n", err)
		}
		_ = enc.Encode(RecordResp{})

	case "ping":
		_ = enc.Encode(state.Ping())

	case "setconfig":
		var req SetConfigReq
		if err := json.Unmarshal(env.Payload, &req); err != nil {
			return
		}
		_ = enc.Encode(state.SetConfig(req))

	case "getconfig":
		_ = enc.Encode(state.GetConfig())
	}
}
