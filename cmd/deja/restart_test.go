package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/giammarcoferranti/deja/internal/daemon"
)

// shortSockPath returns a socket path short enough to bind.
//
// t.TempDir embeds the test's own name in the path. Under macOS $TMPDIR
// (/var/folders/<...>/T/, 49 bytes before anything else) a descriptive test
// name pushes the result past sun_path, which holds 104 bytes there against
// 108 on Linux. os.MkdirTemp omits the name; this mirrors the helper of the
// same name in internal/daemon, which exists for exactly this reason.
func shortSockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "deja")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

// answerPing listens on sock and replies to the liveness probe, standing in for
// a serving daemon. A real one would need a database and loaded state; the
// probe only looks for a pong, and the branch under test only asks the probe.
func answerPing(t *testing.T, sock string) {
	t.Helper()
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen %s: %v", sock, err)
	}
	t.Cleanup(func() { l.Close() })

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var env daemon.Envelope
				if json.NewDecoder(c).Decode(&env) != nil {
					return
				}
				json.NewEncoder(c).Encode(daemon.PingResp{Pong: true})
			}(conn)
		}
	}()
}

// TestStopRunningDaemon_LeavesAnUnrelatedProcessAlone is the regression test.
//
// The pidfile is removed with a defer, so it survives SIGKILL, an OOM kill or a
// power loss. What is left names a pid deja no longer owns, and pids get
// recycled — so `deja daemon --restart` would SIGTERM whatever inherited the
// number, under the user's own uid, where permissions do not stop it.
//
// The setup is that state exactly: a live process that is not deja, a pidfile
// naming it, and nothing listening.
func TestStopRunningDaemon_LeavesAnUnrelatedProcessAlone(t *testing.T) {
	sock := shortSockPath(t)

	standIn := exec.Command("sleep", "60")
	if err := standIn.Start(); err != nil {
		t.Fatalf("start stand-in process: %v", err)
	}
	pid := standIn.Process.Pid

	// Watch for the process ending via Wait, not via kill(pid, 0). A signalled
	// child becomes a zombie until it is reaped, and signal 0 still succeeds
	// against a zombie -- so the obvious liveness check would report the victim
	// as alive and the test would pass while the bug fired.
	exited := make(chan struct{})
	go func() {
		standIn.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		standIn.Process.Kill()
		<-exited
	})

	pidPath := daemon.PidPath(sock)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}

	if err := stopRunningDaemon(sock); err != nil {
		t.Fatalf("stopRunningDaemon: %v", err)
	}

	// Give a delivered signal time to land and be reaped, so a pass cannot come
	// from looking too early.
	select {
	case <-exited:
		t.Errorf("stopRunningDaemon signalled pid %d, which was not the daemon", pid)
	case <-time.After(500 * time.Millisecond):
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("stale pidfile left in place; the next --restart would read it again")
	}
}

// The other half of the contract. A "fix" that never signals anything would
// pass the test above, so this pins that a live daemon is still acted on.
//
// It asserts via the no-pidfile branch rather than by killing anything: with the
// socket answering and no pidfile present, stopRunningDaemon must reach the
// existing "listening but wrote no pidfile" error. Reaching it proves the
// liveness probe said yes and the function did not short-circuit.
func TestStopRunningDaemon_ActsOnALiveDaemon(t *testing.T) {
	sock := shortSockPath(t)
	answerPing(t, sock)

	if !daemon.IsLiveSocket(sock, daemon.ProbeTimeout) {
		t.Fatal("stand-in daemon is not answering; the test would prove nothing")
	}

	err := stopRunningDaemon(sock)
	if err == nil {
		t.Fatal("expected the no-pidfile error for a live daemon, got nil — the live branch was skipped")
	}
	if !strings.Contains(err.Error(), "wrote no pidfile") {
		t.Errorf("unexpected error %q; wanted the live-daemon-without-pidfile path", err)
	}
}

// Nothing running and nothing left behind must stay a silent no-op.
func TestStopRunningDaemon_NoDaemonIsANoOp(t *testing.T) {
	sock := shortSockPath(t)
	if err := stopRunningDaemon(sock); err != nil {
		t.Errorf("stopRunningDaemon on a cold machine returned %v, want nil", err)
	}
}
