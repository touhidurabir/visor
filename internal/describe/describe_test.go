package describe

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func render(ps []Program) string {
	var b strings.Builder
	for i, p := range ps {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[program:" + p.Name + "]\n" + p.Text + "\n")
		for _, f := range p.Flags {
			b.WriteString("! " + f + "\n")
		}
	}
	return b.String()
}

func TestGolden(t *testing.T) {
	inputs, err := filepath.Glob("testdata/describe/*.ini")
	if err != nil || len(inputs) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, in := range inputs {
		name := strings.TrimSuffix(filepath.Base(in), ".ini")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			ps, err := File(string(src))
			if err != nil {
				t.Fatal(err)
			}
			got := render(ps)
			golden := strings.TrimSuffix(in, ".ini") + ".golden"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			if got != string(want) {
				t.Errorf("mismatch for %s\n--- got\n%s--- want\n%s", name, got, want)
			}
		})
	}
}

func TestDescribeNumprocsCopies(t *testing.T) {
	cases := []struct{ ini, want string }{
		{"[program:a]\ncommand=x\nnumprocs=2\nprocess_name=%(program_name)s_%(process_num)02d\n", "Starts 2 copies (a_00 and _01)"},
		{"[program:a]\ncommand=x\nnumprocs=4\nprocess_name=%(program_name)s_%(process_num)02d\nnumprocs_start=1\n", "Starts 4 copies (a_01 to _04)"},
		{"[program:a]\ncommand=x\nnumprocs=3\n", "Starts 3 copies"},
		{"[program:a]\ncommand=x\n", "Starts with supervisord"},
		{"[program:a]\ncommand=x\nnumprocs=2\nautostart=false\nprocess_name=%(process_num)d\n", "Waits for you to start its 2 copies (0 and 1)"},
	}
	for _, c := range cases {
		ps, err := File(c.ini)
		if err != nil || len(ps) != 1 {
			t.Fatalf("%v %v", ps, err)
		}
		if !strings.Contains(ps[0].Text, c.want) {
			t.Errorf("text %q does not contain %q", ps[0].Text, c.want)
		}
	}
}

func TestParseErrorIsReturned(t *testing.T) {
	if _, err := File("[program:a\n"); err == nil {
		t.Error("want a parse error")
	}
}

func TestNoPrograms(t *testing.T) {
	ps, err := File("[group:x]\nprograms=a\n")
	if err != nil || len(ps) != 0 {
		t.Errorf("got %v, %v", ps, err)
	}
}
