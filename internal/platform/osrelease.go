package platform

import (
	"bufio"
	"os"
	"slices"
	"strings"
)

// Family is a Linux distro family: which package manager and service name
// Doctor's commands use.
type Family string

const (
	FamilyDebian Family = "debian" // Debian, Ubuntu, Mint, Pop!_OS…: apt, service "supervisor"
	FamilyFedora Family = "fedora" // Fedora, RHEL, CentOS, Rocky, Alma: dnf, service "supervisord"
	FamilyArch   Family = "arch"   // Arch, Manjaro, EndeavourOS: pacman, service "supervisord"
	FamilyOther  Family = "other"
)

// Distro is what /etc/os-release says about this Linux.
type Distro struct {
	ID, Name string
	Family   Family
}

// ReadDistro reads /etc/os-release (or /usr/lib/os-release). Without either
// the family is FamilyOther.
func ReadDistro() Distro {
	for _, p := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if data, err := os.ReadFile(p); err == nil {
			return ParseOSRelease(string(data))
		}
	}
	return Distro{Family: FamilyOther}
}

// ParseOSRelease reads os-release's ID, ID_LIKE and NAME (quoted or not)
// and picks the family from ID, then from ID_LIKE.
func ParseOSRelease(data string) Distro {
	vals := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		vals[k] = strings.Trim(v, `"'`)
	}
	d := Distro{ID: strings.ToLower(vals["ID"]), Name: vals["NAME"], Family: FamilyOther}
	ids := append([]string{d.ID}, strings.Fields(strings.ToLower(vals["ID_LIKE"]))...)
	for _, id := range ids {
		switch {
		case slices.Contains([]string{"debian", "ubuntu"}, id):
			d.Family = FamilyDebian
		case slices.Contains([]string{"fedora", "rhel", "centos"}, id):
			d.Family = FamilyFedora
		case id == "arch":
			d.Family = FamilyArch
		default:
			continue
		}
		break
	}
	return d
}
