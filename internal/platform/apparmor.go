package platform

import (
	"os"
	"path/filepath"
	"strings"
)

// Ubuntu 23.10 and later set kernel.apparmor_restrict_unprivileged_userns=1:
// a program may make a user namespace only if an AppArmor profile lets it.
// WebKitGTK's sandbox (bubblewrap) needs one, so without a profile Visor's
// window crashes as it opens. The .deb installs a profile; an AppImage or a
// hand-built binary needs one added by hand.

// WebKitSandboxBlocked reports whether that is the case for this process:
// the restriction is on and no profile confines it (its AppArmor label is
// plain "unconfined").
func WebKitSandboxBlocked() bool { return sandboxBlocked(os.ReadFile) }

func sandboxBlocked(read func(string) ([]byte, error)) bool {
	v, err := read("/proc/sys/kernel/apparmor_restrict_unprivileged_userns")
	if err != nil || strings.TrimSpace(string(v)) != "1" {
		return false
	}
	label, err := read("/proc/self/attr/apparmor/current")
	if err != nil {
		if label, err = read("/proc/self/attr/current"); err != nil {
			return false
		}
	}
	return strings.TrimSpace(strings.TrimRight(string(label), "\x00")) == "unconfined"
}

// AppArmorProfileName is the profile the .deb installs, and the one Doctor's
// fix adds by hand (with a suffix for the AppImage).
const AppArmorProfileName = "dev.getvisor.visor"

// AppArmorProfile is a profile that lets exe make user namespaces and
// nothing else (it stays unconfined). An AppImage runs from a mount whose
// name changes every launch (/tmp/.mount_VisorAbc123/…), so that part
// becomes a glob.
func AppArmorProfile(name, exe string) string {
	attach := exe
	if rest, ok := strings.CutPrefix(exe, "/tmp/.mount_"); ok {
		if _, inside, found := strings.Cut(rest, "/"); found {
			attach = "/tmp/.mount_*/" + inside
		}
	}
	return "abi <abi/4.0>,\ninclude <tunables/global>\n\n" +
		"profile " + name + " " + quoteProfilePath(attach) + " flags=(unconfined) {\n" +
		"  userns,\n\n" +
		"  include if exists <local/" + name + ">\n" +
		"}\n"
}

// quoteProfilePath quotes an attachment path with spaces.
func quoteProfilePath(p string) string {
	if strings.ContainsAny(p, " \t") {
		return `"` + p + `"`
	}
	return p
}

// AppArmorFix is the shell steps that install AppArmorProfile for exe.
func AppArmorFix(exe string) string {
	name := AppArmorProfileName
	if strings.HasPrefix(exe, "/tmp/.mount_") {
		name += "-appimage"
	} else if !strings.HasPrefix(exe, "/usr/") && !strings.HasPrefix(exe, "/opt/") {
		name += "-" + strings.ToLower(strings.Trim(strings.NewReplacer("/", "-", ".", "-", " ", "-").Replace(filepath.Dir(exe)), "-"))
	}
	file := "/etc/apparmor.d/" + name
	return "sudo tee " + file + " >/dev/null <<'EOF'\n" + AppArmorProfile(name, exe) + "EOF\n" +
		"sudo apparmor_parser -r " + file + "\n"
}
