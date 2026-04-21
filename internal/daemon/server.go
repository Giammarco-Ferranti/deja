package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

const connDeadline = 2 * time.Second

// Serve listens on sockPath and dispatches envelopes to the state handlers.
// It returns when ctx is cancelled or the listener errors. On return the
// socket file is removed.
func Serve(ctx context.Context, state *State, sockPath string) error {
	_ = os.Remove(sockPath)

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", sockPath, err)
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

func handle(conn net.Conn, state *State) {
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
