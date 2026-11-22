package validate

import "testing"

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
		{"no environment", "[program:a]\ncommand=x\n; environment=A=1 is a comment\n", "[program:a]\ncommand=x\n; environment=A=1 is a comment\n"},
		{"an empty environment stays", "environment=\n", "environment=\n"},
		{"supervisord's own section too", "[supervisord]\nenvironment=X=1\n", "[supervisord]\nenvironment=X=\"«hidden»\"\n"},
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
