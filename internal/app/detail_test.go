package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

func TestConfigDetailPaths(t *testing.T) {
	spammer := supervisortest.Spammer
	spammer.Extra = map[string]string{"directory": "%(here)s/.."}
	inst := supervisortest.Start(t, spammer)
	inst.WaitFor(t, "spammer:spammer", supervisor.StateRunning)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "spammer running", procState("spammer:spammer", "RUNNING"))

	path := filepath.Join(inst.Dir(), "conf.d", "spammer.ini")
	d, err := svc.ConfigDetail(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.Config.Path != path || len(d.Config.Processes) != 1 || d.Programs != 1 {
		t.Errorf("config = %+v", d.Config)
	}
	kinds := map[string][]PathView{}
	for _, p := range d.Paths {
		kinds[p.Kind] = append(kinds[p.Kind], p)
		if !p.Exists {
			t.Errorf("%s %q: Exists = false", p.Label, p.Path)
		}
	}
	if dirs := kinds["dir"]; len(dirs) != 1 || dirs[0].Path != inst.Dir() {
		t.Errorf("dirs = %+v, want %s (with %%(here)s expanded and cleaned)", dirs, inst.Dir())
	}
	if bins := kinds["bin"]; len(bins) != 1 || filepath.Base(bins[0].Path) != "sh" || !filepath.IsAbs(bins[0].Path) || bins[0].Label != "Command (sh)" {
		t.Errorf("bins = %+v, want the resolved sh", bins)
	}
	logs := kinds["log"]
	want := []string{filepath.Join(inst.Dir(), "logs", "spammer.out.log"), filepath.Join(inst.Dir(), "logs", "spammer.err.log")}
	if len(logs) != 2 || logs[0].Path != want[0] || logs[1].Path != want[1] {
		t.Errorf("logs = %+v, want %v", logs, want)
	}
	if len(d.Logs) != 1 || d.Logs[0].Process != "spammer:spammer" || d.Logs[0].Stdout != want[0] || d.Logs[0].Stderr != want[1] {
		t.Errorf("process logs = %+v", d.Logs)
	}
	if d.LastError != nil {
		t.Errorf("LastError = %+v, want nil while running", d.LastError)
	}

	if _, err := svc.ConfigDetail(filepath.Join(inst.Dir(), "conf.d", "missing.ini")); err == nil {
		t.Error("ConfigDetail on a file outside the include glob: want error")
	}
}

func TestConfigDetailLastError(t *testing.T) {
	failer := supervisortest.Failer
	failer.Command = `sh -c 'echo first >&2; echo "boom" >&2; exit 3'`
	inst := supervisortest.Start(t, failer, supervisortest.Sleeper)
	inst.WaitFor(t, "failer:failer", supervisor.StateExited)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitOverview(t, svc, 5*time.Second, "failer exited", procState("failer:failer", "EXITED"))

	d, err := svc.ConfigDetail(filepath.Join(inst.Dir(), "conf.d", "failer.ini"))
	if err != nil {
		t.Fatal(err)
	}
	le := d.LastError
	if le == nil {
		t.Fatal("LastError = nil, want the failer's exit")
	}
	if le.Process != "failer:failer" || le.State != "EXITED" || le.ExitStatus != 3 {
		t.Errorf("last error = %+v", le)
	}
	if !strings.Contains(le.Message, "status 3") {
		t.Errorf("message = %q, want the exit status", le.Message)
	}
	if !strings.HasSuffix(le.StderrTail, "first\nboom") {
		t.Errorf("stderr tail = %q, want its last lines", le.StderrTail)
	}
	if le.StderrLog != filepath.Join(inst.Dir(), "logs", "failer.err.log") {
		t.Errorf("stderr log = %q", le.StderrLog)
	}
	if le.Since == "" {
		t.Error("Since is empty")
	}

	d, err = svc.ConfigDetail(filepath.Join(inst.Dir(), "conf.d", "sleeper.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if d.LastError != nil {
		t.Errorf("sleeper.ini LastError = %+v, want nil", d.LastError)
	}
}

// M2 known gap: a file action without any connection must say why there
// is no connection, not that the file is unknown.
func TestFileActionWithoutConnectionReportsConnection(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "missing.conf")
	rt, _ := startRuntime(t, conf, t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	want := svc.Overview().Server.Error
	for name, do := range map[string]func(string) error{"start": svc.StartFile, "stop": svc.StopFile, "restart": svc.RestartFile} {
		err := do("/tmp/somewhere/x.ini")
		if err == nil || err.Error() != want {
			t.Errorf("%s file = %v, want the connection error %q", name, err, want)
		}
	}
}

// M10: the config page without a connection says why there is none, not
// that the file is not one of supervisord's.
func TestConfigDetailWithoutConnectionReportsConnection(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "missing.conf")
	rt, _ := startRuntime(t, conf, t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	want := svc.Overview().Server.Error
	if _, err := svc.ConfigDetail("/tmp/somewhere/x.ini"); err == nil || err.Error() != want {
		t.Errorf("ConfigDetail = %v, want the connection error %q", err, want)
	}
}

func TestLastErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		p    supervisor.ProcessInfo
		want string
	}{
		{"fatal exited too quickly",
			supervisor.ProcessInfo{State: supervisor.StateFatal, SpawnErr: "Exited too quickly (process log may have details)", ExitStatus: 1},
			"Exited too quickly (process log may have details). supervisord gave up after 4 start attempts."},
		{"fatal spawn error",
			supervisor.ProcessInfo{State: supervisor.StateFatal, SpawnErr: "can't find command '/nonexistent/sync'"},
			"Can't find command '/nonexistent/sync'. supervisord gave up after 4 start attempts."},
		{"backoff",
			supervisor.ProcessInfo{State: supervisor.StateBackoff, SpawnErr: "Exited too quickly (process log may have details)", ExitStatus: 2},
			"Exited too quickly (process log may have details). supervisord is retrying."},
		{"unexpected exit",
			supervisor.ProcessInfo{State: supervisor.StateExited, ExitStatus: 3},
			"Exited with status 3, which this program does not expect (expected 0)."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lastErrorMessage(c.p, 3, []int{0}); got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestCommandBinary(t *testing.T) {
	for in, want := range map[string]string{
		"php artisan queue:work":       "php",
		"  /usr/bin/env  node app.js":  "/usr/bin/env",
		`"/opt/my app/bin/run" --fast`: "/opt/my app/bin/run",
		`'/opt/x y/run'`:               "/opt/x y/run",
		"%(here)s/bin/worker --n=1":    "/etc/sv/bin/worker",
		"":                             "",
	} {
		if got := commandBinary(in, map[string]string{"here": "/etc/sv"}); got != want {
			t.Errorf("commandBinary(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildPathsSharedAndAutoLogs(t *testing.T) {
	src := "[program:worker]\ncommand=php artisan queue:work\ndirectory=/srv/app\nnumprocs=2\nprocess_name=%(program_name)s_%(process_num)02d\n" +
		"redirect_stderr=true\nstdout_logfile=/srv/app/worker.log\n\n" +
		"[program:digest]\ncommand=/srv/app/digest\ndirectory=/srv/app\nstdout_logfile=NONE\n"
	inv := inventoryOf(t, src)
	procs := []supervisor.ProcessInfo{
		{Group: "worker", Name: "worker_00", StdoutLogfile: "/srv/app/worker.log"},
		{Group: "worker", Name: "worker_01", StdoutLogfile: "/srv/app/worker.log"},
		{Group: "digest", Name: "digest", StderrLogfile: "/var/log/sv/digest-stderr---supervisor-x1.log"},
	}
	exists := func(p string) bool { return p != "/srv/app/digest" }
	look := func(name string) (string, error) {
		if name == "php" {
			return "/opt/homebrew/bin/php", nil
		}
		return "", errors.New("not found")
	}
	paths, logs := buildPaths(inv.Files[0], inv, procs, look, exists)
	got := make([]string, 0, len(paths))
	for _, p := range paths {
		got = append(got, p.Kind+"|"+p.Label+"|"+p.Path+"|"+map[bool]string{true: "ok", false: "missing"}[p.Exists])
	}
	want := []string{
		"dir|Working directory|/srv/app|ok",
		"bin|Command (php)|/opt/homebrew/bin/php|ok",
		"bin|Command (digest)|/srv/app/digest|missing",
		"log|worker log|/srv/app/worker.log|ok",
		"log|digest errors (auto)|/var/log/sv/digest-stderr---supervisor-x1.log|ok",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("paths:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	wantLogs := []ProcessLogView{
		{Process: "digest:digest", Stdout: "", Stderr: "/var/log/sv/digest-stderr---supervisor-x1.log"},
		{Process: "worker:worker_00", Stdout: "/srv/app/worker.log", Stderr: "/srv/app/worker.log"},
		{Process: "worker:worker_01", Stdout: "/srv/app/worker.log", Stderr: "/srv/app/worker.log"},
	}
	if len(logs) != len(wantLogs) {
		t.Fatalf("logs = %+v", logs)
	}
	for i := range logs {
		if logs[i] != wantLogs[i] {
			t.Errorf("logs[%d] = %+v, want %+v", i, logs[i], wantLogs[i])
		}
	}
}

// inventoryOf loads an inventory from src written to a temp include dir.
func inventoryOf(t *testing.T, src string) config.Inventory {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.ini"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := config.LoadInventory(config.Discovery{IncludeGlobs: []string{filepath.Join(dir, "*.ini")}})
	if err != nil {
		t.Fatal(err)
	}
	return inv
}
