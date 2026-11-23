// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/qemu"
)

// devRunning reports that the device is running or transitioning to the running state.
// Process state is stored only in the QEMU manager's memory; the database keeps only
// the historical first-start flag (started_at).
func devRunning(db *database.DB, m *qemu.Manager, labName, devName string) bool {
	// A wire cannot be changed while the topology machine is running in at least one instance.
	if m == nil {
		return false
	}
	rows, err := db.Query(`SELECT id FROM instance_devices WHERE device_id=?`, devName)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var rid int64
		if rows.Scan(&rid) == nil && m.Active(rid) {
			return true
		}
	}
	return false
}

// anyDeviceRunning reports whether at least one machine from the topology device list is running.
func anyDeviceRunning(db *database.DB, m *qemu.Manager, labName string, names []string) bool {
	if m == nil || len(names) == 0 {
		return false
	}
	rows, err := db.Query(`SELECT id FROM instance_devices WHERE device_id IN (`+placeholders(len(names))+`)`, nameArgs(names)...)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var rid int64
		if rows.Scan(&rid) == nil && m.Active(rid) {
			return true
		}
	}
	return false
}

type endpoint struct {
	aType, bType string
	aName, bName string
}

// connsOfObject returns cables that have an end of type kind with the name (in the lab).
func connsOfObject(db *database.DB, kind, labName, name string) []endpoint {
	fl, ok := labData(db, labName)
	if !ok {
		return nil
	}
	var out []endpoint
	for _, c := range fl.Connections {
		if (c.A.Type == kind && c.A.Name == name) || (c.B.Type == kind && c.B.Name == name) {
			out = append(out, endpoint{aType: c.A.Type, aName: c.A.Name, bType: c.B.Type, bName: c.B.Name})
		}
	}
	return out
}

// netRunning reports that a running device is connected to the network.
func netRunning(db *database.DB, m *qemu.Manager, labName, netName string) bool {
	for _, e := range connsOfObject(db, labfile.KindNetwork, labName, netName) {
		if (e.aType == "device" && devRunning(db, m, labName, e.aName)) || (e.bType == "device" && devRunning(db, m, labName, e.bName)) {
			return true
		}
	}
	return false
}

// devHasRunningPeer reports that the device is wired to another running device.
func devHasRunningPeer(db *database.DB, m *qemu.Manager, labName, devName string) bool {
	for _, e := range connsOfObject(db, labfile.KindDevice, labName, devName) {
		if e.aType == "device" && e.aName != devName && devRunning(db, m, labName, e.aName) {
			return true
		}
		if e.bType == "device" && e.bName != devName && devRunning(db, m, labName, e.bName) {
			return true
		}
	}
	return false
}

// changed tells the server that the topology has changed and network objects need reconciliation.
func (h *Handler) changed() {
	if h.NetChanged != nil {
		h.NetChanged()
	}
}
