// SPDX-License-Identifier: GPL-3.0-only

package network

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"netrave/internal/natcloud"
)

func TestNATRulesSharedCloud(t *testing.T) {
	nat, fwd := natRules("eth0", true, []string{"nrnat", "nrn1a"})
	if len(nat) != 1 || nat[0] != `ip saddr 10.77.0.0/16 oifname "eth0" masquerade` {
		t.Fatalf("masquerade: %v", nat)
	}
	want := []string{
		"ct state established,related accept",
		`iifname "nrnat" oifname "eth0" accept`,
		`iifname "nrnat" oifname "nrnat" accept`,
		`iifname "nrn1a" oifname "nrn1a" accept`,
		`iifname "nrn*" drop`,
	}
	if strings.Join(fwd, "|") != strings.Join(want, "|") {
		t.Fatalf("forward:\n got %v\nwant %v", fwd, want)
	}
}

func TestNATRulesWithoutEgressOrNAT(t *testing.T) {
	nat, fwd := natRules("", true, nil)
	if len(nat) != 0 {
		t.Fatalf("masquerade must not be present without outbound access: %v", nat)
	}
	if fwd[len(fwd)-1] != `iifname "nrn*" drop` {
		t.Fatalf("the last rule must drop other nrn* traffic: %v", fwd)
	}
	if _, fwd2 := natRules("eth0", false, nil); strings.Contains(strings.Join(fwd2, "|"), "nrnat") {
		t.Fatalf("there must be no NAT rules for nrnat without active NAT: %v", fwd2)
	}
}

// br_netfilter passes bridge-switched frames through ip forward: the bridge must be
// allowed by an iifname/oifname rule before the general drop, otherwise neighbouring
// machines cannot see each other.
func TestNATRulesBridgeAcceptBeforeDrop(t *testing.T) {
	_, fwd := natRules("", true, []string{"nrnat", "nrn2", "nrn2"})
	bridge := `iifname "nrn2" oifname "nrn2" accept`
	count, idx := 0, -1
	for i, r := range fwd {
		if r == bridge {
			count++
			idx = i
		}
	}
	if count != 1 {
		t.Fatalf("there must be exactly one bridge rule: %v", fwd)
	}
	if fwd[len(fwd)-1] != `iifname "nrn*" drop` || idx >= len(fwd)-1 {
		t.Fatalf("bridge rule must come before drop: %v", fwd)
	}
}

func TestIsolationScriptGroups(t *testing.T) {
	groups := map[string][]string{
		"1:4": {"nrt1an0", "nrt2an0", "nrt2an0"}, // a duplicate must not end up in the rule twice
		"2:4": {"nrt3an0"},
	}
	s := isolationScript(groups)
	mustContain := []string{
		"add table bridge netrave_iso",
		"add chain bridge netrave_iso isolate { type filter hook forward priority -100; policy accept; }",
		`add rule bridge netrave_iso isolate iifname { "nrt1an0", "nrt2an0" } oifname != { "nrt1an0", "nrt2an0" } drop`,
		`add rule bridge netrave_iso isolate iifname { "nrt3an0" } oifname != { "nrt3an0" } drop`,
	}
	for _, m := range mustContain {
		if !strings.Contains(s, m) {
			t.Fatalf("scenario has no line %q:\n%s", m, s)
		}
	}
	if strings.Count(s, "nrt2an0\" } oifname") != 1 {
		t.Fatalf("duplicate port was not removed:\n%s", s)
	}
}

// The script must be syntactically valid for nft: check without applying (-c). Requires root and nft.
func TestIsolationScriptIsValidNFT(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root for nft")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft is not installed")
	}
	script := isolationScript(map[string][]string{"1:1": {"nrt1an0", "nrt2an0"}, "2:1": {"nrt3an0"}}) +
		natScriptForCheck()
	cmd := exec.Command("nft", "-c", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nft rejected the scenario: %v\n%s\n---\n%s", err, out, script)
	}
}

func natScriptForCheck() string {
	nat, fwd := natRules("eth0", true, []string{"nrnat", "nrn1a"})
	var b strings.Builder
	b.WriteString("add table ip netrave_check\n")
	b.WriteString("add chain ip netrave_check postrouting { type nat hook postrouting priority srcnat; policy accept; }\n")
	b.WriteString("add chain ip netrave_check forward { type filter hook forward priority -10; policy accept; }\n")
	for _, r := range nat {
		b.WriteString("add rule ip netrave_check postrouting " + r + "\n")
	}
	for _, r := range fwd {
		b.WriteString("add rule ip netrave_check forward " + r + "\n")
	}
	return b.String()
}

func TestSharedNATFieldsAreFixed(t *testing.T) {
	// Shared cloud: the gateway is inside the subnet, the DHCP pool is inside the subnet and excludes the gateway.
	subnet, gw, ds, de := natcloud.Fields()
	if subnet != "10.77.0.0/16" || gw != "10.77.0.1" || ds != "10.77.0.10" || de != "10.77.255.250" {
		t.Fatalf("cloud parameters: %s %s %s %s", subnet, gw, ds, de)
	}
}

func TestTapNameRoundTrip(t *testing.T) {
	name, err := tapName(0xabc, 3)
	if err != nil {
		t.Fatal(err)
	}
	rid, ok := parseTap(name)
	if !ok || rid != 0xabc {
		t.Fatalf("parseTap(%q) = %d, %v", name, rid, ok)
	}
	if _, err := tapName(1<<60, 0); err == nil {
		t.Fatal("too long TAP name must be rejected")
	}
}
