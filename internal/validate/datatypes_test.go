package validate

import (
	"slices"
	"testing"
)

// The expectations below are what supervisor 4.2.5's datatypes.py returns
// for each value (probed with its own Python on macOS): ok means the
// conversion succeeds, so supervisord accepts the value.

func TestPyInt(t *testing.T) {
	for v, ok := range map[string]bool{
		"1": true, "-1": true, "+5": true, " 7 ": true, "1_000": true, "010": true, "-0": true,
		"1__0": false, "ten": false, "": false, "1.5": false, "0x10": false, "_1": false, "1_": false, "+": false,
	} {
		if _, got := pyInt(v); got != ok {
			t.Errorf("pyInt(%q) ok = %v, want %v", v, got, ok)
		}
	}
	if n, _ := pyInt(" +1_000 "); n != 1000 {
		t.Errorf("pyInt value = %d, want 1000", n)
	}
}

func TestBoolean(t *testing.T) {
	for v, ok := range map[string]bool{
		"true": true, "Yes": true, "ON": true, "1": true, "0": true, "no": true, "off": true, "False": true,
		"maybe": false, "": false, "2": false, "y": false,
	} {
		if got := isBoolean(v); got != ok {
			t.Errorf("isBoolean(%q) = %v, want %v", v, got, ok)
		}
	}
}

func TestAutoRestart(t *testing.T) {
	for v, ok := range map[string]bool{
		"true": true, "yes": true, "1": true, "on": true, "false": true, "no": true, "off": true, "0": true,
		"unexpected": true, "UNEXPECTED": true,
		"sometimes": false, "": false, "always": false,
	} {
		if got := isAutoRestart(v); got != ok {
			t.Errorf("isAutoRestart(%q) = %v, want %v", v, got, ok)
		}
	}
}

func TestSignalNumber(t *testing.T) {
	for v, ok := range map[string]bool{
		"TERM": true, "SIGTERM": true, "term": true, "15": true, "WINCH": true, "STOP": true, "SIGINFO": true,
		"IOT": true, "0": true, "31": true, " 9 ": true, "+9": true, "USR1": true, "sigusr2": true, "EMT": true,
		"32": false, "-1": false, "FOO": false, "SIGFOO": false, "": false,
	} {
		if got := isSignal(v); got != ok {
			t.Errorf("isSignal(%q) = %v, want %v", v, got, ok)
		}
	}
}

func TestByteSize(t *testing.T) {
	for v, ok := range map[string]bool{
		"50MB": true, "50mb": true, "1KB": true, "1kb": true, "0": true, "1024": true, "10GB": true,
		"-1KB": true, "+5MB": true, "5 MB": true, "1_0KB": true,
		"lots": false, "MB": false, "5TB": false, "": false,
	} {
		if got := isByteSize(v); got != ok {
			t.Errorf("isByteSize(%q) = %v, want %v", v, got, ok)
		}
	}
}

func TestExitCodes(t *testing.T) {
	for v, ok := range map[string]bool{
		"0": true, "0,2": true, "0, 2": true, "": true, "255": true,
		"256": false, "-1": false, "a": false, "0,,2": false,
	} {
		if got := isExitCodes(v); got != ok {
			t.Errorf("isExitCodes(%q) = %v, want %v", v, got, ok)
		}
	}
}

func TestOctal(t *testing.T) {
	for v, ok := range map[string]bool{
		"022": true, "0o22": true, "-7": true, "+7": true, "0777": true, " 22 ": true,
		"8": false, "": false, "x": false,
	} {
		if got := isOctal(v); got != ok {
			t.Errorf("isOctal(%q) = %v, want %v", v, got, ok)
		}
	}
}

func TestEnvironmentPairs(t *testing.T) {
	tests := []struct {
		in     string
		ok     bool
		values map[string]string // what supervisord would set, when ok
	}{
		{`A=1`, true, map[string]string{"A": "1"}},
		{`A="x y"`, true, map[string]string{"A": "x y"}},
		{`A='x y'`, true, map[string]string{"A": "x y"}},
		{`A=a@b`, false, nil},
		{`A=x=y`, false, nil},
		{`A=base64:abc+/=`, true, map[string]string{"A": "base64:abc+/"}},
		{`A=1,B=2`, true, map[string]string{"A": "1", "B": "2"}},
		{`A=1 B=2`, false, nil},
		{`A=1;B=2`, true, map[string]string{"A": "1", "B": "2"}},
		{`PORT 3000`, false, nil},
		{`NODE_ENV="production",PORT="3000"`, true, map[string]string{"NODE_ENV": "production", "PORT": "3000"}},
		{``, true, map[string]string{}},
		{`A=`, false, nil},
		{`A=""`, true, map[string]string{"A": ""}},
		{"A=\"1\",\n  B=\"2\"", true, map[string]string{"A": "1", "B": "2"}},
		{`A=/usr/bin:/bin`, true, map[string]string{"A": "/usr/bin:/bin"}},
		{`A=café`, true, map[string]string{"A": "caf"}},
		{`A=1,`, true, map[string]string{"A": "1"}},
		{`A=1,B`, false, nil},
		{`A="unterminated`, false, nil},
		{`A=x # comment`, true, map[string]string{"A": "x"}},
		{`A=x-y.z(1)`, true, map[string]string{"A": "x-y.z(1)"}},
		{`KEY=val,KEY2=val2,`, true, map[string]string{"KEY": "val", "KEY2": "val2"}},
		{`A="50%"`, true, map[string]string{"A": "50%"}},
		{`A=it's`, true, map[string]string{"A": "it's"}},
	}
	for _, tc := range tests {
		pairs, err := envPairs(tc.in)
		if (err == nil) != tc.ok {
			t.Errorf("envPairs(%q) err = %v, want ok %v", tc.in, err, tc.ok)
			continue
		}
		if !tc.ok {
			continue
		}
		got := map[string]string{}
		for _, p := range pairs {
			got[p.Key] = p.Value
		}
		if len(got) != len(tc.values) {
			t.Errorf("envPairs(%q) = %v, want %v", tc.in, got, tc.values)
			continue
		}
		for k, v := range tc.values {
			if got[k] != v {
				t.Errorf("envPairs(%q)[%s] = %q, want %q", tc.in, k, got[k], v)
			}
		}
	}
}

// A pair followed by anything but a comma or semicolon loses that text
// (supervisord skips the token after each value): Visor warns.
func TestEnvironmentPairsDropped(t *testing.T) {
	pairs, err := envPairs(`A=base64:abc+/=,B=1`)
	if err == nil {
		t.Fatalf("pairs = %+v, want an error (the = after the value starts no pair)", pairs)
	}
	pairs, err = envPairs(`A=café`)
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.IndexFunc(pairs, func(p envPair) bool { return p.Dropped != "" }); i < 0 || pairs[i].Dropped != "é" {
		t.Errorf("pairs = %+v, want A to note the dropped é", pairs)
	}
}

func TestProcessName(t *testing.T) {
	for v, ok := range map[string]bool{"web": true, " web ": true, "a-b_c.1": true, "a b": false, "a:b": false, "a/b": false} {
		if got := isProcessName(v); got != ok {
			t.Errorf("isProcessName(%q) = %v, want %v", v, got, ok)
		}
	}
}
