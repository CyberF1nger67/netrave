// SPDX-License-Identifier: GPL-3.0-only

package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Check - the result of one environment check.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func lookup(name string) (string, bool) {
	p, err := exec.LookPath(name)
	return p, err == nil
}

// Preflight checks for root, KVM, QEMU and network utilities.
func Preflight(qemuPath string) []Check {
	var cs []Check

	cs = append(cs, Check{"root", os.Geteuid() == 0,
		map[bool]string{true: "running as root", false: "not root: network bridges and TAP devices are unavailable"}[os.Geteuid() == 0]})

	if f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0); err != nil {
		cs = append(cs, Check{"/dev/kvm", false, err.Error()})
	} else {
		f.Close()
		cs = append(cs, Check{"/dev/kvm", true, "available"})
	}

	if p, ok := lookup(qemuPath); !ok {
		cs = append(cs, Check{"qemu", false, qemuPath + " not found in PATH"})
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, p, "--version").Output()
		if err != nil {
			cs = append(cs, Check{"qemu", false, err.Error()})
		} else {
			line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
			cs = append(cs, Check{"qemu", true, line})
		}
	}

	for _, tool := range []string{"qemu-img", "ip"} {
		if p, ok := lookup(tool); ok {
			cs = append(cs, Check{tool, true, p})
		} else {
			cs = append(cs, Check{tool, false, "not found in PATH"})
		}
	}

	if p, ok := lookup("nft"); ok {
		cs = append(cs, Check{"nft", true, p + " (NAT)"})
	} else {
		cs = append(cs, Check{"nft", false, "not found: required for NAT (install nftables)"})
	}
	if p, ok := lookup("dnsmasq"); ok {
		cs = append(cs, Check{"dnsmasq", true, p + " (DHCP)"})
	} else {
		cs = append(cs, Check{"dnsmasq", false, "not found: required for DHCP in NAT networks (install dnsmasq)"})
	}

	if b, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward"); err == nil {
		v := strings.TrimSpace(string(b))
		cs = append(cs, Check{"ip_forward", v == "1", fmt.Sprintf("net.ipv4.ip_forward=%s (enabled when a NAT network is created)", v)})
	}

	// br_netfilter passes bridge-switched frames through ip forward. This is not an error:
	// netrave allows such traffic, but the state is useful to see when diagnosing packet loss.
	_, nfErr := os.Stat("/proc/sys/net/bridge/bridge-nf-call-iptables")
	if nfErr != nil {
		_, nfErr = os.Stat("/sys/module/br_netfilter")
	}
	if nfErr == nil {
		v := "?"
		if b, err := os.ReadFile("/proc/sys/net/bridge/bridge-nf-call-iptables"); err == nil {
			v = strings.TrimSpace(string(b))
		}
		cs = append(cs, Check{"br_netfilter", true, "loaded, bridge-nf-call-iptables=" + v + ": bridged lab traffic is accepted explicitly"})
	} else {
		cs = append(cs, Check{"br_netfilter", true, "not loaded: bridged frames bypass ip firewall"})
	}
	return cs
}
