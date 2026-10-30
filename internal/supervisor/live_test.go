package supervisor_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// liveSockets are the Homebrew default socket paths (Apple Silicon, Intel).
var liveSockets = []string{"/opt/homebrew/var/run/supervisor.sock", "/usr/local/var/run/supervisor.sock"}

// TestLiveReadOnly checks the client against the supervisord on this
// machine. It is opt-in (VISOR_LIVE=1) and calls read-only methods only:
// never add a state-changing call here. VISOR_LIVE_SOCKET overrides the
// socket path.
func TestLiveReadOnly(t *testing.T) {
	if os.Getenv("VISOR_LIVE") != "1" {
		t.Skip("set VISOR_LIVE=1 to run read-only checks against the local supervisord")
	}
	sock := os.Getenv("VISOR_LIVE_SOCKET")
	for _, p := range liveSockets {
		if sock != "" {
			break
		}
		if _, err := os.Stat(p); err == nil {
			sock = p
		}
	}
	if sock == "" {
		t.Fatalf("no supervisord socket found in %v; set VISOR_LIVE_SOCKET", liveSockets)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := supervisor.New(supervisor.UnixTransport{Path: sock})

	v, err := c.GetSupervisorVersion(ctx)
	if err != nil {
		t.Fatalf("%s: %v", sock, err)
	}
	st, err := c.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	methods, err := c.ListMethods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	procs, err := c.GetAllProcessInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Log counts only: process names and log contents stay out of test output.
	byState := map[string]int{}
	for _, p := range procs {
		byState[p.State.String()]++
	}
	t.Logf("%s: supervisord %s, %s, %d methods, %d processes %v", sock, v, st.Name, len(methods), len(procs), byState)
}
