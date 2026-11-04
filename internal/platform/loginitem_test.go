package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testExe = "/Applications/Visor.app/Contents/MacOS/visor"

func TestLaunchAgentWritesAndRemovesPlist(t *testing.T) {
	home := t.TempDir()
	li := NewLaunchAgent(testExe, home, func() bool { return true })
	if st := li.Status(); st != LoginDisabled {
		t.Fatalf("status before = %s", st)
	}
	if err := li.Set(true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, "Library/LaunchAgents/dev.getvisor.visor.plist"))
	if err != nil {
		t.Fatal(err)
	}
	want := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>dev.getvisor.visor</string>
	<key>ProgramArguments</key>
	<array>
		<string>/Applications/Visor.app/Contents/MacOS/visor</string>
		<string>--login</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`
	if string(got) != want {
		t.Fatalf("plist =\n%s", got)
	}
	if st := li.Status(); st != LoginEnabled {
		t.Fatalf("status after on = %s", st)
	}
	if err := li.Set(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "Library/LaunchAgents/dev.getvisor.visor.plist")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plist still there: %v", err)
	}
	if err := li.Set(false); err != nil {
		t.Fatalf("turning off twice: %v", err)
	}
}

func TestLaunchAgentEscapesThePath(t *testing.T) {
	home := t.TempDir()
	li := NewLaunchAgent("/Users/a&b/Apps/V<1>.app/Contents/MacOS/visor", home, func() bool { return true })
	if err := li.Set(true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(home, "Library/LaunchAgents/dev.getvisor.visor.plist"))
	if !strings.Contains(string(got), "<string>/Users/a&amp;b/Apps/V&lt;1&gt;.app/Contents/MacOS/visor</string>") {
		t.Fatalf("plist =\n%s", got)
	}
}

func TestLoginItemRefusesTranslocated(t *testing.T) {
	home := t.TempDir()
	li := NewLaunchAgent(testExe, home, func() bool { return false })
	if err := li.Set(true); !errors.Is(err, ErrNotMoved) {
		t.Fatalf("Set(true) = %v, want ErrNotMoved", err)
	}
	if st := li.Status(); st != LoginNotMoved {
		t.Fatalf("status = %s", st)
	}
	if err := li.Set(false); err != nil {
		t.Fatalf("turning off must always work: %v", err)
	}
}

func TestLoginItemUnsupportedOutsideABundle(t *testing.T) {
	li := NewLaunchAgent("/Users/me/code/visor/bin/visor", t.TempDir(), func() bool { return true })
	if st := li.Status(); st != LoginUnsupported {
		t.Fatalf("status = %s", st)
	}
	if err := li.Set(true); !errors.Is(err, ErrLoginUnsupported) {
		t.Fatalf("Set(true) = %v", err)
	}
}

func TestLoginItemStatusParses(t *testing.T) {
	for code, want := range map[int]LoginItemStatus{
		0: LoginDisabled, 1: LoginEnabled, 2: LoginRequiresApproval, 3: LoginDisabled, 9: LoginUnsupported,
	} {
		if got := appServiceStatus(code); got != want {
			t.Errorf("appServiceStatus(%d) = %s, want %s", code, got, want)
		}
	}
}

// A plist left by another copy of Visor (a test build, an older location)
// would start that copy, not this one: it reads as off until turned on here.
func TestLaunchAgentForAnotherCopyReadsAsOff(t *testing.T) {
	home := t.TempDir()
	other := NewLaunchAgent("/tmp/visor-signcheck/bin/visor.app/Contents/MacOS/visor", home, func() bool { return true })
	if err := other.Set(true); err != nil {
		t.Fatal(err)
	}
	li := NewLaunchAgent(testExe, home, func() bool { return true })
	if st := li.Status(); st != LoginDisabled {
		t.Fatalf("status with another copy's plist = %s, want disabled", st)
	}
	if err := li.Set(true); err != nil {
		t.Fatal(err)
	}
	if st := li.Status(); st != LoginEnabled {
		t.Fatalf("status after turning on here = %s", st)
	}
	if st := other.Status(); st != LoginDisabled {
		t.Errorf("the other copy still reads as on: %s", st)
	}
}

func TestLaunchAgentProgram(t *testing.T) {
	plist := []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>Label</key><string>x</string><key>ProgramArguments</key><array><string>/A &amp; B/visor</string><string>--flag</string></array></dict></plist>`)
	if got := agentProgram(plist); got != "/A & B/visor" {
		t.Errorf("agentProgram = %q", got)
	}
	if got := agentProgram([]byte("not xml")); got != "" {
		t.Errorf("agentProgram(garbage) = %q", got)
	}
}

// A plist written before --login existed is brought up to date at startup,
// so macOS starting it while Visor runs stays quiet.
func TestLaunchAgentRefreshAddsTheLoginArg(t *testing.T) {
	home := t.TempDir()
	li := NewLaunchAgent(testExe, home, func() bool { return true })
	plist := filepath.Join(home, "Library/LaunchAgents/dev.getvisor.visor.plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(fmt.Sprintf(agentPlist, loginLabel, testExe, LoginArg), "\t\t<string>"+LoginArg+"</string>\n", "", 1)
	if err := os.WriteFile(plist, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := li.Refresh(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(plist)
	if !strings.Contains(string(got), "<string>"+LoginArg+"</string>") || li.Status() != LoginEnabled {
		t.Errorf("after Refresh:\n%s", got)
	}
	// Another copy's plist, or none: left alone.
	other := NewLaunchAgent("/tmp/x.app/Contents/MacOS/visor", t.TempDir(), func() bool { return true })
	if err := other.Refresh(); err != nil {
		t.Errorf("Refresh with no plist = %v", err)
	}
}
