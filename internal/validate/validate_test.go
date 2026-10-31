package validate

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/touhidurabir/visor/internal/config"
)

// fakeEnv models a machine where php and /srv/acme exist, /var/log/acme does
// not, and /var/log/ro is read-only.
func fakeEnv() Env {
	fsys := fstest.MapFS{
		"srv/acme":     {Mode: fs.ModeDir | 0o755},
		"srv/acme/bin": {Mode: fs.ModeDir | 0o755},
		"srv/file":     {Data: []byte("x")},
		"var/log/ok":   {Mode: fs.ModeDir | 0o755},
		"var/log/ro":   {Mode: fs.ModeDir | 0o555},
		"opt/noexec":   {Data: []byte("x"), Mode: 0o644},
		"opt/runnable": {Data: []byte("x"), Mode: 0o755},
		"opt/homebrew": {Mode: fs.ModeDir | 0o755},
	}
	return Env{
		Inventory: config.Inventory{Files: []config.ConfigFile{
			{Path: "/conf/mailer.conf", Programs: []config.Program{{Name: "mailer-worker"}}},
			{Path: "/conf/self.conf", Programs: []config.Program{{Name: "self-prog"}}},
		}},
		IncludeGlobs: []string{"/conf/*.conf"},
		LookPath: func(b string) (string, error) {
			switch b {
			case "php":
				return "/opt/homebrew/bin/php", nil
			case "/opt/runnable":
				return b, nil
			}
			return "", errors.New("not found")
		},
		UserExists: func(u string) bool { return u == "root" || u == "www" },
		Stat: func(p string) (fs.FileInfo, error) {
			return fs.Stat(fsys, strings.TrimPrefix(p, "/"))
		},
		Writable: func(d string) bool { return d != "/var/log/ro" },
	}
}

func find(cs []Check, level Level, sub string) *Check {
	for i := range cs {
		if cs[i].Level == level && strings.Contains(cs[i].Message, sub) {
			return &cs[i]
		}
	}
	return nil
}

func TestValidateRules(t *testing.T) {
	const head = "[program:acme]\ncommand=php artisan x\n"
	tests := []struct {
		name, file, content string
		self                string
		level               Level
		contains            string
		line                int
	}{
		{"file name outside glob", "acme.ini", head, "", Error, "acme.ini", 0},
		{"parse error", "acme.conf", "[program:acme\ncommand=x", "", Error, "unterminated", 1},
		{"no program", "acme.conf", "[group:x]\nprograms=a\n", "", Error, "[program:name]", 0},
		{"duplicate name", "new.conf", "[program:mailer-worker]\ncommand=php x\n", "", Error, "already exists in mailer.conf", 0},
		{"own name is fine", "self.conf", "[program:self-prog]\ncommand=php x\n", "/conf/self.conf", OK, "self-prog is unique", 0},
		{"unique name", "new.conf", head, "", OK, "acme is unique", 0},
		{"no command", "a.conf", "[program:acme]\n", "", Error, "has no command", 0},
		{"unknown key far", "a.conf", head + "zzzzzzzz=1\n", "", Warning, "unknown key zzzzzzzz", 3},
		{"unknown key close", "a.conf", head + "autorestrat=true\n", "", Warning, "Did you mean autorestart?", 3},
		{"int bad", "a.conf", head + "startsecs=ten\n", "", Error, "startsecs must be a whole number", 3},
		{"int backups bad", "a.conf", head + "stdout_logfile_backups=-1\n", "", Error, "whole number", 3},
		{"bytes bad", "a.conf", head + "stdout_logfile_maxbytes=lots\n", "", Error, "stdout_logfile_maxbytes", 3},
		{"bytes good", "a.conf", head + "stdout_logfile_maxbytes=50MB\n", "", OK, "acme is unique", 0},
		{"bool bad", "a.conf", head + "autostart=maybe\n", "", Error, "autostart must be", 3},
		{"bool good", "a.conf", head + "autostart=Yes\n", "", OK, "acme is unique", 0},
		{"autorestart bad", "a.conf", head + "autorestart=sometimes\n", "", Error, "true, false or unexpected", 3},
		{"stopsignal bad", "a.conf", head + "stopsignal=STOP\n", "", Error, "stopsignal must be", 3},
		{"numprocs needs process_num", "a.conf", head + "numprocs=2\n", "", Error, "%(process_num)", 3},
		{"numprocs with process_num", "a.conf", head + "numprocs=2\nprocess_name=%(program_name)s_%(process_num)02d\n", "", OK, "acme is unique", 0},
		{"command found", "a.conf", head, "", OK, "php found at /opt/homebrew/bin/php", 0},
		{"command missing", "a.conf", "[program:acme]\ncommand=nosuchtool --x\n", "", Warning, "Could not find nosuchtool", 0},
		{"absolute missing", "a.conf", "[program:acme]\ncommand=/opt/gone\n", "", Warning, "/opt/gone", 0},
		{"absolute not executable", "a.conf", "[program:acme]\ncommand=/opt/noexec\n", "", Error, "not executable", 0},
		{"absolute executable", "a.conf", "[program:acme]\ncommand=/opt/runnable run\n", "", OK, "exists and is executable", 0},
		{"quoted command", "a.conf", "[program:acme]\ncommand=\"/opt/runnable\" run\n", "", OK, "exists and is executable", 0},
		{"directory relative", "a.conf", head + "directory=srv\n", "", Error, "absolute", 3},
		{"directory missing", "a.conf", head + "directory=/srv/nope\n", "", Warning, "/srv/nope does not exist", 3},
		{"directory is a file", "a.conf", head + "directory=/srv/file\n", "", Error, "not a folder", 3},
		{"directory ok", "a.conf", head + "directory=/srv/acme\n", "", OK, "/srv/acme exists", 0},
		{"user missing", "a.conf", head + "user=ghost\n", "", Warning, "user ghost", 3},
		{"user root", "a.conf", head + "user=root\n", "", Warning, "runs as root", 3},
		{"log dir missing", "a.conf", head + "stdout_logfile=/var/log/acme/w.log\n", "", Error, "/var/log/acme does not exist", 3},
		{"log dir read-only", "a.conf", head + "stderr_logfile=/var/log/ro/w.log\n", "", Error, "not writable", 3},
		{"log dir ok", "a.conf", head + "stdout_logfile=/var/log/ok/w.log\n", "", OK, "/var/log/ok is writable", 0},
		{"log AUTO skipped", "a.conf", head + "stdout_logfile=AUTO\nstderr_logfile=NONE\n", "", OK, "acme is unique", 0},
		{"environment bad", "a.conf", head + "environment=PORT 3000\n", "", Error, "KEY=\"value\"", 3},
		{"environment good", "a.conf", head + "environment=NODE_ENV=\"production\",PORT=\"3000\"\n", "", OK, "acme is unique", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := fakeEnv()
			env.SelfPath = tc.self
			cs := File(tc.file, tc.content, env)
			c := find(cs, tc.level, tc.contains)
			if c == nil {
				t.Fatalf("no %s check containing %q in %+v", tc.level, tc.contains, cs)
			}
			if tc.line != 0 && c.Line != tc.line {
				t.Errorf("line = %d, want %d (%+v)", c.Line, tc.line, c)
			}
		})
	}
}

func TestFileNameInsideGlobHasNoCheck(t *testing.T) {
	for _, c := range File("acme.conf", "[program:acme]\ncommand=php x\n", fakeEnv()) {
		if c.Key == "" && c.Program == "" {
			t.Errorf("unexpected file-level check %+v", c)
		}
	}
}

func TestValidateGoodFilesHaveNoErrors(t *testing.T) {
	cs := File("a.conf", "[program:acme]\ncommand=php artisan x\n", fakeEnv())
	if HasErrors(cs) {
		t.Errorf("unexpected errors: %+v", cs)
	}
	if !HasErrors([]Check{{Level: OK}, {Level: Error}}) {
		t.Error("HasErrors missed an error")
	}
}

// The AddRaw wireframe starter text.
func TestValidateWireframeExample(t *testing.T) {
	const start = "[program:acme-worker]\ncommand=php artisan queue:work --tries=3\ndirectory=/srv/acme\nnumprocs=3\nprocess_name=%(program_name)s\nautorestrat=true\nstdout_logfile=/var/log/acme/worker.log\n"
	cs := File("acme-worker.conf", start, fakeEnv())
	type want struct {
		level Level
		sub   string
	}
	wants := []want{
		{Error, "numprocs=3 needs %(process_num)"},
		{Error, "log folder /var/log/acme does not exist"},
		{Warning, "Did you mean autorestart?"},
		{OK, "acme-worker is unique"},
		{OK, "php found at"},
		{OK, "/srv/acme exists"},
	}
	if len(cs) != len(wants) {
		t.Fatalf("got %d checks, want %d: %+v", len(cs), len(wants), cs)
	}
	for _, w := range wants {
		if find(cs, w.level, w.sub) == nil {
			t.Errorf("missing %s %q in %+v", w.level, w.sub, cs)
		}
	}
	// sorted: errors, then warnings, then ok
	rank := map[Level]int{Error: 0, Warning: 1, OK: 2}
	for i := 1; i < len(cs); i++ {
		if rank[cs[i-1].Level] > rank[cs[i].Level] {
			t.Errorf("not sorted at %d: %+v", i, cs)
		}
	}
	if find(cs, Error, "numprocs").Program != "acme-worker" {
		t.Error("Program not set")
	}
}

func TestDefaultsDoNotPanic(t *testing.T) {
	dir := t.TempDir()
	cs := File("a.conf", "[program:acme]\ncommand=/bin/sh -c true\ndirectory="+dir+"\nstdout_logfile="+dir+"/x.log\nuser=root\n", Env{})
	if find(cs, OK, dir+" exists") == nil {
		t.Errorf("real Stat default not used: %+v", cs)
	}
}
