package config

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) File {
	t.Helper()
	f, err := ParseINI(strings.NewReader(src), "test.conf")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParseINIBasics(t *testing.T) {
	t.Parallel()
	f := mustParse(t, `; leading comment
# another comment
[supervisord]
nodaemon = false
LogLevel: info

[program:web]
command=run
`)
	if f.Path != "test.conf" || len(f.Sections) != 2 {
		t.Fatalf("got %+v", f)
	}
	s, ok := f.Section("supervisord")
	if !ok || s.Line != 3 {
		t.Fatalf("supervisord section = %+v, %v", s, ok)
	}
	if v, _ := s.Get("nodaemon"); v != "false" {
		t.Errorf("nodaemon = %q", v)
	}
	if v, ok := s.Get("loglevel"); !ok || v != "info" {
		t.Errorf("loglevel (':' separator, lower-cased) = %q, %v", v, ok)
	}
	if v, ok := s.Get("LogLevel"); !ok || v != "info" {
		t.Errorf("Get is case-insensitive, got %q, %v", v, ok)
	}
	if s.Keys[0].Line != 4 || s.Keys[1].Line != 5 {
		t.Errorf("key lines = %d, %d", s.Keys[0].Line, s.Keys[1].Line)
	}
	if _, ok := f.Section("missing"); ok {
		t.Error("missing section found")
	}
	if got := f.SectionsWithPrefix("program:"); len(got) != 1 || got[0].Name != "program:web" {
		t.Errorf("SectionsWithPrefix = %+v", got)
	}
}

func TestParseINIContinuationAndInlineComments(t *testing.T) {
	t.Parallel()
	f := mustParse(t, `[program:x]
environment=A="1",
    B="2"
command=sh -c 'a;b'
other=run ; note
tabbed=run	# not a comment
`)
	s, _ := f.Section("program:x")
	tests := []struct{ key, want string }{
		{"environment", "A=\"1\",\nB=\"2\""},
		{"command", "sh -c 'a;b'"},
		{"other", "run"},
		{"tabbed", "run\t# not a comment"},
	}
	for _, tc := range tests {
		if got, _ := s.Get(tc.key); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestParseINIDuplicateSectionsMerge(t *testing.T) {
	t.Parallel()
	f := mustParse(t, "[a]\nx=1\ny=1\n[b]\nz=1\n[a]\nx=2\n")
	if len(f.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(f.Sections))
	}
	s, _ := f.Section("a")
	if v, _ := s.Get("x"); v != "2" {
		t.Errorf("x = %q, want last value", v)
	}
	if v, _ := s.Get("y"); v != "1" {
		t.Errorf("y = %q", v)
	}
}

func TestParseINIErrors(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"key before section": "x=1\n",
		"unterminated":       "[oops\n",
		"no separator":       "[a]\njunk\n",
	} {
		if _, err := ParseINI(strings.NewReader(src), "bad.conf"); err == nil {
			t.Errorf("%s: want error", name)
		} else if !strings.Contains(err.Error(), "bad.conf:") {
			t.Errorf("%s: error %q lacks file:line", name, err)
		}
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()
	vars := map[string]string{"here": "/etc/sv", "program_name": "web", "process_num": "3"}
	tests := []struct{ in, want string }{
		{"%(here)s/conf.d", "/etc/sv/conf.d"},
		{"%(program_name)s_%(process_num)02d", "web_03"},
		{"100%%", "100%"},
		{"%(nope)s stays", "%(nope)s stays"},
		{"plain", "plain"},
	}
	for _, tc := range tests {
		if got := Expand(tc.in, vars); got != tc.want {
			t.Errorf("Expand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
