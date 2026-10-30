package supervisor_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// TestLiveReadOnly checks the client against the supervisord on this
// machine. It is opt-in (VISOR_LIVE=1) and calls read-only methods only:
// never add a state-changing call here. VISOR_LIVE_SOCKET overrides the
// socket path; otherwise the socket comes from config.Discover.
func TestLiveReadOnly(t *testing.T) {
	if os.Getenv("VISOR_LIVE") != "1" {
		t.Skip("set VISOR_LIVE=1 to run read-only checks against the local supervisord")
	}
	var (
		tr    supervisor.Transport
		where string
		opts  []supervisor.Option
	)
	if sock := os.Getenv("VISOR_LIVE_SOCKET"); sock != "" {
		tr, where = supervisor.UnixTransport{Path: sock}, sock
	} else {
		d, err := config.Discover(config.DefaultPaths)
		if err != nil {
			t.Fatalf("%v; set VISOR_LIVE_SOCKET", err)
		}
		if d.Transport == nil {
			t.Fatalf("%s defines no socket or port; set VISOR_LIVE_SOCKET", d.ConfigPath)
		}
		tr, where = d.Transport, d.ConfigPath
		if d.Username != "" {
			opts = append(opts, supervisor.WithBasicAuth(d.Username, d.Password))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := supervisor.New(tr, opts...)

	v, err := c.GetSupervisorVersion(ctx)
	if err != nil {
		t.Fatalf("%s: %v", where, err)
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
	t.Logf("%s: supervisord %s, %s, %d methods, %d processes %v", where, v, st.Name, len(methods), len(procs), byState)
}
