package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/giammarcoferranti/deja/internal/daemon"
)

func runPing(args []string) {
	fs := flag.NewFlagSet("ping", flag.ContinueOnError)
	fs.Usage = func() {
		w := os.Stdout
		fs.SetOutput(w)
		fmt.Fprintln(w, "deja ping — check if the daemon is running")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  deja ping")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Connects to ~/.local/share/deja/sock and sends a ping. Prints \"pong\" and")
		fmt.Fprintln(w, "exits 0 on success; prints the underlying error to stderr and exits 1 if")
		fmt.Fprintln(w, "the daemon is unreachable. Useful for debugging missing suggestions.")
	}
	parseFlags(fs, args)

	sock, err := sockPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja ping: %v\n", err)
		os.Exit(1)
	}

	conn, err := net.DialTimeout("unix", sock, 200*time.Millisecond)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deja ping: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(500 * time.Millisecond))

	if err := json.NewEncoder(conn).Encode(daemon.Envelope{Type: "ping"}); err != nil {
		fmt.Fprintf(os.Stderr, "deja ping: %v\n", err)
		os.Exit(1)
	}

	var resp daemon.PingResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		fmt.Fprintf(os.Stderr, "deja ping: %v\n", err)
		os.Exit(1)
	}

	if !resp.Pong {
		os.Exit(1)
	}
	fmt.Println("pong")
}
