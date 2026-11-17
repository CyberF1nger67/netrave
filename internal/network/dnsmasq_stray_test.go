// SPDX-License-Identifier: GPL-3.0-only

package network

import "testing"

func TestDnsmasqServing(t *testing.T) {
	cases := []struct {
		name    string
		cmdline string
		iface   string
		want    bool
	}{
		{"own shared server", "dnsmasq\x00--interface=nrnat\x00--bind-interfaces\x00", "nrnat", true},
		{"absolute path", "/usr/sbin/dnsmasq\x00--interface=nrnat\x00", "nrnat", true},
		{"other bridge", "dnsmasq\x00--interface=nrl1fb\x00", "nrnat", false},
		{"other program", "sshd\x00--interface=nrnat\x00", "nrnat", false},
		{"similar program name", "dnsmasq-helper\x00--interface=nrnat\x00", "nrnat", false},
		{"no interface argument", "dnsmasq\x00--conf-file=/dev/null\x00", "nrnat", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dnsmasqServing([]byte(c.cmdline), c.iface); got != c.want {
				t.Fatalf("dnsmasqServing(%q, %q) = %v, want %v", c.cmdline, c.iface, got, c.want)
			}
		})
	}
}
