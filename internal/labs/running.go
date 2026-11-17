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
func devRunning(db *database.DB, m *qemu.Manager, id int64) bool {
	// A wire cannot be changed while the topology machine is running in at least one instance.
	if m == nil {
		return false
	}
	rows, err := db.Query(`SELECT id FROM instance_devices WHERE device_id=?`, id)
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
func anyDeviceRunning(db *database.DB, m *qemu.Manager, ids []int64) bool {
	if m == nil || len(ids) == 0 {
		return false
	}
	args := idArgs(0, ids)[1:]
	rows, err := db.Query(`SELECT id FROM instance_devices WHERE device_id IN (`+placeholders(len(ids))+`)`, args...)
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
	aID, bID     int64
}

// connsOfObject returns wires that have an end of type kind with number id (in the object's lab).
func connsOfObject(db *database.DB, kind string, id int64) []endpoint {
	labID, ok := labOwnerOf(db, kind, id)
	if !ok {
		return nil
	}
	fl, ok := labData(db, labID)
	if !ok {
		return nil
	}
	var out []endpoint
	for _, c := range fl.Connections {
		if (c.A.Type == kind && c.A.ID == id) || (c.B.Type == kind && c.B.ID == id) {
			out = append(out, endpoint{aType: c.A.Type, aID: c.A.ID, bType: c.B.Type, bID: c.B.ID})
		}
	}
	return out
}

// netRunning reports that a running device is connected to the network.
func netRunning(db *database.DB, m *qemu.Manager, netID int64) bool {
	for _, e := range connsOfObject(db, labfile.KindNetwork, netID) {
		if (e.aType == "device" && devRunning(db, m, e.aID)) || (e.bType == "device" && devRunning(db, m, e.bID)) {
			return true
		}
	}
	return false
}

// devHasRunningPeer reports that the device is wired to another running device.
func devHasRunningPeer(db *database.DB, m *qemu.Manager, devID int64) bool {
	for _, e := range connsOfObject(db, labfile.KindDevice, devID) {
		if e.aType == "device" && e.aID != devID && devRunning(db, m, e.aID) {
			return true
		}
		if e.bType == "device" && e.bID != devID && devRunning(db, m, e.bID) {
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
