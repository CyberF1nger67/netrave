// SPDX-License-Identifier: GPL-3.0-only

// Package instances - lab instances: each user has their own set of working machines
// on top of the shared topology (devices, networks and wires of a lab are the same for everyone).
//
// The unit of execution is an instance_devices row (id = "rid"). All QEMU processes, TAPs, MAC addresses,
// console ports, disk overlays and snapshots are bound to the rid, so instances do not overlap.
package instances

import (
	"database/sql"
	"encoding/json"

	"netrave/internal/database"
)

// Ensure returns the id of the user's lab instance, creating it (and its machine rows)
// on first access. For existing instances it fills in rows of new machines.
func Ensure(db *database.DB, labID, userID int64) (int64, error) {
	if _, err := db.Exec(`INSERT OR IGNORE INTO lab_instances(lab_id, user_id) VALUES(?, ?)`, labID, userID); err != nil {
		return 0, err
	}
	var id int64
	if err := db.QueryRow(`SELECT id FROM lab_instances WHERE lab_id=? AND user_id=?`, labID, userID).Scan(&id); err != nil {
		return 0, err
	}
	if err := SyncLab(db, labID); err != nil {
		return 0, err
	}
	return id, nil
}

// SyncLab creates the missing machine rows for all lab instances.
// Settings are copied from the topology (the lab file); after that they change only in the instance.
func SyncLab(db *database.DB, labID int64) error {
	fl, ok := db.Labs.Lab(labID)
	if !ok {
		return nil
	}
	insts, err := instanceIDs(db, labID)
	if err != nil {
		return err
	}
	for _, instID := range insts {
		for _, d := range fl.Devices {
			var img any
			if d.ImageID != 0 {
				img = d.ImageID
			}
			if _, err := db.Exec(`INSERT INTO instance_devices(instance_id, device_id, image_id, machine, cpu, ram_mb, nics, nic_model, iface_type, iface_start, disk_bus, bios, console, extra_args)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(instance_id, device_id) DO NOTHING`,
				instID, d.ID, img, d.Machine, d.CPU, d.RAMMB, d.NICs, d.NICModel, d.IfaceType, d.IfaceStart, d.DiskBus, d.BIOS, d.Console, d.ExtraArgs); err != nil {
				return err
			}
		}
	}
	// An instance that has not chosen a disk yet gets the default disk from the topology (it may have appeared after
	// the instance row). A disk chosen by the user (image_own=1) is not changed.
	for _, d := range fl.Devices {
		if d.ImageID == 0 {
			continue
		}
		if _, err := db.Exec(`UPDATE instance_devices SET image_id=?
			WHERE device_id=? AND image_own=0 AND image_id IS NULL
			AND instance_id IN (SELECT i.id FROM lab_instances i WHERE i.lab_id=?)`, d.ImageID, d.ID, labID); err != nil {
			return err
		}
	}
	return nil
}

// instanceIDs returns the lab instances.
func instanceIDs(db *database.DB, labID int64) ([]int64, error) {
	rows, err := db.Query(`SELECT id FROM lab_instances WHERE lab_id=?`, labID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DeviceInfo - an instance machine and its owner.
type DeviceInfo struct {
	RID        int64
	InstanceID int64
	DeviceID   int64
	LabID      int64
	OwnerID    int64
	Name       string
}

// Device finds an instance machine by rid. sql.ErrNoRows - there is no machine with this rid.
func Device(db *database.DB, rid int64) (DeviceInfo, error) {
	var d DeviceInfo
	err := db.QueryRow(`SELECT r.id, r.instance_id, r.device_id, i.lab_id, i.user_id
		FROM instance_devices r
		JOIN lab_instances i ON i.id = r.instance_id
		WHERE r.id = ?`, rid).Scan(&d.RID, &d.InstanceID, &d.DeviceID, &d.LabID, &d.OwnerID)
	if err != nil {
		return d, err
	}
	// The machine name comes from the lab topology (file); if the machine is no longer in the file, the name is empty.
	if fd, _, ok := db.Labs.Device(d.DeviceID); ok {
		d.Name = fd.Name
	}
	return d, nil
}

// Owner returns the lab and the instance owner. sql.ErrNoRows - there is no instance.
func Owner(db *database.DB, instanceID int64) (labID, ownerID int64, err error) {
	err = db.QueryRow(`SELECT lab_id, user_id FROM lab_instances WHERE id = ?`, instanceID).Scan(&labID, &ownerID)
	if err == sql.ErrNoRows {
		return 0, 0, err
	}
	return labID, ownerID, err
}

// CanUse - the instance can be used by the owner or an administrator (an administrator sees and manages all).
func CanUse(ownerID, userID int64, admin bool) bool {
	return admin || ownerID == userID
}

// Launch - the machine parameters the QEMU process was started with. Stored in instance_devices.applied
// (while the machine is running). Comparing them with the current settings gives the list of changes awaiting a restart.
type Launch struct {
	Machine   string `json:"machine"`
	CPU       int    `json:"cpu"`
	RAMMB     int    `json:"ram_mb"`
	NICs      int    `json:"nics"`
	NICModel  string `json:"nic_model"`
	DiskBus   string `json:"disk_bus"`
	BIOS      string `json:"bios"`
	Console   string `json:"console"`
	ExtraArgs string `json:"extra_args"`
}

// JSON returns the snapshot as a string for storage.
func (l Launch) JSON() string {
	b, _ := json.Marshal(l)
	return string(b)
}
