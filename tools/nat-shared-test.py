#!/usr/bin/env python3
"""Test of the shared NAT cloud on a Linux kernel (requires root, iproute2, nftables, dnsmasq).

What is checked:
  • one nrnat bridge with a gateway address; DHCP hands out addresses from the shared pool (no duplicates);
  • machines in the same group (one instance, one network) can see each other;
  • machines in different groups cannot see each other at L2 (ARP) or IP, even with address spoofing;
  • all machines reach the gateway (host) and DHCP works.

Bridge ports are named nrt<hex>n<nic>, the same as machine TAPs in network.go, so the rules match production.
Machines are simulated with veth pairs and network namespaces; bridge semantics are the same as for TAPs.

Run: sudo python3 tools/nat-shared-test.py
"""
import os
import re
import shutil
import subprocess
import sys
import time

BRIDGE = "nrnat"
GW = "10.77.0.1/16"
POOL = ("10.77.0.10", "10.77.255.250")
RUN = "/tmp/netrave-nat-test"

# Groups: (instance, network) -> list of bridge ports. Matches the isolationScript format.
GROUPS = {
    "1:4": ["nrt1an0", "nrt2an0"],  # one instance, one NAT network: A and B
    "2:4": ["nrt3an0"],             # another user: C
}
MACHINES = {  # netns name -> bridge port
    "ma": "nrt1an0",
    "mb": "nrt2an0",
    "mc": "nrt3an0",
}

fails = []


def sh(cmd, check=True, quiet=False):
    r = subprocess.run(cmd, shell=True, text=True, capture_output=True)
    if check and r.returncode != 0:
        raise RuntimeError(f"command failed: {cmd}\n{r.stdout}{r.stderr}")
    return r


def check(name, ok, detail=""):
    print(("[ok]   " if ok else "[FAIL] ") + name + (f"  {detail}" if detail and not ok else ""))
    if not ok:
        fails.append(name)


def cleanup():
    for ns in MACHINES:
        sh(f"ip netns del {ns}", check=False, quiet=True)
    sh(f"ip link del {BRIDGE}", check=False, quiet=True)
    sh("nft delete table bridge netrave_iso", check=False, quiet=True)
    sh("nft delete table ip netrave_nat_test", check=False, quiet=True)
    for port in ["nrt1an0", "nrt2an0", "nrt3an0"]:
        sh(f"ip link del {port}", check=False, quiet=True)
    if os.path.exists(f"{RUN}/dnsmasq.pid"):
        pid = open(f"{RUN}/dnsmasq.pid").read().strip()
        sh(f"kill {pid}", check=False, quiet=True)
    shutil.rmtree(RUN, ignore_errors=True)


def isolation_script():
    """Same text as built by network.isolationScript."""
    lines = ["add table bridge netrave_iso", "flush table bridge netrave_iso",
             "add chain bridge netrave_iso isolate { type filter hook forward priority -100; policy accept; }"]
    for key in sorted(GROUPS):
        s = ", ".join(f'"{t}"' for t in sorted(set(GROUPS[key])))
        lines.append(f"add rule bridge netrave_iso isolate iifname {{ {s} }} oifname != {{ {s} }} drop")
    return "\n".join(lines) + "\n"


def setup():
    cleanup()
    os.makedirs(RUN, exist_ok=True)
    sh(f"ip link add {BRIDGE} type bridge")
    sh(f"ip addr add {GW} dev {BRIDGE}")
    sh(f"ip link set {BRIDGE} up")
    for ns, port in MACHINES.items():
        sh(f"ip netns add {ns}")
        sh(f"ip link add {port} type veth peer name eth0 netns {ns}")
        sh(f"ip link set {port} master {BRIDGE}")
        sh(f"ip link set {port} up")
        sh(f"ip -n {ns} link set eth0 up")
        sh(f"ip -n {ns} link set lo up")
    sh(f"echo 1 > /proc/sys/net/ipv4/ip_forward")
    if "--no-isolation" in sys.argv:
        return  # control run: without isolation rules, connectivity between groups should exist
    p = subprocess.run(["nft", "-f", "-"], input=isolation_script(), text=True, capture_output=True)
    if p.returncode != 0:
        raise RuntimeError("nft: " + p.stderr)


def ping(ns, dst):
    r = sh(f"ip netns exec {ns} ping -c 2 -W 1 {dst}", check=False, quiet=True)
    return r.returncode == 0


def dhcp_test():
    # dnsmasq on the bridge with a shared pool, as in ensureDHCP.
    args = [
        f"--interface={BRIDGE}", "--bind-interfaces", "--except-interface=lo", "--conf-file=/dev/null",
        "--no-hosts", "--dhcp-authoritative",
        f"--dhcp-range={POOL[0]},{POOL[1]},12h", "--dhcp-option=option:router,10.77.0.1",
        "--dhcp-option=option:dns-server,10.77.0.1",
        f"--dhcp-leasefile={RUN}/dnsmasq.leases", f"--pid-file={RUN}/dnsmasq.pid", "--user=root", "--group=root",
    ]
    sh("dnsmasq " + " ".join(f"'{a}'" for a in args))
    leases = {}
    for ns in MACHINES:
        sh(f"ip netns exec {ns} dhclient -1 -v -sf /bin/true -pf {RUN}/{ns}.pid -lf {RUN}/{ns}.lease eth0",
           check=False, quiet=True)
        # The guest OS applies the assigned address itself; here dhclient does not configure the interface (-sf /bin/true),
        # so the address is taken from the lease file and assigned the way the client would have done.
        lease = open(f"{RUN}/{ns}.lease").read() if os.path.exists(f"{RUN}/{ns}.lease") else ""
        m = re.findall(r"fixed-address (10\.77\.\d+\.\d+);", lease)
        leases[ns] = m[-1] if m else None
        if leases[ns]:
            sh(f"ip -n {ns} addr add {leases[ns]}/16 dev eth0")
    got = [v for v in leases.values() if v]
    check("DHCP: all machines got an address from the shared pool", len(got) == len(MACHINES), str(leases))
    check("DHCP: addresses are unique (no conflicts)", len(set(got)) == len(got), str(leases))
    return leases


def main():
    if os.geteuid() != 0:
        print("root required")
        return 2
    for tool in ["ip", "nft", "dnsmasq", "dhclient"]:
        if not shutil.which(tool):
            print(f"not found: {tool}")
            return 2
    try:
        setup()
        time.sleep(0.5)
        # Group 1: A and B see each other, and C sees neither A nor B.
        ip_of = dhcp_test()
        a, b, c = (ip_of.get(n) for n in ["ma", "mb", "mc"])
        if not all([a, b, c]):
            print("DHCP did not assign addresses to all machines: further checks are impossible")
            return 1

        check("group 1: A → B (same group) reachable", ping("ma", b))
        check("group 1: B → A (same group) reachable", ping("mb", a))
        check("group 2: C → gateway (host) reachable", ping("mc", "10.77.0.1"))
        check("A → gateway (host) reachable", ping("ma", "10.77.0.1"))
        if "--no-isolation" in sys.argv:
            check("control without isolation: A → C passes", ping("ma", c))
            return 0 if not fails else 1
        check("isolation: A → C by IP is blocked", not ping("ma", c))
        check("isolation: C → A by IP is blocked", not ping("mc", a))
        check("isolation: B → C by IP is blocked", not ping("mb", c))

        # Address spoofing: C poses as A's address. Port-based isolation does not let it through.
        sh("ip -n mc addr add " + a + "/16 dev eth0", check=False, quiet=True)
        check("isolation does not depend on IP: C with A's address cannot reach B", not ping("mc", b))
        sh("ip -n mc addr del " + a + "/16 dev eth0", check=False, quiet=True)

        # C's ARP request to A must not receive a reply (L2 is separated).
        sh("ip -n mc neigh flush all", check=False, quiet=True)
        r = sh(f"ip netns exec mc ping -c 1 -W 1 {a}", check=False, quiet=True)
        check("L2 isolation: C does not get an ARP reply from A", r.returncode != 0)
    finally:
        cleanup()

    if fails:
        print(f"\nFAILED: {len(fails)}")
        return 1
    print("\nAll shared NAT cloud checks passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
