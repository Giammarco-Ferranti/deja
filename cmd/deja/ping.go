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
	fs := flag.NewFlagSet("ping", flag.ExitOnError)
	fs.Parse(args)

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
