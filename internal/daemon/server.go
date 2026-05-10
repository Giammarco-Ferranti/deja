package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime/debug"
	"time"
)

const (
	connDeadline  = 2 * time.Second
	probeTimeout  = 50 * time.Millisecond
)

// Serve listens on sockPath and dispatches envelopes to the state handlers.
// It returns when ctx is cancelled or the listener errors. On return the
// socket file is removed.
//
// If the initial bind fails, Serve probes the existing socket: a live daemon
// (one that answers ping) is left alone and Serve returns an error; a stale
// file (no answer within probeTimeout) is removed and bind is retried once.
func Serve(ctx context.Context, state *State, sockPath string) error {
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		if isLiveSocket(sockPath, probeTimeout) {
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

	go func() {
		<-ctx.Done()
		l.Close()
	}()

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

// isLiveSocket reports whether sockPath has a daemon currently answering ping
// within timeout. Any failure (dial error, no/garbled response, Pong=false) is
// treated as not-live so the caller can clear a stale file and rebind.
func isLiveSocket(sockPath string, timeout time.Duration) bool {
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

func handle(conn net.Conn, state *State) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "deja daemon: connection panic: %v\n%s", r, debug.Stack())
		}
	}()
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(connDeadline))

	dec := json.NewDecoder(conn)
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
	}
}
