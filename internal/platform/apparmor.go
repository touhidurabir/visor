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
//
// WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1 turns WebKit's sandbox off,
// and with it the need for a user namespace: then nothing is blocked.
func WebKitSandboxBlocked() bool {
	if os.Getenv("WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS") != "" {
		return false
	}
	return sandboxBlocked(os.ReadFile)
}

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

// appImageRoots are where an AppImage's files are while it runs: its
// FUSE mount, or the folder --appimage-extract-and-run unpacks it to. The
// name after the prefix changes from launch to launch.
var appImageRoots = []string{"/tmp/.mount_", "/tmp/appimage_extracted_"}

// inAppImage is the path inside the AppImage ("usr/bin/visor"), if exe
// runs from one.
func inAppImage(exe string) (string, bool) {
	for _, root := range appImageRoots {
		if rest, ok := strings.CutPrefix(exe, root); ok {
			if _, inside, found := strings.Cut(rest, "/"); found {
				return inside, true
			}
		}
	}
	return "", false
}

// AppArmorProfile is a profile that lets exe make user namespaces and
// nothing else (it stays unconfined). For an AppImage the folder it runs
// from becomes a glob that covers both ways of running it.
func AppArmorProfile(name, exe string) string {
	attach := exe
	if inside, ok := inAppImage(exe); ok {
		attach = "/tmp/{.mount_*,appimage_extracted_*}/" + inside
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
	// The bare name is the .deb's own profile (/usr/bin/visor): any other
	// copy gets a name of its own, so the fix never replaces that file.
	if _, ok := inAppImage(exe); ok {
		name += "-appimage"
	} else if exe != "/usr/bin/visor" {
		name += "-" + strings.ToLower(strings.Trim(strings.NewReplacer("/", "-", ".", "-", " ", "-").Replace(filepath.Dir(exe)), "-"))
	}
	file := "/etc/apparmor.d/" + name
	return "sudo tee " + file + " >/dev/null <<'EOF'\n" + AppArmorProfile(name, exe) + "EOF\n" +
		"sudo apparmor_parser -r " + file + "\n"
}
