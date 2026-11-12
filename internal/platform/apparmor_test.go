package platform

import (
	"errors"
	"strings"
	"testing"
)

func fakeProc(files map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if v, ok := files[p]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("no such file")
	}
}

func TestSandboxBlocked(t *testing.T) {
	const sysctl, label, old = "/proc/sys/kernel/apparmor_restrict_unprivileged_userns", "/proc/self/attr/apparmor/current", "/proc/self/attr/current"
	cases := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"Ubuntu, no profile", map[string]string{sysctl: "1\n", label: "unconfined\n"}, true},
		{"Ubuntu, with the profile", map[string]string{sysctl: "1\n", label: "dev.getvisor.visor (unconfined)\n"}, false},
		{"restriction off", map[string]string{sysctl: "0\n", label: "unconfined\n"}, false},
		{"no such sysctl (Fedora, macOS)", map[string]string{label: "unconfined"}, false},
		{"older kernel's attr file", map[string]string{sysctl: "1", old: "unconfined\x00"}, true},
		{"label unreadable", map[string]string{sysctl: "1"}, false},
	}
	for _, c := range cases {
		if got := sandboxBlocked(fakeProc(c.files)); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAppArmorProfile(t *testing.T) {
	p := AppArmorProfile("dev.getvisor.visor", "/usr/bin/visor")
	for _, want := range []string{"abi <abi/4.0>,", "profile dev.getvisor.visor /usr/bin/visor flags=(unconfined) {", "  userns,", "include if exists <local/dev.getvisor.visor>"} {
		if !strings.Contains(p, want) {
			t.Errorf("profile lacks %q:\n%s", want, p)
		}
	}
	for _, exe := range []string{"/tmp/.mount_VisorAb12/usr/bin/visor", "/tmp/appimage_extracted_aecc7269/usr/bin/visor"} {
		if p := AppArmorProfile("x", exe); !strings.Contains(p, "profile x /tmp/{.mount_*,appimage_extracted_*}/usr/bin/visor flags") {
			t.Errorf("%s: an AppImage's folder is a glob:\n%s", exe, p)
		}
		if f := AppArmorFix(exe); !strings.Contains(f, "/etc/apparmor.d/dev.getvisor.visor-appimage ") {
			t.Errorf("%s: fix = %s", exe, f)
		}
	}
	if p := AppArmorProfile("x", "/home/ana/My Apps/visor"); !strings.Contains(p, `"/home/ana/My Apps/visor"`) {
		t.Errorf("a path with a space is quoted:\n%s", p)
	}
}

func TestAppArmorFix(t *testing.T) {
	f := AppArmorFix("/tmp/.mount_VisorAb12/usr/bin/visor")
	if !strings.HasPrefix(f, "sudo tee /etc/apparmor.d/dev.getvisor.visor-appimage >/dev/null <<'EOF'\n") || !strings.HasSuffix(f, "EOF\nsudo apparmor_parser -r /etc/apparmor.d/dev.getvisor.visor-appimage\n") {
		t.Errorf("AppImage fix:\n%s", f)
	}
	if f := AppArmorFix("/home/ana/visor/bin/visor"); !strings.Contains(f, "/etc/apparmor.d/dev.getvisor.visor-home-ana-visor-bin ") {
		t.Errorf("dev build fix names its folder:\n%s", f)
	}
	if f := AppArmorFix("/usr/bin/visor"); !strings.Contains(f, "/etc/apparmor.d/dev.getvisor.visor ") {
		t.Errorf("installed fix:\n%s", f)
	}
}

func TestAppArmorFixNeverReplacesThePackagedProfile(t *testing.T) {
	for _, exe := range []string{"/usr/local/bin/visor", "/opt/visor/visor"} {
		if f := AppArmorFix(exe); strings.Contains(f, "/etc/apparmor.d/dev.getvisor.visor ") {
			t.Errorf("%s: the fix would replace the .deb's profile:\n%s", exe, f)
		}
	}
}

func TestSandboxOffMeansNotBlocked(t *testing.T) {
	t.Setenv("WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS", "1")
	if WebKitSandboxBlocked() {
		t.Error("with the sandbox off nothing is blocked")
	}
}
