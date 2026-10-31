package logs_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/logs"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// lineNumbers parses "line N" lines, ignoring a possibly partial last line.
func lineNumbers(t *testing.T, text string) []int {
	t.Helper()
	var out []int
	for _, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(l, "line ") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(l, "line "))
		if err != nil {
			continue // partial line at a poll boundary
		}
		out = append(out, n)
	}
	return out
}

// wholeLines drops a possibly partial last line.
func wholeLines(s string) string {
	if nl := strings.LastIndexByte(s, '\n'); nl >= 0 {
		return s[:nl]
	}
	return ""
}

func TestTailSpammerNoGapsNoDupes(t *testing.T) {
	i := supervisortest.Start(t, supervisortest.Spammer)
	i.WaitFor(t, "spammer", supervisor.StateRunning)
	tl := logs.NewTailer(i.Client(), "spammer", logs.Stdout, 0)
	ctx := context.Background()

	// At least minPolls polls, so reads cross many chunk boundaries, then on
	// until enough lines have arrived: the spammer forks `sleep` per line,
	// which is slow on a loaded CI runner, so a fixed time is not enough.
	// Chunks are concatenated before parsing so a line split across two polls
	// is whole again.
	const minPolls, want = 20, 40
	var got strings.Builder
	var nums []int
	polls := 0
	deadline := time.Now().Add(15 * time.Second)
	for (polls < minPolls || len(nums) < want) && time.Now().Before(deadline) {
		c, err := tl.Poll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if c.Skipped || c.Truncated {
			t.Fatalf("unexpected chunk flags: %+v", c)
		}
		got.WriteString(c.Text)
		polls++
		nums = lineNumbers(t, wholeLines(got.String()))
		time.Sleep(60 * time.Millisecond)
	}
	if len(nums) < want {
		t.Fatalf("only %d lines after %d polls", len(nums), polls)
	}
	for k := 1; k < len(nums); k++ {
		if nums[k] != nums[k-1]+1 {
			t.Fatalf("line %d follows line %d (gap or duplicate)", nums[k], nums[k-1])
		}
	}
	if tl.Buffer() != got.String() {
		t.Errorf("Buffer differs from the concatenated chunks")
	}
}

func TestTailAfterClearLogs(t *testing.T) {
	i := supervisortest.Start(t, supervisortest.Spammer)
	i.WaitFor(t, "spammer", supervisor.StateRunning)
	tl := logs.NewTailer(i.Client(), "spammer", logs.Stdout, 0)
	ctx := context.Background()

	var before []int
	deadline := time.Now().Add(10 * time.Second)
	for len(before) < 40 && time.Now().Before(deadline) {
		if _, err := tl.Poll(ctx); err != nil {
			t.Fatal(err)
		}
		before = lineNumbers(t, wholeLines(tl.Buffer()))
		time.Sleep(50 * time.Millisecond)
	}
	if len(before) < 40 {
		t.Fatalf("only %d lines before clearing", len(before))
	}
	last := before[len(before)-1]

	if err := i.Client().ClearProcessLogs(ctx, "spammer"); err != nil {
		t.Fatal(err)
	}
	truncated := false
	for range 100 {
		c, err := tl.Poll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		truncated = truncated || c.Truncated
		if truncated {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !truncated {
		t.Fatal("clearing the logs was never reported as Truncated")
	}
	// New lines keep arriving, and the buffer holds only the current log.
	time.Sleep(300 * time.Millisecond)
	if _, err := tl.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	after := lineNumbers(t, wholeLines(tl.Buffer()))
	if len(after) == 0 || after[0] <= last-len(before) {
		t.Fatalf("buffer after clear = %v (before ended at %d)", after, last)
	}
	for k := 1; k < len(after); k++ {
		if after[k] != after[k-1]+1 {
			t.Fatalf("gap or duplicate after clear: %v", after)
		}
	}
	if after[len(after)-1] <= last {
		t.Errorf("no new lines after clear: last %d, now %d", last, after[len(after)-1])
	}
}

// supervisord 4.2.x answers a tail of a missing log (stderr_logfile=NONE)
// with an empty result rather than a NO_FILE fault, so it must poll quietly.
func TestTailMissingLogPollsEmpty(t *testing.T) {
	quiet := supervisortest.Spammer
	quiet.Name = "quiet"
	quiet.Extra = map[string]string{"stderr_logfile": "NONE"}
	i := supervisortest.Start(t, quiet)
	i.WaitFor(t, "quiet", supervisor.StateRunning)
	tl := logs.NewTailer(i.Client(), "quiet", logs.Stderr, 0)
	for range 3 {
		c, err := tl.Poll(context.Background())
		if err != nil || c != (logs.Chunk{}) {
			t.Fatalf("poll = %+v, %v; want an empty chunk", c, err)
		}
	}
}
