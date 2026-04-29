package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/giammarcoferranti/deja/internal/daemon"
	"github.com/giammarcoferranti/deja/internal/store"
)

func runRecord(args []string) {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	cmd := fs.String("command", "", "the command that was executed")
	dir := fs.String("dir", "", "working directory at execution time")
	exit := fs.Int("exit", 0, "exit code")
	dur := fs.Int("duration", 0, "duration in milliseconds")
	session := fs.String("session", "", "shell session id")
	prev := fs.String("prev", "", "previously executed command")
	fs.Usage = func() {
		w := os.Stdout
		fs.SetOutput(w)
		fmt.Fprintln(w, "deja record — record an executed command into the local DB")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  deja record [flags]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Flags:")
		fs.PrintDefaults()
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Normally invoked by the zsh preexec/precmd hooks installed by")
		fmt.Fprintln(w, `  eval "$(deja init zsh)"`)
		fmt.Fprintln(w, "and not run interactively. Falls back to a direct SQLite write if the")
		fmt.Fprintln(w, "daemon is unavailable. A missing -command argument is a silent no-op.")
	}
	parseFlags(fs, args)

	if *cmd == "" {
		return
	}

	req := daemon.RecordReq{
		Command:    *cmd,
		Dir:        *dir,
		ExitCode:   *exit,
		DurationMS: *dur,
		SessionID:  *session,
		Prev:       *prev,
	}

	if dialAndRecord(req) == nil {
		return
	}
	fallbackRecord(req)
}

func dialAndRecord(req daemon.RecordReq) error {
	sock, err := sockPath()
	if err != nil {
		return err
	}

	conn, err := net.DialTimeout("unix", sock, dialTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(readTimeout))

	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(conn).Encode(daemon.Envelope{Type: "record", Payload: payload}); err != nil {
		return err
	}

	var resp daemon.RecordResp
	return json.NewDecoder(conn).Decode(&resp)
}

func fallbackRecord(req daemon.RecordReq) {
	path, err := dbPath()
	if err != nil {
		return
	}
	db, err := store.InitDB(path)
	if err != nil {
		return
	}

	_ = store.RecordCommand(db, store.Command{
		Command:    req.Command,
		Directory:  req.Dir,
		Timestamp:  time.Now(),
		ExitCode:   req.ExitCode,
		DurationMS: req.DurationMS,
		SessionID:  req.SessionID,
	}, req.Prev)
}
