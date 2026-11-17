// SPDX-License-Identifier: GPL-3.0-only

package network

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/natcloud"
	"netrave/internal/qemu"
)

// Manager implements qemu.Plumber: bridges, TAPs, NAT and DHCP for lab instances.
//
// Instance isolation: each user has their own bridges and subnets. Names are built from internal ids:
//   - instance network (instance_networks.id)  -> bridge nrn<hex>
//   - instance cable (instance_links.id)      -> bridge nrl<hex>
//   - machine interface TAP (instance_devices.id) -> nrt<hex>n<nic>
//   - Cloud with a host bridge                -> veth nrv<hex> / nrw<hex>
//
// NAT subnets are allocated per instance from 10.77.0.0/16 without overlaps, so two instances of the same
// lab cannot see each other and do not conflict on addresses.
type Manager struct {
	db  *database.DB
	cfg *config.Config
	mu  sync.Mutex
	rt  *qemu.Manager // mutable machine state lives in the QEMU manager's memory
}

func New(db *database.DB, cfg *config.Config) *Manager {
	return &Manager{db: db, cfg: cfg}
}

// SetQEMU connects the source of machine state (the single manager instance in the server process).
func (m *Manager) SetQEMU(q *qemu.Manager) { m.rt = q }

func (m *Manager) active(id int64) bool {
	return m.rt != nil && m.rt.Active(id)
}

func (m *Manager) state(id int64) string {
	if m.rt == nil {
		return "stopped"
	}
	return m.rt.State(id)
}

// ---------- interface names ----------

func netBridge(id int64) string           { return fmt.Sprintf("nrn%x", id) }
func linkBridge(id int64) string          { return fmt.Sprintf("nrl%x", id) }
func vethNames(id int64) (string, string) { return fmt.Sprintf("nrv%x", id), fmt.Sprintf("nrw%x", id) }

// tapName is the TAP of a machine interface: rid (instance string) and interface number.
func tapName(rid int64, nic int) (string, error) {
	n := fmt.Sprintf("nrt%xn%d", rid, nic)
	if len(n) > 15 {
		return "", errors.New("слишком большой идентификатор машины для имени сетевого интерфейса")
	}
	return n, nil
}

func parseTap(name string) (int64, bool) {
	if !strings.HasPrefix(name, "nrt") {
		return 0, false
	}
	rest := name[3:]
	i := strings.IndexByte(rest, 'n')
	if i <= 0 {
		return 0, false
	}
	v, err := strconv.ParseInt(rest[:i], 16, 64)
	return v, err == nil
}

type netRow struct {
	ID        int64
	LabID     int64
	Type      string
	Subnet    string
	Gateway   string
	DHCP      bool
	DHCPStart string
	DHCPEnd   string
	HostIface string
}

// ---------- instances: subnets and cable bridges ----------

// netFor returns the network object's group in the instance: an instance_networks row (created on first connection).
// For NAT networks addresses are shared (natcloud); the row id is the isolation key: machines of different instances and different
// network objects cannot exchange frames on a shared bridge (see applyIsolation).
func (m *Manager) netFor(instID, netID int64) (*netRow, error) {
	var n netRow
	fn, labID, ok := m.db.Labs.Network(netID)
	if !ok {
		return nil, sql.ErrNoRows
	}
	n.LabID, n.Type, n.HostIface, n.DHCP = labID, fn.Type, fn.HostIface, fn.DHCPEnabled
	if n.Type == "nat" {
		n.Subnet, n.Gateway, n.DHCPStart, n.DHCPEnd = natcloud.Fields()
	}
	var rid int64
	err := m.db.QueryRow(`SELECT id,subnet,gateway,dhcp_start,dhcp_end FROM instance_networks WHERE instance_id=? AND network_id=?`, instID, netID).
		Scan(&rid, new(string), new(string), new(string), new(string))
	if err == sql.ErrNoRows {
		res, ierr := m.db.Exec(`INSERT INTO instance_networks(instance_id, network_id, subnet, gateway, dhcp_start, dhcp_end) VALUES(?,?,?,?,?,?)`,
			instID, netID, n.Subnet, n.Gateway, n.DHCPStart, n.DHCPEnd)
		if ierr != nil {
			return nil, ierr
		}
		rid, ierr = res.LastInsertId()
		if ierr != nil {
			return nil, ierr
		}
	} else if err != nil {
		return nil, err
	}
	n.ID = rid
	return &n, nil
}

// linkFor returns the id of the cable's bridge in the instance.
func (m *Manager) linkFor(instID, connID int64) (int64, error) {
	if _, err := m.db.Exec(`INSERT OR IGNORE INTO instance_links(instance_id, connection_id) VALUES(?,?)`, instID, connID); err != nil {
		return 0, err
	}
	var lid int64
	err := m.db.QueryRow(`SELECT id FROM instance_links WHERE instance_id=? AND connection_id=?`, instID, connID).Scan(&lid)
	return lid, err
}

func (m *Manager) runningIDs() (map[int64]bool, error) {
	rows, err := m.db.Query(`SELECT id FROM instance_devices`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil && m.active(id) {
			out[id] = true
		}
	}
	return out, rows.Err()
}

// activeInstances are the instances that have a running machine.
func (m *Manager) activeInstances() (map[int64]bool, error) {
	rows, err := m.db.Query(`SELECT id, instance_id FROM instance_devices`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var rid, inst int64
		if rows.Scan(&rid, &inst) == nil && m.active(rid) {
			out[inst] = true
		}
	}
	return out, rows.Err()
}

// activeNets are instance networks that have running machines.
func (m *Manager) activeNets() ([]netRow, error) {
	active, err := m.activeInstances()
	if err != nil {
		return nil, err
	}
	rows, err := m.db.Query(`SELECT ir.id, ir.instance_id, ir.network_id, ir.subnet, ir.gateway, ir.dhcp_start, ir.dhcp_end
		FROM instance_networks ir`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []netRow
	for rows.Next() {
		var n netRow
		var inst, netID int64
		if err := rows.Scan(&n.ID, &inst, &netID, &n.Subnet, &n.Gateway, &n.DHCPStart, &n.DHCPEnd); err != nil {
			return nil, err
		}
		if !active[inst] {
			continue
		}
		fn, labID, ok := m.db.Labs.Network(netID)
		if !ok {
			continue
		}
		n.LabID, n.Type, n.DHCP, n.HostIface = labID, fn.Type, fn.DHCPEnabled, fn.HostIface
		out = append(out, n)
	}
	return out, rows.Err()
}

// activeLinks are instance cable bridges that have running machines.
func (m *Manager) activeLinks() ([]int64, error) {
	active, err := m.activeInstances()
	if err != nil {
		return nil, err
	}
	rows, err := m.db.Query(`SELECT id, instance_id FROM instance_links`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id, inst int64
		if rows.Scan(&id, &inst) == nil && active[inst] {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

func netdevArg(nic int, tap string) string {
	return fmt.Sprintf("tap,id=net%d,ifname=%s,script=no,downscript=no", nic, tap)
}

// Netdev creates a TAP for interface nic of an instance machine and connects it to the instance's network or cable bridge.
func (m *Manager) Netdev(s *qemu.Spec, nic int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tap, err := m.tapFor(s, nic)
	if err != nil {
		return "", err
	}
	// Starting a machine may have added the first NAT machine: bring up DHCP, masquerade and isolation rules.
	for _, err := range m.syncNAT() {
		log.Printf("network: %v", err)
	}
	return netdevArg(nic, tap), nil
}

func (m *Manager) tapFor(s *qemu.Spec, nic int) (string, error) {
	c, ok := connAtPort(m.connsOf(s.LabID), s.DeviceID, nic)
	if !ok {
		return m.detachedTap(s, nic)
	}
	connID, aType, aID, aIf, bType, bID := c.ID, c.A.Type, c.A.ID, c.A.If, c.B.Type, c.B.ID

	peerType, peerID := aType, aID
	if aType == "device" && aID == s.DeviceID && aIf == nic {
		peerType, peerID = bType, bID
	}

	var br string
	if peerType == "network" {
		n, nerr := m.netFor(s.InstanceID, peerID)
		if nerr != nil {
			return "", nerr
		}
		var err error
		if br, err = m.ensureNetwork(n); err != nil {
			return "", err
		}
	} else {
		lid, lerr := m.linkFor(s.InstanceID, connID)
		if lerr != nil {
			return "", lerr
		}
		br = linkBridge(lid)
		if err := ensureBridge(br); err != nil {
			return "", err
		}
	}

	tap, err := tapName(s.ID, nic)
	if err != nil {
		return "", err
	}
	delLink(tap)
	if _, err := ip("tuntap", "add", "dev", tap, "mode", "tap"); err != nil {
		return "", err
	}
	// Disconnected wire: the TAP is created but does not join the bridge (QEMU will get set_link up=false at startup).
	if m.linkUp(s.InstanceID, connID) {
		if _, err := ip("link", "set", tap, "master", br); err != nil {
			delLink(tap)
			return "", err
		}
	}
	if _, err := ip("link", "set", tap, "up"); err != nil {
		delLink(tap)
		return "", err
	}
	if m.linkUp(s.InstanceID, connID) {
		log.Printf("network: машина %d, интерфейс %d: TAP %s подключён к мосту %s", s.ID, nic, tap, br)
	} else {
		log.Printf("network: машина %d, интерфейс %d: TAP %s создан, провод отключён (мост %s не подключён)", s.ID, nic, tap, br)
	}
	return tap, nil
}

// Release deletes the machine's TAP interfaces. Bridges remain while an instance with running machines exists.
func (m *Manager) Release(s *qemu.Spec) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := fmt.Sprintf("nrt%xn", s.ID)
	for _, name := range ifaceNames() {
		if strings.HasPrefix(name, prefix) {
			_, _ = ip("link", "del", name)
		}
	}
}

// Reconcile brings host bridges, TAPs, NAT and DHCP in line with the database: only bridges of instances
// with running machines; other bridges and TAPs are deleted.
func (m *Manager) Reconcile() []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error

	nets, err := m.activeNets()
	if err != nil {
		return []error{err}
	}
	links, err := m.activeLinks()
	if err != nil {
		return []error{err}
	}
	running, err := m.runningIDs()
	if err != nil {
		return []error{err}
	}

	wantBr := map[string]bool{}
	wantVeth := map[string]bool{}
	for i := range nets {
		n := &nets[i]
		if n.Type == "nat" {
			wantBr[natcloud.Bridge] = true
			continue
		}
		wantBr[netBridge(n.ID)] = true
		if n.Type == "cloud" && n.HostIface != "" && isBridge(n.HostIface) {
			a, b := vethNames(n.ID)
			wantVeth[a], wantVeth[b] = true, true
		}
	}
	for _, id := range links {
		wantBr[linkBridge(id)] = true
	}

	for i := range nets {
		if _, err := m.ensureNetwork(&nets[i]); err != nil {
			errs = append(errs, fmt.Errorf("сеть %s: %w", netBridge(nets[i].ID), err))
		}
	}
	for _, id := range links {
		if err := ensureBridge(linkBridge(id)); err != nil {
			errs = append(errs, fmt.Errorf("кабель %s: %w", linkBridge(id), err))
		}
	}

	for _, name := range ifaceNames() {
		switch {
		case strings.HasPrefix(name, "nrn"), strings.HasPrefix(name, "nrl"):
			if !wantBr[name] {
				delLink(name)
			}
		case strings.HasPrefix(name, "nrv"), strings.HasPrefix(name, "nrw"):
			if !wantVeth[name] {
				delLink(name)
			}
		case strings.HasPrefix(name, "nrt"):
			if id, ok := parseTap(name); !ok || !running[id] {
				delLink(name)
			}
		}
	}

	errs = append(errs, m.syncNAT()...)
	return errs
}

func runCmd(stdin string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func ip(args ...string) (string, error) { return runCmd("", "ip", args...) }

func linkExists(name string) bool {
	_, err := net.InterfaceByName(name)
	return err == nil
}

func ifaceNames() []string {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(ifs))
	for _, i := range ifs {
		out = append(out, i.Name)
	}
	return out
}

func delLink(name string) {
	if linkExists(name) {
		_, _ = ip("link", "del", name)
	}
}

func ensureBridge(name string) error {
	if !linkExists(name) {
		if _, err := ip("link", "add", "name", name, "type", "bridge"); err != nil {
			return err
		}
	}
	_, err := ip("link", "set", name, "up")
	return err
}

func masterOf(name string) string {
	t, err := os.Readlink("/sys/class/net/" + name + "/master")
	if err != nil {
		return ""
	}
	return filepath.Base(t)
}

func isBridge(name string) bool {
	st, err := os.Stat("/sys/class/net/" + name + "/bridge")
	return err == nil && st.IsDir()
}

func defaultIface() string {
	out, err := ip("-4", "route", "show", "default")
	if err != nil {
		return ""
	}
	f := strings.Fields(out)
	for i := 0; i+1 < len(f); i++ {
		if f[i] == "dev" {
			return f[i+1]
		}
	}
	return ""
}

func writeSysctl(path, val string) error {
	cur, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(cur)) == val {
		return nil
	}
	return os.WriteFile(path, []byte(val+"\n"), 0o644)
}

// ensureNATCloud brings up the shared NAT cloud bridge with the gateway address and enables IPv4 forwarding.
func ensureNATCloud() error {
	if err := ensureBridge(natcloud.Bridge); err != nil {
		return err
	}
	_, prefix, _ := strings.Cut(natcloud.Subnet, "/")
	if err := setAddr(natcloud.Bridge, natcloud.Gateway+"/"+prefix); err != nil {
		return err
	}
	if err := writeSysctl("/proc/sys/net/ipv4/ip_forward", "1"); err != nil {
		return fmt.Errorf("включение ip_forward: %w", err)
	}
	return nil
}

// setAddr makes cidr the bridge's only IPv4 address.
func setAddr(br, cidr string) error {
	out, _ := ip("-4", "addr", "show", "dev", br)
	if strings.Contains(out, "inet "+cidr+" ") {
		return nil
	}
	if _, err := ip("-4", "addr", "flush", "dev", br); err != nil {
		return err
	}
	_, err := ip("addr", "add", cidr, "dev", br)
	return err
}

func attachHost(netID int64, br, host string) error {
	if !linkExists(host) {
		return fmt.Errorf("интерфейс хоста %q не найден", host)
	}
	a, b := vethNames(netID)
	if isBridge(host) {
		// Existing host bridge: connect it to our bridge with a veth pair, leaving the host itself untouched.
		if !linkExists(a) {
			if _, err := ip("link", "add", a, "type", "veth", "peer", "name", b); err != nil {
				return err
			}
		}
		for _, args := range [][]string{
			{"link", "set", a, "master", host},
			{"link", "set", b, "master", br},
			{"link", "set", a, "up"},
			{"link", "set", b, "up"},
		} {
			if _, err := ip(args...); err != nil {
				return err
			}
		}
		return nil
	}
	delLink(a)
	delLink(b)
	if defaultIface() == host {
		return fmt.Errorf("интерфейс %s несёт маршрут по умолчанию: его подключение к мосту отключит хост от сети; выберите другой интерфейс или мост", host)
	}
	if m := masterOf(host); m != "" && m != br {
		return fmt.Errorf("интерфейс %s уже входит в мост %s", host, m)
	}
	if masterOf(host) != br {
		if _, err := ip("link", "set", host, "master", br); err != nil {
			return err
		}
	}
	_, err := ip("link", "set", host, "up")
	return err
}

func (m *Manager) ensureNetwork(n *netRow) (string, error) {
	if n.Type == "nat" {
		return natcloud.Bridge, ensureNATCloud()
	}
	br := netBridge(n.ID)
	if err := ensureBridge(br); err != nil {
		return "", err
	}
	switch n.Type {
	case "cloud":
		if n.HostIface != "" {
			if err := attachHost(n.ID, br, n.HostIface); err != nil {
				return "", err
			}
		}
	}
	return br, nil
}

func (m *Manager) detachedTap(s *qemu.Spec, nic int) (string, error) {
	tap, err := tapName(s.ID, nic)
	if err != nil {
		return "", err
	}
	delLink(tap)
	if _, err := ip("tuntap", "add", "dev", tap, "mode", "tap"); err != nil {
		return "", err
	}
	if _, err := ip("link", "set", tap, "down"); err != nil {
		delLink(tap)
		return "", err
	}
	log.Printf("network: машина %d, интерфейс %d: TAP %s создан без провода (нет подключения в топологии)", s.ID, nic, tap)
	return tap, nil
}

// brNetfilterActive reports that br_netfilter is loaded (as a module or built into the kernel).
// In that case, frames switched by a bridge also pass through IP hooks, where iifname/oifname
// contain the bridge name and ibrname/obrname contain the name of the bridge the frame actually traverses.
func brNetfilterActive() bool {
	if _, err := os.Stat("/proc/sys/net/bridge/bridge-nf-call-iptables"); err == nil {
		return true
	}
	_, err := os.Stat("/sys/module/br_netfilter")
	return err == nil
}

// natRules builds masquerade (postrouting) and forward rules for the shared NAT cloud and network bridges.
// The function is pure: its output is tested without root.
//
// bridges are active nrn* bridges (including nrnat). For each one, forwarding of frames
// switched by that bridge is allowed. This is needed when br_netfilter is loaded: such a frame
// passes through ip forward with iifname=oifname=<bridge> and otherwise hits the iifname "nrn*" drop rule.
// That is why a machine's own IP is pingable, but a neighbouring machine's is not. The iifname/oifname
// pair allowance does not let routed traffic through: its output interface is different
// (eth0, an nrn of another network), and it is still dropped by the last rule.
func natRules(egress string, natActive bool, bridges []string) (nat, fwd []string) {
	if natActive && egress != "" {
		nat = append(nat, fmt.Sprintf("ip saddr %s oifname %q masquerade", natcloud.Subnet, egress))
		fwd = append(fwd, fmt.Sprintf("iifname %q oifname %q accept", natcloud.Bridge, egress))
	}
	seen := map[string]bool{}
	for _, br := range bridges {
		if br == "" || seen[br] {
			continue
		}
		seen[br] = true
		fwd = append(fwd, fmt.Sprintf("iifname %q oifname %q accept", br, br))
	}
	fwd = append([]string{"ct state established,related accept"}, fwd...)
	fwd = append(fwd, `iifname "nrn*" drop`)
	return nat, fwd
}

func (m *Manager) applyNAT(nets []netRow) []error {
	egress := defaultIface()
	natActive := false
	var bridges []string
	for i := range nets {
		n := &nets[i]
		if n.Type == "nat" {
			natActive = true
			if linkExists(natcloud.Bridge) {
				bridges = append(bridges, natcloud.Bridge)
			}
			continue
		}
		if br := netBridge(n.ID); linkExists(br) {
			bridges = append(bridges, br)
		}
	}
	natR, fwdR := natRules(egress, natActive && linkExists(natcloud.Bridge), bridges)
	var b strings.Builder
	b.WriteString("add table ip netrave\nflush table ip netrave\n")
	b.WriteString("add chain ip netrave postrouting { type nat hook postrouting priority srcnat; policy accept; }\n")
	b.WriteString("add chain ip netrave forward { type filter hook forward priority -10; policy accept; }\n")
	for _, r := range natR {
		b.WriteString("add rule ip netrave postrouting " + r + "\n")
	}
	for _, r := range fwdR {
		b.WriteString("add rule ip netrave forward " + r + "\n")
	}
	if _, err := runCmd(b.String(), "nft", "-f", "-"); err != nil {
		return []error{fmt.Errorf("nftables: %w", err)}
	}
	if natActive {
		if err := writeSysctl("/proc/sys/net/ipv4/ip_forward", "1"); err != nil {
			return []error{err}
		}
	}
	return nil
}

func dnsPaths(runDir string, id int64) (pidF, argsF, leaseF string) {
	base := filepath.Join(runDir, fmt.Sprintf("dnsmasq-%d", id))
	return base + ".pid", base + ".args", base + ".leases"
}

func readPID(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func dnsmasqAlive(pid int) bool {
	if pid <= 0 || syscall.Kill(pid, 0) != nil {
		return false
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	return err == nil && strings.Contains(string(b), "dnsmasq")
}

func stopDnsmasq(runDir string, id int64) {
	pidF, argsF, _ := dnsPaths(runDir, id)
	if pid := readPID(pidF); dnsmasqAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		for i := 0; i < 30 && dnsmasqAlive(pid); i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	_ = os.Remove(pidF)
	_ = os.Remove(argsF)
}

// sharedDHCPID is the dnsmasq identifier of the shared NAT cloud (network objects are numbered from 1).
const sharedDHCPID int64 = 0

// dnsmasqServing reports whether a /proc/<pid>/cmdline belongs to a dnsmasq process
// started for the given interface.
func dnsmasqServing(cmdline []byte, iface string) bool {
	hasName, hasIface := false, false
	for _, arg := range strings.Split(string(cmdline), "\x00") {
		if filepath.Base(arg) == "dnsmasq" {
			hasName = true
		}
		if arg == "--interface="+iface {
			hasIface = true
		}
	}
	return hasName && hasIface
}

// strayDnsmasq lists dnsmasq processes serving iface that are not the tracked pid.
// These are instances whose pid file was lost (an earlier run, another run directory).
// They keep the bridge address bound, so a new server cannot start.
func strayDnsmasq(iface string, tracked int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == tracked || pid == os.Getpid() {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err == nil && dnsmasqServing(cmdline, iface) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// killStrayDnsmasq stops untracked dnsmasq processes on iface: SIGTERM, then SIGKILL after 3 s.
func killStrayDnsmasq(iface string, tracked int) {
	for _, pid := range strayDnsmasq(iface, tracked) {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	for i := 0; i < 30 && len(strayDnsmasq(iface, tracked)) > 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	for _, pid := range strayDnsmasq(iface, tracked) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// ensureDHCP starts (or restarts when parameters change) dnsmasq on the shared NAT cloud bridge.
// One server for all labs: the address pool is shared, so duplicate address leases never happen.
func (m *Manager) ensureDHCP() error {
	bridge := natcloud.Bridge
	if !linkExists(bridge) {
		return errors.New("мост NAT-облака не создан")
	}
	if _, err := exec.LookPath("dnsmasq"); err != nil {
		return errors.New("dnsmasq не найден (установите пакет dnsmasq для DHCP)")
	}
	pidF, argsF, leaseF := dnsPaths(m.cfg.RunDir, sharedDHCPID)
	args := []string{
		"--interface=" + bridge,
		"--bind-interfaces",
		"--except-interface=lo",
		"--conf-file=/dev/null",
		"--no-hosts",
		"--dhcp-authoritative",
		"--dhcp-range=" + natcloud.DHCPStart + "," + natcloud.DHCPEnd + ",12h",
		"--dhcp-option=option:router," + natcloud.Gateway,
		"--dhcp-option=option:dns-server," + natcloud.Gateway,
		"--dhcp-leasefile=" + leaseF,
		"--pid-file=" + pidF,
		"--user=root",
		"--group=root",
	}
	want := strings.Join(args, "\n")
	if old, err := os.ReadFile(argsF); err == nil && string(old) == want && dnsmasqAlive(readPID(pidF)) {
		return nil
	}
	stopDnsmasq(m.cfg.RunDir, sharedDHCPID)
	killStrayDnsmasq(bridge, 0)
	out, err := exec.Command("dnsmasq", args...).CombinedOutput()
	if err != nil {
		// Do not record the arguments of a server that is not running: the next attempt must start it again.
		_ = os.Remove(argsF)
		return fmt.Errorf("dnsmasq: %s", strings.TrimSpace(string(out)))
	}
	return os.WriteFile(argsF, []byte(want), 0o640)
}

// applyDHCP keeps the shared cloud's dnsmasq running while at least one NAT machine exists.
func (m *Manager) applyDHCP(nets []netRow) []error {
	for i := range nets {
		if nets[i].Type == "nat" {
			if err := m.ensureDHCP(); err != nil {
				return []error{fmt.Errorf("DHCP %s: %w", natcloud.Bridge, err)}
			}
			return nil
		}
	}
	stopDnsmasq(m.cfg.RunDir, sharedDHCPID)
	_, _, lease := dnsPaths(m.cfg.RunDir, sharedDHCPID)
	_ = os.Remove(lease)
	return nil
}

// ---------- wires: disconnecting and connecting ----------

// linkUp reports whether the wire is enabled for the instance. A wire without a record is considered enabled.
func (m *Manager) linkUp(instID, connID int64) bool {
	var up int
	if err := m.db.QueryRow(`SELECT up FROM instance_links WHERE instance_id=? AND connection_id=?`, instID, connID).Scan(&up); err != nil {
		return true
	}
	return up != 0
}

// DownNICs returns the machine interface numbers whose wires are disabled in its instance.
// For them, QEMU immediately receives set_link up=false after startup.
func (m *Manager) DownNICs(s *qemu.Spec) []int {
	rows, err := m.db.Query(`SELECT connection_id FROM instance_links WHERE instance_id = ? AND up = 0`, s.InstanceID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var cid int64
		if rows.Scan(&cid) != nil {
			continue
		}
		c, ok := m.connRow(cid)
		if !ok {
			continue
		}
		if c.A.Type == "device" && c.A.ID == s.DeviceID {
			out = append(out, c.A.If)
		}
		if c.B.Type == "device" && c.B.ID == s.DeviceID {
			out = append(out, c.B.If)
		}
	}
	return out
}

// Interface state codes for the editor's diagram.
const (
	// NICUnready - the link is up but the wire is disabled, or the network has no working connection.
	NICUnready = 0
	// NICNotAttached - the wire exists but the interface is not attached (the machine is not running).
	NICNotAttached = 1
	// NICReady - the interface is attached, the wire is enabled and has left the bridge into the guest network.
	NICReady = 2
)

// NICStates returns the state of each interface of the lab's instance machines: rid -> NIC number -> code.
//
// The source is data managed by the network manager itself: whether TAP interfaces exist (lower
// halves in nrn/nrl bridges) and whether the wire is disabled in the instance. The logic that creates bridges and
// wires does not change: the state is only read.
func (m *Manager) NICStates(labID int64, rids []int64) map[int64]map[int]int {
	out := map[int64]map[int]int{}
	if len(rids) == 0 {
		return out
	}
	// Lab instances: lab_instances belong to labs, machines belong to an instance.
	rows, err := m.db.Query(`SELECT r.id, r.device_id, r.instance_id, r.nics
		FROM instance_devices r
		JOIN lab_instances i ON i.id=r.instance_id
		WHERE i.lab_id=?`, labID)
	if err != nil {
		return out
	}
	type row struct {
		rid, device, instance int64
		nics                  int
	}
	lines := []row{}
	wanted := map[int64]bool{}
	for _, rid := range rids {
		wanted[rid] = true
	}
	for rows.Next() {
		var r row
		if rows.Scan(&r.rid, &r.device, &r.instance, &r.nics) != nil {
			continue
		}
		if wanted[r.rid] {
			lines = append(lines, r)
		}
	}
	rows.Close()

	// Disabled instance wires: the interface at their end remains without a link.
	offLinks := map[int64]map[[2]int64]bool{}
	labConn := map[int64]labfile.Connection{}
	for _, c := range m.connsOf(labID) {
		labConn[c.ID] = c
	}
	lrows, err := m.db.Query(`SELECT instance_id, connection_id FROM instance_links WHERE up = 0`)
	if err == nil {
		for lrows.Next() {
			var inst, cid int64
			if lrows.Scan(&inst, &cid) != nil {
				continue
			}
			c, ok := labConn[cid]
			if !ok {
				continue
			}
			if offLinks[inst] == nil {
				offLinks[inst] = map[[2]int64]bool{}
			}
			if c.A.Type == "device" {
				offLinks[inst][[2]int64{c.A.ID, int64(c.A.If)}] = true
			}
			if c.B.Type == "device" {
				offLinks[inst][[2]int64{c.B.ID, int64(c.B.If)}] = true
			}
		}
		lrows.Close()
	}

	ifaces := hostInterfaceNames()
	for _, line := range lines {
		codes := make(map[int]int, line.nics)
		for nic := 0; nic < line.nics; nic++ {
			tap, err := tapName(line.rid, nic)
			switch {
			case err != nil:
				codes[nic] = NICNotAttached
			case offLinks[line.instance][[2]int64{line.device, int64(nic)}]:
				codes[nic] = NICUnready
			case ifaces[tap]:
				// The TAP exists and is in the bridge: the interface is attached and working.
				codes[nic] = NICReady
			default:
				codes[nic] = NICNotAttached
			}
		}
		out[line.rid] = codes
	}
	return out
}

// hostInterfaceNames returns the set of existing host network interfaces.
func hostInterfaceNames() map[string]bool {
	out := map[string]bool{}
	ifs, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, i := range ifs {
		out[i.Name] = true
	}
	return out
}

// SetCable enables or disables a wire for an instance. The state is stored in instance_links.
// Running instance machines switch immediately: the TAP leaves the cable bridge (or returns to it),
// and QEMU receives set_link for the interface, so the guest OS sees the link go down and come back.
// setLink is a QMP call (implemented by the qemu package); it is not called for machines that are not running.
func (m *Manager) SetCable(instID, connID int64, up bool, setLink func(rid int64, nic int, up bool) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var aT, bT string
	var aID, bID int64
	var aIf, bIf int
	c, ok := m.connRow(connID)
	if !ok {
		return sql.ErrNoRows
	}
	aT, aID, aIf, bT, bID, bIf = c.A.Type, c.A.ID, c.A.If, c.B.Type, c.B.ID, c.B.If
	if _, err := m.db.Exec(`INSERT OR IGNORE INTO instance_links(instance_id, connection_id) VALUES(?,?)`, instID, connID); err != nil {
		return err
	}
	v := 0
	if up {
		v = 1
	}
	if _, err := m.db.Exec(`UPDATE instance_links SET up=? WHERE instance_id=? AND connection_id=?`, v, instID, connID); err != nil {
		return err
	}

	type end struct {
		dev      int64
		nic      int
		peerType string
		peerID   int64
	}
	var ends []end
	if aT == "device" {
		ends = append(ends, end{aID, aIf, bT, bID})
	}
	if bT == "device" {
		ends = append(ends, end{bID, bIf, aT, aID})
	}
	var errs []error
	netTouched := false
	for _, e := range ends {
		var rid int64
		if err := m.db.QueryRow(`SELECT id FROM instance_devices WHERE instance_id=? AND device_id=?`, instID, e.dev).
			Scan(&rid); err != nil {
			continue // the machine is not in this instance
		}
		state := m.state(rid)
		if state != "running" && state != "paused" {
			continue // not running: the wire state will be applied at startup
		}
		tap, err := tapName(rid, e.nic)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if up {
			br, err := m.cableBridge(instID, connID, e.peerType, e.peerID)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if _, err := ip("link", "set", tap, "master", br); err != nil {
				errs = append(errs, err)
			} else {
				log.Printf("network: провод %d: TAP %s машины %d подключён к мосту %s", connID, tap, rid, br)
			}
		} else if _, err := ip("link", "set", tap, "nomaster"); err != nil {
			errs = append(errs, err)
		} else {
			log.Printf("network: провод %d: TAP %s машины %d выведен из моста (link down)", connID, tap, rid)
		}
		if setLink != nil {
			if err := setLink(rid, e.nic, up); err != nil {
				errs = append(errs, err)
			}
		}
		if up && e.peerType == "network" {
			netTouched = true
		}
	}
	if netTouched {
		errs = append(errs, m.syncNAT()...)
	}
	return errors.Join(errs...)
}

// cableBridge returns the bridge to which the wire on the opposite side is connected.
func (m *Manager) cableBridge(instID, connID int64, peerType string, peerID int64) (string, error) {
	if peerType == "network" {
		n, err := m.netFor(instID, peerID)
		if err != nil {
			return "", err
		}
		return m.ensureNetwork(n)
	}
	lid, err := m.linkFor(instID, connID)
	if err != nil {
		return "", err
	}
	br := linkBridge(lid)
	return br, ensureBridge(br)
}

// ---------- shared NAT cloud: synchronization and group isolation ----------

// syncNAT brings the shared NAT cloud in line with running instances: bridge and gateway address, masquerade,
// DHCP and group isolation rules. Called when a machine starts (Netdev) and during reconciliation (Reconcile). The caller holds m.mu.
func (m *Manager) syncNAT() []error {
	nets, err := m.activeNets()
	if err != nil {
		return []error{err}
	}
	errs := m.applyNAT(nets)
	errs = append(errs, m.applyDHCP(nets)...)
	groups, err := m.natGroups()
	if err != nil {
		return append(errs, err)
	}
	if err := applyIsolation(groups); err != nil {
		errs = append(errs, fmt.Errorf("изоляция NAT-облака: %w", err))
	}
	return errs
}

// natGroups assigns the TAP interfaces of machines connected to NAT networks to groups (instance, network object).
// A group is the unit of isolation: within it machines see each other (as on one network), between groups they do not.
// TAP names are built from the machine's rid, so groups are unambiguously tied to instances.
func (m *Manager) natGroups() (map[string][]string, error) {
	// Wires to networks of type nat: all labs whose files are stored in the storage.
	type edge struct {
		net, dev int64
		nic      int
	}
	var edges []edge
	for _, l := range m.db.Labs.Labs() {
		natNets := map[int64]bool{}
		for _, n := range l.Networks {
			if n.Type == "nat" {
				natNets[n.ID] = true
			}
		}
		for _, c := range l.Connections {
			switch {
			case c.A.Type == "network" && natNets[c.A.ID] && c.B.Type == "device":
				edges = append(edges, edge{c.A.ID, c.B.ID, c.B.If})
			case c.B.Type == "network" && natNets[c.B.ID] && c.A.Type == "device":
				edges = append(edges, edge{c.B.ID, c.A.ID, c.A.If})
			}
		}
	}

	groups := map[string][]string{}
	for _, e := range edges {
		irows, err := m.db.Query(`SELECT id, instance_id FROM instance_devices WHERE device_id=?`, e.dev)
		if err != nil {
			return nil, err
		}
		for irows.Next() {
			var rid, inst int64
			if irows.Scan(&rid, &inst) != nil {
				continue
			}
			tap, terr := tapName(rid, e.nic)
			if terr != nil {
				continue
			}
			key := fmt.Sprintf("%d:%d", inst, e.net)
			groups[key] = append(groups[key], tap)
		}
		irows.Close()
	}
	return groups, nil
}

// isolationScript builds the bridge table: a frame arriving from a group's port does not leave through other groups' ports.
// The rule works on ports (iifname/oifname at bridge level) and does not depend on IP addresses, so spoofing an address does not help.
// Broadcast frames (DHCP, ARP) are restricted too: delivery to the host (dnsmasq, gateway) is unaffected.
func isolationScript(groups map[string][]string) string {
	var b strings.Builder
	b.WriteString("add table bridge netrave_iso\nflush table bridge netrave_iso\n")
	b.WriteString("add chain bridge netrave_iso isolate { type filter hook forward priority -100; policy accept; }\n")
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		uniq := map[string]bool{}
		var taps []string
		for _, t := range groups[k] {
			if !uniq[t] {
				uniq[t] = true
				taps = append(taps, strconv.Quote(t))
			}
		}
		sort.Strings(taps)
		set := strings.Join(taps, ", ")
		fmt.Fprintf(&b, "add rule bridge netrave_iso isolate iifname { %s } oifname != { %s } drop\n", set, set)
	}
	return b.String()
}

func applyIsolation(groups map[string][]string) error {
	_, err := runCmd(isolationScript(groups), "nft", "-f", "-")
	return err
}

// ---------- wires: live connection ----------

// AttachLive connects a newly created wire to the interfaces of already running machines in all instances.
// For each running end: the machine's TAP interface joins the wire (or network) bridge of that instance,
// is brought up, and QEMU receives set_link up for the NIC. The machine is not restarted. If the wire touches a NAT network,
// the group isolation rules are updated (syncNAT). Errors are returned as warnings: the wire is already saved.
func (m *Manager) AttachLive(connID int64, setLink func(rid int64, nic int, up bool) error, hotplug func(rid int64, nic int, tap string) error) []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var aT, bT string
	var aID, bID int64
	var aIf, bIf int
	c, ok := m.connRow(connID)
	if !ok {
		return []error{sql.ErrNoRows}
	}
	aT, aID, aIf, bT, bID, bIf = c.A.Type, c.A.ID, c.A.If, c.B.Type, c.B.ID, c.B.If
	type end struct {
		dev, peerID int64
		nic         int
		peerType    string
	}
	var ends []end
	if aT == "device" {
		ends = append(ends, end{aID, bID, aIf, bT})
	}
	if bT == "device" {
		ends = append(ends, end{bID, aID, bIf, aT})
	}
	var errs []error
	natTouched := false
	for _, e := range ends {
		rows, err := m.db.Query(`SELECT r.id,r.instance_id,i.lab_id,r.iface_type,r.iface_start FROM instance_devices r JOIN lab_instances i ON i.id=r.instance_id
			WHERE r.device_id=?`, e.dev)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		type run struct {
			rid, inst, lab int64
			kind           string
			start          int
		}
		var runs []run
		for rows.Next() {
			var r run
			if rows.Scan(&r.rid, &r.inst, &r.lab, &r.kind, &r.start) == nil && m.active(r.rid) {
				runs = append(runs, r)
			}
		}
		rows.Close()
		for _, r := range runs {
			name := ifaceLabel(r.kind, r.start, e.nic)
			tap, err := tapName(r.rid, e.nic)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if !linkExists(tap) {
				// The interface is missing in QEMU (e.g. it was hot-unplugged): create a TAP
				// and ask QEMU to add the netdev + NIC via QMP.
				spec := &qemu.Spec{ID: r.rid, DeviceID: e.dev, InstanceID: r.inst, LabID: r.lab}
				tap, err = m.tapFor(spec, e.nic)
				if err != nil {
					errs = append(errs, fmt.Errorf("машина %d, интерфейс %s: %w", r.rid, name, err))
					continue
				}
				if hotplug == nil {
					delLink(tap)
					errs = append(errs, fmt.Errorf("интерфейс %s машины %d не создан: перезапустите машину", name, r.rid))
					continue
				}
				if err := hotplug(r.rid, e.nic, tap); err != nil {
					delLink(tap)
					errs = append(errs, fmt.Errorf("машина %d, интерфейс %s: %w", r.rid, name, err))
					continue
				}
				log.Printf("network: провод %d: машина %d, интерфейс %s: TAP %s добавлен через QMP", connID, r.rid, name, tap)
			} else {
				br, err := m.cableBridge(r.inst, connID, e.peerType, e.peerID)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if m.linkUp(r.inst, connID) {
					if _, err := ip("link", "set", tap, "master", br); err != nil {
						errs = append(errs, err)
						continue
					}
					log.Printf("network: провод %d: машина %d, интерфейс %s: TAP %s подключён к мосту %s", connID, r.rid, name, tap, br)
				}
				if _, err := ip("link", "set", tap, "up"); err != nil {
					errs = append(errs, err)
					continue
				}
			}
			if e.peerType == "network" {
				// The new wire may have created a network bridge: rebuild nftables (including the bridge traffic
				// allowance under br_netfilter) and DHCP without waiting for background reconciliation.
				natTouched = true
			}
			if setLink != nil && m.linkUp(r.inst, connID) {
				if err := setLink(r.rid, e.nic, true); err != nil {
					errs = append(errs, fmt.Errorf("машина %d, интерфейс %s: %w", r.rid, name, err))
				}
			}
		}
	}
	if natTouched {
		errs = append(errs, m.syncNAT()...)
	}
	return errs
}

// ---------- wires: live removal ----------

// Plug is an interface of a running machine whose wire is being removed.
type Plug struct {
	RID  int64
	NIC  int
	Name string // interface name in machine format (ens0, te1, ...) - for messages only
}

// ifaceLabel is the interface name for messages: ens0/te1/ge2; for an unknown type - e<N>.
func ifaceLabel(kind string, start, nic int) string {
	switch kind {
	case "ens", "te", "ge":
		// A number < 0 is possible only with a -1 shift: nic0 is the MGMT control port.
		if start+nic < 0 {
			return "MGMT"
		}
		return fmt.Sprintf("%s%d", kind, start+nic)
	}
	return fmt.Sprintf("e%d", nic)
}

// PlanDetach returns the interfaces of running machines in all instances connected to the wires being removed.
// Called BEFORE deleting connections rows: after deletion the "wire - machine" link can no longer be restored.
func (m *Manager) PlanDetach(connIDs []int64) []Plug {
	var out []Plug
	for _, cid := range connIDs {
		var aT, bT string
		var aID, bID int64
		var aIf, bIf int
		c, ok := m.connRow(cid)
		if !ok {
			continue
		}
		aT, aID, aIf, bT, bID, bIf = c.A.Type, c.A.ID, c.A.If, c.B.Type, c.B.ID, c.B.If
		for _, e := range []struct {
			t   string
			id  int64
			nic int
		}{{aT, aID, aIf}, {bT, bID, bIf}} {
			if e.t != "device" {
				continue
			}
			rows, err := m.db.Query(`SELECT id,iface_type,iface_start FROM instance_devices WHERE device_id=?`, e.id)
			if err != nil {
				continue
			}
			for rows.Next() {
				var rid int64
				var kind string
				var start int
				if rows.Scan(&rid, &kind, &start) == nil && m.active(rid) {
					out = append(out, Plug{RID: rid, NIC: e.nic, Name: ifaceLabel(kind, start, e.nic)})
				}
			}
			rows.Close()
		}
	}
	return out
}

// Detach physically disconnects the interfaces: the TAP leaves the bridge (if it was in it), QEMU sets the NIC link down
// (QMP set_link). The machine keeps running; the guest OS sees the link loss. Called AFTER the wire is deleted.
// Errors are collected and returned as warnings: the database deletion has already been performed.
func (m *Manager) Detach(plugs []Plug, setLink func(rid int64, nic int, up bool) error, unplug func(rid int64, nic int) error) []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for _, p := range plugs {
		name := p.Name
		if name == "" {
			name = ifaceLabel("", 0, p.NIC)
		}
		if unplug != nil {
			if err := unplug(p.RID, p.NIC); err == nil {
				if tap, err := tapName(p.RID, p.NIC); err == nil {
					delLink(tap)
					log.Printf("network: машина %d, интерфейс %s: TAP %s удалён (провод удалён)", p.RID, name, tap)
				}
				continue
			} else {
				errs = append(errs, fmt.Errorf("машина %d, интерфейс %s: горячее отключение не удалось: %w", p.RID, name, err))
			}
		}
		// Rollback: if hot-unplug is unavailable, the interface remains but the link is disabled,
		// and the TAP is removed from the bridge so that traffic of the removed wire is not forwarded.
		tap, err := tapName(p.RID, p.NIC)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if linkExists(tap) {
			if _, err := ip("link", "set", tap, "nomaster"); err != nil {
				errs = append(errs, err)
			} else {
				log.Printf("network: машина %d, интерфейс %s: TAP %s выведен из моста (горячее отключение недоступно)", p.RID, name, tap)
			}
		}
		if setLink != nil {
			if err := setLink(p.RID, p.NIC, false); err != nil {
				errs = append(errs, fmt.Errorf("машина %d, интерфейс %s: %w", p.RID, name, err))
			}
		}
	}
	return errs
}
