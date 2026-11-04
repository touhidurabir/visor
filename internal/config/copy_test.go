package config

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestCopyAs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		file, content     string
		taken, takenFiles []string
		wantFile, want    string
		wantRenames       []Rename
		wantDoubts        []string // "line key name"
	}{
		{
			name:        "one program",
			file:        "mailer.ini",
			content:     "[program:mailer]\ncommand=php artisan queue:work\nprocess_name=%(program_name)s_%(process_num)02d\n",
			taken:       []string{"mailer"},
			takenFiles:  []string{"mailer.ini"},
			wantFile:    "mailer-copy.ini",
			want:        "[program:mailer-copy]\ncommand=php artisan queue:work\nprocess_name=%(program_name)s_%(process_num)02d\n",
			wantRenames: []Rename{{"program", "mailer", "mailer-copy"}},
		},
		{
			name:     "several programs",
			file:     "mailer.ini",
			content:  "[program:mailer-worker]\ncommand=sleep 1\n\n[program:mailer-digest]\ncommand=sleep 2\n",
			want:     "[program:mailer-worker-copy]\ncommand=sleep 1\n\n[program:mailer-digest-copy]\ncommand=sleep 2\n",
			wantFile: "mailer-copy.ini",
			wantRenames: []Rename{
				{"program", "mailer-worker", "mailer-worker-copy"},
				{"program", "mailer-digest", "mailer-digest-copy"},
			},
		},
		{
			name: "group section",
			file: "web.conf",
			content: "[group:web]\nprograms = api , web ; both\npriority=10\n\n" +
				"[program:api]\ncommand=sleep 1\n\n[program:web]\ncommand=sleep 2\n",
			taken:    []string{"api", "web"},
			wantFile: "web-copy.conf",
			// The group may share web-copy with its own program, as the
			// original group shares web with its program.
			want: "[group:web-copy]\nprograms = api-copy , web-copy ; both\npriority=10\n\n" +
				"[program:api-copy]\ncommand=sleep 1\n\n[program:web-copy]\ncommand=sleep 2\n",
			wantRenames: []Rename{
				{"program", "api", "api-copy"},
				{"program", "web", "web-copy"},
				{"group", "web", "web-copy"},
			},
		},
		{
			name:     "group list over several lines, one member elsewhere",
			file:     "g.ini",
			content:  "[group:g]\nprograms=a,\n  ; note\n  b, shared\n\n[program:a]\ncommand=x\n\n[program:b]\ncommand=y\n",
			wantFile: "g-copy.ini",
			want:     "[group:g-copy]\nprograms=a-copy,\n  ; note\n  b-copy, shared\n\n[program:a-copy]\ncommand=x\n\n[program:b-copy]\ncommand=y\n",
			wantRenames: []Rename{
				{"program", "a", "a-copy"}, {"program", "b", "b-copy"}, {"group", "g", "g-copy"},
			},
			wantDoubts: []string{"2 programs shared"},
		},
		{
			name:    "names already taken, groups and programs as one set",
			file:    "mailer.ini",
			content: "[program:mailer]\ncommand=sleep 1\n\n[group:jobs]\nprograms=mailer\n",
			// mailer-copy is a program elsewhere, mailer-copy-2 a group:
			// both are taken. jobs-copy is a program, so the group can't
			// have it either.
			taken:    []string{"mailer", "mailer-copy", "mailer-copy-2", "jobs", "jobs-copy"},
			wantFile: "mailer-copy.ini",
			want:     "[program:mailer-copy-3]\ncommand=sleep 1\n\n[group:jobs-copy-2]\nprograms=mailer-copy-3\n",
			wantRenames: []Rename{
				{"program", "mailer", "mailer-copy-3"}, {"group", "jobs", "jobs-copy-2"},
			},
		},
		{
			name:     "a program of the copy is not given another's new name",
			file:     "a.ini",
			content:  "[program:a]\ncommand=x\n[program:a-copy]\ncommand=y\n",
			want:     "[program:a-copy-2]\ncommand=x\n[program:a-copy-copy]\ncommand=y\n",
			wantFile: "a-copy.ini",
			wantRenames: []Rename{
				{"program", "a", "a-copy-2"}, {"program", "a-copy", "a-copy-copy"},
			},
		},
		{
			name:        "file name taken, ignoring case",
			file:        "mailer.ini",
			content:     "[program:mailer]\ncommand=x\n",
			takenFiles:  []string{"mailer.ini", "Mailer-Copy.ini", "mailer-copy-2.ini", "mailer-copy-3.conf"},
			wantFile:    "mailer-copy-3.ini",
			want:        "[program:mailer-copy]\ncommand=x\n",
			wantRenames: []Rename{{"program", "mailer", "mailer-copy"}},
		},
		{
			name: "comments, key order and spacing kept",
			file: "web.ini",
			content: "; Web app\r\n# owned by ops\r\n\r\n[program:web]   ; the site\r\n" +
				"Command = node server.js --port=80\r\nautorestart: true\r\nenvironment=A=\"1\",\r\n" +
				"    B=\"2\"\r\nstdout_logfile=/var/log/web.log\r\n\r\n; trailer",
			wantFile: "web-copy.ini",
			want: "; Web app\r\n# owned by ops\r\n\r\n[program:web-copy]   ; the site\r\n" +
				"Command = node server.js --port=80\r\nautorestart: true\r\nenvironment=A=\"1\",\r\n" +
				"    B=\"2\"\r\nstdout_logfile=/var/log/web.log\r\n\r\n; trailer",
			wantRenames: []Rename{{"program", "web", "web-copy"}},
			wantDoubts:  []string{"9 stdout_logfile web"},
		},
		{
			name: "literal mentions are doubts, expansions and longer names are not",
			file: "w.ini",
			content: "[program:w]\ncommand=/srv/w/run --name w-worker\ndirectory=/srv/w\n" +
				"stderr_logfile=/tmp/%(program_name)s.err\n[eventlistener:watch]\ncommand=x\n",
			wantFile: "w-copy.ini",
			want: "[program:w-copy]\ncommand=/srv/w/run --name w-worker\ndirectory=/srv/w\n" +
				"stderr_logfile=/tmp/%(program_name)s.err\n[eventlistener:watch]\ncommand=x\n",
			wantRenames: []Rename{{"program", "w", "w-copy"}},
			wantDoubts:  []string{"2 command w", "3 directory w", "5  watch"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := CopyAs(tt.file, tt.content, tt.taken, tt.takenFiles)
			if err != nil {
				t.Fatal(err)
			}
			if got.FileName != tt.wantFile {
				t.Errorf("file name = %q, want %q", got.FileName, tt.wantFile)
			}
			if got.Content != tt.want {
				t.Errorf("content =\n%q\nwant\n%q", got.Content, tt.want)
			}
			if !reflect.DeepEqual(got.Renames, tt.wantRenames) {
				t.Errorf("renames = %+v, want %+v", got.Renames, tt.wantRenames)
			}
			var doubts []string
			for _, d := range got.Doubts {
				doubts = append(doubts, strings.Join([]string{strconv.Itoa(d.Line), d.Key, d.Name}, " "))
				if d.Message == "" {
					t.Errorf("doubt %+v has no message", d)
				}
			}
			if !reflect.DeepEqual(doubts, tt.wantDoubts) {
				t.Errorf("doubts = %q, want %q (%+v)", doubts, tt.wantDoubts, got.Doubts)
			}
			// The copy parses, and its programs are the new names.
			if _, err := CopyAs(got.FileName, got.Content, nil, nil); err != nil {
				t.Errorf("the copy does not copy again: %v", err)
			}
		})
	}
}

func TestCopyAsErrors(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"unparsable":  "[program:x\ncommand=y\n",
		"no programs": "; nothing\n[group:g]\nprograms=a\n",
		"empty":       "",
	} {
		if _, err := CopyAs("x.ini", content, nil, nil); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestInventoryNames(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "a.conf", "[group:jobs]\nprograms=mail,digest\n[program:mail]\ncommand=x\n[program:digest]\ncommand=x\n")
	writeConf(t, dir, "b.conf", "[program:solo]\ncommand=x\n[group:empty]\nprograms=ghost\n")
	inv, err := LoadInventory(disc(dir))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"digest", "empty", "jobs", "mail", "solo"}
	if got := inv.Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("Names = %q, want %q", got, want)
	}
}
