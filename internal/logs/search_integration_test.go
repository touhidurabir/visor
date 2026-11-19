package logs_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/touhidurabir/visor/internal/logs"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// searchAll runs SearchBack from the end to the start of the log.
func searchAll(t *testing.T, c logs.SearchClient, query string, step, limit int) ([]string, int) {
	t.Helper()
	var got []string
	before, steps := -1, 0
	for {
		p, err := logs.SearchBack(context.Background(), c, "quiet", logs.Stdout, query, before, step, limit-len(got))
		if err != nil {
			t.Fatalf("step %d (before %d): %v", steps, before, err)
		}
		got = append(got, p.Lines...)
		steps++
		if p.Next == 0 || p.Capped || len(got) == limit {
			return got, steps
		}
		before = p.Next
	}
}

// A real supervisord: steps that split multi-byte characters and \r\n line
// endings still find every matching line once, newest first.
func TestSearchBackWholeLog(t *testing.T) {
	quiet := supervisortest.Program{Name: "quiet", Command: "sleep 1000"}
	i := supervisortest.Start(t, quiet)
	i.WaitFor(t, "quiet", supervisor.StateRunning)
	path := filepath.Join(i.Dir(), "logs", "quiet.out.log")
	var b strings.Builder
	var want []string
	for n := 1; n <= 400; n++ {
		end := "\n"
		if n%3 == 0 {
			end = "\r\n"
		}
		line := fmt.Sprintf("[2026-11-19 10:%02d] production.INFO: héllo wörld ✓ %d", n%60, n)
		if n%7 == 0 {
			line = fmt.Sprintf("[2026-11-19 10:%02d] production.ERROR: Fäiled job %d", n%60, n)
			want = append(want, line)
		}
		b.WriteString(line + end)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	slices.Reverse(want)

	for _, step := range []int{97, 1000, 1 << 20} {
		got, steps := searchAll(t, i.Client(), "fäiled", step, 1000)
		if !slices.Equal(got, want) {
			t.Errorf("step %d: got %d lines, want %d\n got %q\nwant %q", step, len(got), len(want), got[:min(3, len(got))], want[:3])
		}
		t.Logf("step %d: %d steps", step, steps)
	}
	got, _ := searchAll(t, i.Client(), "ERROR", 1000, 5)
	if !slices.Equal(got, want[:5]) {
		t.Errorf("limit 5: got %q, want the newest 5", got)
	}
}

// Bytes that aren't UTF-8 fail the read in supervisord; SearchBack says so.
func TestSearchBackNotText(t *testing.T) {
	quiet := supervisortest.Program{Name: "quiet", Command: "sleep 1000"}
	i := supervisortest.Start(t, quiet)
	i.WaitFor(t, "quiet", supervisor.StateRunning)
	if err := os.WriteFile(filepath.Join(i.Dir(), "logs", "quiet.out.log"), []byte("ok\n\xff\xfe bad\nok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := logs.SearchBack(context.Background(), i.Client(), "quiet", logs.Stdout, "ok", -1, 1<<20, 10)
	if !errors.Is(err, logs.ErrNotText) {
		t.Fatalf("err = %v, want ErrNotText", err)
	}
}
