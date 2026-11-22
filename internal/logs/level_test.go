package logs

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The same cases as frontend/src/lib/logview.test.ts ("levelOf"), so the
// Logs tab's Errors filter and MCP's errors_only agree.
func TestLineLevel(t *testing.T) {
	cases := []struct{ text, want string }{
		// Laravel / Monolog
		{`[2026-11-19 10:00:01] production.ERROR: SQLSTATE[HY000] Connection refused {"exception":"[object] ..."}`, "error"},
		{`[2026-11-19 10:00:01] local.WARNING: Slow query`, "warn"},
		{`[2026-11-19T10:00:01.123456+00:00] app.INFO: User logged in`, "info"},
		{`[2026-11-19 10:00:01] production.DEBUG: cache hit`, "debug"},
		{`[2026-11-19 10:00:01] production.CRITICAL: down`, "error"},
		// Horizon / queue:work, new and old
		{`  2026-11-17 15:32:00 Modules\Machinery\Listeners\HandleMaintenanceCompletion ...... 81.02ms FAIL`, "error"},
		{`  2026-11-17 15:32:00 App\Jobs\SendMail ............................. 12.10ms DONE`, "info"},
		{`  2026-11-17 15:32:00 App\Jobs\SendMail ............................. RUNNING`, "debug"},
		{`[2026-11-17 15:32:00][abc123] Failed:     App\Jobs\SendMail`, "error"},
		{`[2026-11-17 15:32:00][abc123] Processed:  App\Jobs\SendMail`, "info"},
		// JSON lines
		{`{"level":50,"time":1700000000,"msg":"boom"}`, "error"},
		{`{"level":30,"msg":"listening"}`, "info"},
		{`{"message":"x","level":400,"level_name":"ERROR"}`, "error"},
		{`{"ts":"2026-11-19T10:00:00Z","severity":"warning","msg":"retry"}`, "warn"},
		// logfmt / Go slog
		{`time=2026-11-19T10:00:00Z level=ERROR msg="dial failed"`, "error"},
		{`ts=1 lvl=warn msg=slow`, "warn"},
		// Python, Rails, klog, nginx, Celery
		{`ERROR:root:something broke`, "error"},
		{`2026-11-19 10:00:00,123 - app - WARNING - disk at 90%`, "warn"},
		{`E, [2026-11-19T10:00:00.000 #1234] ERROR -- : boom`, "error"},
		{`W1119 10:00:00.000000   1 reflector.go:1] watch closed`, "warn"},
		{`2026/11/19 10:00:00 [error] 12#12: *1 connect() failed`, "error"},
		{`[2026-11-19 10:00:00,000: INFO/MainProcess] Connected`, "info"},
		// Thrown, no level word
		{`Traceback (most recent call last):`, "error"},
		{`TypeError: Cannot read properties of undefined`, "error"},
		{`Illuminate\Database\QueryException: SQLSTATE`, "error"},
		{`panic: runtime error: index out of range`, "error"},
		// No level
		{`Server started on port 8000`, ""},
		{`no error found in 12 files`, ""},
		{`processing 4 items`, ""},
		{`{"msg":"no level here"}`, ""},
		{``, ""},
	}
	for _, c := range cases {
		if got := LineLevel(c.text); got != c.want {
			t.Errorf("LineLevel(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestErrorsOnly(t *testing.T) {
	log := strings.Join([]string{
		`[2026-11-19 10:00:00] production.ERROR: boom {"exception":"x"}`,
		`[stacktrace]`,
		`#0 /app/vendor/Foo.php(12): bar()`,
		`#1 {main}`,
		`"} `,
		`[2026-11-19 10:00:01] production.INFO: fine`,
		`plain line`,
		`    indented, but the line above it has no level`,
		`Exception in thread "main" java.lang.IllegalStateException`,
		"\tat com.example.Main.main(Main.java:5)",
		`INFO ok`,
	}, "\n") + "\n"
	want := strings.Join([]string{
		`[2026-11-19 10:00:00] production.ERROR: boom {"exception":"x"}`,
		`[stacktrace]`,
		`#0 /app/vendor/Foo.php(12): bar()`,
		`#1 {main}`,
		`"} `,
		`Exception in thread "main" java.lang.IllegalStateException`,
		"\tat com.example.Main.main(Main.java:5)",
	}, "\n") + "\n"
	if got := ErrorsOnly(log); got != want {
		t.Fatalf("ErrorsOnly:\n%s\nwant\n%s", got, want)
	}
	if got := ErrorsOnly("all fine\nINFO ok\n"); got != "" {
		t.Fatalf("no errors: %q", got)
	}
}

func TestCutKeepsTheEnd(t *testing.T) {
	big := strings.Repeat("é", 100*1024) // one 200 KB line
	got, cut := CutFront(big, 64*1024)
	if !cut || len(got) > 64*1024 || !utf8.ValidString(got) || !strings.HasSuffix(big, got) {
		t.Fatalf("one huge line: cut %v, %d bytes, valid %v", cut, len(got), utf8.ValidString(got))
	}

	lines := "first\nsecond\nthird\n"
	if got, cut := CutFront(lines, 12); !cut || got != "third\n" {
		t.Fatalf("starts at a line: %q %v", got, cut)
	}
	if got, cut := CutFront(lines, 100); cut || got != lines {
		t.Fatalf("short text: %q %v", got, cut)
	}
	if got, _ := CutFront("ok \xff\xfe bytes", 100); !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8 kept: %q", got)
	}
}
