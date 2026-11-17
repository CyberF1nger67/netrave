// SPDX-License-Identifier: GPL-3.0-only

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	cases := []struct {
		data string
		id   string
		name string
	}{
		{"ID=debian\nNAME=\"Debian GNU/Linux\"\nVERSION_ID=\"12\"", "debian", "Debian GNU/Linux"},
		{"ID=ubuntu\nNAME=Ubuntu\nVERSION_ID=\"24.04\"", "ubuntu", "Ubuntu"},
		{"ID=\"rocky\"\nNAME=\"Rocky Linux\"\nID_LIKE=\"rhel centos fedora\"", "rocky", "Rocky Linux"},
		{"ID=void\nNAME=\"Void Linux\"", "void", "Void Linux"},
	}
	for _, tc := range cases {
		release := ParseOSRelease([]byte(tc.data))
		if release.ID != tc.id || release.Name != tc.name {
			t.Fatalf("unexpected release: %+v", release)
		}
	}
}

func TestAdaptersForSupportedDistributions(t *testing.T) {
	expected := map[string]Family{
		"debian": FamilyDebian, "ubuntu": FamilyDebian, "rhel": FamilyRedHat, "fedora": FamilyRedHat,
		"rocky": FamilyRedHat, "almalinux": FamilyRedHat, "void": FamilyVoid, "alpine": FamilyAlpine,
		"arch": FamilyArch, "manjaro": FamilyArch,
	}
	for id, family := range expected {
		adapter := AdapterFor(id)
		if adapter.ID != id || adapter.Family != family {
			t.Fatalf("adapter %s: %+v", id, adapter)
		}
		if adapter.PackageManager == "" {
			t.Fatalf("adapter %s has no package manager", id)
		}
	}
}

func TestDetectInit(t *testing.T) {
	root := t.TempDir()
	if got := DetectInit(root, "systemd"); got != InitSystemd {
		t.Fatalf("override: %s", got)
	}
	if err := os.MkdirAll(filepath.Join(root, "run", "systemd", "system"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectInit(root, ""); got != InitSystemd {
		t.Fatalf("systemd detection: %s", got)
	}
	other := t.TempDir()
	if err := os.MkdirAll(filepath.Join(other, "sbin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "sbin", "openrc-run"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectInit(other, ""); got != InitOpenRC {
		t.Fatalf("openrc detection: %s", got)
	}
	runit := t.TempDir()
	if err := os.MkdirAll(filepath.Join(runit, "etc", "runit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectInit(runit, ""); got != InitRunit {
		t.Fatalf("runit detection: %s", got)
	}
}
