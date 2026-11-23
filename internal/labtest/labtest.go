// SPDX-License-Identifier: GPL-3.0-only

// Package labtest provides test helpers: they create labs, machines, networks, cables and lab permissions
// directly in topology files (through the labfile store), the way the application does.
package labtest

import (
	"testing"

	"netrave/internal/database"
	"netrave/internal/hostarch"
	"netrave/internal/labfile"
	"netrave/internal/msgs"
)

// Lab creates a lab owned by owner with public level public (empty - "none") and returns its name.
func Lab(t testing.TB, db *database.DB, owner int64, name, public string) string {
	t.Helper()
	if public == "" {
		public = "none"
	}
	saved, err := db.Labs.Save(labfile.Lab{OwnerID: owner, Name: name, PublicLevel: public, CanvasJSON: "{}"})
	if err != nil {
		t.Fatalf("lab creation: %v", err)
	}
	return saved.Name
}

// Device adds a machine to the lab and returns its name. Empty settings are filled with default
// values, as when creating a machine in the application (otherwise the CHECK constraints in the
// instance table fail).
func Device(t testing.TB, db *database.DB, lab string, d labfile.Device) string {
	t.Helper()
	defaults := []struct {
		empty *string
		value string
	}{
		{&d.Arch, hostarch.QEMUArch()}, {&d.Machine, hostarch.DefaultMachine()}, {&d.NICModel, "virtio-net-pci"}, {&d.DiskBus, "virtio"},
		{&d.BIOS, hostarch.DefaultBIOS()}, {&d.Console, "serial"}, {&d.IfaceType, "ens"},
	}
	for _, f := range defaults {
		if *f.empty == "" {
			*f.empty = f.value
		}
	}
	if d.CPU == 0 {
		d.CPU = 1
	}
	if d.RAMMB == 0 {
		d.RAMMB = 1024
	}
	if d.NICs == 0 {
		d.NICs = 1
	}
	got, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		l.Devices = append(l.Devices, d)
		return nil
	})
	if err != nil {
		t.Fatalf("machine add: %v", err)
	}
	return got.Devices[len(got.Devices)-1].Name
}

// Network adds a network to the lab and returns its name.
func Network(t testing.TB, db *database.DB, lab string, n labfile.Network) string {
	t.Helper()
	got, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		l.Networks = append(l.Networks, n)
		return nil
	})
	if err != nil {
		t.Fatalf("network add: %v", err)
	}
	return got.Networks[len(got.Networks)-1].Name
}

// Connection adds a cable to the lab and returns its key. The ends are machines and networks of this lab.
func Connection(t testing.TB, db *database.DB, lab string, c labfile.Connection) string {
	t.Helper()
	got, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		l.Connections = append(l.Connections, c)
		return nil
	})
	if err != nil {
		t.Fatalf("cable add: %v", err)
	}
	return got.Connections[len(got.Connections)-1].Key()
}

// Perm sets a user's permission on the lab (value=false - explicit deny).
func Perm(t testing.TB, db *database.DB, lab string, uid int64, perm string, value bool) {
	t.Helper()
	if _, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		for i := range l.Permissions {
			if l.Permissions[i].UserID == uid && l.Permissions[i].Perm == perm {
				l.Permissions[i].Value = value
				return nil
			}
		}
		l.Permissions = append(l.Permissions, labfile.Permission{UserID: uid, Perm: perm, Value: value})
		return nil
	}); err != nil {
		t.Fatalf("lab permission: %v", err)
	}
}

// Public changes the lab's public level.
func Public(t testing.TB, db *database.DB, lab string, level string) {
	t.Helper()
	if _, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		l.PublicLevel = level
		return nil
	}); err != nil {
		t.Fatalf("public level: %v", err)
	}
}

// Set modifies a lab machine with fn (for example, nics or icon).
func Set(t testing.TB, db *database.DB, lab string, devName string, fn func(*labfile.Device)) {
	t.Helper()
	if _, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		for i := range l.Devices {
			if l.Devices[i].Name == devName {
				fn(&l.Devices[i])
				return nil
			}
		}
		return msgs.New("Machine {name} not found", map[string]string{"name": devName})
	}); err != nil {
		t.Fatalf("machine change: %v", err)
	}
}
