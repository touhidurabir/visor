package validate

import (
	"strings"
	"testing"
)

func TestHideEnv(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"two pairs",
			"[program:a]\ncommand=php artisan queue:work\nenvironment=A=\"1\",B=two\nuser=www\n",
			"[program:a]\ncommand=php artisan queue:work\nenvironment=A=\"«hidden»\",B=\"«hidden»\"\nuser=www\n"},
		{"a quoted comma stays one value",
			"environment=A=\"x,y\",B=2\n",
			"environment=A=\"«hidden»\",B=\"«hidden»\"\n"},
		{"spaces around the key and continuation lines",
			"[program:a]\nenvironment = DB_PASSWORD=\"s3cret\",\n    APP_KEY=\"base64:abc\"\nautostart=true\n",
			"[program:a]\nenvironment = DB_PASSWORD=\"«hidden»\",APP_KEY=\"«hidden»\"\nautostart=true\n"},
		{"expansions are hidden too",
			"environment=HOME=\"%(ENV_HOME)s\"\n",
			"environment=HOME=\"«hidden»\"\n"},
		{"an unreadable value is hidden whole",
			"environment=A=\"open\n",
			"environment=«hidden»\n"},
		{"no environment", "[program:a]\ncommand=x\n; the environment is set below\n", "[program:a]\ncommand=x\n; the environment is set below\n"},
		{"commented-out values are hidden too", "[program:a]\n;environment=OLD_PASSWORD=\"x\"\n# environment = B=2\n", "[program:a]\n;environment=OLD_PASSWORD=\"«hidden»\"\n# environment = B=\"«hidden»\"\n"},
		{"an empty environment stays", "environment=\n", "environment=\n"},
		{"supervisord's own section too", "[supervisord]\nenvironment=X=1\n", "[supervisord]\nenvironment=X=\"«hidden»\"\n"},
		{"a continuation after a blank line",
			"environment=A=1,\n\n    SECRET=\"x\"\nuser=w\n",
			"environment=A=\"«hidden»\",SECRET=\"«hidden»\"\nuser=w\n"},
		{"a continuation after a comment line",
			"environment=A=1,\n; note\n  # more\n\tSECRET=\"x\"\n",
			"environment=A=\"«hidden»\",SECRET=\"«hidden»\"\n"},
		{"an indented comment after the last value line is hidden with it",
			"environment=A=1,\n    B=2\n    ;OLD_SECRET=\"x\"\nuser=w\n",
			"environment=A=\"«hidden»\",B=\"«hidden»\"\nuser=w\n"},
		{"an indented comment before the next section is hidden",
			"environment=A=1\n\t# TOKEN=abc\n[program:b]\n",
			"environment=A=\"«hidden»\"\n[program:b]\n"},
		{"an indented comment after a blank line is hidden",
			"environment=A=1\n\n    ;OLD=\"y\"\n",
			"environment=A=\"«hidden»\"\n"},
		{"a blank line before the next section stays",
			"environment=A=1\n\n; b\n[program:b]\n",
			"environment=A=\"«hidden»\"\n\n; b\n[program:b]\n"},
		{"CRLF and no final newline", "environment=A=1\r\nuser=x", "environment=A=\"«hidden»\"\r\nuser=x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := HideEnv(c.in)
			if got != c.want {
				t.Fatalf("HideEnv:\n%q\nwant\n%q", got, c.want)
			}
			if c.in != c.want && !HasHidden(got) {
				t.Fatal("HasHidden is false after hiding")
			}
			if back, err := RestoreEnv(got, c.in); err != nil || back != c.in {
				t.Fatalf("RestoreEnv: %q, %v", back, err)
			}
		})
	}
	if HasHidden("environment=A=1") {
		t.Fatal("HasHidden on plain text")
	}
}

func TestHideEnvValue(t *testing.T) {
	if got := HideEnvValue(`A="1",B=2`); got != `A="«hidden»",B="«hidden»"` {
		t.Fatalf("got %q", got)
	}
	if got := HideEnvValue(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
}

func TestRestoreEnv(t *testing.T) {
	disk := "[program:a]\ncommand=old\nenvironment = DB_PASSWORD=\"s3cret\",\n    APP_KEY=\"k\"\n;environment=OLD=\"x\"\nuser=www\n"
	hidden := HideEnv(disk)
	edited := strings.Replace(hidden, "command=old", "command=new", 1)
	got, err := RestoreEnv(edited, disk)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(disk, "command=old", "command=new", 1); got != want {
		t.Fatalf("RestoreEnv:\n%q\nwant\n%q", got, want)
	}
	// A hidden line moved to another program keeps its values only where it was.
	if _, err := RestoreEnv(strings.Replace(hidden, "DB_PASSWORD", "DB_PASS", 1), disk); err == nil {
		t.Fatal("an edited hidden line was accepted")
	}
	// Text without hidden values is left as it is.
	plain := "[program:a]\nenvironment=A=\"new\"\n"
	if got, err := RestoreEnv(plain, disk); err != nil || got != plain {
		t.Fatalf("plain text: %q %v", got, err)
	}
}
