// SPDX-License-Identifier: GPL-3.0-only

package network

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"netrave/internal/natcloud"
	"netrave/internal/qemu"
)

// Diag is a snapshot of a lab's network path: QEMU adapters, TAPs, bridges, nftables rules and
// br_netfilter state. It is used to find the stage where packets are lost.
type Diag struct {
	LabID        int64             `json:"lab_id"`
	BRNetfilter  bool              `json:"br_netfilter"`
	BridgeNFCall map[string]string `json:"bridge_nf_call"`
	IPForward    string            `json:"ip_forward"`
	Interfaces   []DiagIface       `json:"interfaces"`
	Bridges      []DiagBridge      `json:"bridges"`
	NftNetrave   string            `json:"nft_ip_netrave"`
	NftIsolation string            `json:"nft_bridge_netrave_iso"`
	ForwardDrops []string          `json:"host_forward_drops"`
	Warnings     []string          `json:"warnings"`
}

// DiagIface is a single machine interface: the expected bridge and the actual TAP state.
type DiagIface struct {
	RID        int64  `json:"rid"`
	Instance   int64  `json:"instance_id"`
	Device     string `json:"device"`
	State      string `json:"state"`
	NIC        int    `json:"nic"`
	Label      string `json:"label"`
	Model      string `json:"model"`
	MAC        string `json:"mac"`
	Tap        string `json:"tap"`
	TapExists  bool   `json:"tap_exists"`
	TapMaster  string `json:"tap_master"`
	TapState   string `json:"tap_state"`
	Expected   string `json:"bridge_expected"`
	CableUp    bool   `json:"cable_up"`
	Connected  string `json:"connection"`
	BridgeOK   bool   `json:"bridge_ok"`
	BridgeNote string `json:"note,omitempty"`
}

// DiagBridge is a bridge and its ports as the kernel sees them.
type DiagBridge struct {
	Name   string     `json:"name"`
	Exists bool       `json:"exists"`
	State  string     `json:"state"`
	Ports  []DiagPort `json:"ports"`
}

// DiagPort is a bridge port (a machine TAP or a veth/physical interface).
type DiagPort struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Master string `json:"master"`
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func linkState(name string) string {
	return readTrim("/sys/class/net/" + name + "/operstate")
}

func bridgePorts(name string) []DiagPort {
	ents, err := os.ReadDir(filepath.Join("/sys/class/net", name, "brif"))
	if err != nil {
		return nil
	}
	out := make([]DiagPort, 0, len(ents))
	for _, e := range ents {
		port := e.Name()
		out = append(out, DiagPort{Name: port, State: linkState(port), Master: masterOf(port)})
	}
	return out
}

// bridgeForNic returns the interface's expected bridge without creating database rows.
func (m *Manager) bridgeForNic(labID, instID, devID int64, nic int) (br, conn string, up bool) {
	up = true
	c, ok := connAtPort(m.connsOf(labID), devID, nic)
	if !ok {
		return "(нет провода)", "(нет провода)", true
	}
	connID, aType, aID, aIf, bType, bID := c.ID, c.A.Type, c.A.ID, c.A.If, c.B.Type, c.B.ID
	peerType, peerID := aType, aID
	if aType == "device" && aID == devID && aIf == nic {
		peerType, peerID = bType, bID
	}
	var upv int
	if err := m.db.QueryRow(`SELECT up FROM instance_links WHERE instance_id=? AND connection_id=?`, instID, connID).Scan(&upv); err == nil {
		up = upv != 0
	}
	if peerType == "network" {
		conn = fmt.Sprintf("сеть %d", peerID)
		pn, _, ok := m.db.Labs.Network(peerID)
		if !ok {
			return "(сеть не найдена)", conn, up
		}
		if pn.Type == "nat" {
			return natcloud.Bridge, conn, up
		}
		var nid int64
		if m.db.QueryRow(`SELECT id FROM instance_networks WHERE instance_id=? AND network_id=?`, instID, peerID).Scan(&nid) != nil {
			return "(мост ещё не создан)", conn, up
		}
		return netBridge(nid), conn, up
	}
	conn = fmt.Sprintf("устройство %d", peerID)
	var lid int64
	if m.db.QueryRow(`SELECT id FROM instance_links WHERE instance_id=? AND connection_id=?`, instID, connID).Scan(&lid) != nil {
		return "(мост ещё не создан)", conn, up
	}
	return linkBridge(lid), conn, up
}

// Diagnostics collects a lab's network state. Read-only: no database rows are created.
func (m *Manager) Diagnostics(labID int64) *Diag {
	d := &Diag{
		LabID:        labID,
		BRNetfilter:  brNetfilterActive(),
		BridgeNFCall: map[string]string{},
		IPForward:    readTrim("/proc/sys/net/ipv4/ip_forward"),
	}
	for _, k := range []string{"bridge-nf-call-iptables", "bridge-nf-call-ip6tables", "bridge-nf-call-arptables"} {
		if v := readTrim("/proc/sys/net/bridge/" + k); v != "" {
			d.BridgeNFCall[k] = v
		}
	}

	rows, err := m.db.Query(`SELECT r.id,i.id,r.device_id,r.nics,r.nic_model,r.iface_type,r.iface_start
		FROM instance_devices r
		JOIN lab_instances i ON i.id=r.instance_id
		WHERE i.lab_id=? ORDER BY i.id,r.id`, labID)
	if err != nil {
		d.Warnings = append(d.Warnings, "ошибка базы данных: "+err.Error())
		return d
	}
	defer rows.Close()

	want := map[string]bool{}
	for rows.Next() {
		var rid, inst, devID, nics int64
		var model, kind string
		var start int
		if rows.Scan(&rid, &inst, &devID, &nics, &model, &kind, &start) != nil {
			continue
		}
		state := m.state(rid)
		dev := ""
		if fd, _, ok := m.db.Labs.Device(devID); ok {
			dev = fd.Name
		}
		limit := int(nics)
		if limit > 32 {
			limit = 32
		}
		for nic := 0; nic < limit; nic++ {
			br, conn, up := m.bridgeForNic(labID, inst, devID, nic)
			note := ""
			if strings.HasPrefix(br, "(") {
				note = br
			}
			item := DiagIface{
				RID: rid, Instance: inst, Device: dev, State: state, NIC: nic,
				Label: ifaceLabel(kind, start, nic), Model: model, MAC: qemu.MAC(rid, nic),
				Expected: br, CableUp: up, Connected: conn, BridgeNote: note,
			}
			tap, terr := tapName(rid, nic)
			if terr == nil {
				item.Tap = tap
				if ifc, err := net.InterfaceByName(tap); err == nil {
					item.TapExists = true
					item.MAC = ifc.HardwareAddr.String()
					item.TapMaster = masterOf(tap)
					item.TapState = linkState(tap)
				}
			}
			if br != "" && !strings.HasPrefix(br, "(") {
				want[br] = true
			}
			if state == "running" || state == "paused" {
				switch {
				case !item.TapExists:
					d.Warnings = append(d.Warnings, fmt.Sprintf("машина %d (%s), %s: TAP отсутствует", rid, dev, item.Label))
				case up && strings.HasPrefix(br, "nr") && item.TapMaster != br:
					d.Warnings = append(d.Warnings, fmt.Sprintf("машина %d (%s), %s: TAP %s не в мосту %s (сейчас: %q)",
						rid, dev, item.Label, tap, br, item.TapMaster))
				case !up && item.TapMaster != "":
					d.Warnings = append(d.Warnings, fmt.Sprintf("машина %d (%s), %s: провод отключён, но TAP %s в мосту %s",
						rid, dev, item.Label, tap, item.TapMaster))
				}
			}
			item.BridgeOK = strings.HasPrefix(br, "nr") && item.TapMaster == br
			d.Interfaces = append(d.Interfaces, item)
		}
	}
	if err := rows.Err(); err != nil {
		d.Warnings = append(d.Warnings, "ошибка чтения базы данных: "+err.Error())
	}

	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	// Stable order: nrnat first, then by name.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		b := DiagBridge{Name: name, Exists: linkExists(name), State: linkState(name)}
		if b.Exists {
			b.Ports = bridgePorts(name)
		} else if strings.HasPrefix(name, "nr") {
			d.Warnings = append(d.Warnings, "мост "+name+" не существует")
		}
		d.Bridges = append(d.Bridges, b)
	}

	d.NftNetrave = nftTable("ip", "netrave")
	d.NftIsolation = nftTable("bridge", "netrave_iso")
	d.ForwardDrops = hostForwardDrops()
	if d.BRNetfilter {
		if d.BridgeNFCall["bridge-nf-call-iptables"] == "1" {
			d.Warnings = append(d.Warnings, "br_netfilter активен: мостовой трафик проходит ip forward; netrave разрешает его правилом ibrname/obrname")
		}
		if len(d.ForwardDrops) > 0 {
			d.Warnings = append(d.Warnings, "в других таблицах nftables есть drop/reject в forward: при br_netfilter они могут отбрасывать кадры между машинами")
		}
	}
	return d
}

func nftTable(family, name string) string {
	out, err := runCmd("", "nft", "list", "table", family, name)
	if err != nil {
		if strings.TrimSpace(out) == "" {
			return err.Error()
		}
		return strings.TrimSpace(out)
	}
	return strings.TrimSpace(out)
}

// hostForwardDrops prints forward chain headers and drop/reject rules from foreign tables.
// Netrave's own tables are skipped: their drop rules are needed for routing isolation.
func hostForwardDrops() []string {
	out, err := runCmd("", "nft", "list", "ruleset")
	if err != nil {
		return nil
	}
	var res []string
	table, chainForward := "", false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "table ") {
			f := strings.Fields(line)
			if len(f) >= 3 {
				table = f[2]
			}
			chainForward = false
			continue
		}
		if table == "netrave" || table == "netrave_iso" {
			continue
		}
		if strings.Contains(line, "hook forward") {
			chainForward = true
			if strings.Contains(line, "policy drop") {
				res = append(res, table+": "+line)
			}
			continue
		}
		if chainForward && (strings.HasSuffix(line, " drop") || strings.Contains(line, " reject")) {
			res = append(res, table+": "+line)
		}
		if line == "}" {
			chainForward = false
		}
		if len(res) >= 20 {
			break
		}
	}
	return res
}
