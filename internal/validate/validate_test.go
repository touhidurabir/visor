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
		{"int backups bad", "a.conf", head + "stdout_logfile_backups=x\n", "", Error, "whole number", 3},
		{"bytes bad", "a.conf", head + "stdout_logfile_maxbytes=lots\n", "", Error, "stdout_logfile_maxbytes", 3},
		{"bytes good", "a.conf", head + "stdout_logfile_maxbytes=50MB\n", "", OK, "acme is unique", 0},
		{"bool bad", "a.conf", head + "autostart=maybe\n", "", Error, "autostart must be", 3},
		{"bool good", "a.conf", head + "autostart=Yes\n", "", OK, "acme is unique", 0},
		{"autorestart bad", "a.conf", head + "autorestart=sometimes\n", "", Error, "true, false or unexpected", 3},
		{"stopsignal bad", "a.conf", head + "stopsignal=FOO\n", "", Error, "stopsignal must be", 3},
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
		{"user missing", "a.conf", head + "user=ghost\n", "", Error, "user ghost", 3},
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

// Values supervisord 4.2.5 accepts (datatypes.py) must not block a save:
// a working config has to stay savable and restorable.
func TestValidateAcceptsWhatSupervisordAccepts(t *testing.T) {
	const head = "[program:acme]\ncommand=php artisan x\n"
	for _, extra := range []string{
		"autorestart=yes", "autorestart=1", "autorestart=on", "autorestart=no", "autorestart=off",
		"autorestart=0", "autorestart=UNEXPECTED", "autorestart=True",
		"stopsignal=15", "stopsignal=WINCH", "stopsignal=STOP", "stopsignal=SIGINFO", "stopsignal=sigusr2",
		"priority=-1", "stdout_logfile_backups=-1", "startsecs=+5", "startretries=1_000",
		"stdout_logfile_maxbytes=5 MB", "stdout_logfile_maxbytes=-1KB", "stderr_capture_maxbytes=1kb",
		"environment=A='x y'", "environment=A=/usr/bin:/bin", "environment=A=\"1\",\n  B=\"2\"",
		"environment=A=1;B=2", "environment=HOME=\"%(ENV_HOME)s\"", "environment=", "environment=A=\"50%%\"",
		"exitcodes=0,2", "exitcodes=0, 2", "umask=022", "umask=0o22",
		"numprocs=%(ENV_COPIES)s\nprocess_name=%(program_name)s_%(process_num)s",
		"stopasgroup=true\nkillasgroup=true", "killasgroup=true",
		"process_name= acme ",
	} {
		cs := File("a.conf", head+extra+"\n", fakeEnv())
		for _, c := range cs {
			if c.Level == Error {
				t.Errorf("%q: unexpected error %q", extra, c.Message)
			}
		}
	}
}

// Values supervisord rejects (its reloadConfig would fail with CANT_REREAD
// for every file) block the save.
func TestValidateRejectsWhatSupervisordRejects(t *testing.T) {
	const head = "[program:acme]\ncommand=php artisan x\n"
	tests := []struct{ extra, key, contains string }{
		{"environment=A=a@b", "environment", "environment"},
		{"environment=A=x=y", "environment", "environment"},
		{"environment=A=", "environment", "environment"},
		{"environment=A=\"open", "environment", "quote"},
		{"environment=A=1 B=2", "environment", "environment"},
		{"stopsignal=FOO", "stopsignal", "signal"},
		{"stopsignal=32", "stopsignal", "signal"},
		{"autorestart=always", "autorestart", "unexpected"},
		{"autostart=maybe", "autostart", "true or false"},
		{"exitcodes=256", "exitcodes", "0 to 255"},
		{"exitcodes=0,,2", "exitcodes", "0 to 255"},
		{"umask=8", "umask", "octal"},
		{"priority=1.5", "priority", "whole number"},
		{"stdout_logfile_maxbytes=5TB", "stdout_logfile_maxbytes", "KB, MB or GB"},
		{"stopasgroup=true\nkillasgroup=false", "killasgroup", "killasgroup"},
		{"process_name=a b", "process_name", "space"},
		{"user=ghost", "user", "ghost"},
	}
	for _, tc := range tests {
		cs := File("a.conf", head+tc.extra+"\n", fakeEnv())
		found := false
		for _, c := range cs {
			if c.Level == Error && c.Key == tc.key && strings.Contains(c.Message, tc.contains) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: no error on %s containing %q in %+v", tc.extra, tc.key, tc.contains, cs)
		}
	}
	cs := File("a.conf", "[program:a b]\ncommand=php x\n", fakeEnv())
	if c := find(cs, Error, "space"); c == nil {
		t.Errorf("[program:a b]: no error in %+v", cs)
	}
}

// supervisord sets A to "caf" for A=café (it skips the token after each
// value): that saves, with a warning.
func TestValidateWarnsAboutDroppedEnvironmentText(t *testing.T) {
	cs := File("a.conf", "[program:acme]\ncommand=php x\nenvironment=A=café\n", fakeEnv())
	if HasErrors(cs) {
		t.Errorf("errors: %+v", cs)
	}
	if c := find(cs, Warning, `"caf"`); c == nil || c.Key != "environment" || c.Line != 3 {
		t.Errorf("no warning naming what A becomes: %+v", cs)
	}
}

// Include files are read into the same parser as supervisord.conf, so these
// sections there would override supervisord's own settings (brief §5: never
// edit them).
func TestValidateRefusesSupervisordSections(t *testing.T) {
	for _, sec := range []string{"supervisord", "unix_http_server", "inet_http_server", "supervisorctl", "include", "rpcinterface:supervisor", "rpcinterface:x"} {
		cs := File("a.conf", "[program:acme]\ncommand=php x\n\n["+sec+"]\nfile=/tmp/x.sock\n", fakeEnv())
		c := find(cs, Error, "["+sec+"]")
		if c == nil || c.Line != 4 || !strings.Contains(c.Message, "supervisord.conf") {
			t.Errorf("[%s]: no error on line 4 in %+v", sec, cs)
		}
	}
}

func TestUserExistsTakesUIDs(t *testing.T) {
	if !userExists("0") || !userExists("root") {
		t.Error("uid 0 / root not found")
	}
	if userExists("visor-no-such-user") || userExists("987654") {
		t.Error("a missing user was found")
	}
}

// Every problem says how to fix it; some carry the exact edit.
func TestChecksSayHowToFix(t *testing.T) {
	const head = "[program:acme]\ncommand=php x\n"
	tests := []struct {
		name, content, contains string
		fix                     string
		edit                    *Edit
	}{
		{"numprocs on process_name line", head + "numprocs=3\nprocess_name=%(program_name)s\n", "%(process_num)", "acme_00, acme_01, acme_02",
			&Edit{Line: 4, Text: "process_name=%(program_name)s_%(process_num)02d"}},
		{"numprocs without process_name", head + "numprocs=2\n", "%(process_num)", "acme_00, acme_01",
			&Edit{Line: 3, Insert: true, Text: "process_name=%(program_name)s_%(process_num)02d"}},
		{"typo", head + "autorestrat=true\n", "Did you mean", "autorestart", &Edit{Line: 3, Text: "autorestart=true", Rename: "autorestart"}},
		{"typo on a value that spans lines", head + "enviroment=A=\"1\",\n  B=\"2\"\n", "Did you mean", "environment",
			&Edit{Line: 3, Text: "environment=A=\"1\", …", Rename: "environment"}},
		{"killasgroup", head + "stopasgroup=true\nkillasgroup=false\n", "killasgroup can't", "", &Edit{Line: 4, Text: "killasgroup=true"}},
		{"missing binary", "[program:acme]\ncommand=/opt/gone --x\n", "/opt/gone", "full path", nil},
		{"not on PATH", "[program:acme]\ncommand=nosuchtool\n", "nosuchtool", "which nosuchtool", nil},
		{"not executable", "[program:acme]\ncommand=/opt/noexec\n", "not executable", "chmod +x /opt/noexec", nil},
		{"missing folder", head + "directory=/srv/nope\n", "/srv/nope", "project folder", nil},
		{"missing user", head + "user=ghost\n", "user ghost", "remove the line", nil},
		{"missing log folder", head + "stdout_logfile=/var/log/acme/w.log\n", "/var/log/acme", "mkdir -p /var/log/acme", nil},
		{"bad bool", head + "autostart=maybe\n", "autostart must be", "true or false", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cs := File("a.conf", tc.content, fakeEnv())
			var c *Check
			for i := range cs {
				if cs[i].Level != OK && strings.Contains(cs[i].Message, tc.contains) {
					c = &cs[i]
				}
			}
			if c == nil {
				t.Fatalf("no problem containing %q in %+v", tc.contains, cs)
			}
			if tc.fix != "" && !strings.Contains(c.Fix, tc.fix) {
				t.Errorf("Fix = %q, want it to mention %q", c.Fix, tc.fix)
			}
			if c.Fix == "" {
				t.Errorf("no Fix for %+v", c)
			}
			if (tc.edit == nil) != (c.Edit == nil) || (tc.edit != nil && *tc.edit != *c.Edit) {
				t.Errorf("Edit = %+v, want %+v", c.Edit, tc.edit)
			}
			if tc.edit != nil && !tc.edit.Insert && c.Line != tc.edit.Line {
				t.Errorf("check on line %d, its fix edits line %d", c.Line, tc.edit.Line)
			}
		})
	}
}

func TestRemoteChecksAskTheServer(t *testing.T) {
	content := "[program:w]\ncommand=/srv/app/bin/worker\ndirectory=/srv/app\nuser=www-data\nstdout_logfile=/var/log/app/w.log\n"
	asked := map[string]bool{}
	env := Env{Remote: true, SkipWritable: true,
		Stat: func(p string) (fs.FileInfo, error) {
			asked["stat "+p] = true
			return nil, fs.ErrNotExist
		},
		LookPath:   func(string) (string, error) { return "", fs.ErrNotExist },
		UserExists: func(u string) bool { asked["user "+u] = true; return false },
		Writable:   func(string) bool { t.Fatal("writable checked although skipped"); return false },
	}
	checks := File("w.conf", content, env)
	for _, k := range []string{"stat /srv/app", "stat /srv/app/bin/worker", "user www-data", "stat /var/log/app"} {
		if !asked[k] {
			t.Errorf("not asked: %s", k)
		}
	}
	text := ""
	for _, c := range checks {
		text += c.Message + " | " + c.Fix + "\n"
		if strings.Contains(c.Message+c.Fix, "Terminal") || strings.Contains(c.Message+c.Fix, "Mac") {
			t.Errorf("local wording on a server: %+v", c)
		}
	}
	for _, want := range []string{"user www-data does not exist on the server", "on the server: sudo mkdir -p /var/log/app", "/srv/app does not exist yet"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	// As root, a log folder that exists is checked for writing; not as root.
	env.Stat = func(string) (fs.FileInfo, error) { return dirInfo{}, nil }
	if cs := File("w.conf", content, env); !hasMessage(cs, "log folder /var/log/app exists") {
		t.Errorf("skipped writable: %+v", cs)
	}
}

type dirInfo struct{ fs.FileInfo }

func (dirInfo) IsDir() bool { return true }

func hasMessage(cs []Check, msg string) bool {
	for _, c := range cs {
		if c.Message == msg {
			return true
		}
	}
	return false
}
