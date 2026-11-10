package platform

import "testing"

func TestParseOSRelease(t *testing.T) {
	cases := []struct {
		name, data string
		id         string
		want       Family
	}{
		{"Ubuntu", "PRETTY_NAME=\"Ubuntu 24.04.5 LTS\"\nNAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nID=ubuntu\nID_LIKE=debian\n", "ubuntu", FamilyDebian},
		{"Debian", "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nNAME=\"Debian GNU/Linux\"\nID=debian\n", "debian", FamilyDebian},
		{"Mint", "NAME=\"Linux Mint\"\nID=linuxmint\nID_LIKE=\"ubuntu debian\"\n", "linuxmint", FamilyDebian},
		{"Fedora", "NAME=\"Fedora Linux\"\nVERSION_ID=40\nID=fedora\n", "fedora", FamilyFedora},
		{"RHEL", "NAME=\"Red Hat Enterprise Linux\"\nID=\"rhel\"\nID_LIKE=\"fedora\"\n", "rhel", FamilyFedora},
		{"Rocky", "NAME=\"Rocky Linux\"\nID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n", "rocky", FamilyFedora},
		{"Arch", "NAME=\"Arch Linux\"\nID=arch\n", "arch", FamilyArch},
		{"Manjaro", "NAME=\"Manjaro Linux\"\nID=manjaro\nID_LIKE=arch\n", "manjaro", FamilyArch},
		{"openSUSE", "NAME=\"openSUSE Tumbleweed\"\nID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"\n", "opensuse-tumbleweed", FamilyOther},
		{"unknown", "", "", FamilyOther},
		{"comments", "# ID=ubuntu\nID=nixos\n", "nixos", FamilyOther},
	}
	for _, c := range cases {
		d := ParseOSRelease(c.data)
		if d.Family != c.want || d.ID != c.id {
			t.Errorf("%s: %+v, want family %s id %q", c.name, d, c.want, c.id)
		}
	}
	if d := ParseOSRelease("NAME=\"Fedora Linux\"\nID=fedora\n"); d.Name != "Fedora Linux" {
		t.Errorf("name = %q", d.Name)
	}
}
