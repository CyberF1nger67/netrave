// SPDX-License-Identifier: GPL-3.0-only

package database

import "testing"

func TestCategoryFor(t *testing.T) {
	cases := []struct{ vendor, typ, want string }{
		{"Cisco", "router", "cisco"},
		{"Cisco", "firewall", "cisco"},
		{"MikroTik", "router", "mikrotik"},
		{"Microsoft", "windows", "windows"},
		{"Generic", "linux", "linux"},
		{"Alpine", "linux", "linux"},
		{"Juniper", "firewall", "firewall"},
		{"Fortinet", "firewall", "firewall"},
		{"VyOS", "router", "network"},
		{"Arista", "switch", "network"},
		{"Dell", "server", "server"},
		{"", "generic", "other"},
	}
	for _, c := range cases {
		if got := CategoryFor(c.vendor, c.typ); got != c.want {
			t.Errorf("CategoryFor(%q,%q)=%q, want %q", c.vendor, c.typ, got, c.want)
		}
	}
}

func TestValidCategory(t *testing.T) {
	if !ValidCategory("firewall") || ValidCategory("unknown") || ValidCategory("") {
		t.Fatal("ValidCategory: неверный результат")
	}
}
