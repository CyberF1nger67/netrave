#!/bin/sh
# Diagnostics of netrave network resources on a Linux host. Run as root.
# Shows: bridges and TAPs, which bridges each TAP is attached to, nftables rules,
# dnsmasq DHCP processes, forwarding enabled, running QEMU instances and their network interfaces.
set -u
sec() { printf '\n== %s ==\n' "$1"; }

sec "bridges and TAP nr* (name, state, MAC)"
IP=$(command -v ip || echo /sbin/ip)
$IP -br link show | grep -E '^nr' || echo "(no nr* interfaces)"

sec "TAP: which bridge each device interface is attached to"
for i in $(ls /sys/class/net | grep '^nrt'); do
  m=$(readlink /sys/class/net/$i/master 2>/dev/null | xargs -r basename)
  st=$(cat /sys/class/net/$i/operstate 2>/dev/null)
  printf '%-18s master=%-14s state=%s\n' "$i" "${m:-none}" "$st"
done

sec "bridge ports (brif)"
for b in $(ls /sys/class/net | grep '^nr[nl]'); do
  printf '%s: %s\n' "$b" "$(ls /sys/class/net/$b/brif 2>/dev/null | tr '\n' ' ')"
done

sec "NAT bridge addresses"
$IP -4 -br addr show | grep -E '^nrn' || echo "(no addresses on nrn*)"

sec "bridge FDB (which MAC is behind which port; no static entries)"
bridge fdb show 2>/dev/null | grep -v permanent | head -40

sec "nftables: netrave table"
nft list table ip netrave 2>/dev/null || echo "(no netrave table)"

sec "IPv4 forwarding and default route"
echo "ip_forward=$(cat /proc/sys/net/ipv4/ip_forward)"
$IP -4 route show default

sec "br_netfilter: whether bridged traffic passes through ip hooks"
if [ -e /proc/sys/net/bridge/bridge-nf-call-iptables ]; then
  for k in bridge-nf-call-iptables bridge-nf-call-ip6tables bridge-nf-call-arptables; do
    [ -e "/proc/sys/net/bridge/$k" ] && echo "$k=$(cat /proc/sys/net/bridge/$k)"
  done
  echo "br_netfilter loaded: frames between machines on the same bridge are visible to ip forward."
  echo "netrave allows them with an iifname/oifname <bridge> accept rule before iifname \"nrn*\" drop."
else
  echo "br_netfilter not loaded: bridged frames do not reach the ip filter."
fi

sec "foreign drop/reject rules in the forward hook (may drop frames between machines)"
nft list ruleset 2>/dev/null | awk '
  /^table / {t=$3; fwd=0; next}
  /hook forward/ {fwd=1; if ($0 ~ /policy drop/) print t ": " $0; next}
  fwd && (/ drop$/ || / reject/) {print t ": " $0}
  /^\}/ {fwd=0}
' | grep -v -E '^(netrave|netrave_iso):' | head -20 || true
echo "(if there are foreign drop/reject rules above, with br_netfilter they override the accept rules from the netrave table)"

sec "dnsmasq DHCP servers on nrn* bridges"
ps -eo pid,comm,args | awk '$2=="dnsmasq" && /nrn/ {print}' | grep . || echo "(dnsmasq for nrn* not running)"

sec "running QEMU processes and their network interfaces"
ps -eo pid,comm,args | awk '$2 ~ /^qemu-system/' | tr ' ' '\n' | grep -E '^(ifname=|id=net)' | paste - - 2>/dev/null \
  || echo "(QEMU not running)"

sec "TAP without a device (possible leak if QEMU is not running)"
running=$(ps -eo comm,args | awk '$1 ~ /^qemu-system/' | grep -o 'ifname=nrt[0-9a-f]*n[0-9]*' | cut -d= -f2)
for i in $(ls /sys/class/net | grep '^nrt'); do
  echo "$running" | grep -qx "$i" || echo "WARNING: $i is not used by any QEMU"
done
echo "(check complete)"
